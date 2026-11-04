import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Button, type ButtonVariant } from './Button'

describe('ui-kit/Button', () => {
  it.each(['primary', 'secondary', 'danger'] as const)(
    'variant=%s 渲染可见文案且可点击',
    (variant: ButtonVariant) => {
      const onClick = vi.fn()
      render(
        <Button variant={variant} onClick={onClick}>
          保存
        </Button>,
      )

      const button = screen.getByRole('button', { name: '保存' })
      expect(button).toBeEnabled()
      fireEvent.click(button)
      expect(onClick).toHaveBeenCalledTimes(1)
    },
  )

  it('缺省 variant 为 primary，可正常点击', () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>默认按钮</Button>)

    fireEvent.click(screen.getByRole('button', { name: '默认按钮' }))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('禁用态不可点击（onClick 不触发）且呈现禁用语义', () => {
    const onClick = vi.fn()
    render(
      <Button disabled onClick={onClick}>
        删除
      </Button>,
    )

    const button = screen.getByRole('button', { name: '删除' })
    expect(button).toBeDisabled()
    fireEvent.click(button)
    expect(onClick).not.toHaveBeenCalled()
  })

  it('className 透传可覆盖样式，原生属性（aria-*）透传', () => {
    render(
      <Button className="custom-button" aria-label="提交表单">
        提交
      </Button>,
    )

    const button = screen.getByRole('button', { name: '提交表单' })
    expect(button.className).toContain('custom-button')
  })
})
