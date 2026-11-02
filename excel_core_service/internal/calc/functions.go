// calc/functions.go：表驱动的 26 个 P0 公式函数与 & 文本拼接语义。
package calc

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type functionDefinition struct {
	minArgs  int
	maxArgs  int
	evaluate func(*evalContext, []expression) (evalValue, error)
}

var functionRegistry = map[string]functionDefinition{
	"IF":          {minArgs: 3, maxArgs: 3, evaluate: evaluateIf},
	"AND":         {minArgs: 1, maxArgs: -1, evaluate: evaluateAnd},
	"OR":          {minArgs: 1, maxArgs: -1, evaluate: evaluateOr},
	"NOT":         {minArgs: 1, maxArgs: 1, evaluate: evaluateNot},
	"ISBLANK":     {minArgs: 1, maxArgs: 1, evaluate: evaluateIsBlank},
	"SUM":         {minArgs: 1, maxArgs: -1, evaluate: aggregateFunction("SUM", aggregateSum)},
	"AVERAGE":     {minArgs: 1, maxArgs: -1, evaluate: aggregateFunction("AVERAGE", aggregateAverage)},
	"MAX":         {minArgs: 1, maxArgs: -1, evaluate: aggregateFunction("MAX", aggregateMax)},
	"MIN":         {minArgs: 1, maxArgs: -1, evaluate: aggregateFunction("MIN", aggregateMin)},
	"ABS":         {minArgs: 1, maxArgs: 1, evaluate: evaluateAbs},
	"ROUND":       {minArgs: 1, maxArgs: 2, evaluate: evaluateRound},
	"INT":         {minArgs: 1, maxArgs: 1, evaluate: evaluateInt},
	"MOD":         {minArgs: 2, maxArgs: 2, evaluate: evaluateMod},
	"CONCATENATE": {minArgs: 1, maxArgs: -1, evaluate: evaluateConcatenate},
	"LEFT":        {minArgs: 2, maxArgs: 2, evaluate: evaluateLeft},
	"RIGHT":       {minArgs: 2, maxArgs: 2, evaluate: evaluateRight},
	"MID":         {minArgs: 3, maxArgs: 3, evaluate: evaluateMid},
	"LEN":         {minArgs: 1, maxArgs: 1, evaluate: evaluateLen},
	"FIND":        {minArgs: 2, maxArgs: 3, evaluate: evaluateFind},
	"SUBSTITUTE":  {minArgs: 3, maxArgs: 4, evaluate: evaluateSubstitute},
	"TODAY":       {minArgs: 0, maxArgs: 0, evaluate: evaluateToday},
	"NOW":         {minArgs: 0, maxArgs: 0, evaluate: evaluateNow},
	"YEAR":        {minArgs: 1, maxArgs: 1, evaluate: evaluateYear},
	"MONTH":       {minArgs: 1, maxArgs: 1, evaluate: evaluateMonth},
	"DAY":         {minArgs: 1, maxArgs: 1, evaluate: evaluateDay},
	"DATEDIF":     {minArgs: 3, maxArgs: 3, evaluate: evaluateDateDif},
}

func evaluateIf(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	condition, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	boolean, err := requireBool("IF", 1, condition)
	if err != nil {
		return evalValue{}, err
	}
	if boolean {
		return arguments[1].eval(evaluationContext)
	}
	return arguments[2].eval(evaluationContext)
}

func evaluateAnd(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	result := true
	for index, argument := range arguments {
		value, err := argument.eval(evaluationContext)
		if err != nil {
			return evalValue{}, err
		}
		boolean, err := requireBool("AND", index+1, value)
		if err != nil {
			return evalValue{}, err
		}
		if !boolean {
			result = false
		}
	}
	return evalValue{kind: valueBool, boolean: result}, nil
}

func evaluateOr(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	result := false
	for index, argument := range arguments {
		value, err := argument.eval(evaluationContext)
		if err != nil {
			return evalValue{}, err
		}
		boolean, err := requireBool("OR", index+1, value)
		if err != nil {
			return evalValue{}, err
		}
		if boolean {
			result = true
		}
	}
	return evalValue{kind: valueBool, boolean: result}, nil
}

func evaluateNot(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	value, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	boolean, err := requireBool("NOT", 1, value)
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueBool, boolean: !boolean}, nil
}

func evaluateIsBlank(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	value, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueBool, boolean: value.kind == valueEmpty}, nil
}

type aggregateOperation func([]float64) (float64, error)

