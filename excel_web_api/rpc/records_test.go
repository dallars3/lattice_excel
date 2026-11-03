package rpc_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	"excel_web_api/rpc"
)

type recordsWriteTestServer struct {
	recordsv1.UnimplementedRecordsServiceServer
	createRequest   *recordsv1.CreateRecordRequest
	updateRequest   *recordsv1.UpdateCellRequest
	batchRequest    *recordsv1.BatchUpdateRequest
	deleteRequest   *recordsv1.DeleteRecordsRequest
	undoRequest     *recordsv1.UndoDeleteRequest
	rollbackRequest *recordsv1.RollbackRequest
}

func (server *recordsWriteTestServer) CreateRecord(
	_ context.Context,
	request *recordsv1.CreateRecordRequest,
) (*recordsv1.CreateRecordResponse, error) {
	server.createRequest = request
	return &recordsv1.CreateRecordResponse{Record: &recordsv1.Record{Id: "created"}}, nil
}

func (server *recordsWriteTestServer) UpdateCell(
	_ context.Context,
	request *recordsv1.UpdateCellRequest,
) (*recordsv1.UpdateCellResponse, error) {
	server.updateRequest = request
	return &recordsv1.UpdateCellResponse{Value: request.GetValue(), Version: 2}, nil
}

func (server *recordsWriteTestServer) BatchUpdate(
	_ context.Context,
	request *recordsv1.BatchUpdateRequest,
) (*recordsv1.BatchUpdateResponse, error) {
	server.batchRequest = request
	return &recordsv1.BatchUpdateResponse{SkippedCount: 1}, nil
}

func (server *recordsWriteTestServer) DeleteRecords(
	_ context.Context,
	request *recordsv1.DeleteRecordsRequest,
) (*recordsv1.DeleteRecordsResponse, error) {
	server.deleteRequest = request
	return &recordsv1.DeleteRecordsResponse{DeletedCount: 1}, nil
}

func (server *recordsWriteTestServer) UndoDelete(
	_ context.Context,
	request *recordsv1.UndoDeleteRequest,
) (*recordsv1.UndoDeleteResponse, error) {
	server.undoRequest = request
	return &recordsv1.UndoDeleteResponse{Record: &recordsv1.Record{Id: request.GetRecordId()}}, nil
}

func (server *recordsWriteTestServer) Rollback(
	_ context.Context,
	request *recordsv1.RollbackRequest,
) (*recordsv1.RollbackResponse, error) {
	server.rollbackRequest = request
	return &recordsv1.RollbackResponse{Record: &recordsv1.Record{Id: "rolled-back"}}, nil
}

func TestRecordsClientForwardsWriteMethods(t *testing.T) {
	serverImpl := &recordsWriteTestServer{}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	recordsv1.RegisterRecordsServiceServer(server, serverImpl)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///records-rpc-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := rpc.NewRecordsClient(connection)
	ctx := context.Background()

	create, err := client.CreateRecord(ctx, &recordsv1.CreateRecordRequest{
		TableId:         "table_1",
		ClientRequestId: "req_create",
	})
	if err != nil || create.GetRecord().GetId() != "created" ||
		serverImpl.createRequest.GetTableId() != "table_1" {
		t.Fatalf("CreateRecord() = %v, %v request=%v", create, err, serverImpl.createRequest)
	}
	update, err := client.UpdateCell(ctx, &recordsv1.UpdateCellRequest{
		RecordId: "record_1",
		FieldId:  "field_1",
		Value: &commonv1.Value{
			Kind: &commonv1.Value_IntValue{IntValue: 7},
		},
		ExpectVersion: 1,
	})
	if err != nil || update.GetValue().GetIntValue() != 7 ||
		serverImpl.updateRequest.GetRecordId() != "record_1" {
		t.Fatalf("UpdateCell() = %v, %v request=%v", update, err, serverImpl.updateRequest)
	}
	batch, err := client.BatchUpdate(ctx, &recordsv1.BatchUpdateRequest{
		ClientRequestId: "req_batch",
	})
	if err != nil || batch.GetSkippedCount() != 1 ||
		serverImpl.batchRequest.GetClientRequestId() != "req_batch" {
		t.Fatalf("BatchUpdate() = %v, %v request=%v", batch, err, serverImpl.batchRequest)
	}
	deleted, err := client.DeleteRecords(ctx, &recordsv1.DeleteRecordsRequest{
		RecordIdList: []string{"record_1"},
	})
	if err != nil || deleted.GetDeletedCount() != 1 ||
		len(serverImpl.deleteRequest.GetRecordIdList()) != 1 {
		t.Fatalf("DeleteRecords() = %v, %v request=%v", deleted, err, serverImpl.deleteRequest)
	}
	undo, err := client.UndoDelete(ctx, &recordsv1.UndoDeleteRequest{RecordId: "record_1"})
	if err != nil || undo.GetRecord().GetId() != "record_1" ||
		serverImpl.undoRequest.GetRecordId() != "record_1" {
		t.Fatalf("UndoDelete() = %v, %v request=%v", undo, err, serverImpl.undoRequest)
	}
	rollback, err := client.Rollback(ctx, &recordsv1.RollbackRequest{NodeId: "history_1"})
	if err != nil || rollback.GetRecord().GetId() != "rolled-back" ||
		serverImpl.rollbackRequest.GetNodeId() != "history_1" {
		t.Fatalf("Rollback() = %v, %v request=%v", rollback, err, serverImpl.rollbackRequest)
	}
}
