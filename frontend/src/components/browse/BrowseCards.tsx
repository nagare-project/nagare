import { memo, useId, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { useNavigate } from '@tanstack/react-router'
import type { SummaryMediaData } from '../../lib/endpoints'
import { HUE_FAMILIES, type HueFamilyKey } from '../../lib/home/hueFamilies'
import { oklchCss, toneLadder } from '../../lib/home/tone'
import { SEASON_LABEL } from '../catalog/season'
import { CoverImage } from '../home/CoverImage'
import { useCollection } from '../media/CollectionContext'
import { genreFilter } from '../media/genres'
import { airingDistance, FORMAT_LABELS } from '../media/media-format'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { MediaListEditor } from '../media/MediaListEditor'
import { CheckIcon, ChevronIcon, FaceIcon, PlusIcon } from './icons'
import cards from './cards.module.css'
import chart from './chart.module.css'
import controls from './controls.module.css'
import rows from './rows.module.css'
import tags from './tags.module.css'

export type BrowseViewMode = 'grid' | 'chart' | 'table'

export const STATUS_LABELS: Record<string, string> = {
  RELEASING: '连载中',
  FINISHED: '已完结',
  NOT_YET_RELEASED: '尚未播出',
  HIATUS: '暂停放送',
  CANCELLED: '已取消',
}

export const SOURCE_LABELS: Record<string, string> = {
  ORIGINAL: '原创',
  MANGA: '漫画改编',
  LIGHT_NOVEL: '轻小说',
  NOVEL: '小说',
  VIDEO_GAME: '游戏改编',
  VISUAL_NOVEL: '视觉小说',
  WEB_NOVEL: '网文改编',
  OTHER: '其他',
}

function scoreBand(score: number): 'high' | 'mid' | 'low' {
  if (score >= 75) return 'high'
  if (score >= 60) return 'mid'
  return 'low'
}

function formatPopularity(n?: number): string {
  if (!n || n <= 0) return ''
  if (n >= 10000) return `${(n / 10000).toFixed(1)} 万人在追`
  return `${n.toLocaleString()} 人在追`
}

function seasonYearText(item: SummaryMediaData): string {
  const s = item.season ? SEASON_LABEL[item.season as keyof typeof SEASON_LABEL] : undefined
  if (item.year && s) return `${item.year} ${s}季`
  if (item.year) return `${item.year} 年`
  return ''
}

function infoLine(item: SummaryMediaData): string {
  const parts: string[] = []
  if (item.format) parts.push(FORMAT_LABELS[item.format] ?? item.format)
  if (item.episodes) parts.push(`${item.episodes} 集`)
  else if (item.duration) parts.push(`${item.duration} 分钟`)
  return parts.join(' · ')
}

function metaLine(item: SummaryMediaData): string {
  const parts: string[] = []
  if (item.source && SOURCE_LABELS[item.source]) parts.push(SOURCE_LABELS[item.source])
  if (item.format) parts.push(FORMAT_LABELS[item.format] ?? item.format)
  if (item.episodes) parts.push(`${item.episodes} 集`)
  if (item.bangumiScore && item.bangumiScore > 0) parts.push(`Bangumi ${item.bangumiScore.toFixed(1)}`)
  return parts.join(' · ')
}

function airingLine(item: SummaryMediaData): { key: string; value: string; short: string } {
  if (item.nextAiring && item.nextAiring.at > Date.now() / 1000) {
    const dist = airingDistance(item.nextAiring.at)
    return {
      key: `第 ${item.nextAiring.episode} 集`,
      value: `${dist}播出`,
      short: `第 ${item.nextAiring.episode} 集 · ${dist}`,
    }
  }
  const sy = seasonYearText(item)
  if (item.status === 'FINISHED') {
    return { key: sy || '已完结', value: item.episodes ? `全 ${item.episodes} 集` : '已完结', short: sy || '已完结' }
  }
  if (item.status === 'NOT_YET_RELEASED') {
    return { key: sy || '即将播出', value: item.startDate || '尚未开播', short: item.startDate || sy || '未开播' }
  }
  return {
    key: sy || '连载中',
    value: item.episodes ? `共 ${item.episodes} 集` : STATUS_LABELS[item.status ?? ''] ?? '连载中',
    short: sy || (STATUS_LABELS[item.status ?? ''] ?? ''),
  }
}

export function Score({
  score,
  placeholder,
  inChart = false,
}: {
  score?: number | null
  placeholder?: string
  inChart?: boolean
}) {
  if (!score && placeholder === undefined) return null
  return (
    <span
      className={inChart ? `${cards.score} ${cards.scoreChart}` : cards.score}
      data-band={score ? scoreBand(score) : undefined}
    >
      {score ? <FaceIcon className={cards.face} /> : null}
      {score ? `${score}%` : placeholder}
    </span>
  )
}

export function Pills({
  genres,
  tone,
  onTone,
  limit,
  small,
  inline,
  twoOnPhone,
  className,
}: {
  genres: readonly string[]
  tone: string
  onTone: string
  limit: number
  small?: boolean
  inline?: boolean
  twoOnPhone?: boolean
  className?: string
}) {
  const labels = genres.slice(0, limit).map((g) => genreFilter(g) ?? g)
  if (labels.length === 0) return null
  const cls = [
    cards.pills,
    small ? cards.pillsSm : '',
    inline ? cards.pillsInline : '',
    twoOnPhone ? cards.pillsTwo : '',
    className ?? '',
  ]
    .filter(Boolean)
    .join(' ')
  return (
    <div className={cls}>
      {labels.map((label) => (
        <span key={label} className={cards.pill} style={{ background: tone, color: onTone }}>
          {label}
        </span>
      ))}
    </div>
  )
}

function FollowCircle({ item, variant }: { item: SummaryMediaData; variant: 'cover' | 'chart' }) {
  const collection = useCollection()
  const navigate = useNavigate()
  const entry = collection.entries.find((e) => e.anilistId === item.anilistId)
  const cls = variant === 'cover' ? cards.add : cards.chAdd
  const ariaLabel = entry ? `编辑「${item.title}」的追番状态` : `追番：${item.title}`
  const icon = (
    <>
      <PlusIcon className={cards.iconPlus} />
      <CheckIcon className={cards.iconCheck} />
    </>
  )

  if (!collection.loggedIn) {
    return (
      <button
        type="button"
        className={cls}
        data-follow="off"
        aria-label={ariaLabel}
        onClick={() => {
          void navigate({ to: '/settings', hash: 'account' })
        }}
      >
        {icon}
      </button>
    )
  }

  return (
    <MediaListEditor
      media={{
        id: item.anilistId,
        title: item.title,
        cover: item.cover,
        episodes: item.episodes ?? null,
        watched: entry?.currentEpisode ?? 0,
        genres: item.genres,
        format: item.format,
        status: item.status,
        year: item.year,
        season: item.season,
      }}
      trigger={(props) => (
        <button
          {...props}
          type="button"
          className={cls}
          data-follow={entry ? 'on' : 'off'}
          aria-label={ariaLabel}
        >
          {icon}
        </button>
      )}
    />
  )
}

export interface HueOption {
  key: HueFamilyKey
  hue: number
  label: string
  count: number
}

/** 1:1 对齐网站 HueDots：默认 32px 圆点，悬停或选中后横向展开为「蓝调 26」胶囊 */
export function BrowseHueDots({
  options,
  selected,
  onPick,
  phone = false,
}: {
  options: readonly HueOption[]
  selected: HueFamilyKey | null
  onPick: (key: HueFamilyKey | null) => void
  phone?: boolean
}) {
  if (options.length === 0) return null
  return (
    <div className={phone ? tags.hueRow : tags.hues} role="group" aria-label="按封面色调筛选">
      {options.map((o) => {
        const on = selected === o.key
        return (
          <button
            key={o.key}
            type="button"
            className={tags.hue}
            aria-pressed={on}
            onClick={() => onPick(on ? null : o.key)}
          >
            <i className={tags.hueDot} style={{ background: oklchCss(72, 0.13, o.hue) }} aria-hidden="true" />
            <span className={tags.hueName}>{o.label}</span>
            <em className={tags.hueCount}>{o.count}</em>
          </button>
        )
      })}
    </div>
  )
}

/** 1:1 对齐网站 MenuField：带上方标签的 196px 自定义下拉按钮与浮层容器 */
export function MenuField({
  label,
  value,
  isSet,
  open,
  onToggle,
  onClose,
  popup = 'menu',
  children,
}: {
  label: string
  value: string
  isSet: boolean
  open: boolean
  onToggle: () => void
  onClose: () => void
  popup?: 'menu' | 'listbox'
  children: (ctx: { labelId: string }) => ReactNode
}) {
  const labelId = useId()
  const valueId = useId()
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Escape' || !open) return
    e.stopPropagation()
    onClose()
    ;(e.currentTarget.querySelector('button') as HTMLButtonElement | null)?.focus({ preventScroll: true })
  }
  return (
    <div className={controls.field} data-menu-field onKeyDown={onKeyDown}>
      <span id={labelId} className={controls.label}>
        {label}
      </span>
      <button
        type="button"
        className={controls.control}
        aria-haspopup={popup}
        aria-expanded={open}
        aria-labelledby={`${labelId} ${valueId}`}
        data-set={isSet ? 'true' : undefined}
        onClick={onToggle}
      >
        <span id={valueId} className={controls.value}>{value}</span>
        <ChevronIcon className={controls.chev} />
      </button>
      {children({ labelId })}
    </div>
  )
}

