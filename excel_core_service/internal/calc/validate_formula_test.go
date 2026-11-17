// calc/validate_formula_test.go：公式定义期表达式校验（票 07，BUG-05 后端）。
// 函数清单唯一事实源 = functionRegistry（连接函数 CONCATENATE，不存在 CONCAT）。
package calc

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateFormulaExpression(t *testing.T) {
	cases := []struct {
		name       string
		formula    string
		wantReason string // 空串表示应通过
	}{
		{name: "unknown function CONCAT", formula: `CONCAT({名称}, "x")`, wantReason: "未知函数: CONCAT"},
		{name: "unknown function name upper normalized", formula: `concat({名称})`, wantReason: "未知函数: CONCAT"},
		{name: "unknown function nested in arguments", formula: `SUM(FOO({金额}), 1)`, wantReason: "未知函数: FOO"},
		{name: "valid CONCATENATE", formula: `CONCATENATE({名称}, "-后缀")`},
		{name: "valid arithmetic with function", formula: `IF({金额} > 0, {金额} * 2, 0)`},
		{name: "syntax error missing paren", formula: `SUM({金额}`, wantReason: "语法错误"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateFormulaExpression(test.formula)
			if test.wantReason == "" {
				if err != nil {
					t.Fatalf("ValidateFormulaExpression(%q) error = %v, want nil", test.formula, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateFormulaExpression(%q) error = nil, want reason %q", test.formula, test.wantReason)
			}
			var formulaError *FormulaExpressionError
			if !errors.As(err, &formulaError) {
				t.Fatalf("ValidateFormulaExpression(%q) error = %T, want *FormulaExpressionError", test.formula, err)
			}
			if !strings.Contains(formulaError.Reason, test.wantReason) {
				t.Fatalf("reason = %q, want contains %q", formulaError.Reason, test.wantReason)
			}
		})
	}
}
