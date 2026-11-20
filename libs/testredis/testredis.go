// Package testredis 提供核心与 worker Go 测试共用的 Redis 隔离入口。
//
// 默认沿用 live stack 的 redis_addr，但使用独立的 DB 1；受限部署也可通过
// EXCEL_TEST_REDIS_ADDR 显式指向另一 Redis 实例，并通过 EXCEL_TEST_REDIS_DB
// 指定该实例上的逻辑库。目标仍与 live stack 的 redis_addr/DB 0 相同时，入口在
// 创建客户端或执行任何清理前直接失败，绝不回退到共享 Redis。
package testredis

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	// TestRedisAddrEnv 显式选择专用测试 Redis 实例；缺省复用 live 的 redis_addr。
	TestRedisAddrEnv = "EXCEL_TEST_REDIS_ADDR"
	// TestRedisDBEnv 选择测试逻辑库；缺省使用 DB 1。
	TestRedisDBEnv = "EXCEL_TEST_REDIS_DB"
	// DefaultTestDB 是未显式配置时使用的测试逻辑库，有意避开 live stack 的 DB 0。
	DefaultTestDB = 1
	// LiveStackDB 是当前服务配置使用的 Redis 逻辑库。
	LiveStackDB = 0
)

// Target 是测试 Redis 客户端必须连接的隔离目标。
type Target struct {
	Addr string
	DB   int
}

// Resolve 从 live stack 的 redis_addr 与显式测试设置解析隔离目标。
func Resolve(liveAddr string) (Target, error) {
	liveAddr = strings.TrimSpace(liveAddr)
	if liveAddr == "" {
		return Target{}, fmt.Errorf(
			"测试 Redis 未隔离：live stack 的 redis_addr 为空；请提供有效 redis_addr，并通过 %s/%s 显式指向专用测试实例或逻辑库",
			TestRedisAddrEnv, TestRedisDBEnv)
	}

	testAddr := strings.TrimSpace(os.Getenv(TestRedisAddrEnv))
	if testAddr == "" {
		testAddr = liveAddr
	}

	testDB := DefaultTestDB
	if raw := strings.TrimSpace(os.Getenv(TestRedisDBEnv)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return Target{}, fmt.Errorf(
				"测试 Redis 未隔离：%s=%q 不是有效非负整数；请改用专用逻辑库 DB 1（或其它非 live DB），或用 %s 指向专用测试实例",
				TestRedisDBEnv, raw, TestRedisAddrEnv)
		}
		testDB = parsed
	}

	if sameRedisAddr(testAddr, liveAddr) && testDB == LiveStackDB {
		return Target{}, fmt.Errorf(
			"测试 Redis 未隔离：%s=%q + %s=%d 与 live stack redis_addr=%q + DB %d 相同；请把 %s 设为非 0 逻辑库，或把 %s 指向专用测试实例；禁止静默回退共享 Redis",
			TestRedisAddrEnv, testAddr, TestRedisDBEnv, testDB, liveAddr, LiveStackDB,
			TestRedisDBEnv, TestRedisAddrEnv)
	}
	return Target{Addr: testAddr, DB: testDB}, nil
}

// Open 在解析并校验隔离目标之后创建测试 Redis 客户端；共享 live 目标会在
// 建立连接之前失败。
func Open(ctx context.Context, liveAddr string) (*goredis.Client, error) {
	target, err := Resolve(liveAddr)
	if err != nil {
		return nil, err
	}
	client := goredis.NewClient(&goredis.Options{
		Addr:         target.Addr,
		DB:           target.DB,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping test redis (addr=%s db=%d): %w", target.Addr, target.DB, err)
	}
	return client, nil
}

// FlushTestDB 清理测试逻辑库。执行前同时校验显式测试设置和客户端实际
// addr/DB，任何仍指向 live stack 的目标都会在发出 FlushDB 之前失败。
func FlushTestDB(
	ctx context.Context,
	client *goredis.Client,
	liveAddr string,
	label string,
) error {
	target, err := Resolve(liveAddr)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("%s: 测试 Redis 未隔离：客户端为空；请通过 %s/%s 创建测试客户端",
			label, TestRedisAddrEnv, TestRedisDBEnv)
	}
	options := client.Options()
	if !sameRedisAddr(options.Addr, target.Addr) || options.DB != target.DB {
		return fmt.Errorf(
			"%s: 测试 Redis 未隔离：客户端 addr=%q db=%d，与要求的测试目标 addr=%q db=%d 不同；请通过 %s/%s 创建客户端，禁止清理共享 Redis",
			label, options.Addr, options.DB, target.Addr, target.DB,
			TestRedisAddrEnv, TestRedisDBEnv)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		return fmt.Errorf("%s: flush test Redis DB %d: %w", label, target.DB, err)
	}
	return nil
}

func sameRedisAddr(left, right string) bool {
	leftHost, leftPort, leftErr := net.SplitHostPort(strings.TrimSpace(left))
	rightHost, rightPort, rightErr := net.SplitHostPort(strings.TrimSpace(right))
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
	}
	return normalizeRedisHost(leftHost) == normalizeRedisHost(rightHost) &&
		leftPort == rightPort
}

func normalizeRedisHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return "127.0.0.1"
	}
	return host
}
