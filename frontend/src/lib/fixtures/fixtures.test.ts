import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * 假数据的渗漏闸。
 *
 * 六个后端缺口的界面都已经建好，跑在**编出来的**数据形状上。这本身没问题
 * ——问题是那个形状会往外爬。它已经爬过一次：`FakeMedia` 曾是 `MediaCard`
 * 与 `DiscoverHero` 的 prop 类型，两个已发布的共享组件。那意味着接上真接口时，
 * 「删掉 fixture 目录」不是删一个目录，是给共享组件重新定型。
 *
 * 这条测试把方向钉死：**组件定契约，fixture 去满足它**。
 * 假数据可以往页面里流（那些页面本来就是演示），但不能往组件层流。
 *
 * 为什么值得一条测试而不是一条口头规矩：加一行 import 是十秒钟的事，
 * 而它造成的损失要到几个月后接真接口时才结算 —— 那时写这行的人早忘了。
 */

const SRC = new URL('../..', import.meta.url).pathname

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (p.endsWith('.ts') || p.endsWith('.tsx')) out.push(p)
  }
  return out
}

/** 抠出一个文件里所有 import 的来源路径 */
function importsIn(source: string): string[] {
  const out: string[] = []
  for (const m of source.matchAll(/from\s+'([^']+)'/g)) out.push(m[1]!)
  return out
}

const files = walk(SRC)

describe('假数据不得往组件层渗漏', () => {
  it('src/components/ 下没有任何文件 import lib/fixtures/', () => {
    const leaks: string[] = []
    for (const f of files) {
      const rel = relative(SRC, f)
      if (!rel.startsWith('components/')) continue
      for (const spec of importsIn(readFileSync(f, 'utf8'))) {
        if (spec.includes('lib/fixtures')) leaks.push(`${rel} → ${spec}`)
      }
    }
    // 需要占位封面 / 作品形状的组件：用 lib/placeholderArt 与
    // components/media/types 的 MediaSummary，不要伸手去 fixtures 里拿。
    expect(leaks).toEqual([])
  })

  it('fixtures 目录之外没人再引用已经搬走的 placeholder 路径', () => {
    // placeholderArt 不是假数据（真作品也会没有封面），已经搬到 lib/。
    // 旧路径留在某处只会让人以为「占位图属于假数据」，接真接口时顺手删掉。
    const stale = files
      // 测试文件里提到旧路径是在【讲】这件事（比如上面那段注释），不是引用它
      .filter((f) => !f.endsWith('.test.ts') && !f.endsWith('.test.tsx'))
      .map((f) => [relative(SRC, f), readFileSync(f, 'utf8')] as const)
      .filter(([, src]) => src.includes('fixtures/placeholder'))
      .map(([rel]) => rel)
    expect(stale).toEqual([])
  })

  it('扫描器本身是活的', () => {
    // 目录挪动会让上面两条静默地变成「零个文件、零个问题」
    expect(files.filter((f) => relative(SRC, f).startsWith('components/')).length).toBeGreaterThan(
      10,
    )
  })
})
