// excel_web_api：唯一公网入口（HTTP/WS，零 MySQL）。
// 分层约束见《整体技术方案 v1.3》§10：rpc/redis/deal 为业务无关封装，internal/ 仅业务逻辑。
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"excel-libs/eventbus"
	"excel_web_api/deal"
	"excel_web_api/internal/gateway"
	"excel_web_api/internal/realtime"
	redisclient "excel_web_api/redis"
	"excel_web_api/rpc"
)

const configPath = "configs/web_api_config.yaml"

type redisReadiness struct {
	client *goredis.Client
}

func (readiness redisReadiness) Ready(ctx context.Context) error {
	if err := readiness.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("web redis not ready: %w", err)
	}
	return nil
}

type grpcHealthReadiness struct {
	dependency string
	client     healthpb.HealthClient
}

func (readiness grpcHealthReadiness) Ready(ctx context.Context) error {
	response, err := readiness.client.Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("%s health check: %w", readiness.dependency, err)
	}
	if response.Status != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("%s is %s", readiness.dependency, response.Status)
	}
	return nil
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
	config, err := deal.LoadConfig(path)
	if err != nil {
		return fmt.Errorf("load web API config: %w", err)
	}

	redis := goredis.NewClient(&goredis.Options{
		Addr:         config.RedisAddr,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	defer redis.Close()
	if err := redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping web redis: %w", err)
	}

	coreConnection, err := grpc.NewClient(config.CoreGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("create core gRPC client: %w", err)
	}
	defer coreConnection.Close()
	workerConnection, err := grpc.NewClient(config.WorkerGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("create worker gRPC client: %w", err)
	}
	defer workerConnection.Close()

	cache := redisclient.NewCache(redis)
	iamClient := rpc.NewIamClient(coreConnection)
	pipeline := gateway.NewPipeline(
		iamClient,
		rpc.NewShareClient(coreConnection),
		cache,
		slog.Default(),
	)
	pipeline.RegisterAuthRoutes(iamClient)
	gateway.RegisterRoutes(pipeline, rpc.NewRecordsClient(coreConnection))
	gateway.RegisterNotifyRoutes(pipeline, rpc.NewNotifyClient(workerConnection))
	gateway.RegisterFileRoutes(pipeline, rpc.NewFileClient(workerConnection))
	gateway.RegisterImportExportRoutes(pipeline, rpc.NewImportExportClient(workerConnection))
	invalidator := redisclient.NewAuthInvalidator(
		cache,
		eventbus.NewRedis(redisclient.NewEventBridge(redis), eventbus.RedisConfig{}),
		slog.Default(),
	)
	if err := invalidator.Start(ctx); err != nil {
		return fmt.Errorf("start auth cache invalidator: %w", err)
	}
	defer invalidator.Close()
	hub := realtime.NewHub(realtime.HubDeps{
		Session: iamClient,
		Events:  nil, // TODO: 适配 records.GetEventsSince 到补发帧（后续波）
		Bus: eventbus.NewRedis(
			redisclient.NewEventBridge(redis),
			eventbus.RedisConfig{ChannelPrefix: "excel:live:"},
		),
	})
	if err := hub.Start(ctx); err != nil {
		return fmt.Errorf("start realtime hub: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if err := hub.Close(closeCtx); err != nil {
			log.Printf("[excel_web_api] close realtime hub: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/healthz", gateway.Healthz())
	mux.Handle("/readyz", gateway.Readyz(
		redisReadiness{client: redis},
		grpcHealthReadiness{dependency: "core", client: healthpb.NewHealthClient(coreConnection)},
		grpcHealthReadiness{dependency: "worker", client: healthpb.NewHealthClient(workerConnection)},
	))
	mux.Handle("/api/v1/", pipeline.Handler())
	mux.Handle("/bases/{base_id}/tables/{table_id}/ws", hub.Handler())

	listener, err := net.Listen("tcp", config.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.Addr, err)
	}
	server := &http.Server{
		Addr:              config.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	log.Printf("[excel_web_api] serving HTTP on %s", config.Addr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown web API: %w", err)
		}
		log.Println("[excel_web_api] stopped")
		return nil
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve web API: %w", err)
		}
		return nil
	}
}
