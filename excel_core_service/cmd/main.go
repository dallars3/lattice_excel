// excel_core_service：业务主域（gRPC :9091，仅 localhost）。
// 分层约束见《整体技术方案 v1.3》§10；internal/ 只留业务逻辑，DB 一律经 base/。
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"excel_core_service/base"
	redisclient "excel_core_service/redis"
)

const configPath = "configs/core_service_config.yaml"

func newGRPCServer(ctx context.Context, ready func(context.Context) error) *grpc.Server {
	server := grpc.NewServer()
	healthServer := health.NewServer()

	status := healthpb.HealthCheckResponse_SERVING
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ready(probeCtx); err != nil {
		status = healthpb.HealthCheckResponse_NOT_SERVING
	}
	healthServer.SetServingStatus("", status)
	healthpb.RegisterHealthServer(server, healthServer)
	return server
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, configPath); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, path string) error {
	config, err := base.LoadConfig(path)
	if err != nil {
		return fmt.Errorf("load core config: %w", err)
	}

	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := base.MigrateUp(db); err != nil {
		return err
	}

	redis, err := redisclient.Open(ctx, config.RedisAddr)
	if err != nil {
		return err
	}
	defer redis.Close()

	ready := func(probeCtx context.Context) error {
		if err := db.PingContext(probeCtx); err != nil {
			return fmt.Errorf("core mysql not ready: %w", err)
		}
		return redisclient.Ping(probeCtx, redis)
	}

	listener, err := net.Listen("tcp", config.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.GRPCAddr, err)
	}
	server := newGRPCServer(ctx, ready)
	log.Printf("[excel_core_service] serving gRPC on %s", config.GRPCAddr)
	log.Printf("[excel_core_service] permission config: %s", config.PermissionConfigPath)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		server.GracefulStop()
		log.Println("[excel_core_service] stopped")
		return nil
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve core gRPC: %w", err)
		}
		return nil
	}
}
