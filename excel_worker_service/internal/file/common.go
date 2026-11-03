// file/common.go：附件服务、校验规则与跨接口复用逻辑。
package file

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	filev1 "excel/api/file/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
	workerredis "excel_worker_service/redis"
)

const (
	maxFileBytes = 20 << 20
	streamChunk  = 64 << 10
)

var blacklistedExtensions = map[string]struct{}{
	"app": {}, "apk": {}, "asp": {}, "aspx": {}, "bash": {}, "bat": {},
	"bin": {}, "cgi": {}, "cmd": {}, "com": {}, "dll": {}, "dylib": {},
	"exe": {}, "htm": {}, "html": {}, "jsp": {}, "jspx": {}, "js": {},
	"jar": {}, "lnk": {}, "msi": {}, "phtml": {}, "php": {}, "php3": {},
	"php4": {}, "php5": {}, "pl": {}, "ps1": {}, "py": {}, "rb": {},
	"scr": {}, "shtml": {}, "sh": {}, "so": {}, "vbs": {}, "wsf": {},
	"zsh": {},
}

var blacklistedMIMEs = map[string]struct{}{
	"application/java-archive":    {},
	"application/x-dosexec":       {},
	"application/x-executable":    {},
	"application/x-httpd-php":     {},
	"application/x-msdownload":    {},
	"application/x-msdos-program": {},
	"application/x-sh":            {},
	"application/x-shellscript":   {},
	"application/xhtml+xml":       {},
	"text/html":                   {},
	"text/javascript":             {},
}

// Service implements the worker FileService.
type Service struct {
	filev1.UnimplementedFileServiceServer

	repository *base.FileRepository
	quota      *workerredis.AttachmentQuota
	attachDir  string
	now        func() time.Time
	logger     *slog.Logger

	mu      sync.Mutex
	pending map[string]*pendingUpload
}

type pendingUpload struct {
	meta      base.FileMeta
	tempPath  string
	reserved  int64
	committed bool
}

// NewService constructs the attachment service.
func NewService(
	repository *base.FileRepository,
	quota *workerredis.AttachmentQuota,
	attachDir string,
	now func() time.Time,
	loggers ...*slog.Logger,
) *Service {
	if now == nil {
		now = time.Now
	}
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return &Service{
		repository: repository,
		quota:      quota,
		attachDir:  filepath.Clean(attachDir),
		now:        now,
		logger:     logger,
		pending:    make(map[string]*pendingUpload),
	}
}

// Register installs the file service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	filev1.RegisterFileServiceServer(registrar, service)
}

// AttachDir returns the configured attachment root.
func (service *Service) AttachDir() string {
	if service == nil {
		return ""
	}
	return service.attachDir
}

func principalFromContext(ctx context.Context) (*commonv1.Principal, error) {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	payloadList := incoming.Get("x-principal")
	if len(payloadList) == 0 {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	payload, err := base64.StdEncoding.DecodeString(payloadList[0])
	if err != nil {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	var principal commonv1.Principal
	if err := proto.Unmarshal(payload, &principal); err != nil ||
		principal.GetUserId() == "" {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	return &principal, nil
}

func incomingMetadata(ctx context.Context, key string) string {
	incoming, _ := metadata.FromIncomingContext(ctx)
	values := incoming.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func validateUploadMetadata(request *filev1.InitUploadRequest) (string, string, error) {
	if request == nil {
		return "", "", rpcError(deal.CodeInvalidArgument)
	}
	name := filepath.Base(strings.ReplaceAll(strings.TrimSpace(request.GetName()), "\\", "/"))
	if name == "" || name == "." {
		return "", "", rpcError(deal.CodeInvalidArgument)
	}
	ext := normalizeExtension(request.GetExt())
	if ext == "" {
		ext = normalizeExtension(filepath.Ext(name))
	}
	if ext == "" {
		return "", "", rpcError(deal.CodeInvalidArgument)
	}
	if _, blocked := blacklistedExtensions[ext]; blocked ||
		hasBlacklistedNameExtension(name) {
		return "", "", rpcError(deal.CodeFileRejected)
	}
	claimedMIME := normalizeMIME(request.GetMime())
	if claimedMIME == "" || isBlacklistedMIME(claimedMIME) {
		return "", "", rpcError(deal.CodeFileRejected)
	}
	if request.GetSize() <= 0 || request.GetSize() > maxFileBytes {
		return "", "", rpcError(deal.CodeFileRejected)
	}
	if _, ok := filev1.FileUsage_name[int32(request.GetUsage())]; !ok ||
		request.GetUsage() == filev1.FileUsage_FILE_USAGE_UNSPECIFIED {
		return "", "", rpcError(deal.CodeInvalidArgument)
	}
	return name, ext, nil
}

func hasBlacklistedNameExtension(name string) bool {
	parts := strings.Split(strings.ToLower(name), ".")
	for _, part := range parts[1:] {
		if _, blocked := blacklistedExtensions[part]; blocked {
			return true
		}
	}
	return false
}

func normalizeExtension(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, ".")
	if strings.ContainsAny(value, `/\`) {
		return ""
	}
	return value
}

func normalizeMIME(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.ToLower(mediaType)
}

func isBlacklistedMIME(value string) bool {
	_, blocked := blacklistedMIMEs[normalizeMIME(value)]
	return blocked
}

func inspectChunk(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	detected := http.DetectContentType(chunk)
	if isBlacklistedMIME(detected) {
		return rpcError(deal.CodeFileRejected)
	}
	return nil
}

func usageName(usage filev1.FileUsage) string {
	switch usage {
	case filev1.FileUsage_FILE_USAGE_ATTACHMENT:
		return "attachment"
	case filev1.FileUsage_FILE_USAGE_IMPORT_TEMP:
		return "import_temp"
	case filev1.FileUsage_FILE_USAGE_AVATAR:
		return "avatar"
	default:
		return ""
	}
}

func fileMetaToProto(meta base.FileMeta) *filev1.FileMeta {
	return &filev1.FileMeta{
		Id:          meta.ID,
		Name:        meta.Name,
		DisplayName: meta.DisplayName,
		Ext:         meta.Ext,
		Mime:        meta.Mime,
		Size:        meta.Size,
		Uploader:    meta.Uploader,
		CreatedAt:   meta.CreatedAt,
	}
}

func (service *Service) releasePending(pending *pendingUpload) {
	if pending == nil {
		return
	}
	_ = os.Remove(pending.tempPath)
	if pending.reserved > 0 {
		_ = service.quota.Release(context.Background(), pending.reserved)
		pending.reserved = 0
	}
}

func setFileHeaders(ctx context.Context, name, mimeType string) {
	_ = grpc.SetHeader(ctx, metadata.Pairs(
		"x-file-name", name,
		"x-file-mime", mimeType,
	))
}

func rpcError(code int) error {
	spec, ok := deal.LookupError(code)
	if !ok {
		spec, _ = deal.LookupError(deal.CodeServiceUnavailable)
		code = deal.CodeServiceUnavailable
	}
	withDetails, err := status.New(spec.GRPCCode, spec.Message).WithDetails(
		&errdetails.ErrorInfo{Reason: strconv.Itoa(code)},
	)
	if err != nil {
		return status.Error(spec.GRPCCode, spec.Message)
	}
	return withDetails.Err()
}

func invalidArgument(message string) error {
	return status.Error(codes.InvalidArgument, message)
}

func isNotFound(err error) bool {
	return errors.Is(err, base.ErrFileNotFound)
}

func cleanDisplayName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 255 {
		return "", fmt.Errorf("invalid display name")
	}
	return value, nil
}