const CoverCardItem = memo(function CoverCardItem({
  item,
  rank,
  eager,
}: {
  item: SummaryMediaData
  rank: number | null
  eager: boolean
}) {
  const hue = item.hue ?? item.posterTones?.hue ?? null
  const tone = toneLadder(hue)
  const popBg = oklchCss(21, 0.02, hue)
  const air = airingLine(item)
  const studio = item.studios?.[0]

  return (
    <li className={cards.card} style={{ color: tone.text } as CSSProperties}>
      <div className={cards.cwrap}>
        <MediaEntryLink
          id={item.anilistId}
          className={cards.cover}
          style={{ background: tone.fill }}
          tabIndex={-1}
          aria-hidden="true"
        >
          <span className={cards.art}>
            <CoverImage src={item.cover ?? null} className={cards.coverImg} eager={eager} />
          </span>
        </MediaEntryLink>
        {rank !== null ? (
          <span className={cards.rank} style={{ background: tone.text, color: tone.onSolid }} aria-hidden="true">
            <i>#</i>
            {rank}
          </span>
        ) : null}
        <FollowCircle item={item} variant="cover" />
      </div>
      <MediaEntryLink id={item.anilistId} className={cards.title}>
        {rank !== null ? <span className="visually-hidden">{`第 ${rank} 名 `}</span> : null}
        {item.title}
      </MediaEntryLink>
      {rank !== null ? (
        <div className={cards.rankMeta}>
          <Score score={item.score} />
          <span className={cards.rankWhen}>{seasonYearText(item) || infoLine(item)}</span>
        </div>
      ) : (
        <span className={cards.air}>{air.short}</span>
      )}
      <div className={cards.pop} style={{ background: popBg }} aria-hidden="true">
        <i className={cards.arrow} style={{ color: popBg }} />
        <div className={cards.popHead}>
          <div className={cards.popAir}>{rank !== null ? seasonYearText(item) || air.key : `${air.key} · ${air.value}`}</div>
          <Score score={item.score} />
        </div>
        {studio ? (
          <div className={cards.popStudio} style={{ color: tone.text }}>
            {studio}
          </div>
        ) : null}
        <div className={cards.popInfo}>{infoLine(item)}</div>
        {item.bangumiScore && item.bangumiScore > 0 ? (
          <div className={cards.popBgm}>
            Bangumi<b>{item.bangumiScore.toFixed(1)}</b>
          </div>
        ) : null}
        <Pills genres={item.genres} tone={tone.text} onTone={tone.onSolid} limit={3} />
      </div>
    </li>
  )
})

