import { useRef, useState } from 'react'
import type { MouseEvent } from 'react'
import { useRouter } from '@tanstack/react-router'
import type { LibraryCluster, LibraryItem } from '../../lib/endpoints'
import { inFlight } from '../../lib/busy'
import { isBackdropEvent } from '../../lib/dialog'
import { formatBytes, formatEpisode, progressPercent } from '../../lib/format'
import type { LibraryVersion } from '../../lib/librarySeries'
import { EpisodeArtwork } from '../media/EpisodeArtwork'
import type { EpisodeMetadata, MediaSummary } from '../media/types'
import { Icon } from '../ui/Icon'
import { buildEpisodeList } from './episodeRows'
import type { EpisodeRowData } from './episodeRows'
import './episode-list.css'

export type EpisodeViewMode = 'list' | 'grid'

type Props = {
  /** 本地剧集（合并后的几个版本，或单个分组）；本地没有这部作品的文件时缺省 */
  cluster?: LibraryCluster
  /** 同一部作品有多个版本时：主版本 fileId → 同一集的其他版本（列在那一集下面） */
  versions?: Map<string, LibraryVersion[]>
  /** 目录作品：有它才有逐集标题，也才知道哪些集「库里没有」 */
  catalog?: MediaSummary
  /** 接下来该看的那一集 */
  next?: LibraryItem
  /**
   * 媒体库里有本地作品、只是还没选出（或认定）哪一个是这部作品：此时说不准哪一集「库里没有」，
   * 目录里的集照常列出，但不标「库里没有」
   */
  unlinked?: boolean
  pending: boolean
  activeFileId?: string
  /** 视图模式：默认列表态（双列左图右文），可切换为宽图网格态 */
  view?: EpisodeViewMode
  onPlay: (item: LibraryItem) => void
}

/**
 * 作品页的集数列表：统一为双列剧照卡（默认列表态左图右文，可切宽图网格），
 * 本地状态叠在剧照与文案上（DESIGN.md「作品页」）：已看 ✓ / 看到一半的进度条 / 版本数 / 库里没有的集置灰。
 * 采用铺满卡片的语义按钮（.ep-card-hit::after）与 z-index 分层，避免嵌套按钮。
 */
export function EpisodeList({ cluster, versions, catalog, next, unlinked = false, pending, activeFileId, view = 'list', onPlay }: Props) {
  const { sections, extras, hasPlayable } = buildEpisodeList(cluster, catalog)
  const dialog = useRef<HTMLDialogElement>(null)
  const downOnBackdrop = useRef(false)
  /** 打开「本集资料」的那个按钮：关掉后把焦点还给它（Safari 里点按钮不聚焦，浏览器自己的归还会落空） */
  const opener = useRef<HTMLElement | null>(null)
  const [details, setDetails] = useState<EpisodeMetadata>()
  const showDetails = (meta: EpisodeMetadata, from: HTMLElement) => {
    opener.current = from
    setDetails(meta)
    dialog.current?.showModal()
  }
  const rowProps = { cluster, catalog, versions, next, unlinked, pending, activeFileId, onPlay, onDetails: showDetails }
  const many = sections.length > 1
  const gridClass = view === 'grid' ? 'ep-grid' : 'ep-grid ep-grid--list'

  return (
    <div className="epl-list-wrap">
      {sections.map((section) => (
        <section key={section.key} className="epl-section" aria-label={many ? section.label : undefined}>
          {many && <h3 className="epl-section-title">{section.label}<span>{section.rows.length}</span></h3>}
          <ul className={gridClass}>
            {section.rows.map((row) => <EpisodeCard key={rowKey(row)} row={row} {...rowProps} />)}
          </ul>
        </section>
      ))}
      {extras.length > 0 && (
        <>
          {!hasPlayable && <p className="epl-note">这里只有附加内容（特典、片头片尾等），没有正片。</p>}
          <details className="epl-extras">
            <summary>{hasPlayable ? '附加内容' : '显示附加内容'}<span>{extras.length}</span></summary>
            <ul className="epl-extras-list">
              {extras.map((item) => (
                <ExtraRow
                  key={item.fileId}
                  item={item}
                  versions={versions}
                  pending={pending}
                  activeFileId={activeFileId}
                  onPlay={onPlay}
                />
              ))}
            </ul>
          </details>
        </>
      )}
      <dialog
        ref={dialog}
        className="epl-details"
        aria-label="本集资料"
        onClose={() => {
          if (opener.current?.isConnected) opener.current.focus()
          opener.current = null
        }}
        onPointerDown={(event) => { downOnBackdrop.current = dialog.current !== null && isBackdropEvent(dialog.current, event) }}
        onClick={(event) => {
          if (downOnBackdrop.current && dialog.current && isBackdropEvent(dialog.current, event)) dialog.current.close()
          downOnBackdrop.current = false
        }}
      >
        <button type="button" className="icon-button" aria-label="关闭本集资料" onClick={() => dialog.current?.close()}><Icon name="close" /></button>
        {details && (
          <>
            <h2>第 {details.episode} 集{details.title ? ` · ${details.title}` : ''}</h2>
            <p>{details.description || '暂无本集简介。'}</p>
            {details.airDate && <p className="epl-details-date">播出日期：{details.airDate}</p>}
          </>
        )}
      </dialog>
    </div>
  )
}

