import { useState } from 'react'
import './poster-tile.css'

/** 压在封面上的徽标：遮罩底 + 只让文字带颜色（DESIGN.md 颜色规则 6） */
export interface PosterBadge {
  text: string
  tone?: 'warning' | 'info' | 'plain'
  /** 鼠标悬停时的补充说明（读屏走可见文字，这里只是补充） */
  hint?: string
}

const BADGE_CLASS = {
  warning: 'poster-tile-badge poster-tile-badge--warning',
  info: 'poster-tile-badge poster-tile-badge--info',
  plain: 'poster-tile-badge',
} as const

type Props = {
  cover?: string
  title: string
  /** 「12 集 · 已看 7」：数字 tabular-nums */
  meta?: string
  badges?: PosterBadge[]
}

/**
 * 海报卡的内容（DESIGN.md「卡片 · 海报卡」）：3:4 封面在上，标题两行截断，副标题一行。
 * 外面那层链接由调用方给（媒体库用路由 Link，作品页推荐用 MediaEntryLink），整张卡可点。
 * 链接的名字就是卡片里的可见文字（标题 → 徽标 → 副标题），不另写 aria-label：
 * 另写的名字一旦不包含可见文字，语音控制就点不到它（WCAG 2.5.3）。
 */
export function PosterTile({ cover, title, meta, badges = [] }: Props) {
  // 记下的是哪个地址加载失败：改了对应作品之后封面换了地址，旧的失败不该把新图也藏起来
  const [failedSrc, setFailedSrc] = useState<string>()
  const hasCover = cover !== undefined && cover !== '' && failedSrc !== cover
  return (
    <>
      <span className="poster-tile-art">
        {hasCover ? (
          <img src={cover} alt="" loading="lazy" onError={() => setFailedSrc(cover)} />
        ) : (
          <span className="poster-tile-blank" aria-hidden="true">流</span>
        )}
      </span>
      <span className="poster-tile-title">{title}</span>
      {badges.length > 0 && (
        <span className="poster-tile-badges">
          {badges.map((badge) => (
            <span key={badge.text} className={BADGE_CLASS[badge.tone ?? 'plain']} title={badge.hint}>
              {badge.text}
            </span>
          ))}
        </span>
      )}
      {meta && <span className="poster-tile-meta">{meta}</span>}
    </>
  )
}
