// excel_worker_service：异步与 IO 域（gRPC :9092，仅 localhost + 本地任务循环）。
// 分层约束见《整体技术方案 v1.3》§10。
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

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"excel_worker_service/base"
)

const configPath = "configs/worker_service_config.yaml"

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
		return fmt.Errorf("load worker config: %w", err)
	}

	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := base.MigrateUp(db); err != nil {
		return err
	}

	redis := goredis.NewClient(&goredis.Options{
		Addr:         config.RedisAddr,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	defer redis.Close()
	if err := redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping worker redis: %w", err)
	}

	ready := func(probeCtx context.Context) error {
		if err := db.PingContext(probeCtx); err != nil {
			return fmt.Errorf("worker mysql not ready: %w", err)
		}
		if err := redis.Ping(probeCtx).Err(); err != nil {
			return fmt.Errorf("worker redis not ready: %w", err)
		}
		return nil
	}

	listener, err := net.Listen("tcp", config.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.GRPCAddr, err)
	}
	server := newGRPCServer(ctx, ready)
	log.Printf("[excel_worker_service] serving gRPC on %s", config.GRPCAddr)
	log.Printf("[excel_worker_service] attachment pool: %s (%d bytes)", config.AttachDir, config.AttachPoolLimitBytes)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		server.GracefulStop()
		log.Println("[excel_worker_service] stopped")
		return nil
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve worker gRPC: %w", err)
		}
		return nil
	}
}
