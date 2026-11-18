// 新增字段对话框组件级测试（票 07）：公式创建被后端 1001 拒绝时，
// 校验原因行内展示在对话框内（复用既有 role=alert 样式），不弹全局横幅。
import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import { createField } from '../../api/field'
import type { FieldRecord } from '../../api/field'
import { FieldCreateDialog } from './FieldCreateDialog'

const FIELDS: FieldRecord[] = [
  {
    id: 'fld_primary',
    tableId: 'tbl_1',
    name: '标题',
    type: 'text',
    configMap: {},
    isPrimary: true,
    position: 0,
    description: '',
  },
]

describe('FieldCreateDialog（票 07 公式校验行内展示）', () => {
  it('公式创建返回 1001 时行内展示服务端原因，不关闭对话框', async () => {
    let requestBody: Record<string, unknown> | null = null
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/fields',
        method: 'post',
        code: 1001,
        message: '未知函数: CONCAT',
        onRequest: async (request) => {
          requestBody = (await request.json()) as Record<string, unknown>
        },
      }),
    )
    const onCreate = vi.fn((input: Parameters<typeof createField>[1]) =>
      createField('tbl_1', input).then(() => undefined),
    )
    const onClose = vi.fn()
    render(
      <FieldCreateDialog
        fields={FIELDS}
        tables={[]}
        onCreate={onCreate}
        onClose={onClose}
      />,
    )

    fireEvent.change(screen.getByLabelText('字段名称'), { target: { value: '拼接' } })
    fireEvent.change(screen.getByLabelText('字段类型'), { target: { value: 'formula' } })
    fireEvent.change(screen.getByLabelText('公式表达式'), {
      target: { value: 'CONCAT({标题}, "-后缀")' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('未知函数: CONCAT')
    // 行内 = 渲染在对话框节点内部，而非全局横幅位置。
    expect(screen.getByRole('dialog')).toContainElement(alert)
    expect(onClose).not.toHaveBeenCalled()
    expect(onCreate).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(requestBody).not.toBeNull()
    })
    expect((requestBody as unknown as Record<string, unknown>).config_map).toEqual({
      expression: 'CONCAT({标题}, "-后缀")',
    })
  })
})
