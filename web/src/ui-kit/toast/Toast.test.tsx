import '@testing-library/jest-dom/vitest'

import { act, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ToastProvider, useToast } from './Toast'

// 测试消费方：每次点击 push 一条文案递增的 toast（外部行为断言走可见文案）
function ToastTester() {
  const { push } = useToast()
  const [count, setCount] = useState(0)

  return (
    <button
      type="button"
      onClick={() => {
        push(`提示 ${count + 1}`)
        setCount(count + 1)
      }}
    >
      入栈
    </button>
  )
}

afterEach(() => {
  vi.useRealTimers()
})

describe('ui-kit/Toast', () => {
  it('入栈后文案可见，点击手动关闭按钮后消失', () => {
    render(
      <ToastProvider>
        <ToastTester />
      </ToastProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: '入栈' }))
    expect(screen.getByText('提示 1')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '关闭提示' }))
    expect(screen.queryByText('提示 1')).not.toBeInTheDocument()
  })

  it('自动消失时长可配：短时长先消失，默认 3000ms 的仍可见', () => {
    vi.useFakeTimers()

    function DurationTester() {
      const { push } = useToast()

      return (
        <>
          <button type="button" onClick={() => push('默认时长')}>
            默认
          </button>
          <button type="button" onClick={() => push('短时长', { duration: 1000 })}>
            短
          </button>
        </>
      )
    }

    render(
      <ToastProvider>
        <DurationTester />
      </ToastProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: '默认' }))
    fireEvent.click(screen.getByRole('button', { name: '短' }))
    expect(screen.getByText('默认时长')).toBeInTheDocument()
    expect(screen.getByText('短时长')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(screen.queryByText('短时长')).not.toBeInTheDocument()
    expect(screen.getByText('默认时长')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(screen.queryByText('默认时长')).not.toBeInTheDocument()
  })

  it('多 toast 叠加：新 toast 入栈不使旧 toast 提前消失（各自计时）', () => {
    vi.useFakeTimers()

    render(
      <ToastProvider>
        <ToastTester />
      </ToastProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: '入栈' })) // 提示 1
    act(() => {
      vi.advanceTimersByTime(2000)
    })
    fireEvent.click(screen.getByRole('button', { name: '入栈' })) // 提示 2（提示 1 已计时 2s）

    expect(screen.getByText('提示 1')).toBeInTheDocument()
    expect(screen.getByText('提示 2')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(1000) // 提示 1 满 3000ms，提示 2 仅 1000ms
    })
    expect(screen.queryByText('提示 1')).not.toBeInTheDocument()
    expect(screen.getByText('提示 2')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(2000) // 提示 2 满 3000ms
    })
    expect(screen.queryByText('提示 2')).not.toBeInTheDocument()
  })
})