const ChartCardItem = memo(function ChartCardItem({
  item,
  rank,
  eager,
}: {
  item: SummaryMediaData
  rank: number | null
  eager: boolean
}) {
  const hue = item.hue ?? item.posterTones?.hue ?? null
  const tone = toneLadder(hue)
  const air = airingLine(item)
  const studio = item.studios?.[0]

  return (
    <li
      className={chart.chart}
      style={{ background: oklchCss(21, 0.02, hue), color: tone.text } as CSSProperties}
    >
      <MediaEntryLink id={item.anilistId} className={chart.chCover} style={{ background: tone.fill }}>
        <span className={chart.chArt}>
          <CoverImage src={item.cover ?? null} className={chart.chImg} eager={eager} />
        </span>
        <div className={chart.chOverlay}>
          <div className={chart.chTitle}>
            {rank !== null ? <span className="visually-hidden">{`第 ${rank} 名 `}</span> : null}
            {item.title}
          </div>
          {studio ? <div className={chart.chStudio}>{studio}</div> : null}
        </div>
      </MediaEntryLink>
      <div className={chart.chData}>
        <div className={chart.chBody}>
          <div className={chart.chHead}>
            <div>
              <div className={chart.chKey}>{rank !== null ? seasonYearText(item) || '目录作品' : air.key}</div>
              <div className={chart.chValue}>{rank !== null ? infoLine(item) || air.value : air.value}</div>
            </div>
            {rank !== null ? (
              <div className={chart.chRankCol}>
                <span className={chart.chRank} aria-hidden="true">
                  <i>#</i>
                  {rank}
                </span>
                <Score score={item.score} inChart />
              </div>
            ) : (
              <Score score={item.score} inChart />
            )}
          </div>
          <div className={chart.chMeta}>{metaLine(item)}</div>
          <div className={chart.chDesc}>{item.description || '暂无简介。'}</div>
        </div>
        <div className={chart.chFoot}>
          <Pills genres={item.genres} tone={tone.text} onTone={tone.onSolid} limit={3} small inline twoOnPhone />
          <FollowCircle item={item} variant="chart" />
        </div>
      </div>
    </li>
  )
})

