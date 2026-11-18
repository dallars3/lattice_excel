// 选项维护组件级测试（票 04）：删除选项前的引用检测与确认交互。
// 只在组件公开接缝断言（props → 确认框出现/消失、选项行增删、保存出参），不触达内部状态。
import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { OptionsEditor } from './OptionsEditor'

const CONFIG_MAP = {
  option_list: [
    { id: 'opt_1', name: '进行中', color: 'blue' },
    { id: 'opt_2', name: '完成', color: 'green' },
  ],
  allow_create: false,
}

function renderOptionsEditor(props: {
  configMap?: Record<string, unknown>
  usedOptionIds?: ReadonlySet<string>
} = {}) {
  const onSave = vi.fn(async () => undefined)
  const onClose = vi.fn()
  render(
    <OptionsEditor
      configMap={props.configMap ?? CONFIG_MAP}
      usedOptionIds={props.usedOptionIds}
      onSave={onSave}
      onClose={onClose}
    />,
  )
  return { onSave, onClose }
}

describe('OptionsEditor（票 04 删除选项引用检测）', () => {
  it('删除被引用选项：先弹确认，取消不删，确认后才移除', () => {
    renderOptionsEditor({ usedOptionIds: new Set(['opt_1']) })

    fireEvent.click(screen.getByRole('button', { name: '删除选项：进行中' }))
    expect(
      screen.getByText('「进行中」已被记录使用，删除后对应记录值将清空。'),
    ).toBeInTheDocument()

    // 取消：选项保留
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.getByLabelText('选项名称：进行中')).toBeInTheDocument()
    expect(screen.queryByText(/已被记录使用/)).not.toBeInTheDocument()

    // 确认后才真正移除
    fireEvent.click(screen.getByRole('button', { name: '删除选项：进行中' }))
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
    expect(screen.queryByLabelText('选项名称：进行中')).not.toBeInTheDocument()
    expect(screen.getByLabelText('选项名称：完成')).toBeInTheDocument()
  })

  it('删除未被已加载记录引用的选项：不弹确认直接移除', () => {
    renderOptionsEditor({ usedOptionIds: new Set(['opt_1']) })

    fireEvent.click(screen.getByRole('button', { name: '删除选项：完成' }))

    expect(screen.queryByText(/已被记录使用/)).not.toBeInTheDocument()
    expect(screen.queryByLabelText('选项名称：完成')).not.toBeInTheDocument()
    expect(screen.getByLabelText('选项名称：进行中')).toBeInTheDocument()
  })

  it('未注入 usedOptionIds 时保留既有行为：已保存选项删除一律先确认', () => {
    renderOptionsEditor()

    fireEvent.click(screen.getByRole('button', { name: '删除选项：完成' }))
    expect(
      screen.getByText('「完成」已被记录使用，删除后对应记录值将清空。'),
    ).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.getByLabelText('选项名称：完成')).toBeInTheDocument()
  })

  it('未保存的新选项（无 id）删除不弹确认，既有行为不变', () => {
    renderOptionsEditor({ usedOptionIds: new Set<string>() })

    fireEvent.change(screen.getByLabelText('新选项名称'), { target: { value: '已归档' } })
    fireEvent.click(screen.getByRole('button', { name: '添加选项' }))
    fireEvent.click(screen.getByRole('button', { name: '删除选项：已归档' }))

    expect(screen.queryByText(/已被记录使用/)).not.toBeInTheDocument()
    expect(screen.queryByLabelText('选项名称：已归档')).not.toBeInTheDocument()
  })
})
