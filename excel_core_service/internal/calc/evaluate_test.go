package calc

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	calcapi "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestEvaluateSupportsAllP0FunctionsAndConcatenation(t *testing.T) {
	fixedNow := time.Date(2026, time.November, 2, 9, 10, 11, 120_000_000, time.FixedZone("Asia/Shanghai", 8*60*60))
	dateStart := time.Date(2024, time.January, 2, 0, 0, 0, 0, fixedNow.Location()).UnixMilli()
	dateEnd := time.Date(2024, time.March, 5, 0, 0, 0, 0, fixedNow.Location()).UnixMilli()
	service := NewService(nil, nil, nil, nil, func() time.Time { return fixedNow })
	schema, row := evaluationFixture(dateStart, dateEnd)

	cases := []struct {
		name       string
		expression string
		assert     func(*testing.T, *calcapi.EvalResult)
	}{
		{name: "IF", expression: `IF({N}>5,"yes","no")`, assert: wantString("yes")},
		{name: "AND", expression: `AND({N}>5,{N}<20)`, assert: wantBool(true)},
		{name: "OR", expression: `OR({N}<5,{N}>5)`, assert: wantBool(true)},
		{name: "NOT", expression: `NOT({N}=10)`, assert: wantBool(false)},
		{name: "ISBLANK", expression: `ISBLANK({Blank})`, assert: wantBool(true)},
		{name: "SUM", expression: `SUM(N,Blank,123)`, assert: wantNumber(133)},
		{name: "AVERAGE", expression: `AVERAGE({N},{Blank},8)`, assert: wantNumber(9)},
		{name: "MAX", expression: `MAX({N},{Blank},8)`, assert: wantNumber(10)},
		{name: "MIN", expression: `MIN({N},{Blank},8)`, assert: wantNumber(8)},
		{name: "ABS", expression: `ABS(-7)`, assert: wantNumber(7)},
		{name: "ROUND", expression: `ROUND(12.345,2)`, assert: wantNumber(12.35)},
		{name: "INT", expression: `INT(-1.2)`, assert: wantNumber(-2)},
		{name: "MOD", expression: `MOD(10,3)`, assert: wantNumber(1)},
		{name: "CONCATENATE", expression: `CONCATENATE("Hello"," ",{Text})`, assert: wantString("Hello Hello World")},
		{name: "ampersand", expression: `"ID-"&{N}`, assert: wantString("ID-10")},
		{name: "LEFT", expression: `LEFT({Text},5)`, assert: wantString("Hello")},
		{name: "RIGHT", expression: `RIGHT({Text},5)`, assert: wantString("World")},
		{name: "MID", expression: `MID({Text},7,5)`, assert: wantString("World")},
		{name: "LEN", expression: `LEN({Text})`, assert: wantNumber(11)},
		{name: "FIND", expression: `FIND("World",{Text})`, assert: wantNumber(7)},
		{name: "SUBSTITUTE", expression: `SUBSTITUTE({Text},"World","Calc")`, assert: wantString("Hello Calc")},
		{name: "TODAY", expression: `TODAY()`, assert: wantDate(time.Date(2026, time.November, 2, 0, 0, 0, 0, fixedNow.Location()).UnixMilli())},
		{name: "NOW", expression: `NOW()`, assert: wantDate(fixedNow.UnixMilli())},
		{name: "YEAR", expression: `YEAR({Date})`, assert: wantNumber(2024)},
		{name: "MONTH", expression: `MONTH({Date})`, assert: wantNumber(1)},
		{name: "DAY", expression: `DAY({Date})`, assert: wantNumber(2)},
		{name: "DATEDIF", expression: `DATEDIF({DateStart},{DateEnd},"D")`, assert: wantNumber(63)},
	}

	expressions := make([]string, 0, len(cases))
	for _, testCase := range cases {
		expressions = append(expressions, testCase.expression)
	}
	response, err := service.Evaluate(context.Background(), &calcapi.EvaluateRequest{
		ExpressionList: expressions,
		SchemaMap:      schema,
		RowContextMap:  row,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(response.GetResultList()) != len(cases) {
		t.Fatalf("result count = %d, want %d", len(response.GetResultList()), len(cases))
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := response.GetResultList()[index]
			if result.GetIsError() {
				t.Fatalf("Evaluate(%q) = #ERROR: %s", testCase.expression, result.GetReason())
			}
			testCase.assert(t, result)
		})
	}
}