func aggregateFunction(name string, operation aggregateOperation) func(*evalContext, []expression) (evalValue, error) {
	return func(
		evaluationContext *evalContext,
		arguments []expression,
	) (evalValue, error) {
		numbers := make([]float64, 0, len(arguments))
		for index, argument := range arguments {
			value, err := argument.eval(evaluationContext)
			if err != nil {
				return evalValue{}, err
			}
			if value.kind == valueEmpty {
				continue
			}
			number, err := requireNumber(name, index+1, value)
			if err != nil {
				return evalValue{}, err
			}
			numbers = append(numbers, number)
		}
		if len(numbers) == 0 {
			return evalValue{}, failf("函数 %s 无可用数值", name)
		}
		number, err := operation(numbers)
		if err != nil {
			return evalValue{}, err
		}
		return evalValue{kind: valueNumber, number: number}, nil
	}
}

func aggregateSum(numbers []float64) (float64, error) {
	total := 0.0
	for _, number := range numbers {
		total += number
	}
	return finiteNumber(total)
}

func aggregateAverage(numbers []float64) (float64, error) {
	total, err := aggregateSum(numbers)
	if err != nil {
		return 0, err
	}
	return finiteNumber(total / float64(len(numbers)))
}

func aggregateMax(numbers []float64) (float64, error) {
	maximum := numbers[0]
	for _, number := range numbers[1:] {
		if number > maximum {
			maximum = number
		}
	}
	return finiteNumber(maximum)
}

func aggregateMin(numbers []float64) (float64, error) {
	minimum := numbers[0]
	for _, number := range numbers[1:] {
		if number < minimum {
			minimum = number
		}
	}
	return finiteNumber(minimum)
}

func evaluateAbs(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	value, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	number, err := requireNumber("ABS", 1, value)
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueNumber, number: math.Abs(number)}, nil
}

func evaluateRound(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	value, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	number, err := requireNumber("ROUND", 1, value)
	if err != nil {
		return evalValue{}, err
	}
	digits := int64(0)
	if len(arguments) == 2 {
		precision, err := arguments[1].eval(evaluationContext)
		if err != nil {
			return evalValue{}, err
		}
		digits, err = requireInteger("ROUND", 2, precision)
		if err != nil {
			return evalValue{}, err
		}
	}
	if digits > 308 || digits < -308 {
		return evalValue{}, failf("函数 ROUND 小数位超出范围")
	}
	factor := math.Pow(10, math.Abs(float64(digits)))
	rounded := math.Round(number*factor) / factor
	if digits < 0 {
		rounded = math.Round(number/factor) * factor
	}
	rounded, err = finiteNumber(rounded)
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueNumber, number: rounded}, nil
}

func evaluateInt(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	value, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	number, err := requireNumber("INT", 1, value)
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueNumber, number: math.Floor(number)}, nil
}

func evaluateMod(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	dividend, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	divisor, err := arguments[1].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	if divisor.kind == valueEmpty {
		return evalValue{}, failf("除数为空")
	}
	divisorNumber, err := requireNumber("MOD", 2, divisor)
	if err != nil {
		return evalValue{}, err
	}
	if divisorNumber == 0 {
		return evalValue{}, failf("除数为零")
	}
	dividendNumber, err := requireNumber("MOD", 1, dividend)
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueNumber, number: math.Mod(dividendNumber, divisorNumber)}, nil
}

func evaluateConcatenate(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	values := make([]evalValue, 0, len(arguments))
	for _, argument := range arguments {
		value, err := argument.eval(evaluationContext)
		if err != nil {
			return evalValue{}, err
		}
		values = append(values, value)
	}
	return concatValues(values), nil
}

func evaluateLeft(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	text, length, err := textAndLength("LEFT", evaluationContext, arguments)
	if err != nil {
		return evalValue{}, err
	}
	runes := []rune(text)
	if length >= int64(len(runes)) {
		return evalValue{kind: valueText, text: text}, nil
	}
	return evalValue{kind: valueText, text: string(runes[:length])}, nil
}

func evaluateRight(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	text, length, err := textAndLength("RIGHT", evaluationContext, arguments)
	if err != nil {
		return evalValue{}, err
	}
	runes := []rune(text)
	if length >= int64(len(runes)) {
		return evalValue{kind: valueText, text: text}, nil
	}
	return evalValue{kind: valueText, text: string(runes[len(runes)-int(length):])}, nil
}

func textAndLength(
	name string,
	evaluationContext *evalContext,
	arguments []expression,
) (string, int64, error) {
	textValue, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return "", 0, err
	}
	text, err := requireText(name, 1, textValue)
	if err != nil {
		return "", 0, err
	}
	lengthValue, err := arguments[1].eval(evaluationContext)
	if err != nil {
		return "", 0, err
	}
	length, err := requireInteger(name, 2, lengthValue)
	if err != nil {
		return "", 0, err
	}
	if length < 0 {
		return "", 0, failf("函数 %s 个数不能为负数", name)
	}
	return text, length, nil
}

