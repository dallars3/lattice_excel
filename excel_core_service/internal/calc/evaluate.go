// calc/evaluate.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package calc

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	calcapi "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
)

type valueKind uint8

const (
	valueEmpty valueKind = iota
	valueText
	valueNumber
	valueBool
	valueDate
	valueList
)

type evalValue struct {
	kind    valueKind
	text    string
	number  float64
	boolean bool
	dateMS  int64
	list    []evalValue
}

type evalContext struct {
	schema    map[string]fieldType
	row       map[string]any
	now       func() time.Time
	location  *time.Location
	functions map[string]functionDefinition
}

type fieldType uint8

const (
	fieldTypeAny fieldType = iota
	fieldTypeText
	fieldTypeNumber
	fieldTypeDate
)

type evalFailure struct {
	reason string
}

func (failure *evalFailure) Error() string {
	return failure.reason
}

func failf(format string, arguments ...any) error {
	return &evalFailure{reason: fmt.Sprintf(format, arguments...)}
}

// Evaluate evaluates independent expressions against one row context. Invalid
// formulas remain cell-level #ERROR results so callers can position the cursor.
func (service *Service) Evaluate(
	ctx context.Context,
	request *calcapi.EvaluateRequest,
) (*calcapi.EvaluateResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "evaluate request is required")
	}
	evaluationContext, err := buildEvalContext(service, request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	response := &calcapi.EvaluateResponse{
		ResultList: make([]*calcapi.EvalResult, 0, len(request.GetExpressionList())),
	}
	for _, source := range request.GetExpressionList() {
		response.ResultList = append(response.ResultList, evaluateExpression(source, evaluationContext))
	}
	return response, nil
}

func buildEvalContext(service *Service, request *calcapi.EvaluateRequest) (*evalContext, error) {
	schemaMap := make(map[string]fieldType, len(request.GetSchemaMap().GetFields()))
	for name, value := range request.GetSchemaMap().GetFields() {
		typeName := strings.TrimSpace(value.GetStringValue())
		parsed, err := parseFieldType(typeName)
		if err != nil {
			return nil, fmt.Errorf("字段 %s 类型无效: %w", name, err)
		}
		schemaMap[name] = parsed
	}
	rowMap := request.GetRowContextMap().AsMap()
	location := shanghaiLocation()
	clock := time.Now
	if service != nil {
		if service.location != nil {
			location = service.location
		}
		if service.now != nil {
			clock = service.now
		}
	}
	return &evalContext{
		schema:    schemaMap,
		row:       rowMap,
		now:       clock,
		location:  location,
		functions: functionRegistry,
	}, nil
}

func parseFieldType(name string) (fieldType, error) {
	switch name {
	case "FIELD_TYPE_TEXT", "FIELD_TYPE_SELECT", "FIELD_TYPE_PERSON",
		"FIELD_TYPE_ATTACHMENT", "FIELD_TYPE_URL":
		return fieldTypeText, nil
	case "FIELD_TYPE_NUMBER":
		return fieldTypeNumber, nil
	case "FIELD_TYPE_DATE":
		return fieldTypeDate, nil
	case "FIELD_TYPE_MULTISELECT", "FIELD_TYPE_FORMULA", "FIELD_TYPE_RELATION",
		"FIELD_TYPE_LOOKUP":
		return fieldTypeAny, nil
	default:
		return fieldTypeAny, failf("未知字段类型 %q", name)
	}
}

func evaluateExpression(source string, evaluationContext *evalContext) *calcapi.EvalResult {
	expression, err := parseFormula(source)
	if err != nil {
		return errorResult(fmt.Errorf("语法错误: %w", err))
	}
	result, err := expression.eval(evaluationContext)
	if err != nil {
		return errorResult(err)
	}
	return &calcapi.EvalResult{Value: result.protoValue()}
}

func errorResult(err error) *calcapi.EvalResult {
	reason := "公式计算失败"
	if err != nil && err.Error() != "" {
		reason = err.Error()
	}
	return &calcapi.EvalResult{IsError: true, Reason: reason}
}

func (value evalValue) protoValue() *commonv1.Value {
	switch value.kind {
	case valueText:
		return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: value.text}}
	case valueNumber:
		if normalized, ok := normalizeInteger(value.number); ok {
			return &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: normalized}}
		}
		return &commonv1.Value{Kind: &commonv1.Value_DoubleValue{DoubleValue: value.number}}
	case valueBool:
		return &commonv1.Value{Kind: &commonv1.Value_BoolValue{BoolValue: value.boolean}}
	case valueDate:
		return &commonv1.Value{Kind: &commonv1.Value_DateValueMs{DateValueMs: value.dateMS}}
	case valueList:
		items := make([]*commonv1.Value, 0, len(value.list))
		for _, item := range value.list {
			items = append(items, item.protoValue())
		}
		return &commonv1.Value{Kind: &commonv1.Value_ListValue{
			ListValue: &commonv1.ValueList{ValueList: items},
		}}
	default:
		return &commonv1.Value{}
	}
}

