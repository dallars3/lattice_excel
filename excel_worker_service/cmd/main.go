// excel_worker_service：异步与 IO 域（gRPC :9092，仅 localhost + 本地任务循环）。
// 分层约束见《整体技术方案 v1.3》§10。
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

const defaultGRPCAddr = ":9092"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[excel_worker_service] start on %s (localhost only)", defaultGRPCAddr)

	// TODO(骨架)：
	// 1) 加载 configs/worker_service_config.yaml（MySQL/Redis/附件目录/池上限）
	// 2) base/、redis/、bus/、rpc/：基础设施初始化（rpc/ 封装对 core 的 client）
	// 3) 装配 internal/{file,importexport,notify,scheduler} 到 grpc-go server
	//    + scheduler 的 MySQL 任务轮询循环（FOR UPDATE SKIP LOCKED）
	// 4) 权限已由 excel_web_api 统一校验，本进程不重复角色校验

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[excel_worker_service] stopped")
}