func evaluateMid(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	textValue, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	text, err := requireText("MID", 1, textValue)
	if err != nil {
		return evalValue{}, err
	}
	startValue, err := arguments[1].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	start, err := requireInteger("MID", 2, startValue)
	if err != nil {
		return evalValue{}, err
	}
	lengthValue, err := arguments[2].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	length, err := requireInteger("MID", 3, lengthValue)
	if err != nil {
		return evalValue{}, err
	}
	if start < 1 || length < 0 {
		return evalValue{}, failf("函数 MID 的起始位置必须≥1且长度不能为负数")
	}
	runes := []rune(text)
	startIndex := int(start - 1)
	if startIndex >= len(runes) || length == 0 {
		return evalValue{kind: valueText, text: ""}, nil
	}
	end := startIndex + int(length)
	if end > len(runes) {
		end = len(runes)
	}
	return evalValue{kind: valueText, text: string(runes[startIndex:end])}, nil
}

func evaluateLen(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	value, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	text, err := requireText("LEN", 1, value)
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueNumber, number: float64(utf8.RuneCountInString(text))}, nil
}

func evaluateFind(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	needleValue, err := arguments[0].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	needle, err := requireText("FIND", 1, needleValue)
	if err != nil {
		return evalValue{}, err
	}
	haystackValue, err := arguments[1].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	haystack, err := requireText("FIND", 2, haystackValue)
	if err != nil {
		return evalValue{}, err
	}
	start := int64(1)
	if len(arguments) == 3 {
		startValue, err := arguments[2].eval(evaluationContext)
		if err != nil {
			return evalValue{}, err
		}
		start, err = requireInteger("FIND", 3, startValue)
		if err != nil {
			return evalValue{}, err
		}
	}
	haystackRunes := []rune(haystack)
	if start < 1 || start > int64(len(haystackRunes)+1) {
		return evalValue{}, failf("函数 FIND 的起始位置超出范围")
	}
	index := strings.Index(string(haystackRunes[start-1:]), needle)
	if index < 0 {
		return evalValue{}, failf("函数 FIND 未找到文本")
	}
	return evalValue{
		kind:   valueNumber,
		number: float64(start + int64(utf8.RuneCountInString(string(haystackRunes[start-1:])[:index]))),
	}, nil
}

func evaluateSubstitute(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	values := make([]string, 3)
	for index := 0; index < 3; index++ {
		value, err := arguments[index].eval(evaluationContext)
		if err != nil {
			return evalValue{}, err
		}
		values[index], err = requireText("SUBSTITUTE", index+1, value)
		if err != nil {
			return evalValue{}, err
		}
	}
	if values[1] == "" {
		return evalValue{}, failf("函数 SUBSTITUTE 的被替换文本不能为空")
	}
	if len(arguments) == 3 {
		return evalValue{kind: valueText, text: strings.ReplaceAll(values[0], values[1], values[2])}, nil
	}
	instanceValue, err := arguments[3].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	instance, err := requireInteger("SUBSTITUTE", 4, instanceValue)
	if err != nil {
		return evalValue{}, err
	}
	if instance < 1 {
		return evalValue{}, failf("函数 SUBSTITUTE 的替换序号必须≥1")
	}
	parts := strings.Split(values[0], values[1])
	if instance >= int64(len(parts)) {
		return evalValue{kind: valueText, text: values[0]}, nil
	}
	parts[instance] = values[2]
	return evalValue{kind: valueText, text: strings.Join(parts, values[1])}, nil
}

func evaluateToday(
	evaluationContext *evalContext,
	_ []expression,
) (evalValue, error) {
	if evaluationContext.now == nil || evaluationContext.location == nil {
		return evalValue{}, failf("TODAY 时钟未配置")
	}
	now := evaluationContext.now().In(evaluationContext.location)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, evaluationContext.location)
	return evalValue{kind: valueDate, dateMS: midnight.UnixMilli()}, nil
}

func evaluateNow(
	evaluationContext *evalContext,
	_ []expression,
) (evalValue, error) {
	if evaluationContext.now == nil || evaluationContext.location == nil {
		return evalValue{}, failf("NOW 时钟未配置")
	}
	return evalValue{
		kind:   valueDate,
		dateMS: evaluationContext.now().In(evaluationContext.location).UnixMilli(),
	}, nil
}

func evaluateYear(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	date, err := dateArgument("YEAR", evaluationContext, arguments[0])
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueNumber, number: float64(date.Year())}, nil
}

func evaluateMonth(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	date, err := dateArgument("MONTH", evaluationContext, arguments[0])
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueNumber, number: float64(date.Month())}, nil
}

