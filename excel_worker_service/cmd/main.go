// excel_worker_service：异步与 IO 域（gRPC :9092，仅 localhost + 本地任务循环）。
// 分层约束见《整体技术方案 v1.3》§10。
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"os/signal"
	"syscall"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"excel-libs/eventbus"
	"excel_worker_service/base"
	workerbus "excel_worker_service/bus"
	filelogic "excel_worker_service/internal/file"
	"excel_worker_service/internal/importexport"
	"excel_worker_service/internal/notify"
	"excel_worker_service/internal/scheduler"
	worker_rpc "excel_worker_service/rpc"
	workerredis "excel_worker_service/redis"
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
	broker := eventbus.NewRedis(workerredis.NewEventBridge(redis), eventbus.RedisConfig{})
	coreConnection, err := grpc.NewClient(
		config.CoreGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("create core gRPC client: %w", err)
	}
	defer coreConnection.Close()
	attachDir, err := filepath.Abs(config.AttachDir)
	if err != nil {
		return fmt.Errorf("resolve attach dir: %w", err)
	}
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		return fmt.Errorf("ensure attach dir: %w", err)
	}
	fileService := filelogic.NewService(
		base.NewFileRepository(db),
		workerredis.NewAttachmentQuota(redis, config.AttachPoolLimitBytes),
		attachDir,
		time.Now,
	)
	filelogic.Register(server, fileService)
	importExportService := importexport.NewService(
		base.NewFileRepository(db),
		base.NewImportExportRepository(db),
		worker_rpc.NewRecordsClient(coreConnection),
		worker_rpc.NewWorkspaceClient(coreConnection),
		worker_rpc.NewStructureClient(coreConnection),
		broker,
		attachDir,
		time.Now,
	)
	importexport.Register(server, importExportService)
	notifyService := notify.NewService(
		base.NewNotifyRepository(db),
		workerredis.NewNotifyCounters(redis),
		broker,
		time.Now,
	)
	notify.Register(server, notifyService)
	enterpriseCleanup, err := scheduler.NewEnterpriseCleanup(scheduler.EnterpriseCleanupDependencies{
		Files:         fileService,
		Tasks:         importExportService,
		Notifications: notifyService,
	})
	if err != nil {
		return fmt.Errorf("construct enterprise cleanup: %w", err)
	}
	runner := scheduler.NewRunner(base.NewTaskRepository(db), scheduler.RunnerOptions{})
	iamClient := worker_rpc.NewIAMClient(coreConnection)
	if err := runner.Register("deactivation", (&scheduler.DeactivationCooloff{IAM: iamClient}).Handle); err != nil {
		return fmt.Errorf("register deactivation task: %w", err)
	}
	if err := runner.Register("enterprise_dissolution", (&scheduler.EnterpriseDissolution{IAM: iamClient}).Handle); err != nil {
		return fmt.Errorf("register enterprise dissolution task: %w", err)
	}
	retentionCleanup := scheduler.RetentionCleanupHandler{
		Job: scheduler.RetentionJob{
			Tracking: base.NewTrackingRetentionRepository(db),
		},
		Now: func() int64 { return time.Now().UTC().UnixMilli() },
	}
	if err := runner.Register(base.TaskTypeRetentionCleanup, retentionCleanup.Handle); err != nil {
		return fmt.Errorf("register retention cleanup task: %w", err)
	}
	if err := runner.Start(ctx); err != nil {
		return fmt.Errorf("start scheduler runner: %w", err)
	}
	consumer := workerbus.NewConsumer(db, broker, workerbus.ConsumerConfig{})
	if err := notify.Consumers(consumer, notifyService); err != nil {
		return fmt.Errorf("register notify consumers: %w", err)
	}
	if err := scheduler.Consumers(consumer, enterpriseCleanup); err != nil {
		return fmt.Errorf("register lifecycle consumers: %w", err)
	}
	lifecycleIntake, err := scheduler.NewLifecycleIntake(base.NewTaskRepository(db))
	if err != nil {
		return fmt.Errorf("construct lifecycle intake: %w", err)
	}
	if err := scheduler.RegisterLifecycleIntake(consumer, lifecycleIntake); err != nil {
		return fmt.Errorf("register lifecycle intake consumers: %w", err)
	}
	if err := consumer.Start(ctx); err != nil {
		return fmt.Errorf("start worker event consumers: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if err := consumer.Stop(stopCtx); err != nil {
			log.Printf("[excel_worker_service] stop event consumers: %v", err)
		}
		if err := runner.Stop(stopCtx); err != nil {
			log.Printf("[excel_worker_service] stop scheduler runner: %v", err)
		}
	}()
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
