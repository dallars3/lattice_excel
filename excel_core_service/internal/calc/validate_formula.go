// calc/validate_formula.go：公式定义期（字段保存前）表达式校验，票 07。
// 函数清单唯一事实源 = functionRegistry（连接函数 CONCATENATE，不存在 CONCAT）；
// 字段引用的存在性由结构层依赖图校验（dependency.go），此处只做语法与函数名。
package calc

import (
	"fmt"
	"strings"
)

// FormulaExpressionError 表示公式定义期校验失败，Reason 面向用户（如
// 「未知函数: CONCAT」「语法错误: …」）；保存路径据此把文案透传给网关。
type FormulaExpressionError struct {
	Reason string
}

func (err *FormulaExpressionError) Error() string {
	return err.Reason
}

// ValidateFormulaExpression parses a formula definition and rejects syntax
// errors and unknown function names. Argument-count and operand-type mismatches
// stay runtime #ERROR sentinels（存量哨兵行为不变）。
func ValidateFormulaExpression(formula string) error {
	parsed, err := parseFormula(formula)
	if err != nil {
		return &FormulaExpressionError{Reason: fmt.Sprintf("语法错误: %s", err.Error())}
	}
	if unknown := firstUnknownFunction(parsed); unknown != "" {
		return &FormulaExpressionError{Reason: fmt.Sprintf("未知函数: %s", unknown)}
	}
	return nil
}

// firstUnknownFunction walks the parsed expression and returns the first call
// name absent from functionRegistry（与求值期 callExpression.eval 同为大写归一）。
func firstUnknownFunction(parsed expression) string {
	switch typed := parsed.(type) {
	case callExpression:
		name := strings.ToUpper(typed.name)
		if _, ok := functionRegistry[name]; !ok {
			return name
		}
		for _, argument := range typed.arguments {
			if unknown := firstUnknownFunction(argument); unknown != "" {
				return unknown
			}
		}
	case unaryExpression:
		return firstUnknownFunction(typed.operand)
	case binaryExpression:
		if unknown := firstUnknownFunction(typed.left); unknown != "" {
			return unknown
		}
		return firstUnknownFunction(typed.right)
	default:
		return ""
	}
	return ""
}
