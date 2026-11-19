package iam

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"excel_core_service/base"
)

// 评审整改回归（双轴评审第 1 项）：补建失败注入后 ensureDefaultEnterprise 必须
// 返回**原 user**（孤立态保持）而非零值——login/get_me 据此继续「仅告警不阻断」，
// 会话主体与响应携带真实 user_id；零值用户会建出空 user_id 会话（CODING_STANDARDS
// §3）。失败注入：repository 指向已关闭连接池，BeginTx 必然失败。
func TestEnsureDefaultEnterpriseKeepsUserOnBackfillFailure(t *testing.T) {
	broken, err := sql.Open("mysql", testMySQLDSN)
	if err != nil {
		t.Fatalf("open broken repository db: %v", err)
	}
	broken.Close()
	service := &IamService{repository: base.NewIAMRepository(broken), now: time.Now}
	user := base.IAMUser{
		ID:       "01J000000000000000000AD201",
		Username: "Orphan_Fail",
		Internal: true,
	}

	updated, err := service.ensureDefaultEnterprise(context.Background(), user)
	if err == nil {
		t.Fatal("ensureDefaultEnterprise() error = nil, want backfill failure")
	}
	if updated.ID != user.ID || updated.Username != user.Username ||
		updated.EnterpriseID != "" || !updated.Internal {
		t.Fatalf(
			"failure returned mutated/zero user: %+v, want original %+v",
			updated,
			user,
		)
	}
}

// 票 02（N7）：默认企业名由用户名派生——「{username} 的企业」，超 30 rune 截断、
// 不足 2 rune 以固定文案兜底。表内期望值为独立手工推演，不复用实现逻辑。
func TestDefaultEnterpriseNameDerivation(t *testing.T) {
	cases := []struct {
		label    string
		username string
		want     string
	}{
		{"normal username", "Member_01", "Member_01 的企业"},
		{"minimum username", "abcd", "abcd 的企业"},
		{"cjk username", "陈晨", "陈晨 的企业"},
		{"boundary exactly 30 runes", strings.Repeat("u", 26), strings.Repeat("u", 26) + " 的企业"},
		{"over cap truncated to 30 runes", strings.Repeat("u", 28), strings.Repeat("u", 28) + " 的"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			got := defaultEnterpriseName(c.username)
			if got != c.want {
				t.Fatalf("defaultEnterpriseName(%q) = %q, want %q", c.username, got, c.want)
			}
			if count := len([]rune(got)); count < 2 || count > 30 {
				t.Fatalf("defaultEnterpriseName(%q) = %q has %d runes, want within 2–30", c.username, got, count)
			}
		})
	}
	// 兜底分支说明：派生候选恒含「 的企业」后缀（至少 4 rune），截断下限 30，
	// 故任何输入都无法产生 <2 rune 的结果——固定文案兜底是票面要求的防御性
	// 规则，无法经公开行为触达，不设表项。
	if fallback := defaultEnterpriseNameFallback; len([]rune(fallback)) < 2 {
		t.Fatalf("fallback name %q violates the 2-rune minimum", fallback)
	}
}
