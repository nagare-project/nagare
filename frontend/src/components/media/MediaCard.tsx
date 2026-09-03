import type { ReactNode } from 'react'
import { placeholderArt } from '../../lib/fixtures/placeholder'
import type { FakeMedia } from '../../lib/fixtures/types'

/**
 * 作品海报卡（发现 / 我的列表 / 放送表共用）。
 *
 * 与库页的 PosterCard 是两个东西，不要合并：那个指向本机已有的文件
 * （链到 /anime/$clusterKey，有真实封面与集数），这个展示的是【元数据】
 * ——用户可能根本没有这部番的文件。合并会让「能不能点开看」这件事变得含糊。
 *
 * 比例与 hover 放大用同一套值（3:4 / scale-1.1 / 200ms），视觉上是一家。
 *
 * 信息分两层，这个分法是有意的：
 *   常驻可见 —— 评分 · 年份季度 · 集数 · 类型标签
 *   hover 浮出 —— 简介
 * 规则是「重要信息不能只在 hover 里」：hover 在触屏上根本不存在，
 * 键盘用户也要多一步。所以只有简介这种补充信息放浮层，
 * 而且它照样留在 DOM 里，读屏与搜索都拿得到，只是视觉上默认收起来。
 *
 * 浮层画在海报框【内部】而不是往外弹：发现页的板块是横向滚动容器，
 * 弹出去会被 overflow 裁掉。
 */
export function MediaCard({ media, footer }: { media: FakeMedia; footer?: ReactNode }) {
  const { title, titleNative, episodes, watched, score, genres, description } = media
  const pct = episodes !== null && episodes > 0 ? Math.min(100, (watched / episodes) * 100) : 0

  return (
    <li className="poster">
      <div className="poster-hit">
        <span className="poster-art" style={{ background: placeholderArt(title) }}>
          {/* 假封面：确定性渐变 + 首字，不引外部图（CSP img-src 'self'） */}
          <span className="poster-art-mark" aria-hidden="true">
            {title.slice(0, 1)}
          </span>

          {description !== undefined && (
            <span className="poster-over">
              <span className="poster-over-desc">{description}</span>
            </span>
          )}

          {watched > 0 && (
            <span className="poster-bar" aria-hidden="true">
              <span className="poster-bar-fill" style={{ width: `${pct}%` }} />
            </span>
          )}
        </span>

        <p className="poster-title">{title}</p>
        <p className="poster-meta">{footer ?? <DefaultMeta media={media} />}</p>
        {titleNative !== undefined && <p className="poster-native">{titleNative}</p>}

        {genres.length > 0 && (
          <p className="poster-genres">
            {/* 最多三个：再多会挤成两行，把卡片高度撑得参差不齐 */}
            {genres.slice(0, 3).map((g) => (
              <span key={g} className="poster-genre">
                {g}
              </span>
            ))}
          </p>
        )}

        {score !== undefined && score > 0 && (
          <span className="poster-score" aria-label={`评分 ${score}`}>
            {score}
          </span>
        )}
      </div>
    </li>
  )
}

/** 默认的第二行：优先显示「看到哪了」，没看过才显示年份与总集数 */
function DefaultMeta({ media }: { media: FakeMedia }) {
  const { year, season, episodes, watched } = media
  const when = year !== undefined ? `${year} 年${season ?? ''}` : ''
  if (watched > 0 && episodes !== null) return <>{`${watched} / ${episodes} 集`}</>
  if (episodes !== null) return <>{when === '' ? `${episodes} 集` : `${when} · ${episodes} 集`}</>
  return <>{when === '' ? '未定' : when}</>
}
