package realtime_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	structurev1 "excel/api/structure/v1"
	"excel_web_api/internal/realtime"
)

type shareAccessFake struct {
	response *sharev1.CheckAccessResponse
	err      error
}

func (fake *shareAccessFake) CheckAccess(
	_ context.Context,
	_ *sharev1.CheckAccessRequest,
) (*sharev1.CheckAccessResponse, error) {
	return fake.response, fake.err
}

type tableProbeFake struct {
	err       error
	lastCtx   context.Context
	lastTable string
}

func (fake *tableProbeFake) ListFields(
	ctx context.Context,
	request *structurev1.ListFieldsRequest,
) (*structurev1.ListFieldsResponse, error) {
	fake.lastCtx = ctx
	fake.lastTable = request.GetTableId()
	return &structurev1.ListFieldsResponse{}, fake.err
}

func TestRPCRoomAuthorizerMapsVerdicts(t *testing.T) {
	principal := &commonv1.Principal{UserId: "usr_alice", Internal: true}
	tests := []struct {
		name       string
		share      *shareAccessFake
		probe      *tableProbeFake
		wantErr    error
		wantStatus int
	}{
		{
			name:  "allowed base with existing table authorizes",
			share: &shareAccessFake{response: &sharev1.CheckAccessResponse{
				Allow: true, Role: sharev1.Role_ROLE_MANAGER,
				RowScope: sharev1.RowScope_ROW_SCOPE_ALL,
			}},
			probe: &tableProbeFake{},
		},
		{
			name:       "denied base is forbidden",
			share:      &shareAccessFake{response: &sharev1.CheckAccessResponse{Allow: false}},
			probe:      &tableProbeFake{},
			wantErr:    realtime.ErrRoomForbidden,
			wantStatus: 403,
		},
		{
			name:       "missing base is not found",
			share:      &shareAccessFake{err: status.Error(codes.NotFound, "4002")},
			probe:      &tableProbeFake{},
			wantErr:    realtime.ErrRoomNotFound,
			wantStatus: 404,
		},
		{
			name:       "share outage stays unavailable",
			share:      &shareAccessFake{err: status.Error(codes.Unavailable, "9001")},
			probe:      &tableProbeFake{},
			wantStatus: 503,
		},
		{
			name:  "missing table is not found",
			share: &shareAccessFake{response: &sharev1.CheckAccessResponse{
				Allow: true, Role: sharev1.Role_ROLE_EDITOR,
				RowScope: sharev1.RowScope_ROW_SCOPE_ALL,
			}},
			probe:      &tableProbeFake{err: status.Error(codes.NotFound, "4002")},
			wantErr:    realtime.ErrRoomNotFound,
			wantStatus: 404,
		},
		{
			name:  "table without read grant is forbidden",
			share: &shareAccessFake{response: &sharev1.CheckAccessResponse{
				Allow: true, Role: sharev1.Role_ROLE_EDITOR,
				RowScope: sharev1.RowScope_ROW_SCOPE_ALL,
			}},
			probe:      &tableProbeFake{err: status.Error(codes.PermissionDenied, "3001")},
			wantErr:    realtime.ErrRoomForbidden,
			wantStatus: 403,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := realtime.NewRPCRoomAuthorizer(test.share, test.probe)
			err := authorizer.AuthorizeRoom(
				context.Background(),
				principal,
				"base_ok",
				"table_ok",
			)
			switch {
			case test.wantErr != nil:
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("AuthorizeRoom() error = %v, want %v", err, test.wantErr)
				}
			case test.wantStatus == 503:
				if err == nil {
					t.Fatal("AuthorizeRoom() error = nil, want propagated outage error")
				}
			case err != nil:
				t.Fatalf("AuthorizeRoom() error = %v, want nil", err)
			}
			if got := realtime.HandshakeStatus(err); got != test.wantStatus {
				t.Fatalf("HandshakeStatus(%v) = %d, want %d", err, got, test.wantStatus)
			}
		})
	}
}

func TestRPCRoomAuthorizerForwardsIdentityToTableProbe(t *testing.T) {
	principal := &commonv1.Principal{UserId: "usr_alice", EnterpriseId: "ent_1"}
	share := &shareAccessFake{response: &sharev1.CheckAccessResponse{
		Allow:    true,
		Role:     sharev1.Role_ROLE_FILLER,
		Internal: true,
		RowScope: sharev1.RowScope_ROW_SCOPE_OWN,
	}}
	probe := &tableProbeFake{}
	authorizer := realtime.NewRPCRoomAuthorizer(share, probe)

	if err := authorizer.AuthorizeRoom(
		context.Background(),
		principal,
		"base_ok",
		"table_ok",
	); err != nil {
		t.Fatalf("AuthorizeRoom() error = %v, want nil", err)
	}
	if probe.lastTable != "table_ok" {
		t.Fatalf("probe addressed table=%q, want table_ok", probe.lastTable)
	}
	meta, ok := metadata.FromOutgoingContext(probe.lastCtx)
	if !ok {
		t.Fatal("probe context carries no outgoing metadata")
	}
	encoded := meta.Get("x-principal")
	if len(encoded) != 1 {
		t.Fatalf("x-principal = %v, want one encoded principal", encoded)
	}
	payload, err := base64.StdEncoding.DecodeString(encoded[0])
	if err != nil {
		t.Fatalf("decode x-principal: %v", err)
	}
	var forwarded commonv1.Principal
	if err := proto.Unmarshal(payload, &forwarded); err != nil {
		t.Fatalf("unmarshal x-principal: %v", err)
	}
	if forwarded.GetUserId() != "usr_alice" {
		t.Fatalf("forwarded principal user = %q, want usr_alice", forwarded.GetUserId())
	}
}

func TestRPCRoomAuthorizerRejectsInvalidRoomAddress(t *testing.T) {
	probe := &tableProbeFake{}
	authorizer := realtime.NewRPCRoomAuthorizer(&shareAccessFake{}, probe)

	if err := authorizer.AuthorizeRoom(
		context.Background(),
		&commonv1.Principal{UserId: "usr_alice"},
		"",
		"table_ok",
	); !errors.Is(err, realtime.ErrRoomNotFound) {
		t.Fatalf("AuthorizeRoom(base=\"\") error = %v, want ErrRoomNotFound", err)
	}
	if probe.lastTable != "" {
		t.Fatalf("probe called for invalid address table=%q", probe.lastTable)
	}
}
