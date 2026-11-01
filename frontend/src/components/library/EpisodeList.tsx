import { useRef, useState } from 'react'
import type { MouseEvent } from 'react'
import { useRouter } from '@tanstack/react-router'
import type { LibraryCluster, LibraryItem } from '../../lib/endpoints'
import { isBackdropEvent } from '../../lib/dialog'
import { formatBytes, formatEpisode, progressPercent } from '../../lib/format'
import type { LibraryVersion } from '../../lib/librarySeries'
import type { EpisodeMetadata, MediaSummary } from '../media/types'
import { Icon } from '../ui/Icon'
import { buildEpisodeList } from './episodeRows'
import type { EpisodeRowData } from './episodeRows'
import './episode-list.css'

type Props = {
  /** 本地剧集（合并后的几个版本，或单个分组）；本地没有这部作品的文件时缺省 */
  cluster?: LibraryCluster
  /** 同一部作品有多个版本时：主版本 fileId → 同一集的其他版本（列在那一集下面） */
  versions?: Map<string, LibraryVersion[]>
  /** 目录作品：有它才有逐集标题，也才知道哪些集「库里没有」 */
  catalog?: MediaSummary
  /** 接下来该看的那一集 */
  next?: LibraryItem
  pending: boolean
  activeFileId?: string
  onPlay: (item: LibraryItem) => void
}

/**
 * 作品页的集数列表：每集一行，本地状态叠在目录资料上（DESIGN.md「作品页」）——
 * 已看 ✓ / 看到一半的进度条 / 版本数 / 库里没有的集置灰。
 * 整行是一个按钮（点了就播），同一集的其他版本、浏览器内播放、本集资料是行下面各自的控件。
 */