func TestEvaluateReturnsReasonedErrorsForEmptyArithmeticAndBadArguments(t *testing.T) {
	schema, row := evaluationFixture(0, 0)
	service := NewService(nil, nil, nil, nil, time.Now)
	tests := []struct {
		name       string
		expression string
		reason     string
	}{
		{name: "divide by empty", expression: `10/{Blank}`, reason: "除数为空"},
		{name: "divide by zero", expression: `10/0`, reason: "除数为零"},
		{name: "empty arithmetic", expression: `{Blank}+1`, reason: "算术运算遇空值"},
		{name: "wrong argument type", expression: `SUM({N},"not-a-number")`, reason: "参数类型不符"},
		{name: "wrong argument count", expression: `NOT(TRUE,FALSE)`, reason: "参数个数不符"},
		{name: "unknown function", expression: `UNKNOWN(1)`, reason: "未知函数"},
		{name: "unknown field", expression: `{Missing}+1`, reason: "字段不存在"},
	}
	expressions := make([]string, 0, len(tests))
	for _, testCase := range tests {
		expressions = append(expressions, testCase.expression)
	}
	response, err := service.Evaluate(context.Background(), &calcapi.EvaluateRequest{
		ExpressionList: expressions,
		SchemaMap:      schema,
		RowContextMap:  row,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result := response.GetResultList()[index]
			if !result.GetIsError() {
				t.Fatalf("Evaluate(%q) = %v, want #ERROR", testCase.expression, result.GetValue())
			}
			if !strings.Contains(result.GetReason(), testCase.reason) {
				t.Fatalf("Evaluate(%q) reason = %q, want containing %q", testCase.expression, result.GetReason(), testCase.reason)
			}
		})
	}
}

