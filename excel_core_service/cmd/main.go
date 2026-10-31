// excel_core_service：业务主域（gRPC :9091，仅 localhost）。
// 分层约束见《整体技术方案 v1.3》§10；internal/ 只留业务逻辑，DB 一律经 base/。
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

const defaultGRPCAddr = ":9091"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[excel_core_service] start on %s (localhost only)", defaultGRPCAddr)

	// TODO(骨架)：
	// 1) 加载 configs/core_service_config.yaml 与 excel_permission_config 权限策略 JSON（share 模块热更）
	// 2) base/：初始化 MySQL 连接池与迁移（golang-migrate，仅本模块表）
	// 3) redis/、bus/：初始化 Redis 与 EventBus（outbox 转发 + audit/calc 消费注册）
	// 4) 装配 internal/{iam,share,audit,workspace,structure,records,calc,comment} 到 grpc-go server
	// 5) net.Listen(defaultGRPCAddr)；本进程不暴露公网，principal 仅信任网关注入

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[excel_core_service] stopped")
}