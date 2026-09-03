import { placeholderArt } from '../../lib/fixtures/placeholder'
import type { FakeMedia } from '../../lib/fixtures/types'

/**
 * 作品海报卡（发现 / 我的列表 / 放送表共用）。
 *
 * 与库页的 PosterCard 是两个东西，不要合并：那个指向本机已有的文件
 * （链到 /anime/$clusterKey，有真实封面与集数），这个展示的是【元数据】
 * ——用户可能根本没有这部番的文件。合并会让「能不能点开看」这件事变得含糊。
 *
 * 比例与 hover 用同一套值（3:4 / scale-1.1 / 200ms），视觉上是一家。
 */
export function MediaCard({ media, footer }: { media: FakeMedia; footer?: React.ReactNode }) {
  const { title, titleNative, episodes, watched } = media
  const pct = episodes !== null && episodes > 0 ? Math.min(100, (watched / episodes) * 100) : 0

  return (
    <li className="poster">
      <div className="poster-hit">
        <span className="poster-art" style={{ background: placeholderArt(title) }}>
          {/* 假封面：确定性渐变 + 首字，不引外部图（CSP img-src 'self'） */}
          <span className="poster-art-mark" aria-hidden="true">
            {title.slice(0, 1)}
          </span>
          {watched > 0 && (
            <span className="poster-bar" aria-hidden="true">
              <span className="poster-bar-fill" style={{ width: `${pct}%` }} />
            </span>
          )}
        </span>
        <p className="poster-title">{title}</p>
        <p className="poster-meta">
          {footer ?? <DefaultMeta media={media} />}
        </p>
        {titleNative !== undefined && <p className="poster-native">{titleNative}</p>}
      </div>
    </li>
  )
}

function DefaultMeta({ media }: { media: FakeMedia }) {
  const { year, season, episodes, watched } = media
  const when = year !== undefined ? `${year} 年${season ?? ''}` : ''
  if (watched > 0 && episodes !== null) return <>{`${watched} / ${episodes} 集`}</>
  if (episodes !== null) return <>{when === '' ? `${episodes} 集` : `${when} · ${episodes} 集`}</>
  return <>{when === '' ? '未定' : when}</>
}