const rowKey = (row: EpisodeRowData) => (row.kind === 'local' ? row.item.fileId : `missing-${row.meta.episode}`)

type CardProps = {
  row: EpisodeRowData
  cluster?: LibraryCluster
  catalog?: MediaSummary
  versions?: Map<string, LibraryVersion[]>
  next?: LibraryItem
  unlinked: boolean
  pending: boolean
  activeFileId?: string
  onPlay: (item: LibraryItem) => void
  onDetails: (meta: EpisodeMetadata, opener: HTMLElement) => void
}

function EpisodeCard({ row, cluster, catalog, versions, next, unlinked, pending, activeFileId, onPlay, onDetails }: CardProps) {
  if (row.kind === 'missing') {
    return <MissingCard meta={row.meta} cluster={cluster} catalog={catalog} unlinked={unlinked} onDetails={onDetails} />
  }
  const { item, meta } = row
  const others = versions?.get(item.fileId) ?? []
  const playing = activeFileId !== undefined && (item.fileId === activeFileId || others.some((alt) => alt.item.fileId === activeFileId))
  const progress = item.progress
  const watched = progress?.completed === true
  const percent = progress && !watched && progress.positionSec > 0 ? progressPercent(progress.positionSec, progress.durationSec) : null
  const isNext = next?.fileId === item.fileId && !playing
  const name = episodeName(item, false)
  const title = meta?.title || (item.episode === null ? item.fileName : name)
  const desc = meta?.description || (title !== item.fileName ? item.fileName : undefined)
  const durationMin = episodeDuration(meta, catalog, progress?.durationSec)
  const spec = [item.group, item.resolution, formatBytes(item.sizeBytes), others.length > 0 ? `${others.length + 1} 版本` : null]
    .filter(Boolean).join(' · ')
  const state = playing ? '正在播放' : watched ? '已看完' : percent !== null ? `看到 ${percent}%` : isNext ? '接下来' : '未看'

  const cardClass = [
    'ep-card',
    isNext ? 'ep-card--next' : '',
    percent !== null ? 'ep-card--progress' : '',
    playing ? 'ep-card--selected' : '',
  ].filter(Boolean).join(' ')

  const hasActions = others.length > 0 || Boolean(item.stream) || Boolean(meta?.description)

  return (
    <li className={cardClass} title={item.fileName}>
      <span className="ep-still">
        <EpisodeArtwork image={meta?.image} banner={catalog?.banner} cover={catalog?.cover ?? cluster?.cover} />
        <span className="ep-still-play" aria-hidden="true">
          <span className="ep-still-play-btn">
            <Icon name="play" size={15} style={{ fill: 'currentColor', strokeWidth: 0 }} />
          </span>
        </span>
        {playing ? (
          <span className="ep-still-badge ep-still-badge--playing" aria-hidden="true">正在播放</span>
        ) : watched ? (
          <span className="ep-still-badge ep-still-badge--done" aria-hidden="true"><Icon name="check" size={11} />已看</span>
        ) : percent !== null ? (
          <span className="ep-still-badge ep-still-badge--progress" aria-hidden="true">看到 {percent}%</span>
        ) : null}
        {durationMin !== null && <span className="ep-still-dur" aria-hidden="true">{durationMin}m</span>}
        {percent !== null && <span className="ep-still-progress" aria-hidden="true"><span style={{ width: `${percent}%` }} /></span>}
      </span>
      <div className="ep-copy">
        <span className="ep-head-line">
          <span className="ep-num" aria-hidden="true">{item.episode === null ? '—' : `EP ${formatEpisode(item.episode)}`}</span>
          {playing ? (
            <span className="ep-head-state ep-head-state--playing" aria-hidden="true">播放中</span>
          ) : isNext ? (
            <span className="ep-head-state ep-head-state--next" aria-hidden="true">接下来</span>
          ) : null}
        </span>
        <button type="button" className="ep-card-hit" {...inFlight(pending, () => onPlay(item))}>
          <span className="visually-hidden">播放{name}：</span>
          <span className="ep-title">{title}</span>
          <span className="visually-hidden">（{state}）</span>
        </button>
        {desc && <span className="ep-desc">{desc}</span>}
      </div>
      {(spec || hasActions) && (
        <div className="ep-local-foot">
          {spec && <span className="ep-local-spec">{spec}</span>}
          {hasActions && (
            <span className="ep-local-actions">
              {others.length > 0 && (
                <span className="epl-versions" role="group" aria-label={`${name}的其他版本`}>
                  {others.map((alt) => (
                    <button type="button" key={alt.item.fileId} className="link ep-foot-link" title={alt.item.fileName} {...inFlight(pending, () => onPlay(alt.item))}>
                      <span className="visually-hidden">播放{name}的其他版本：</span>{alt.label}
                      {alt.item.progress?.completed && <><span aria-hidden="true"> ✓</span><span className="visually-hidden">（已看完）</span></>}
                    </button>
                  ))}
                </span>
              )}
              {item.stream && <WatchLink fileId={item.fileId} label={name} />}
              {meta?.description && <DetailsButton meta={meta} onDetails={onDetails} />}
            </span>
          )}
        </div>
      )}
    </li>
  )
}

