// @vitest-environment node
// grep 断言式测试（票 03 AC 抽查项）：扫描 ui-kit 组件源码（tsx + CSS Modules），
// 断言零硬编码颜色与 px 间距/尺寸魔法值——全部样式取值必须走 tokens.css 的 var(--token)。
import { readdirSync, readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

interface SourceFile {
  path: string
  content: string
}

function collectSourceFiles(dir: URL, prefix = ''): SourceFile[] {
  const files: SourceFile[] = []

  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      files.push(...collectSourceFiles(new URL(`${entry.name}/`, dir), `${prefix}${entry.name}/`))
      continue
    }
    // 组件源码 = .tsx（排除测试）+ .module.css；tokens.css 不在 ui-kit 目录，天然不在扫描面内
    if (!/\.module\.css$/.test(entry.name) && !(/\.tsx$/.test(entry.name) && !entry.name.includes('.test.'))) {
      continue
    }
    files.push({ path: `${prefix}${entry.name}`, content: readFileSync(new URL(entry.name, dir), 'utf8') })
  }

  return files
}

const sourceFiles = collectSourceFiles(new URL('.', import.meta.url))

describe('ui-kit 组件源码零魔法值（票 03 AC 抽查，grep 断言式）', () => {
  it('扫描面非空：ui-kit 下至少 8 个组件源码文件（4 组件 × tsx/css）', () => {
    expect(sourceFiles.length).toBeGreaterThanOrEqual(8)
  })

  it.each(sourceFiles)('$path 不含硬编码颜色（hex / rgb / rgba）', ({ content }: SourceFile) => {
    expect(content).not.toMatch(/#[0-9a-fA-F]{3,8}\b|\brgba?\(/)
  })

  it.each(sourceFiles)('$path 不含 px 魔法值（间距/尺寸一律 var token）', ({ content }: SourceFile) => {
    expect(content).not.toMatch(/\d+(\.\d+)?px\b/)
  })
})
