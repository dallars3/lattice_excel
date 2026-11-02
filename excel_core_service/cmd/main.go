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

	"excel-libs/eventbus"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/audit"
	"excel_core_service/internal/calc"
	"excel_core_service/internal/iam"
	"excel_core_service/internal/share"
	"excel_core_service/internal/structure"
	"excel_core_service/internal/workspace"
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

	redisClient, err := redisclient.Open(ctx, config.RedisAddr)
	if err != nil {
		return err
	}
	defer redisClient.Close()

	ready := func(probeCtx context.Context) error {
		if err := db.PingContext(probeCtx); err != nil {
			return fmt.Errorf("core mysql not ready: %w", err)
		}
		return redisclient.Ping(probeCtx, redisClient)
	}

	listener, err := net.Listen("tcp", config.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.GRPCAddr, err)
	}
	server := newGRPCServer(ctx, ready)
	shareRepository := base.NewShareRepository(db)
	// TODO(装配)：Bases/Members 分别待 09(workspace)/06(iam 成员) 提供真实实现后，
	// 再注册 share.NewService + share.Register；当前无外部调用方，保持未注册。
	iamService := iam.NewIamService(
		base.NewIAMRepository(db),
		redisclient.NewSessionStore(redisClient),
		share.NewLinkResolver(shareRepository),
		bus.NewOutbox(base.NewBusStore(db)),
	)
	iam.Register(server, iamService)
	auditService := audit.NewService(
		base.NewAuditRepository(db),
		iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
	)
	audit.Register(server, auditService)

	busStore := base.NewBusStore(db)
	broker := eventbus.NewRedis(redisclient.NewEventBridge(redisClient), eventbus.RedisConfig{})
	structureRepository := base.NewStructureRepository(db)
	structureService := structure.NewService(
		structureRepository,
		iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
		nil, nil, nil, // RecordsImpactCounter/CalcValidator 待 12/自身默认
	)
	structure.Register(server, structureService)
	calcService := calc.NewService(
		base.NewCalcRepository(db),
		structure.NewSchemaProvider(structureRepository),
		nil, // CellMaterializer 待 12 接入
		bus.NewOutbox(busStore),
		nil,
	)
	calc.Register(server, calcService)
	workspaceService := workspace.NewService(
		base.NewWorkspaceRepository(db),
		iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
		workspace.NewIamInvitationProvider(iamService),
		nil, nil, nil, // 12/14 的 records/comment 复制清理 seam 后接
		workspace.NewStructureBootstrap(structureRepository),
		workspace.NewStructureCleanup(structureRepository),
		func(ctx context.Context, baseID string) error {
			return share.DeleteAllForBase(ctx, shareRepository, baseID)
		},
		bus.NewOutbox(busStore),
		nil,
		workspace.NewStructureDuplicator(structureRepository, time.Now),
	)
	workspace.Register(server, workspaceService)
	forwarder := bus.NewForwarder(
		busStore,
		broker,
		bus.ForwarderConfig{},
	)
	if err := forwarder.Start(ctx); err != nil {
		return fmt.Errorf("start outbox forwarder: %w", err)
	}
	consumer := bus.NewConsumer(busStore, broker, bus.ConsumerConfig{})
	if err := audit.Consumers(consumer, auditService); err != nil {
		return fmt.Errorf("register audit consumers: %w", err)
	}
	if err := calc.Consumers(consumer, calcService); err != nil {
		return fmt.Errorf("register calc consumers: %w", err)
	}
	if err := consumer.Start(ctx); err != nil {
		return fmt.Errorf("start core event consumers: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if err := consumer.Stop(stopCtx); err != nil {
			log.Printf("[excel_core_service] stop core event consumers: %v", err)
		}
		if err := forwarder.Stop(stopCtx); err != nil {
			log.Printf("[excel_core_service] stop outbox forwarder: %v", err)
		}
	}()
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