func normalizeInteger(number float64) (int64, bool) {
	if math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) {
		return 0, false
	}
	if number < math.MinInt64 || number > math.MaxInt64 {
		return 0, false
	}
	return int64(number), true
}

type expression interface {
	eval(*evalContext) (evalValue, error)
}

type literalExpression struct {
	value evalValue
}

func (expression literalExpression) eval(*evalContext) (evalValue, error) {
	return expression.value, nil
}

type referenceExpression struct {
	name string
}

func (expression referenceExpression) eval(evaluationContext *evalContext) (evalValue, error) {
	expected, ok := evaluationContext.schema[expression.name]
	if !ok {
		return evalValue{}, failf("字段不存在: %s", expression.name)
	}
	raw, ok := evaluationContext.row[expression.name]
	if !ok || raw == nil {
		return evalValue{kind: valueEmpty}, nil
	}
	return coerceFieldValue(raw, expected, expression.name)
}

func coerceFieldValue(raw any, expected fieldType, name string) (evalValue, error) {
	if raw == nil {
		return evalValue{kind: valueEmpty}, nil
	}
	switch expected {
	case fieldTypeNumber:
		number, ok := numericValue(raw)
		if !ok {
			return evalValue{}, failf("字段 %s 的值不是数字", name)
		}
		return evalValue{kind: valueNumber, number: number}, nil
	case fieldTypeDate:
		number, ok := numericValue(raw)
		if !ok || number != math.Trunc(number) {
			return evalValue{}, failf("字段 %s 的值不是日期", name)
		}
		return evalValue{kind: valueDate, dateMS: int64(number)}, nil
	case fieldTypeText:
		switch value := raw.(type) {
		case string:
			return evalValue{kind: valueText, text: value}, nil
		case []any:
			items := make([]evalValue, 0, len(value))
			for _, item := range value {
				text, ok := item.(string)
				if !ok {
					return evalValue{}, failf("字段 %s 的值不是文本", name)
				}
				items = append(items, evalValue{kind: valueText, text: text})
			}
			return evalValue{kind: valueList, list: items}, nil
		default:
			return evalValue{}, failf("字段 %s 的值不是文本", name)
		}
	default:
		return inferValue(raw)
	}
}

func inferValue(raw any) (evalValue, error) {
	switch value := raw.(type) {
	case string:
		return evalValue{kind: valueText, text: value}, nil
	case bool:
		return evalValue{kind: valueBool, boolean: value}, nil
	case float64:
		return evalValue{kind: valueNumber, number: value}, nil
	case float32:
		return evalValue{kind: valueNumber, number: float64(value)}, nil
	case int:
		return evalValue{kind: valueNumber, number: float64(value)}, nil
	case int32:
		return evalValue{kind: valueNumber, number: float64(value)}, nil
	case int64:
		return evalValue{kind: valueNumber, number: float64(value)}, nil
	case []any:
		items := make([]evalValue, 0, len(value))
		for _, element := range value {
			converted, err := inferValue(element)
			if err != nil {
				return evalValue{}, err
			}
			items = append(items, converted)
		}
		return evalValue{kind: valueList, list: items}, nil
	default:
		return evalValue{}, failf("不支持的字段值类型 %T", raw)
	}
}

func numericValue(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, !math.IsNaN(value) && !math.IsInf(value, 0)
	case float32:
		converted := float64(value)
		return converted, !math.IsNaN(converted) && !math.IsInf(converted, 0)
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	default:
		return 0, false
	}
}

type unaryExpression struct {
	operator string
	operand  expression
}

func (expression unaryExpression) eval(evaluationContext *evalContext) (evalValue, error) {
	operand, err := expression.operand.eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	switch expression.operator {
	case "+":
		if operand.kind != valueNumber {
			return evalValue{}, failf("一元 + 的操作数必须为数字")
		}
		return operand, nil
	case "-":
		if operand.kind == valueEmpty {
			return evalValue{}, failf("算术运算遇空值")
		}
		if operand.kind != valueNumber {
			return evalValue{}, failf("一元 - 的操作数必须为数字")
		}
		operand.number = -operand.number
		return operand, nil
	default:
		return evalValue{}, failf("未知一元运算符 %s", expression.operator)
	}
}

