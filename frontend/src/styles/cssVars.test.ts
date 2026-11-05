import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * 结构测试：样式表里每一处不带回落值的 var(--x)，--x 都得在什么地方被定义过。
 *
 * 引用一个不存在的自定义属性在 CSS 里是【彻底静默】的：整条声明在计算时作废，
 * 元素照常渲染，只是那一项没了 —— 比如磁力缓冲条的底轨引用了只输出给播放器界面的 token，
 * 2px 的轨道就成了透明的，控制台一个字都不出。删旧别名、改 token 名之后最容易留下这种洞。
 *
 * 「定义过」包括三处：任何一个样式表里的声明（tokens.css 与各组件自己的局部变量），
 * 以及 TS / TSX 里以字符串写出的自定义属性名（海报色这类写在行内 style 上的变量）。
 * 带回落值的 var(--x, …) 不算：没定义时用回落值，是有意的。
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
const scriptFiles = files.filter((f) => /\.tsx?$/.test(f) && !/\.test\.tsx?$/.test(f))

/** 去掉 CSS 注释：注释里举例的 var(--x) 不算引用 */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, (comment) => comment.replace(/[^\n]/g, ' '))
}

const defined = new Set<string>()
for (const f of cssFiles) {
  for (const m of stripComments(readFileSync(f, 'utf8')).matchAll(/(?<![\w-])(--[a-zA-Z][\w-]*)\s*:/g)) defined.add(m[1]!)
}
for (const f of scriptFiles) {
  for (const m of readFileSync(f, 'utf8').matchAll(/['"`](--[a-zA-Z][\w-]*)['"`]/g)) defined.add(m[1]!)
}

/** 每一处不带回落值的引用：文件:行 → 变量名 */
function undefinedReferences(): string[] {
  const missing: string[] = []
  for (const f of cssFiles) {
    const lines = stripComments(readFileSync(f, 'utf8')).split('\n')
    lines.forEach((line, i) => {
      for (const m of line.matchAll(/var\(\s*(--[a-zA-Z][\w-]*)\s*\)/g)) {
        if (!defined.has(m[1]!)) missing.push(`${relative(SRC, f)}:${i + 1} ${m[1]}`)
      }
    })
  }
  return missing
}

describe('样式表里的自定义属性', () => {
  it('扫得到定义：tokens.css 的 token、组件自己的局部变量、行内写的海报色都在', () => {
    // 扫描器本身失灵（一个都没扫到）时，下面那条会静默通过 —— 先确认它看得见三类定义
    for (const name of ['--accent', '--progress-track', '--catalog-gutter', '--poster-tone']) expect(defined.has(name), name).toBe(true)
  })

  it('不带回落值的 var(--x) 都有定义（没定义的整条声明会静默作废）', () => {
    expect(undefinedReferences()).toEqual([])
  })
})