/** 目录里已经播出、媒体库里没有文件的一集：置灰，不能点。还没关联本地作品时说不准有没有，不标「库里没有」 */
function MissingCard({ meta, cluster, catalog, unlinked, onDetails }: {
  meta: EpisodeMetadata
  cluster?: LibraryCluster
  catalog?: MediaSummary
  unlinked: boolean
  onDetails: (meta: EpisodeMetadata, opener: HTMLElement) => void
}) {
  const durationMin = episodeDuration(meta, catalog)
  return (
    <li className="ep-card ep-card--missing">
      <span className="ep-still">
        <EpisodeArtwork image={meta.image} banner={catalog?.banner} cover={catalog?.cover ?? cluster?.cover} />
        {!unlinked && <span className="ep-still-badge ep-still-badge--missing">库里没有</span>}
        {durationMin !== null && <span className="ep-still-dur" aria-hidden="true">{durationMin}m</span>}
      </span>
      <div className="ep-copy">
        <span className="ep-head-line">
          <span className="ep-num">EP {formatEpisode(meta.episode)}</span>
        </span>
        <span className="ep-title">{meta.title || `第 ${meta.episode} 集`}</span>
        {meta.description && <span className="ep-desc">{meta.description}</span>}
      </div>
      {(meta.airDate || meta.description) && (
        <div className="ep-local-foot">
          {meta.airDate && <span className="ep-local-spec"><time dateTime={meta.airDate}>播出于 {meta.airDate.replaceAll('-', '/')}</time></span>}
          {meta.description && (
            <span className="ep-local-actions">
              <DetailsButton meta={meta} onDetails={onDetails} />
            </span>
          )}
        </div>
      )}
    </li>
  )
}