func evaluateDay(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	date, err := dateArgument("DAY", evaluationContext, arguments[0])
	if err != nil {
		return evalValue{}, err
	}
	return evalValue{kind: valueNumber, number: float64(date.Day())}, nil
}

func dateArgument(name string, evaluationContext *evalContext, argument expression) (time.Time, error) {
	value, err := argument.eval(evaluationContext)
	if err != nil {
		return time.Time{}, err
	}
	if value.kind == valueEmpty {
		return time.Time{}, failf("函数 %s 的日期参数为空", name)
	}
	if value.kind != valueDate {
		return time.Time{}, failf("函数 %s 参数类型不符: 应为日期", name)
	}
	return time.UnixMilli(value.dateMS).In(evaluationContext.location), nil
}

func evaluateDateDif(
	evaluationContext *evalContext,
	arguments []expression,
) (evalValue, error) {
	start, err := dateArgument("DATEDIF", evaluationContext, arguments[0])
	if err != nil {
		return evalValue{}, err
	}
	end, err := dateArgument("DATEDIF", evaluationContext, arguments[1])
	if err != nil {
		return evalValue{}, err
	}
	unitValue, err := arguments[2].eval(evaluationContext)
	if err != nil {
		return evalValue{}, err
	}
	unit, err := requireText("DATEDIF", 3, unitValue)
	if err != nil {
		return evalValue{}, err
	}
	if start.After(end) {
		return evalValue{}, failf("函数 DATEDIF 的开始日期晚于结束日期")
	}
	unit = strings.ToUpper(unit)
	months := wholeMonths(start, end)
	days := civilDays(start, end)
	var result int64
	switch unit {
	case "D":
		result = days
	case "M":
		result = months
	case "Y":
		result = months / 12
	case "YM":
		result = months % 12
	case "MD":
		result = days - civilDays(start, addMonthsClamped(start, months))
	case "YD":
		result = days - civilDays(start, addMonthsClamped(start, (months/12)*12))
	default:
		return evalValue{}, failf("函数 DATEDIF 单位无效: %s", unit)
	}
	return evalValue{kind: valueNumber, number: float64(result)}, nil
}

func wholeMonths(start, end time.Time) int64 {
	months := int64(end.Year()-start.Year())*12 + int64(end.Month()-start.Month())
	if end.Day() < start.Day() {
		months--
	}
	return months
}

func civilDays(start, end time.Time) int64 {
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	return int64(endDate.Sub(startDate) / (24 * time.Hour))
}

func addMonthsClamped(date time.Time, months int64) time.Time {
	year, month, day := date.Date()
	targetMonth := time.Month(int(month) + int(months))
	targetYear := year + (int(targetMonth)-1)/12
	targetMonth = time.Month((int(targetMonth)-1)%12 + 1)
	lastDay := time.Date(targetYear, targetMonth+1, 0, 0, 0, 0, 0, date.Location()).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(targetYear, targetMonth, day, 0, 0, 0, 0, date.Location())
}

func requireNumber(name string, index int, value evalValue) (float64, error) {
	if value.kind == valueEmpty {
		return 0, failf("函数 %s 参数 %d 为空", name, index)
	}
	if value.kind != valueNumber {
		return 0, failf("函数 %s 参数类型不符: 第 %d 个参数应为数字", name, index)
	}
	return value.number, nil
}

func requireInteger(name string, index int, value evalValue) (int64, error) {
	number, err := requireNumber(name, index, value)
	if err != nil {
		return 0, err
	}
	integer, ok := normalizeInteger(number)
	if !ok {
		return 0, failf("函数 %s 参数类型不符: 第 %d 个参数应为整数", name, index)
	}
	return integer, nil
}

func requireText(name string, index int, value evalValue) (string, error) {
	if value.kind == valueEmpty {
		return "", nil
	}
	if value.kind != valueText {
		return "", failf("函数 %s 参数类型不符: 第 %d 个参数应为文本", name, index)
	}
	return value.text, nil
}

func requireBool(name string, index int, value evalValue) (bool, error) {
	if value.kind == valueEmpty {
		return false, failf("函数 %s 参数 %d 为空", name, index)
	}
	if value.kind != valueBool {
		return false, failf("函数 %s 参数类型不符: 第 %d 个参数应为布尔值", name, index)
	}
	return value.boolean, nil
}

func finiteNumber(number float64) (float64, error) {
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, failf("数值运算结果无效")
	}
	return number, nil
}

func formatNumber(number float64) string {
	if integer, ok := normalizeInteger(number); ok {
		return strconv.FormatInt(integer, 10)
	}
	return strconv.FormatFloat(number, 'g', -1, 64)
}
