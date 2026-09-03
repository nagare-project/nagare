import { useEffect, useState } from 'react'
import { placeholderArt } from '../../lib/placeholderArt'
import type { MediaSummary } from './types'

/**
 * 发现页顶部的轮播横幅（对齐 seanime 的 DiscoverPageHeader）。
 *
 * 与它的两处差别，都是有意的：
 *  1. 不放预告片。seanime 那里嵌的是 YouTube iframe —— CSP 的 frame-src 没开，
 *     而为一个装饰性预告片放开 iframe 白名单不划算。
 *  2. 横幅是渐变不是图。真数据里 animego 只给竖版封面、没有横幅图，
 *     所以这条路线本来就要靠封面派生；假数据阶段用同一套渐变，形状是对的。
 */

/** 自动轮播间隔。太快会打断阅读简介，太慢又看不出它会动。 */
const ROTATE_MS = 7000

export function DiscoverHero({ items }: { items: MediaSummary[] }) {
  const [index, setIndex] = useState(0)
  const [paused, setPaused] = useState(false)

  useEffect(() => {
    if (paused || items.length < 2) return
    const t = setTimeout(() => setIndex((i) => (i + 1) % items.length), ROTATE_MS)
    return () => clearTimeout(t)
  }, [index, paused, items.length])

  if (items.length === 0) return null
  const media = items[index] ?? items[0]!

  return (
    <section
      className="hero"
      aria-label="精选作品"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
    >
      <div className="hero-bg" style={{ background: placeholderArt(media.title) }} aria-hidden="true" />

      <div className="hero-body">
        <span className="hero-art" style={{ background: placeholderArt(media.title) }} aria-hidden="true">
          <span className="poster-art-mark">{media.title.slice(0, 1)}</span>
        </span>

        <div className="hero-text">
          <h2 className="hero-title">{media.title}</h2>
          {media.titleNative !== undefined && <p className="hero-native">{media.titleNative}</p>}

          <p className="hero-meta">
            {media.score !== undefined && media.score > 0 && (
              <span className="hero-score">{media.score} 分</span>
            )}
            {media.year !== undefined && <span>{media.year} 年{media.season ?? ''}</span>}
            {media.episodes !== null && <span>{media.episodes} 集</span>}
          </p>

          <p className="hero-genres">
            {media.genres.slice(0, 3).map((g) => (
              <span key={g} className="badge">
                {g}
              </span>
            ))}
          </p>

          {media.description !== undefined && <p className="hero-desc">{media.description}</p>}
        </div>
      </div>

      {items.length > 1 && (
        <div className="hero-dots" role="tablist" aria-label="切换精选作品">
          {items.map((m, i) => (
            <button
              key={m.id}
              type="button"
              role="tab"
              aria-selected={i === index}
              aria-label={m.title}
              className={i === index ? 'hero-dot hero-dot--on' : 'hero-dot'}
              onClick={() => setIndex(i)}
            />
          ))}
        </div>
      )}
    </section>
  )
}
