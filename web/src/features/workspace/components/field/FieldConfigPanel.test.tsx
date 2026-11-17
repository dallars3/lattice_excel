// 字段配置面板组件级测试（票 07）：编辑公式配置被后端 1001 拒绝时，
// 校验原因行内展示在对话框内，配置不改写、对话框不关闭。
import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import { updateFieldConfig } from '../../api/field'
import type { FieldRecord } from '../../api/field'
import { FieldConfigPanel } from './FieldConfigPanel'

const FORMULA_FIELD: FieldRecord = {
  id: 'fld_formula',
  tableId: 'tbl_1',
  name: '倍数',
  type: 'formula',
  configMap: { expression: '{数量} * 2' },
  isPrimary: false,
  position: 1,
  description: '',
}

describe('FieldConfigPanel（票 07 公式校验行内展示）', () => {
  it('公式配置保存返回 1001 时行内展示服务端原因，不关闭对话框', async () => {
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/config',
        method: 'put',
        code: 1001,
        message: '公式 tbl_1.fld_formula 引用不存在字段 "库存"',
      }),
    )
    const onSaveConfig = vi.fn((configMap: Record<string, unknown>) =>
      updateFieldConfig(FORMULA_FIELD.id, configMap),
    )
    const onSaveOptions = vi.fn()
    const onClose = vi.fn()
    render(
      <FieldConfigPanel
        field={FORMULA_FIELD}
        tables={[]}
        currentTableFields={[FORMULA_FIELD]}
        onSaveConfig={onSaveConfig}
        onSaveOptions={onSaveOptions}
        onClose={onClose}
      />,
    )

    fireEvent.change(screen.getByLabelText('公式表达式'), {
      target: { value: '{库存} * 2' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('公式 tbl_1.fld_formula 引用不存在字段 "库存"')
    expect(screen.getByRole('dialog')).toContainElement(alert)
    expect(onClose).not.toHaveBeenCalled()
    expect(onSaveOptions).not.toHaveBeenCalled()
  })
})
