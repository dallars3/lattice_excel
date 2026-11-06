// records/formula_value_provider.go：一文件一接口。公式值读接口缝适配。
package records

import (
	"context"

	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
)

// NewCalcFormulaValueProvider adapts CalcRepository materialized values to the
// FormulaValueProvider seam. H-48/H-49 的公式格以 excel_calc_materialized 为
// 权威投影（同行写事务物化 + calc.job 跨行重算同表更新）；缺失物化返回
// ok=false，读取渲染为空格。
func NewCalcFormulaValueProvider(
	repository *base.CalcRepository,
) FormulaValueProvider {
	return calcFormulaValueProvider{repository: repository}
}

type calcFormulaValueProvider struct {
	repository *base.CalcRepository
}

func (provider calcFormulaValueProvider) Value(
	ctx context.Context,
	recordID, fieldID string,
) (*commonv1.Value, bool, error) {
	return provider.repository.Value(ctx, recordID, fieldID)
}
