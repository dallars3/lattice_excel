// importexport/common.go：导入导出任务、身份与共享协议转换。
package importexport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"excel-libs/eventbus"
	importexportv1 "excel/api/importexport/v1"
	"excel_worker_service/base"
	worker_rpc "excel_worker_service/rpc"
)

const importExportChunkBytes = 64 << 10

const (
	maxImportFileBytes = 20 << 20
	maxImportRows      = 50_000
	importPreviewRows  = 5

	statusImporting = "IMPORTING"
	statusDone      = "DONE"
	statusFailed    = "FAILED"
	statusPending   = "PENDING"
)

// Service implements the worker ImportExportService.
type Service struct {
	importexportv1.UnimplementedImportExportServiceServer

	files     *base.FileRepository
	tasks     *base.ImportExportRepository
	records   worker_rpc.RecordsRPC
	workspace worker_rpc.WorkspaceRPC
	structure worker_rpc.StructureRPC
	bus       eventbus.EventBus
	attachDir string
	now       func() time.Time
	logger    *slog.Logger
}

// NewService constructs the import/export service.
func NewService(
	files *base.FileRepository,
	tasks *base.ImportExportRepository,
	records worker_rpc.RecordsRPC,
	workspace worker_rpc.WorkspaceRPC,
	structure worker_rpc.StructureRPC,
	bus eventbus.EventBus,
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
		files:     files,
		tasks:     tasks,
		records:   records,
		workspace: workspace,
		structure: structure,
		bus:       bus,
		attachDir: attachDir,
		now:       now,
		logger:    logger,
	}
}

// Register installs the import/export service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	importexportv1.RegisterImportExportServiceServer(registrar, service)
}


func metadataValue(ctx context.Context, key string) string {
	incoming, _ := metadata.FromIncomingContext(ctx)
	values := incoming.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func taskToProto(task base.ImportExportTask) (*importexportv1.ImportTask, error) {
	statusValue, ok := importexportv1.ImportStatus_value["IMPORT_STATUS_"+task.Status]
	if !ok {
		return nil, errors.New("unknown import status " + task.Status)
	}
	return &importexportv1.ImportTask{
		Id:           task.ID,
		Status:       importexportv1.ImportStatus(statusValue),
		BaseId:       task.BaseID,
		TotalRows:    task.TotalRows,
		OkRows:       task.OKRows,
		FailRows:     task.FailRows,
		ReportFileId: task.ReportFileID,
		CreatedAt:    task.CreatedAt,
	}, nil
}

func exportTaskToProto(task base.ImportExportTask) (*importexportv1.ExportTask, error) {
	statusValue, ok := importexportv1.ExportStatus_value["EXPORT_STATUS_"+task.Status]
	if !ok {
		return nil, errors.New("unknown export status " + task.Status)
	}
	formatValue, ok := importexportv1.ExportFormat_value["EXPORT_FORMAT_"+strings.ToUpper(task.Format)]
	if !ok {
		return nil, errors.New("unknown export format " + task.Format)
	}
	payload := exportPayload{}
	if len(task.Payload) > 0 {
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode export task payload: %w", err)
		}
	}
	return &importexportv1.ExportTask{
		Id:        task.ID,
		Status:    importexportv1.ExportStatus(statusValue),
		Format:    importexportv1.ExportFormat(formatValue),
		FileName:  payload.FileName,
		Rows:      task.OKRows,
		CreatedAt: task.CreatedAt,
	}, nil
}

type exportPayload struct {
	FileName     string   `json:"file_name,omitempty"`
	Path         string   `json:"path,omitempty"`
	RowList      []string `json:"row_list,omitempty"`
	CreatorID    string   `json:"creator_id,omitempty"`
	EnterpriseID string   `json:"enterprise_id,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type importReportSender interface {
	Send(*importexportv1.ReportChunk) error
}

type exportChunkSender interface {
	Send(*importexportv1.ExportChunk) error
}

func streamImportChunks(reader io.Reader, sender importReportSender) error {
	buffer := make([]byte, importExportChunkBytes)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			chunk := append([]byte(nil), buffer[:count]...)
			if sendErr := sender.Send(&importexportv1.ReportChunk{Chunk: chunk}); sendErr != nil {
				return sendErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func streamExportChunks(reader io.Reader, sender exportChunkSender) error {
	buffer := make([]byte, importExportChunkBytes)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			chunk := append([]byte(nil), buffer[:count]...)
			if sendErr := sender.Send(&importexportv1.ExportChunk{Chunk: chunk}); sendErr != nil {
				return sendErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

