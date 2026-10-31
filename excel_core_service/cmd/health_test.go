package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestGRPCHealthReportsServingWhenDependenciesAreReady(t *testing.T) {
	client, stop := startHealthTestServer(t, func(context.Context) error { return nil })
	defer stop()

	response, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if response.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v, want SERVING", response.Status)
	}
}

func TestGRPCHealthReportsNotServingWhenDependencyFails(t *testing.T) {
	client, stop := startHealthTestServer(t, func(context.Context) error {
		return errors.New("mysql unavailable")
	})
	defer stop()

	response, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if response.Status != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status = %v, want NOT_SERVING", response.Status)
	}
}

func startHealthTestServer(t *testing.T, ready func(context.Context) error) (healthpb.HealthClient, func()) {
	t.Helper()

	listener := bufconn.Listen(1 << 20)
	server := newGRPCServer(context.Background(), ready)
	go func() {
		_ = server.Serve(listener)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	connection, err := grpc.DialContext(
		ctx,
		"bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("DialContext() error = %v", err)
	}

	stop := func() {
		_ = connection.Close()
		server.Stop()
		_ = listener.Close()
	}
	return healthpb.NewHealthClient(connection), stop
}
