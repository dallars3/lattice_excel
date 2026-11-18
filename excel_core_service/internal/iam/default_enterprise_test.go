package iam

import (
	"strings"
	"testing"
)

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
