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
	"excel_core_service/internal/comment"
	"excel_core_service/internal/iam"
	"excel_core_service/internal/records"
	"excel_core_service/internal/share"
	"excel_core_service/internal/structure"
	"excel_core_service/internal/tracking"
	"excel_core_service/internal/workspace"
	redisclient "excel_core_service/redis"
)

const configPath = "configs/core_service_config.yaml"

// shareLinksCleaner adapts share.DeleteAllForBase to the workspace seam.
type shareLinksCleaner struct {
	repository *base.ShareRepository
}

func (cleaner shareLinksCleaner) DeleteAllForBase(ctx context.Context, baseID string) error {
	return share.DeleteAllForBase(ctx, cleaner.repository, baseID)
}

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
		iam.ServiceOptions{
			Outbox:     bus.NewOutbox(base.NewBusStore(db)),
			AdminToken: config.AdminToken,
			BaseLister: workspace.NewBaseLister(base.NewWorkspaceRepository(db)),
		},
	)
	iam.Register(server, iamService)
	auditService := audit.NewService(
		base.NewAuditRepository(db),
		iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
	)
	audit.Register(server, auditService)

	busStore := base.NewBusStore(db)
	broker := eventbus.NewRedis(redisclient.NewEventBridge(redisClient), eventbus.RedisConfig{})
	workspaceRepository := base.NewWorkspaceRepository(db)
	iamRepository := base.NewIAMRepository(db)
	shareUserLookup := iam.NewShareUserLookup(iamRepository)
	shareService, err := share.NewService(share.ServiceDeps{
		Repository:           shareRepository,
		Outbox:               bus.NewOutbox(busStore),
		Bases:                workspace.NewShareBaseResolver(workspaceRepository),
		ImportLock:           workspace.NewShareBaseResolver(workspaceRepository),
		BaseOwner:            workspace.NewShareBaseOwnerProvider(workspaceRepository),
		Members:              iam.NewMemberRoleProvider(iamRepository),
		UserLookup:           shareUserLookup,
		Profiles:             shareUserLookup, // 协作者条目 username/display_name 取真实值（H-63）
		Users:                nil,             // AddCollaborator 搜索暂不接进程内 client
		PermissionConfigPath: config.PermissionConfigPath,
	})
	if err != nil {
		return fmt.Errorf("construct share service: %w", err)
	}
	share.Register(server, shareService)
	structureRepository := base.NewStructureRepository(db)
	recordsRepository := base.NewRecordsRepository(db)
	structureService := structure.NewService(
		structureRepository,
		iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
		// 字段转换预览/确认迁移与删除影响面共用 records 适配器（缺陷 #26：装配
		// 缺失时预览会 9001）。
		records.NewFieldConversionAdapter(recordsRepository, structureRepository),
		nil, nil, // CalcValidator/now 用默认值
		structure.NewShareReadAccess(shareService),
		// 表删除与 table.deleted 审计事件同事务落 outbox（缺陷 #23）。
		structure.ServiceOptions{Outbox: bus.NewOutbox(busStore)},
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
	commentRepository := base.NewCommentRepository(db)
	workspaceService := workspace.NewService(
		workspaceRepository,
		iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
		workspace.NewIamInvitationProvider(iamService),
		// 复制 Base 的记录行复制器（缺陷 #11：装配缺失时 DuplicateBase 恒 9001）。
		records.NewTableRowsDuplicator(
			recordsRepository,
			structureRepository,
			base.NewCalcRepository(db),
			time.Now,
		),
		records.NewBaseRowsDeleter(recordsRepository),
		comment.NewBaseCommentsDeleter(commentRepository),
		workspace.NewStructureBootstrap(structureRepository),
		workspace.NewStructureCleanup(structureRepository),
		shareLinksCleaner{repository: shareRepository},
		bus.NewOutbox(busStore),
		nil,
		workspace.NewStructureDuplicator(structureRepository, time.Now),
	)
	workspace.Register(server, workspaceService)
	recordsService := records.NewService(
		recordsRepository,
		records.NewStructureViewAccessor(structureRepository, records.NewShareBaseAccess(shareService)),
		records.NewShareRowScopeProvider(shareService),
		// 公式值权威物化投影（H-48/H-49 公式格读取；含 #ERROR 哨兵，工单 09）。
		records.NewCalcFormulaValueProvider(base.NewCalcRepository(db)),
		nil, // now 提供者用默认值
		commentRepository,
		records.WriterOptions{
			Outbox: bus.NewOutbox(busStore),
			Bases: records.NewStructureBaseContextProvider(
				structureRepository,
				records.NewShareBaseAccess(shareService),
			),
			Evaluator:      calcService,
			ComputedWriter: records.NewCalcComputedWriter(base.NewCalcRepository(db)),
		},
	)
	records.Register(server, recordsService)
	commentService := comment.NewService(
		commentRepository,
		bus.NewOutbox(busStore),
		time.Now,
	)
	comment.Register(server, commentService)
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
	if err := workspace.Consumers(consumer, workspaceService); err != nil {
		return fmt.Errorf("register workspace consumers: %w", err)
	}
	trackingService := tracking.NewService(base.NewTrackingRepository(db))
	if err := tracking.Consumers(consumer, trackingService); err != nil {
		return fmt.Errorf("register tracking consumers: %w", err)
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