func TestEvaluateFunctionNamesAreCaseInsensitive(t *testing.T) {
	schema, row := evaluationFixture(0, 0)
	service := NewService(nil, nil, nil, nil, time.Now)
	response, err := service.Evaluate(context.Background(), &calcapi.EvaluateRequest{
		ExpressionList: []string{`sum({N},{Blank},5)`, `left({Text},5)`},
		SchemaMap:      schema,
		RowContextMap:  row,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	wantNumber(15)(t, response.GetResultList()[0])
	wantString("Hello")(t, response.GetResultList()[1])
}

func TestAllP0FunctionsRejectWrongArgumentCount(t *testing.T) {
	schema, row := evaluationFixture(0, 0)
	service := NewService(nil, nil, nil, nil, time.Now)
	tests := map[string]string{
		"IF":          `IF(TRUE,1)`,
		"AND":         `AND()`,
		"OR":          `OR()`,
		"NOT":         `NOT(TRUE,FALSE)`,
		"ISBLANK":     `ISBLANK()`,
		"SUM":         `SUM()`,
		"AVERAGE":     `AVERAGE()`,
		"MAX":         `MAX()`,
		"MIN":         `MIN()`,
		"ABS":         `ABS()`,
		"ROUND":       `ROUND()`,
		"INT":         `INT()`,
		"MOD":         `MOD(1)`,
		"CONCATENATE": `CONCATENATE()`,
		"LEFT":        `LEFT("x")`,
		"RIGHT":       `RIGHT("x")`,
		"MID":         `MID("x",1)`,
		"LEN":         `LEN()`,
		"FIND":        `FIND("x")`,
		"SUBSTITUTE":  `SUBSTITUTE("x","y")`,
		"TODAY":       `TODAY(1)`,
		"NOW":         `NOW(1)`,
		"YEAR":        `YEAR()`,
		"MONTH":       `MONTH()`,
		"DAY":         `DAY()`,
		"DATEDIF":     `DATEDIF({Date},{Date})`,
	}
	expressions := make([]string, 0, len(tests))
	names := make([]string, 0, len(tests))
	for name, formula := range tests {
		names = append(names, name)
		expressions = append(expressions, formula)
	}
	response, err := service.Evaluate(context.Background(), &calcapi.EvaluateRequest{
		ExpressionList: expressions,
		SchemaMap:      schema,
		RowContextMap:  row,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	for index, name := range names {
		result := response.GetResultList()[index]
		if !result.GetIsError() || !strings.Contains(result.GetReason(), "参数个数不符") {
			t.Fatalf("%s wrong-arity result = %+v, want #ERROR with 参数个数不符", name, result)
		}
	}
}

func TestTypedP0FunctionsRejectWrongArgumentTypes(t *testing.T) {
	schema, row := evaluationFixture(0, 0)
	service := NewService(nil, nil, nil, nil, time.Now)
	tests := map[string]string{
		"IF":         `IF(1,2,3)`,
		"AND":        `AND(TRUE,1)`,
		"OR":         `OR(FALSE,1)`,
		"NOT":        `NOT(1)`,
		"SUM":        `SUM("x")`,
		"AVERAGE":    `AVERAGE("x")`,
		"MAX":        `MAX("x")`,
		"MIN":        `MIN("x")`,
		"ABS":        `ABS("x")`,
		"ROUND":      `ROUND("x",2)`,
		"INT":        `INT("x")`,
		"MOD":        `MOD(10,"x")`,
		"LEFT":       `LEFT(1,1)`,
		"RIGHT":      `RIGHT(1,1)`,
		"MID":        `MID(1,1,1)`,
		"LEN":        `LEN(1)`,
		"FIND":       `FIND(1,"x")`,
		"SUBSTITUTE": `SUBSTITUTE("x",1,"y")`,
		"YEAR":       `YEAR(1)`,
		"MONTH":      `MONTH(1)`,
		"DAY":        `DAY(1)`,
		"DATEDIF":    `DATEDIF({Date},{Date},1)`,
	}
	expressions := make([]string, 0, len(tests))
	names := make([]string, 0, len(tests))
	for name, formula := range tests {
		names = append(names, name)
		expressions = append(expressions, formula)
	}
	response, err := service.Evaluate(context.Background(), &calcapi.EvaluateRequest{
		ExpressionList: expressions,
		SchemaMap:      schema,
		RowContextMap:  row,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	for index, name := range names {
		result := response.GetResultList()[index]
		if !result.GetIsError() || !strings.Contains(result.GetReason(), "参数类型不符") {
			t.Fatalf("%s wrong-type result = %+v, want #ERROR with 参数类型不符", name, result)
		}
	}
}

func evaluationFixture(dateStart, dateEnd int64) (*structpb.Struct, *structpb.Struct) {
	schema, _ := structpb.NewStruct(map[string]any{
		"Text":      "FIELD_TYPE_TEXT",
		"N":         "FIELD_TYPE_NUMBER",
		"Blank":     "FIELD_TYPE_NUMBER",
		"Date":      "FIELD_TYPE_DATE",
		"DateStart": "FIELD_TYPE_DATE",
		"DateEnd":   "FIELD_TYPE_DATE",
	})
	row, _ := structpb.NewStruct(map[string]any{
		"Text":      "Hello World",
		"N":         10,
		"Blank":     nil,
		"Date":      dateStart,
		"DateStart": dateStart,
		"DateEnd":   dateEnd,
	})
	return schema, row
}

func wantString(want string) func(*testing.T, *calcapi.EvalResult) {
	return func(t *testing.T, result *calcapi.EvalResult) {
		t.Helper()
		if got := result.GetValue().GetStringValue(); got != want {
			t.Fatalf("string value = %q, want %q", got, want)
		}
	}
}

func wantBool(want bool) func(*testing.T, *calcapi.EvalResult) {
	return func(t *testing.T, result *calcapi.EvalResult) {
		t.Helper()
		if got := result.GetValue().GetBoolValue(); got != want {
			t.Fatalf("bool value = %t, want %t", got, want)
		}
	}
}

func wantNumber(want float64) func(*testing.T, *calcapi.EvalResult) {
	return func(t *testing.T, result *calcapi.EvalResult) {
		t.Helper()
		got := scalarNumber(result.GetValue())
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("number value = %v, want %v", got, want)
		}
	}
}

func wantDate(want int64) func(*testing.T, *calcapi.EvalResult) {
	return func(t *testing.T, result *calcapi.EvalResult) {
		t.Helper()
		if got := result.GetValue().GetDateValueMs(); got != want {
			t.Fatalf("date value = %d, want %d", got, want)
		}
	}
}

func scalarNumber(value *commonv1.Value) float64 {
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_IntValue:
		return float64(kind.IntValue)
	case *commonv1.Value_DoubleValue:
		return kind.DoubleValue
	default:
		return math.NaN()
	}
}
