// @vitest-environment node
// 纯文件内容断言，无需 DOM；node 环境下 import.meta.url 为 file:// 可直接读取。
import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

// tokens.css 是全站样式的唯一事实源（《前端-技术方案-v0.1.md》第 11 章）。
// 本测试以表驱动锁定 token 清单契约，防止后续票误删/漏定义，
// 破坏消费方（ui-kit、占位首页、票 07/09 网格三档行高切换、presence 专属色）。
const tokensCss = readFileSync(new URL('./tokens.css', import.meta.url), 'utf8')
// 暗色扩展位单独成文件（dark.css）：仅占位，默认不生效，亮色渲染不受影响
const darkCss = readFileSync(new URL('./dark.css', import.meta.url), 'utf8')

interface TokenCase {
  name: string
  variable: string
}

const cases: TokenCase[] = [
  // 中性阶
  { name: '中性阶：页面底色', variable: '--color-bg-page:' },
  { name: '中性阶：次级表面', variable: '--color-bg-subtle:' },
  { name: '中性阶：浮层表面', variable: '--color-bg-elevated:' },
  { name: '中性阶：主文字', variable: '--color-text-primary:' },
  { name: '中性阶：次级文字', variable: '--color-text-secondary:' },
  { name: '中性阶：三级文字', variable: '--color-text-tertiary:' },
  { name: '中性阶：禁用文字', variable: '--color-text-disabled:' },
  { name: '中性阶：边框', variable: '--color-border:' },
  { name: '中性阶：分隔线', variable: '--color-divider:' },
  // 品牌色
  { name: '品牌色：主色', variable: '--color-brand:' },
  { name: '品牌色：hover', variable: '--color-brand-hover:' },
  { name: '品牌色：active', variable: '--color-brand-active:' },
  { name: '品牌色：浅底', variable: '--color-brand-bg:' },
  // 状态四色
  { name: '状态色：info 前景', variable: '--color-info-text:' },
  { name: '状态色：info 底', variable: '--color-info-bg:' },
  { name: '状态色：success 前景', variable: '--color-success-text:' },
  { name: '状态色：success 底', variable: '--color-success-bg:' },
  { name: '状态色：warning 前景', variable: '--color-warning-text:' },
  { name: '状态色：warning 底', variable: '--color-warning-bg:' },
  { name: '状态色：danger 前景', variable: '--color-danger-text:' },
  { name: '状态色：danger 底', variable: '--color-danger-bg:' },
  // 功能色
  { name: '功能色：链接', variable: '--color-link:' },
  { name: '功能色：遮罩', variable: '--color-mask:' },
  { name: '功能色：焦点环', variable: '--color-focus-ring:' },
  { name: '功能色：禁用底', variable: '--color-disabled-bg:' },
  // 间距刻度
  { name: '间距：2xs', variable: '--spacing-2xs:' },
  { name: '间距：xs', variable: '--spacing-xs:' },
  { name: '间距：sm', variable: '--spacing-sm:' },
  { name: '间距：md', variable: '--spacing-md:' },
  { name: '间距：lg', variable: '--spacing-lg:' },
  { name: '间距：xl', variable: '--spacing-xl:' },
  { name: '间距：2xl', variable: '--spacing-2xl:' },
  // 字号阶
  { name: '字号：sm', variable: '--font-size-sm:' },
  { name: '字号：md', variable: '--font-size-md:' },
  { name: '字号：lg', variable: '--font-size-lg:' },
  { name: '字号：xl', variable: '--font-size-xl:' },
  // 行高三档（票 07/09 网格切换用）
  { name: '行高：紧凑', variable: '--line-height-compact:' },
  { name: '行高：中等', variable: '--line-height-standard:' },
  { name: '行高：宽松', variable: '--line-height-relaxed:' },
  // 圆角阶
  { name: '圆角：sm', variable: '--radius-sm:' },
  { name: '圆角：md', variable: '--radius-md:' },
  { name: '圆角：lg', variable: '--radius-lg:' },
  // 描边宽度
  { name: '描边宽度', variable: '--border-width:' },
  // z-index 分级
  { name: 'z-index：base', variable: '--z-index-base:' },
  { name: 'z-index：sticky', variable: '--z-index-sticky:' },
  { name: 'z-index：dropdown', variable: '--z-index-dropdown:' },
  { name: 'z-index：modal', variable: '--z-index-modal:' },
  { name: 'z-index：toast', variable: '--z-index-toast:' },
]

// presence 专属 10 色（技术方案第 8 章 presenceStore 消费：头像、他人光标、编辑描边）
const presenceCases: TokenCase[] = Array.from({ length: 10 }, (_, index) => ({
  name: `presence 专属色 ${index + 1}/10`,
  variable: `--presence-color-${index + 1}:`,
}))

describe('styles/tokens design token 清单（技术方案第 11 章，表驱动）', () => {
  it.each([...cases, ...presenceCases])('$name（$variable）已定义', ({ variable }) => {
    expect(tokensCss).toContain(variable)
  })

  it('暗色仅保留 [data-theme="dark"] 空覆盖位（dark.css）：默认不生效，亮色渲染不受影响', () => {
    expect(tokensCss).not.toMatch(/\[data-theme=["']dark["']\]/)
    const match = darkCss.match(/\[data-theme=["']dark["']\]\s*\{([^}]*)\}/)
    expect(match).not.toBeNull()
    // 占位层不得包含任何 token 覆盖声明（spec：暗色不实现）
    expect(match?.[1]).not.toMatch(/--[\w-]+\s*:/)
  })
})