/** 附加内容（特典、片头片尾、菜单等）：收在折叠区里的紧凑双列行 */
function ExtraRow({ item, versions, pending, activeFileId, onPlay }: {
  item: LibraryItem
  versions?: Map<string, LibraryVersion[]>
  pending: boolean
  activeFileId?: string
  onPlay: (item: LibraryItem) => void
}) {
  const others = versions?.get(item.fileId) ?? []
  const playing = activeFileId !== undefined && (item.fileId === activeFileId || others.some((alt) => alt.item.fileId === activeFileId))
  const watched = item.progress?.completed === true
  const name = episodeName(item, true)
  const meta = [playing ? '正在播放' : watched ? '已看完' : null, item.resolution, formatBytes(item.sizeBytes)]
    .filter(Boolean).join(' · ')
  return (
    <li className={playing ? 'epl-extra-item epl-extra-item--selected' : 'epl-extra-item'} title={item.fileName}>
      <button type="button" className="epl-extra-row" {...inFlight(pending, () => onPlay(item))}>
        <span className="visually-hidden">播放{name}：</span>
        {item.kind && <span className="badge">{item.kind.toUpperCase()}</span>}
        <span className="epl-extra-name">{item.fileName}</span>
        {meta && <small>{meta}</small>}
      </button>
      {(others.length > 0 || item.stream) && (
        <div className="epl-extra-actions">
          {others.length > 0 && (
            <span className="epl-versions" role="group" aria-label={`${name}的其他版本`}>
              {others.map((alt) => (
                <button type="button" key={alt.item.fileId} className="link ep-foot-link" title={alt.item.fileName} {...inFlight(pending, () => onPlay(alt.item))}>
                  <span className="visually-hidden">播放{name}的其他版本：</span>{alt.label}
                  {alt.item.progress?.completed && <><span aria-hidden="true"> ✓</span><span className="visually-hidden">（已看完）</span></>}
                </button>
              ))}
            </span>
          )}
          {item.stream && <WatchLink fileId={item.fileId} label={name} />}
        </div>
      )}
    </li>
  )
}

function DetailsButton({ meta, onDetails }: { meta: EpisodeMetadata; onDetails: (meta: EpisodeMetadata, opener: HTMLElement) => void }) {
  return (
    <button type="button" className="link ep-foot-link" onClick={(event) => onDetails(meta, event.currentTarget)}>
      <Icon name="info" size={13} />本集资料<span className="visually-hidden">：第 {meta.episode} 集</span>
    </button>
  )
}

/** 浏览器内播放（补充路径：不转码、没有弹幕与字幕）。在路由里走客户端导航，不在时退回普通链接 */
function WatchLink({ fileId, label }: { fileId: string; label: string }) {
  const router = useRouter({ warn: false })
  const href = `/watch/${encodeURIComponent(fileId)}`
  const navigate = (event: MouseEvent<HTMLAnchorElement>) => {
    if (!router || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()
    void router.navigate({ to: '/watch/$fileId', params: { fileId } })
  }
  return (
    <a className="link ep-foot-link" href={href} title="在浏览器里播放" onClick={navigate}>
      <Icon name="monitor" size={13} /><span className="visually-hidden">在</span>浏览器<span className="visually-hidden">里播放：{label}</span>
    </a>
  )
}

function episodeDuration(meta?: EpisodeMetadata, catalog?: MediaSummary, durationSec?: number): number | null {
  if (meta?.duration && meta.duration > 0) return Math.round(meta.duration)
  if (durationSec && durationSec > 0) return Math.max(1, Math.round(durationSec / 60))
  if (catalog?.duration && catalog.duration > 0) return Math.round(catalog.duration)
  return null
}

function episodeName(item: LibraryItem, extra: boolean): string {
  if (extra || item.episode === null) return ` ${item.fileName}`
  return `第 ${formatEpisode(item.episode)} 集`
}
