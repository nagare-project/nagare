import { FixtureNotice } from './ListsPage'
import { fakeAiringThisWeek } from '../lib/fixtures/library'
import type { FakeAiring } from '../lib/fixtures/types'
import { mono } from '../theme'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/schedule` 放送表：本周每天播出的新集。
 *
 * FIXME(G3): 假数据（日期按本周一现算，所以哪天打开都合理）。
 * 真数据需要一个放送表接口，animego 侧是否有待查。缺口见 todos.md。
 */

const DAY_LABEL = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

export function SchedulePage({ embedded = false }: { embedded?: boolean } = {}) {
  const now = new Date()
  const airings = fakeAiringThisWeek(now)
  const todayIdx = (now.getDay() + 6) % 7 // 周日是 0，换算成周一起算

  // 按「周几」分桶。注意用本地时间取 day —— airingAt 是 ISO（含时区），
  // 直接切字符串会在跨时区/跨零点时错一天。
  const byDay: FakeAiring[][] = [[], [], [], [], [], [], []]
  for (const a of airings) {
    const d = new Date(a.airingAt)
    byDay[(d.getDay() + 6) % 7]!.push(a)
  }
  for (const bucket of byDay) {
    bucket.sort((x, y) => x.airingAt.localeCompare(y.airingAt))
  }

  const body = (
    <>
      <FixtureNotice gap="G3" what="播出时间" />

      <div className="week">
        {DAY_LABEL.map((label, i) => (
          <section
            key={label}
            className={i === todayIdx ? 'day day--today' : 'day'}
            aria-label={label}
          >
            <h2 className="day-head">
              {label}
              {i === todayIdx && <span className="badge badge--accent">今天</span>}
            </h2>
            {byDay[i]!.length === 0 ? (
              <p className="day-empty">—</p>
            ) : (
              <ul className="day-list">
                {byDay[i]!.map((a) => (
                  <li key={a.id} className="airing">
                    <span className="airing-time" style={mono}>
                      {new Date(a.airingAt).toLocaleTimeString('zh-CN', {
                        hour: '2-digit',
                        minute: '2-digit',
                        hour12: false,
                      })}
                    </span>
                    <span className="airing-title">{a.title}</span>
                    <span className="airing-ep">第 {a.episode} 集</span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        ))}
      </div>
    </>
  )

  // 内嵌进发现页的「放送表」标签时不重复页头，也不再套一层 <main>
  if (embedded) return body

  return (
    <main className="lib-shell">
      <header className="page-head">
        <h1 className="page-title">放送表</h1>
      </header>
      {body}
    </main>
  )
}