type binaryExpression struct {
	operator    string
	left, right expression
}

func (expression binaryExpression) eval(evaluationContext *evalContext) (evalValue, error) {
	left, err := expression.left.eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	right, err := expression.right.eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	switch expression.operator {
	case "&":
		return concatValues([]evalValue{left, right}), nil
	case "+", "-", "*", "/":
		if expression.operator == "/" && right.kind == valueEmpty {
			return evalValue{}, failf("除数为空")
		}
		if right.kind == valueNumber && right.number == 0 && expression.operator == "/" {
			return evalValue{}, failf("除数为零")
		}
		if left.kind == valueEmpty || right.kind == valueEmpty {
			return evalValue{}, failf("算术运算遇空值")
		}
		if left.kind != valueNumber || right.kind != valueNumber {
			return evalValue{}, failf("算术运算的操作数必须为数字")
		}
		number := 0.0
		switch expression.operator {
		case "+":
			number = left.number + right.number
		case "-":
			number = left.number - right.number
		case "*":
			number = left.number * right.number
		case "/":
			number = left.number / right.number
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return evalValue{}, failf("数值运算结果无效")
		}
		return evalValue{kind: valueNumber, number: number}, nil
	case "=", "==", "!=", "<>", "<", "<=", ">", ">=":
		return compareValues(expression.operator, left, right)
	default:
		return evalValue{}, failf("未知运算符 %s", expression.operator)
	}
}

func concatValues(values []evalValue) evalValue {
	var builder strings.Builder
	for _, value := range values {
		switch value.kind {
		case valueEmpty:
		case valueText:
			builder.WriteString(value.text)
		case valueNumber:
			if integer, ok := normalizeInteger(value.number); ok {
				builder.WriteString(strconv.FormatInt(integer, 10))
			} else {
				builder.WriteString(strconv.FormatFloat(value.number, 'g', -1, 64))
			}
		case valueBool:
			builder.WriteString(strconv.FormatBool(value.boolean))
		case valueDate:
			builder.WriteString(strconv.FormatInt(value.dateMS, 10))
		}
	}
	return evalValue{kind: valueText, text: builder.String()}
}

func compareValues(operator string, left, right evalValue) (evalValue, error) {
	if left.kind == valueEmpty || right.kind == valueEmpty {
		if left.kind == valueEmpty && right.kind == valueEmpty {
			equal := operator == "=" || operator == "=="
			if operator == "!=" || operator == "<>" {
				equal = false
			}
			return evalValue{kind: valueBool, boolean: equal}, nil
		}
		switch operator {
		case "=", "==":
			return evalValue{kind: valueBool, boolean: false}, nil
		case "!=", "<>":
			return evalValue{kind: valueBool, boolean: true}, nil
		default:
			return evalValue{}, failf("比较运算遇空值")
		}
	}

	var order int
	switch {
	case left.kind == valueNumber && right.kind == valueNumber:
		order = compareOrdered(left.number, right.number)
	case left.kind == valueDate && right.kind == valueDate:
		order = compareOrdered(left.dateMS, right.dateMS)
	case left.kind == valueText && right.kind == valueText:
		order = strings.Compare(left.text, right.text)
	case left.kind == valueBool && right.kind == valueBool:
		if operator != "=" && operator != "==" && operator != "!=" && operator != "<>" {
			return evalValue{}, failf("布尔值不能进行大小比较")
		}
		order = 0
		if left.boolean != right.boolean {
			order = 1
		}
	default:
		return evalValue{}, failf("比较运算的操作数类型不符")
	}
	result := false
	switch operator {
	case "=", "==":
		result = order == 0
	case "!=", "<>":
		result = order != 0
	case "<":
		result = order < 0
	case "<=":
		result = order <= 0
	case ">":
		result = order > 0
	case ">=":
		result = order >= 0
	}
	return evalValue{kind: valueBool, boolean: result}, nil
}

type ordered interface {
	~int64 | ~float64 | ~string
}

func compareOrdered[T ordered](left, right T) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

type callExpression struct {
	name      string
	arguments []expression
}

func (expression callExpression) eval(evaluationContext *evalContext) (evalValue, error) {
	name := strings.ToUpper(expression.name)
	definition, ok := evaluationContext.functions[name]
	if !ok {
		return evalValue{}, failf("未知函数: %s", name)
	}
	if len(expression.arguments) < definition.minArgs ||
		(definition.maxArgs >= 0 && len(expression.arguments) > definition.maxArgs) {
		return evalValue{}, failf(
			"函数 %s 参数个数不符: 实际 %d 个",
			name,
			len(expression.arguments),
		)
	}
	return definition.evaluate(evaluationContext, expression.arguments)
}

