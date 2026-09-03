import { FixtureNotice } from './ListsPage'
import { FAKE_SCANS } from '../lib/fixtures/scans'
import { mono } from '../theme'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/scan-summaries` 扫描记录。
 *
 * FIXME(G5): 假数据。`Rescan()` 现在只返回 `{videos, clusters}` 计数、不留历史，
 * 要真数据得在扫描时把结果落盘（store 加一张表）。缺口见 todos.md。
 *
 * 这一页真正的价值在 `unresolved`：解析不出集号的文件会被静默跳过，
 * 用户唯一能察觉的方式就是「某一集怎么不在库里」。把它列出来是 CQ3
 * 那条「零结果 ≠ 失败」的同一个道理。
 */
export function ScanSummariesPage() {
  return (
    <main className="lib-shell">
      <header className="page-head">
        <h1 className="page-title">扫描记录</h1>
      </header>

      <FixtureNotice gap="G5" what="扫描历史" />

      <ul className="scan-list">
        {FAKE_SCANS.map((s) => (
          <li key={s.id} className="panel scan">
            <header className="scan-head">
              <h2 className="scan-folder">{s.folder}</h2>
              <span className="scan-at" style={mono}>
                {new Date(s.at).toLocaleString('zh-CN', { hour12: false })}
              </span>
            </header>
            <p className="scan-stats" style={mono}>
              {s.videos} 个视频 · {s.clusters} 个作品簇 · 用时 {(s.durationMs / 1000).toFixed(1)}s
            </p>
            {s.unresolved.length > 0 && (
              <div className="scan-unresolved">
                <p className="scan-unresolved-head">
                  {s.unresolved.length} 个文件解析不出集号，已跳过：
                </p>
                <ul>
                  {s.unresolved.map((f) => (
                    <li key={f} style={mono}>
                      {f}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </li>
        ))}
      </ul>
    </main>
  )
}