export function EpisodeList({ cluster, versions, catalog, next, pending, activeFileId, onPlay }: Props) {
  const { sections, extras, hasPlayable } = buildEpisodeList(cluster, catalog)
  const dialog = useRef<HTMLDialogElement>(null)
  const downOnBackdrop = useRef(false)
  const [details, setDetails] = useState<EpisodeMetadata>()
  const showDetails = (meta: EpisodeMetadata) => {
    setDetails(meta)
    dialog.current?.showModal()
  }
  const rowProps = { versions, next, pending, activeFileId, onPlay, onDetails: showDetails }
  const many = sections.length > 1

  return (
    <div className="epl-list-wrap">
      {sections.map((section) => (
        <section key={section.key} className="epl-section" aria-label={many ? section.label : undefined}>
          {many && <h3 className="epl-section-title">{section.label}<span>{section.rows.length}</span></h3>}
          <ul className="epl-list">
            {section.rows.map((row) => <EpisodeRow key={rowKey(row)} row={row} {...rowProps} />)}
          </ul>
        </section>
      ))}
      {extras.length > 0 && (
        <>
          {!hasPlayable && <p className="epl-note">这里只有附加内容（特典、片头片尾等），没有正片。</p>}
          <details className="epl-extras">
            <summary>{hasPlayable ? '附加内容' : '显示附加内容'}<span>{extras.length}</span></summary>
            <ul className="epl-list">
              {extras.map((item) => <EpisodeRow key={item.fileId} row={{ kind: 'local', item }} {...rowProps} extra />)}
            </ul>
          </details>
        </>
      )}
      <dialog
        ref={dialog}
        className="media-description-dialog"
        aria-label="本集资料"
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

type RowProps = {
  row: EpisodeRowData
  versions?: Map<string, LibraryVersion[]>
  next?: LibraryItem
  pending: boolean
  activeFileId?: string
  onPlay: (item: LibraryItem) => void
  onDetails: (meta: EpisodeMetadata) => void
  /** 附加内容：不叫「第 N 集」，标出类型 */
  extra?: boolean
}

function EpisodeRow({ row, versions, next, pending, activeFileId, onPlay, onDetails, extra = false }: RowProps) {
  if (row.kind === 'missing') return <MissingRow meta={row.meta} onDetails={onDetails} />
  const { item, meta } = row
  const others = versions?.get(item.fileId) ?? []
  const playing = activeFileId !== undefined && (item.fileId === activeFileId || others.some((alt) => alt.item.fileId === activeFileId))
  const progress = item.progress
  const watched = progress?.completed === true
  const percent = progress && !watched && progress.positionSec > 0 ? progressPercent(progress.positionSec, progress.durationSec) : null
  const isNext = !extra && next?.fileId === item.fileId
  const name = episodeName(item, extra)
  const title = meta?.title || (item.episode === null || extra ? item.fileName : name)
  // 没有目录标题时，行标题只是「第 N 集」：文件名另起一行，本地分组页上靠它认文件
  const showFile = !meta?.title && item.episode !== null && !extra
  const sub = [extra && item.kind ? item.kind.toUpperCase() : null, item.group, item.resolution, formatBytes(item.sizeBytes), others.length > 0 ? `${others.length + 1} 个版本` : null]
    .filter(Boolean).join(' · ')
  const state = playing ? '正在播放' : watched ? '已看完' : percent !== null ? `看到 ${percent}%` : isNext ? '接下来' : '未看'
  const tone = playing || isNext ? 'info' : watched ? 'done' : 'plain'
  const dotClass = watched ? 'epl-dot epl-dot--done' : percent !== null ? 'epl-dot epl-dot--progress' : 'epl-dot'

  return (
    <li className={isNext ? 'epl-item epl-item--next' : 'epl-item'} title={item.fileName}>
      <button type="button" className="epl-item-main" disabled={pending} onClick={() => onPlay(item)}>
        {/* 名字从这里开始念：圆点与集号对读屏是装饰，集号已经在这句里了 */}
        <span className="visually-hidden">播放{name}：</span>
        <span className={dotClass} aria-hidden="true">
          {watched && <Icon name="check" size={12} />}
        </span>
        <span className="epl-no" aria-hidden="true">{extra || item.episode === null ? '—' : formatEpisode(item.episode)}</span>
        <span className="epl-text">
          <span className="epl-title">{title}</span>
          {showFile && <span className="epl-file">{item.fileName}</span>}
          <span className="epl-sub">{sub}</span>
        </span>
        <span className={`epl-state epl-state--${tone}`}>{state}</span>
        {percent !== null && <span className="epl-bar" aria-hidden="true"><span style={{ width: `${percent}%` }} /></span>}
      </button>
      {(others.length > 0 || item.stream || meta?.description) && (
        <div className="epl-item-extra">
          {others.length > 0 && (
            <div className="epl-versions" role="group" aria-label={`${name}的其他版本`}>
              <span aria-hidden="true">其他版本</span>
              {others.map((alt) => (
                <button type="button" key={alt.item.fileId} className="link" disabled={pending} title={alt.item.fileName} onClick={() => onPlay(alt.item)}>
                  <span className="visually-hidden">播放{name}的其他版本：</span>{alt.label}
                  {alt.item.progress?.completed && <><span aria-hidden="true"> ✓</span><span className="visually-hidden">（已看完）</span></>}
                </button>
              ))}
            </div>
          )}
          {item.stream && <WatchLink fileId={item.fileId} label={name} />}
          {meta?.description && <DetailsButton meta={meta} onDetails={onDetails} />}
        </div>
      )}
    </li>
  )
}

/** 目录里已经播出、媒体库里没有文件的一集：置灰，不能点 */
function MissingRow({ meta, onDetails }: { meta: EpisodeMetadata; onDetails: (meta: EpisodeMetadata) => void }) {
  return (
    <li className="epl-item epl-item--missing">
      <div className="epl-item-main">
        <span className="epl-dot epl-dot--missing" aria-hidden="true" />
        <span className="epl-no">{formatEpisode(meta.episode)}</span>
        <span className="epl-text">
          <span className="epl-title">{meta.title || `第 ${meta.episode} 集`}</span>
          {meta.airDate && <span className="epl-sub"><time dateTime={meta.airDate}>播出于 {meta.airDate.replaceAll('-', '/')}</time></span>}
        </span>
        <span className="epl-state epl-state--plain">库里没有</span>
      </div>
      {meta.description && <div className="epl-item-extra"><DetailsButton meta={meta} onDetails={onDetails} /></div>}
    </li>
  )
}

function DetailsButton({ meta, onDetails }: { meta: EpisodeMetadata; onDetails: (meta: EpisodeMetadata) => void }) {
  return (
    <button type="button" className="link epl-extra-link" onClick={() => onDetails(meta)}>
      <Icon name="info" size={14} />本集资料<span className="visually-hidden">：第 {meta.episode} 集</span>
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
    <a className="link epl-extra-link" href={href} onClick={navigate}>
      <Icon name="monitor" size={14} />在浏览器里播放<span className="visually-hidden">：{label}</span>
    </a>
  )
}

function episodeName(item: LibraryItem, extra: boolean): string {
  if (extra || item.episode === null) return ` ${item.fileName}`
  return `第 ${formatEpisode(item.episode)} 集`
}
