package file_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	commonv1 "excel/api/common/v1"
	filev1 "excel/api/file/v1"
	"excel_worker_service/base"
	filelogic "excel_worker_service/internal/file"
	workerredis "excel_worker_service/redis"
)

const maxUploadBytes = 20 << 20

type fileHarness struct {
	db      *sql.DB
	service *filelogic.Service
	quota   *workerredis.AttachmentQuota
	client  filev1.FileServiceClient
	userID  string
	baseID  string
	ctx     context.Context
}

func newFileHarness(t *testing.T) *fileHarness {
	t.Helper()
	config, err := base.LoadConfig(filepath.Join("..", "..", "configs", "worker_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ensureFileTables(db); err != nil {
		t.Fatalf("ensure file tables: %v", err)
	}

	redisClient, err := workerredis.Open(context.Background(), config.RedisAddr)
	if err != nil {
		t.Fatalf("redis.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	quota := workerredis.NewAttachmentQuota(redisClient, config.AttachPoolLimitBytes)
	attachDir := t.TempDir()
	service := filelogic.NewService(base.NewFileRepository(db), quota, attachDir, time.Now)
	listener := bufconn.Listen(4 << 20)
	server := grpc.NewServer()
	filelogic.Register(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient(
		"passthrough:///file-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	harness := &fileHarness{
		db:      db,
		service: service,
		quota:   quota,
		client:  filev1.NewFileServiceClient(connection),
		userID:  fmt.Sprintf("01J%023d", time.Now().UnixNano()%1000000000000000000),
		baseID:  fmt.Sprintf("01K%023d", time.Now().UnixNano()%1000000000000000000),
	}
	harness.ctx = uploadPrincipal(t, harness.userID, "enterprise_1", harness.baseID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM excel_file_meta WHERE uploader = ?`,
			harness.userID,
		)
		_ = redisClient.Del(context.Background(), workerredis.AttachmentQuotaKey()).Err()
	})
	return harness
}

func ensureFileTables(db *sql.DB) error {
	var exists int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_name = 'excel_file_meta'
	`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return base.MigrateUp(db)
	}
	return nil
}

func TestInitUploadRejectsBlacklistedOversizedAndPoolFullFiles(t *testing.T) {
	harness := newFileHarness(t)

	t.Run("blacklisted extension and MIME", func(t *testing.T) {
		stream, err := harness.client.InitUpload(harness.ctx)
		if err != nil {
			t.Fatalf("InitUpload() error = %v", err)
		}
		if err := stream.Send(&filev1.InitUploadRequest{
			Name:  "shell.php",
			Ext:   ".php",
			Mime:  "text/html",
			Size:  8,
			Usage: filev1.FileUsage_FILE_USAGE_ATTACHMENT,
			Chunk: []byte("<script>"),
		}); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		_, err = stream.CloseAndRecv()
		assertFileRejected(t, err)
	})

	t.Run("webshell double extension", func(t *testing.T) {
		stream, err := harness.client.InitUpload(harness.ctx)
		if err != nil {
			t.Fatalf("InitUpload() error = %v", err)
		}
		if err := stream.Send(&filev1.InitUploadRequest{
			Name:  "shell.php.jpg",
			Ext:   ".jpg",
			Mime:  "image/jpeg",
			Size:  8,
			Usage: filev1.FileUsage_FILE_USAGE_ATTACHMENT,
			Chunk: []byte("not-jpeg"),
		}); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		_, err = stream.CloseAndRecv()
		assertFileRejected(t, err)
	})

	t.Run("single file over 20MB", func(t *testing.T) {
		stream, err := harness.client.InitUpload(harness.ctx)
		if err != nil {
			t.Fatalf("InitUpload() error = %v", err)
		}
		if err := stream.Send(&filev1.InitUploadRequest{
			Name:  "large.txt",
			Ext:   ".txt",
			Mime:  "text/plain",
			Size:  maxUploadBytes + 1,
			Usage: filev1.FileUsage_FILE_USAGE_ATTACHMENT,
		}); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		_, err = stream.CloseAndRecv()
		assertFileRejected(t, err)
	})

	t.Run("attachment pool full", func(t *testing.T) {
		if err := harness.quota.Sync(
			context.Background(),
			harness.quota.Limit(),
		); err != nil {
			t.Fatalf("Sync() error = %v", err)
		}
		stream, err := harness.client.InitUpload(harness.ctx)
		if err != nil {
			t.Fatalf("InitUpload() error = %v", err)
		}
		if err := stream.Send(&filev1.InitUploadRequest{
			Name:  "note.txt",
			Ext:   ".txt",
			Mime:  "text/plain",
			Size:  4,
			Usage: filev1.FileUsage_FILE_USAGE_ATTACHMENT,
			Chunk: []byte("note"),
		}); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		_, err = stream.CloseAndRecv()
		assertFileRejected(t, err)
	})
}

func TestConfirmUploadRejectsChecksumMismatchAndReleasesQuota(t *testing.T) {
	harness := newFileHarness(t)
	stream, err := harness.client.InitUpload(harness.ctx)
	if err != nil {
		t.Fatalf("InitUpload() error = %v", err)
	}
	content := []byte("hello attachment")
	if err := stream.Send(&filev1.InitUploadRequest{
		Name:  "hello.txt",
		Ext:   ".txt",
		Mime:  "text/plain",
		Size:  int64(len(content)),
		Usage: filev1.FileUsage_FILE_USAGE_ATTACHMENT,
		Chunk: content,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	initialized, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv() error = %v", err)
	}

	_, err = harness.client.ConfirmUpload(harness.ctx, &filev1.ConfirmUploadRequest{
		FileId:   initialized.GetFileMeta().GetId(),
		Checksum: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	assertFileRejected(t, err)
	if _, err := base.NewFileRepository(harness.db).Get(
		context.Background(),
		initialized.GetFileMeta().GetId(),
	); !errors.Is(err, base.ErrFileNotFound) {
		t.Fatalf("Get() after mismatch error = %v, want ErrFileNotFound", err)
	}
	used, err := harness.quota.Lookup(context.Background())
	if err != nil {
		t.Fatalf("quota Lookup() error = %v", err)
	}
	if used != 0 {
		t.Fatalf("quota used = %d, want 0 after mismatch", used)
	}
}

func TestImportTempUploadLandsInAttachDirectory(t *testing.T) {
	harness := newFileHarness(t)
	stream, err := harness.client.InitUpload(harness.ctx)
	if err != nil {
		t.Fatalf("InitUpload() error = %v", err)
	}
	content := []byte("name,age\nAlice,30\n")
	checksum := sha256Hex(content)
	if err := stream.Send(&filev1.InitUploadRequest{
		Name:  "people.csv",
		Ext:   ".csv",
		Mime:  "text/csv",
		Size:  int64(len(content)),
		Usage: filev1.FileUsage_FILE_USAGE_IMPORT_TEMP,
		Chunk: content,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	initialized, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv() error = %v", err)
	}
	confirmed, err := harness.client.ConfirmUpload(harness.ctx, &filev1.ConfirmUploadRequest{
		FileId:   initialized.GetFileMeta().GetId(),
		Checksum: checksum,
	})
	if err != nil {
		t.Fatalf("ConfirmUpload() error = %v", err)
	}
	if confirmed.GetFileMeta().GetName() != "people.csv" ||
		confirmed.GetFileMeta().GetSize() != int64(len(content)) {
		t.Fatalf("confirmed file = %+v", confirmed.GetFileMeta())
	}
	meta, err := base.NewFileRepository(harness.db).Get(
		context.Background(),
		confirmed.GetFileMeta().GetId(),
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if meta.Usage != "import_temp" {
		t.Fatalf("usage = %q, want import_temp", meta.Usage)
	}
	path := filepath.Join(harness.service.AttachDir(), meta.StoragePath)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stored temp file %s: %v", path, err)
	}
	quota, err := harness.client.GetQuota(harness.ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetQuota() error = %v", err)
	}
	if quota.GetUsed() != int64(len(content)) ||
		quota.GetLimit() <= 0 ||
		quota.GetAvailable() != quota.GetLimit()-quota.GetUsed() {
		t.Fatalf("quota = %+v", quota)
	}
}

func TestDownloadAndThumbnailStreamsUseStoredFileMetadata(t *testing.T) {
	harness := newFileHarness(t)
	source := image.NewRGBA(image.Rect(0, 0, 256, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 256; x++ {
			source.Set(x, y, color.RGBA{R: 20, G: 120, B: 220, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	content := encoded.Bytes()
	fileID := uploadConfirmedFile(
		t,
		harness,
		"picture.png",
		".png",
		"image/png",
		filev1.FileUsage_FILE_USAGE_ATTACHMENT,
		content,
	)

	var downloadHeader metadata.MD
	download, err := harness.client.DownloadStream(
		harness.ctx,
		&filev1.DownloadStreamRequest{FileId: fileID},
		grpc.Header(&downloadHeader),
	)
	if err != nil {
		t.Fatalf("DownloadStream() error = %v", err)
	}
	var downloaded []byte
	for {
		chunk, recvErr := download.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatalf("DownloadStream.Recv() error = %v", recvErr)
		}
		downloaded = append(downloaded, chunk.GetChunk()...)
	}
	if !bytes.Equal(downloaded, content) ||
		downloadHeader.Get("x-file-name")[0] != "picture.png" ||
		downloadHeader.Get("x-file-mime")[0] != "image/png" {
		t.Fatalf("download header=%v bytes_match=%v", downloadHeader, bytes.Equal(downloaded, content))
	}

	var thumbHeader metadata.MD
	thumb, err := harness.client.GetThumb(
		harness.ctx,
		&filev1.GetThumbRequest{FileId: fileID},
		grpc.Header(&thumbHeader),
	)
	if err != nil {
		t.Fatalf("GetThumb() error = %v", err)
	}
	var thumbnail []byte
	for {
		chunk, recvErr := thumb.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatalf("GetThumb.Recv() error = %v", recvErr)
		}
		thumbnail = append(thumbnail, chunk.GetChunk()...)
	}
	decodedThumb, err := jpeg.Decode(bytes.NewReader(thumbnail))
	if err != nil {
		t.Fatalf("jpeg.Decode(thumbnail) error = %v", err)
	}
	if decodedThumb.Bounds().Dx() != 128 ||
		decodedThumb.Bounds().Dy() != 32 ||
		thumbHeader.Get("x-file-mime")[0] != "image/jpeg" {
		t.Fatalf(
			"thumbnail bounds=%v header=%v",
			decodedThumb.Bounds(),
			thumbHeader,
		)
	}
}

func uploadConfirmedFile(
	t *testing.T,
	harness *fileHarness,
	name string,
	ext string,
	mimeType string,
	usage filev1.FileUsage,
	content []byte,
) string {
	t.Helper()
	stream, err := harness.client.InitUpload(harness.ctx)
	if err != nil {
		t.Fatalf("InitUpload() error = %v", err)
	}
	if err := stream.Send(&filev1.InitUploadRequest{
		Name:  name,
		Ext:   ext,
		Mime:  mimeType,
		Size:  int64(len(content)),
		Usage: usage,
		Chunk: content,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	initialized, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv() error = %v", err)
	}
	confirmed, err := harness.client.ConfirmUpload(harness.ctx, &filev1.ConfirmUploadRequest{
		FileId:   initialized.GetFileMeta().GetId(),
		Checksum: sha256Hex(content),
	})
	if err != nil {
		t.Fatalf("ConfirmUpload() error = %v", err)
	}
	return confirmed.GetFileMeta().GetId()
}

func uploadPrincipal(t *testing.T, userID, enterpriseID, baseID string) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: enterpriseID,
		Internal:     true,
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal", base64.StdEncoding.EncodeToString(payload),
		"x-enterprise-id", enterpriseID,
		"x-base-id", baseID,
	)
}

func assertFileRejected(t *testing.T, err error) {
	t.Helper()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error code = %s (%v), want InvalidArgument", status.Code(err), err)
	}
	got := status.Convert(err)
	for _, detail := range got.Details() {
		if info, ok := detail.(interface{ GetReason() string }); ok && info.GetReason() == "5002" {
			return
		}
	}
	t.Fatalf("error details = %v, want business reason 5002", got.Details())
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
