import '@testing-library/jest-dom/vitest'

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { DEFAULT_NOTICE_TEXT } from '@/lib/env'

import { App } from './App'

describe('App 占位首页', () => {
  it('渲染产品标题与试运行声明文案', () => {
    render(<App />)

    const heading = screen.getByRole('heading', { level: 1 })
    expect(heading).toHaveTextContent('协作多维表格')
    expect(screen.getByText(DEFAULT_NOTICE_TEXT)).toBeInTheDocument()
  })
})