type tokenKind uint8

const (
	tokenEOF tokenKind = iota
	tokenNumber
	tokenString
	tokenReference
	tokenIdentifier
	tokenOperator
	tokenLeftParen
	tokenRightParen
	tokenComma
)

type token struct {
	kind   tokenKind
	lexeme string
}

type formulaParser struct {
	tokens []token
	index  int
}

func parseFormula(source string) (expression, error) {
	tokens, err := lexFormula(source)
	if err != nil {
		return nil, err
	}
	parser := &formulaParser{tokens: tokens}
	expression, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if parser.current().kind != tokenEOF {
		return nil, fmt.Errorf("未预期的 token %q", parser.current().lexeme)
	}
	return expression, nil
}

func lexFormula(source string) ([]token, error) {
	tokens := make([]token, 0, 16)
	for index := 0; index < len(source); {
		current := rune(source[index])
		if unicode.IsSpace(current) {
			index++
			continue
		}
		switch current {
		case '(':
			tokens = append(tokens, token{kind: tokenLeftParen, lexeme: "("})
			index++
		case ')':
			tokens = append(tokens, token{kind: tokenRightParen, lexeme: ")"})
			index++
		case ',':
			tokens = append(tokens, token{kind: tokenComma, lexeme: ","})
			index++
		case '{':
			end := strings.IndexRune(source[index+1:], '}')
			if end < 0 {
				return nil, fmt.Errorf("字段引用缺少右花括号")
			}
			name := strings.TrimSpace(source[index+1 : index+1+end])
			if name == "" {
				return nil, fmt.Errorf("字段引用不能为空")
			}
			tokens = append(tokens, token{kind: tokenReference, lexeme: name})
			index += end + 2
		case '"', '\'':
			value, next, err := scanString(source, index, current)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token{kind: tokenString, lexeme: value})
			index = next
		default:
			if unicode.IsDigit(current) || (current == '.' && index+1 < len(source) &&
				unicode.IsDigit(rune(source[index+1]))) {
				start := index
				for index < len(source) && (unicode.IsDigit(rune(source[index])) || source[index] == '.') {
					index++
				}
				lexeme := source[start:index]
				if _, err := strconv.ParseFloat(lexeme, 64); err != nil {
					return nil, fmt.Errorf("数字字面量无效 %q", lexeme)
				}
				tokens = append(tokens, token{kind: tokenNumber, lexeme: lexeme})
				continue
			}
			if unicode.IsLetter(current) || current == '_' {
				start := index
				for index < len(source) {
					candidate := rune(source[index])
					if !unicode.IsLetter(candidate) && !unicode.IsDigit(candidate) &&
						candidate != '_' && candidate != '.' && candidate != ' ' {
						break
					}
					index++
				}
				lexeme := strings.TrimSpace(source[start:index])
				tokens = append(tokens, token{kind: tokenIdentifier, lexeme: lexeme})
				continue
			}
			operator, width, ok := scanOperator(source[index:])
			if !ok {
				return nil, fmt.Errorf("无法识别的字符 %q", string(current))
			}
			tokens = append(tokens, token{kind: tokenOperator, lexeme: operator})
			index += width
		}
	}
	tokens = append(tokens, token{kind: tokenEOF})
	return tokens, nil
}

func scanString(source string, start int, quote rune) (string, int, error) {
	var builder strings.Builder
	for index := start + int(quoteWidth(quote)); index < len(source); {
		current := rune(source[index])
		if current == quote {
			if index+1 < len(source) && rune(source[index+1]) == quote {
				builder.WriteRune(quote)
				index += 2
				continue
			}
			return builder.String(), index + 1, nil
		}
		if current == '\\' {
			if index+1 >= len(source) {
				return "", 0, fmt.Errorf("字符串转义不完整")
			}
			next := rune(source[index+1])
			switch next {
			case 'n':
				builder.WriteByte('\n')
			case 'r':
				builder.WriteByte('\r')
			case 't':
				builder.WriteByte('\t')
			default:
				builder.WriteRune(next)
			}
			index += 2
			continue
		}
		builder.WriteRune(current)
		index++
	}
	return "", 0, fmt.Errorf("字符串缺少结束引号")
}

func quoteWidth(quote rune) int {
	if quote == 0 {
		return 0
	}
	return len(string(quote))
}

