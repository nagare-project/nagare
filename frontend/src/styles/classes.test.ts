import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * 结构测试：TSX 里引用的每个类名，CSS 里都要有定义。
 *
 * 为什么值得为它写一个测试 —— 类名写错在这套架构里是【彻底静默】的：
 * 元素照常渲染，只是没样式，控制台一个字都不出。改版时把 .badge--amber
 * 合并进 .badge--warn 却漏改一处调用，336 个测试全绿，界面上那个徽标
 * 却变成裸的。这条正是那次漏网补上的网。
 *
 * 反向（CSS 定义了但没人用）不在这里管：那是死代码，不影响用户。
 */

const SRC = new URL('..', import.meta.url).pathname

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else out.push(p)
  }
  return out
}

const files = walk(SRC)
const cssFiles = files.filter((f) => f.endsWith('.css'))
const tsxFiles = files.filter((f) => f.endsWith('.tsx') && !f.endsWith('.test.tsx'))

/** CSS 里出现过的所有类名 */
const defined = new Set<string>()
for (const f of cssFiles) {
  for (const m of readFileSync(f, 'utf8').matchAll(/\.([a-zA-Z][\w-]*)/g)) {
    defined.add(m[1]!)
  }
}

/**
 * 动态拼接的类名白名单：`src-chip--${state}` 这种，静态扫不出来。
 * 加进来之前先确认 CSS 里【每一个】可能的取值都有定义 —— 白名单是为了
 * 让扫描器闭嘴，不是为了让漏洞过关。
 */
const DYNAMIC_PREFIXES = ['src-chip--', 'result--', 'mpv-dot--', 'tsb-phase--']

/**
 * 只作为测试查询钩子存在、刻意没有样式的类名。
 * 加进来要有理由：它必须真的被某个 .test.tsx 用 querySelector 查。
 */
const TEST_HOOKS = new Set(['res-title-text'])

/** 从 className 属性里抠出静态类名 */
function classNamesIn(source: string): string[] {
  const out: string[] = []
  for (const m of source.matchAll(/className=(?:"([^"]*)"|\{`([^`]*)`\}|\{'([^']*)'\})/g)) {
    const blob = m[1] ?? m[2] ?? m[3] ?? ''
    for (const cls of blob.split(/\s+/)) {
      // 模板串里的插值段（`a b--${x}`）跳过，交给 DYNAMIC_PREFIXES
      if (cls === '' || cls.includes('$') || cls.includes('{')) continue
      out.push(cls)
    }
  }
  return out
}

describe('样式类名完整性', () => {
  it('TSX 里引用的类名在 CSS 里都有定义', () => {
    const missing: string[] = []
    for (const f of tsxFiles) {
      for (const cls of classNamesIn(readFileSync(f, 'utf8'))) {
        if (defined.has(cls)) continue
        if (DYNAMIC_PREFIXES.some((p) => cls.startsWith(p))) continue
        if (TEST_HOOKS.has(cls)) continue
        missing.push(`${f.slice(SRC.length)}: .${cls}`)
      }
    }
    expect(missing).toEqual([])
  })

  it('扫描器本身是活的（能读到 CSS 与 TSX）', () => {
    // 目录挪动 / 打包方式变更会让上面那条静默地变成"零个文件、零个问题"
    expect(cssFiles.length).toBeGreaterThan(5)
    expect(tsxFiles.length).toBeGreaterThan(20)
    expect(defined.size).toBeGreaterThan(100)
  })
})
