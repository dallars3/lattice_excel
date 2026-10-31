// excel_web_api：唯一公网入口（HTTP/WS，零 MySQL）。
// 分层约束见《整体技术方案 v1.3》§10：rpc/redis/deal 为业务无关封装，internal/ 仅业务逻辑。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const defaultAddr = ":8080"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[excel_web_api] start on %s", defaultAddr)

	// TODO(骨架)：
	// 1) 加载 configs/web_api_config.yaml（端口、Redis 地址、core/worker gRPC 目标）
	// 2) 初始化 redis/（限速、会话缓存、PubSub 订阅）与 rpc/（core/worker client）
	// 3) 接入 Gin：/assets 静态资源、/api/v1/* REST→gRPC 桥接、/ws 协作长连接（gorilla/websocket）
	// 4) 统一鉴权编排：internal/gateway 解析会话 → rpc 调 share.CheckAccess → 放行/403
	// 5) /s/{link_token} 链接入口：解析 LinkContext（TTL<60s 会话缓存）

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		// TODO(骨架)：依赖 core/worker/Redis 可达后返回 200，否则 503
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: defaultAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[excel_web_api] shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("[excel_web_api] stopped")
}