func scanOperator(source string) (string, int, bool) {
	for _, operator := range []string{"<=", ">=", "<>", "!=", "==", "+", "-", "*", "/", "&", "=", "<", ">"} {
		if strings.HasPrefix(source, operator) {
			return operator, len(operator), true
		}
	}
	return "", 0, false
}

func (parser *formulaParser) parseExpression() (expression, error) {
	return parser.parseComparison()
}

func (parser *formulaParser) parseComparison() (expression, error) {
	left, err := parser.parseConcatenation()
	if err != nil {
		return nil, err
	}
	for parser.current().kind == tokenOperator && isComparisonOperator(parser.current().lexeme) {
		operator := parser.advance().lexeme
		right, err := parser.parseConcatenation()
		if err != nil {
			return nil, err
		}
		left = binaryExpression{operator: operator, left: left, right: right}
	}
	return left, nil
}

func (parser *formulaParser) parseConcatenation() (expression, error) {
	left, err := parser.parseAdditive()
	if err != nil {
		return nil, err
	}
	for parser.current().kind == tokenOperator && parser.current().lexeme == "&" {
		operator := parser.advance().lexeme
		right, err := parser.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = binaryExpression{operator: operator, left: left, right: right}
	}
	return left, nil
}

func (parser *formulaParser) parseAdditive() (expression, error) {
	left, err := parser.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for parser.current().kind == tokenOperator &&
		(parser.current().lexeme == "+" || parser.current().lexeme == "-") {
		operator := parser.advance().lexeme
		right, err := parser.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = binaryExpression{operator: operator, left: left, right: right}
	}
	return left, nil
}

func (parser *formulaParser) parseMultiplicative() (expression, error) {
	left, err := parser.parseUnary()
	if err != nil {
		return nil, err
	}
	for parser.current().kind == tokenOperator &&
		(parser.current().lexeme == "*" || parser.current().lexeme == "/") {
		operator := parser.advance().lexeme
		right, err := parser.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binaryExpression{operator: operator, left: left, right: right}
	}
	return left, nil
}

func (parser *formulaParser) parseUnary() (expression, error) {
	if parser.current().kind == tokenOperator &&
		(parser.current().lexeme == "+" || parser.current().lexeme == "-") {
		operator := parser.advance().lexeme
		operand, err := parser.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryExpression{operator: operator, operand: operand}, nil
	}
	return parser.parsePrimary()
}

func (parser *formulaParser) parsePrimary() (expression, error) {
	current := parser.advance()
	switch current.kind {
	case tokenNumber:
		number, _ := strconv.ParseFloat(current.lexeme, 64)
		return literalExpression{value: evalValue{kind: valueNumber, number: number}}, nil
	case tokenString:
		return literalExpression{value: evalValue{kind: valueText, text: current.lexeme}}, nil
	case tokenReference:
		return referenceExpression{name: current.lexeme}, nil
	case tokenIdentifier:
		upper := strings.ToUpper(current.lexeme)
		if parser.current().kind == tokenLeftParen {
			parser.advance()
			arguments := make([]expression, 0, 4)
			if parser.current().kind != tokenRightParen {
				for {
					argument, err := parser.parseExpression()
					if err != nil {
						return nil, err
					}
					arguments = append(arguments, argument)
					if parser.current().kind != tokenComma {
						break
					}
					parser.advance()
				}
			}
			if parser.current().kind != tokenRightParen {
				return nil, fmt.Errorf("函数 %s 缺少右括号", current.lexeme)
			}
			parser.advance()
			return callExpression{name: current.lexeme, arguments: arguments}, nil
		}
		if upper == "TRUE" || upper == "FALSE" {
			return literalExpression{value: evalValue{kind: valueBool, boolean: upper == "TRUE"}}, nil
		}
		return referenceExpression{name: current.lexeme}, nil
	case tokenLeftParen:
		inner, err := parser.parseExpression()
		if err != nil {
			return nil, err
		}
		if parser.current().kind != tokenRightParen {
			return nil, fmt.Errorf("表达式缺少右括号")
		}
		parser.advance()
		return inner, nil
	default:
		return nil, fmt.Errorf("未预期的 token %q", current.lexeme)
	}
}

func (parser *formulaParser) current() token {
	if parser.index >= len(parser.tokens) {
		return token{kind: tokenEOF}
	}
	return parser.tokens[parser.index]
}

func (parser *formulaParser) advance() token {
	current := parser.current()
	if parser.index < len(parser.tokens) {
		parser.index++
	}
	return current
}

func isComparisonOperator(operator string) bool {
	switch operator {
	case "=", "==", "!=", "<>", "<", "<=", ">", ">=":
		return true
	default:
		return false
	}
}