const TableRowItem = memo(function TableRowItem({
  item,
  rank,
  eager,
}: {
  item: SummaryMediaData
  rank: number | null
  eager: boolean
}) {
  const hue = item.hue ?? item.posterTones?.hue ?? null
  const tone = toneLadder(hue)
  const sy = seasonYearText(item)
  const pop = formatPopularity(item.popularity)
  const rowContent = (
    <div className={rows.row} style={rank === null ? ({ color: tone.text } as CSSProperties) : undefined}>
      <MediaEntryLink
        id={item.anilistId}
        className={rows.rowCover}
        style={{ background: tone.fill }}
        tabIndex={-1}
        aria-hidden="true"
      >
        <CoverImage src={item.cover ?? null} className={rows.rowImg} eager={eager} />
      </MediaEntryLink>
      <div className={rows.rowMain}>
        <MediaEntryLink id={item.anilistId} className={rows.rowTitle}>
          {rank !== null ? <span className="visually-hidden">{`第 ${rank} 名 `}</span> : null}
          {item.title}
        </MediaEntryLink>
        {rank !== null ? <div className={rows.rowSub}>{[sy, infoLine(item)].filter(Boolean).join(' · ')}</div> : null}
        <Pills
          genres={item.genres}
          tone={tone.text}
          onTone={tone.onSolid}
          limit={3}
          small
          inline
          className={rows.rowPills}
        />
      </div>
      <div className={rows.col} data-col="score">
        <div className={rows.colMain}>
          <Score score={item.score} placeholder="—" />
        </div>
        <div className={rows.colSub}>{pop || (item.bangumiScore ? `BGM ${item.bangumiScore.toFixed(1)}` : '')}</div>
      </div>
      <div className={rows.col} data-col="format">
        <div className={rows.colMain}>{item.format ? FORMAT_LABELS[item.format] ?? item.format : '—'}</div>
        <div className={rows.colSub}>
          {item.episodes ? `${item.episodes} 集` : item.duration ? `${item.duration} 分钟` : ''}
        </div>
      </div>
      <div className={rows.col} data-col="status">
        <div className={rows.colMain}>{item.status ? STATUS_LABELS[item.status] ?? item.status : '—'}</div>
        <div className={rows.colSub}>{sy}</div>
      </div>
    </div>
  )

  if (rank === null) {
    return <li>{rowContent}</li>
  }

  return (
    <li className={rows.ranked} style={{ color: tone.text } as CSSProperties}>
      <span className={rows.rankNum} aria-hidden="true">
        <i>#</i>
        {rank}
      </span>
      {rowContent}
    </li>
  )
})

/** 统一的三视图结果渲染：封面网格（6列/排行5列）、图表卡（双列）、表格行（排行带左侧 #1 大名次列） */
export function BrowseResults({
  items,
  view,
  ranked = false,
}: {
  items: readonly SummaryMediaData[]
  view: BrowseViewMode
  ranked?: boolean
}) {
  return (
    <div className={cards.results}>
      {view === 'grid' ? (
        <ol className={ranked ? cards.grid5 : cards.grid}>
          {items.map((item, idx) => (
            <CoverCardItem key={item.anilistId} item={item} rank={ranked ? item.rank || idx + 1 : null} eager={idx < 12} />
          ))}
        </ol>
      ) : view === 'table' ? (
        <ol className={rows.rows}>
          {items.map((item, idx) => (
            <TableRowItem key={item.anilistId} item={item} rank={ranked ? item.rank || idx + 1 : null} eager={idx < 10} />
          ))}
        </ol>
      ) : (
        <ol className={chart.charts}>
          {items.map((item, idx) => (
            <ChartCardItem key={item.anilistId} item={item} rank={ranked ? item.rank || idx + 1 : null} eager={idx < 6} />
          ))}
        </ol>
      )}
    </div>
  )
}

export function buildHueOptions(items: readonly SummaryMediaData[], hueFamilyOfFn: (h: number | null) => HueFamilyKey | null): HueOption[] {
  const counts = new Map<HueFamilyKey, number>()
  for (const item of items) {
    const key = hueFamilyOfFn(item.hue ?? item.posterTones?.hue ?? null)
    if (key) counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  return HUE_FAMILIES.filter((f) => counts.has(f.key)).map((f) => ({
    key: f.key,
    hue: f.hue,
    label: f.label,
    count: counts.get(f.key) ?? 0,
  }))
}
