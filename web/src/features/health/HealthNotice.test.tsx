import '@testing-library/jest-dom/vitest'

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { DEFAULT_NOTICE_TEXT } from '@/lib/env'

import { HealthNotice } from './HealthNotice'

describe('HealthNotice 试运行免责声明', () => {
  it('渲染环境变量提供的声明文案（默认 DEFAULT_NOTICE_TEXT）', () => {
    render(<HealthNotice />)

    expect(screen.getByText(DEFAULT_NOTICE_TEXT)).toBeInTheDocument()
  })

  it('以警告语义渲染（warning Banner → role=alert，数据风险提示）', () => {
    render(<HealthNotice />)

    expect(screen.getByRole('alert')).toHaveTextContent(DEFAULT_NOTICE_TEXT)
  })
})
