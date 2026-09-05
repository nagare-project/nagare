import { useEffect, useId, useRef, useState } from 'react'
import { fetchLibrary, playFile } from '../../lib/endpoints'
import type { LibraryCluster, LibraryData, LibraryItem } from '../../lib/endpoints'
import { errorText, formatEpisode } from '../../lib/format'
import { Icon } from '../ui/Icon'
import type { MediaSummary } from './types'
import './media-play.css'

const associationKey = (id: number) => `nagare:media-library:${id}`
function readAssociation(id: number): string | null {
  try { return localStorage.getItem(associationKey(id)) } catch { return null }
}

/** 只从实际库文件和进度选下一集，演示榜单的 watched 不参与播放。 */
export function nextLibraryEpisode(cluster: LibraryCluster): LibraryItem | undefined {
  const items = cluster.groups.flatMap(group => group.items)
  const episodes = items.filter(item => item.kind === 'main' || item.kind === 'movie' || item.kind === '')
    .sort((a, b) => (a.episode ?? Infinity) - (b.episode ?? Infinity))
  return episodes.find(item => item.progress && item.progress.positionSec > 0 && !item.progress.completed)
    ?? episodes.find(item => !item.progress?.completed) ?? episodes[0]
}

/** 首次由用户选择本地作品；成功播放后记住关联，之后直接播放实际下一集。 */
export function MediaPlayButton({ media, onOpenChange }: { media: MediaSummary; onOpenChange: (open: boolean) => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const requestVersion = useRef(0)
  const playing = useRef(false)
  const headingId = useId()
  const [library, setLibrary] = useState<LibraryData | null>(null)
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [query, setQuery] = useState(media.title)
  const [loading, setLoading] = useState(false)
  const [pending, setPending] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [launched, setLaunched] = useState(false)
  const [resume, setResume] = useState(false)
  useEffect(() => () => { requestVersion.current++ }, [])

  async function start(cluster: LibraryCluster, item: LibraryItem, version = requestVersion.current) {
    if (playing.current) return
    playing.current = true
    setPending(item.fileId)
    setError(null)
    setLaunched(false)
    try {
      await playFile(item.fileId)
      // 关联来自用户选择的真实文件；不按 AniList ID 或相似标题猜测。
      try { localStorage.setItem(associationKey(media.id), cluster.clusterKey) } catch { /* 禁用存储时仍可播放 */ }
      if (version !== requestVersion.current) return
      setResume(true)
      setLaunched(true)
    } catch (err) {
      if (version === requestVersion.current) setError(errorText(err, '播放失败'))
    } finally {
      playing.current = false
      if (version === requestVersion.current) setPending(null)
    }
  }

  async function load(autoplay: boolean) {
    const version = ++requestVersion.current
    setLoading(true)
    setError(null)
    setLaunched(false)
    setLibrary(null)
    setSelectedKey(null)
    try {
      const data = await fetchLibrary()
      if (version !== requestVersion.current) return
      setLibrary(data)
      const saved = readAssociation(media.id)
      const cluster = data.clusters.find(item => item.clusterKey === saved)
      if (cluster) {
        setSelectedKey(cluster.clusterKey)
        const next = nextLibraryEpisode(cluster)
        setResume(!!next?.progress?.positionSec && !next.progress.completed)
        if (autoplay && next) await start(cluster, next, version)
      } else if (saved) {
        setQuery('')
        setError('之前选择的本地作品已不在媒体库中，请重新选择。')
      }
    } catch (err) {
      if (version === requestVersion.current) setError(errorText(err, '读取媒体库失败'))
    } finally {
      if (version === requestVersion.current) setLoading(false)
    }
  }

  const selected = library?.clusters.find(cluster => cluster.clusterKey === selectedKey)
  const next = selected ? nextLibraryEpisode(selected) : undefined
  const visible = library?.clusters.filter(cluster => cluster.title.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())) ?? []
  const busy = loading || pending !== null

  return <>
    <button type="button" className="discover-card-play" ref={trigger} aria-label={`${resume ? '继续观看' : '播放'} ${media.title}`}
      onClick={() => {
        if (playing.current) return
        dialog.current?.showModal()
        onOpenChange(true)
        setQuery(media.title)
        void load(true)
      }}><Icon name="play" size={24} style={{ fill: 'currentColor', strokeWidth: 0 }} />{resume ? '继续观看' : '播放'}</button>
    <dialog className="media-play-dialog" ref={dialog} aria-labelledby={headingId}
      onClose={() => { requestVersion.current++; setPending(null); trigger.current?.focus(); onOpenChange(false) }}
      onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <div className="media-play-body">
        <button type="button" className="icon-button media-play-close" aria-label="关闭播放选集" onClick={() => dialog.current?.close()}><Icon name="close" /></button>
        <p className="media-play-kicker">本地播放</p>
        <h2 id={headingId}>{media.title}</h2>
        {error && <p className="result result--err" role="alert">{error}</p>}
        {loading && !pending && <p className="result result--dim" role="status">正在读取媒体库…</p>}
        {pending && <p className="result result--dim" role="status">正在启动播放器…</p>}
        {launched && <p className="result result--ok" role="status">已交给 mpv 播放。</p>}
        {!library && !loading && <button type="button" className="btn" onClick={() => void load(false)}>重新读取</button>}
        {library && !selected && <>
          <p className="media-play-hint">选择这部作品在媒体库中的版本，成功播放后会记住你的选择。</p>
          <div className="media-play-search">
            <input type="search" className="input" aria-label="查找本地作品" placeholder="输入本地作品名…" value={query} onChange={event => setQuery(event.target.value)} />
            {query && <button type="button" className="btn btn--sm" onClick={() => setQuery('')}>显示全部</button>}
          </div>
          {visible.length ? <ul className="media-play-choices">{visible.map(cluster => <li key={cluster.clusterKey}>
            <button type="button" className="media-play-choice" onClick={() => { setSelectedKey(cluster.clusterKey); setError(null) }}>
              <Icon name="folder" size={20} /><span>{cluster.title}<small>{cluster.season === null ? '' : `第 ${cluster.season} 季 · `}{cluster.episodeCount} 集</small></span><Icon name="right" size={18} />
            </button>
          </li>)}</ul> : <p className="media-play-hint" role="status">{library.clusters.length ? '没有找到同名的本地作品。可以显示全部，按文件夹中的名称选择。' : '媒体库里还没有视频，请先导入本地文件。'}</p>}
          <a className="link" href="/">管理媒体库</a>
        </>}
        {selected && <>
          <div className="media-play-selection"><h3>{selected.title}</h3><button type="button" className="btn btn--sm" disabled={busy} onClick={() => { setSelectedKey(null); setQuery(''); setLaunched(false) }}>更换作品</button></div>
          {next && <button type="button" className="btn btn--primary" disabled={busy} onClick={() => void start(selected, next)}>
            <Icon name="play" size={18} />{next.progress?.positionSec && !next.progress.completed ? '继续观看' : '播放'} {next.episode === null ? next.fileName : `第 ${formatEpisode(next.episode)} 集`}
          </button>}
          {selected.groups.map(group => <section className="media-play-group" key={group.groupKey}>
            <h4>{group.label || '剧集'}</h4><ul className="media-play-choices">{group.items.map(item => <li key={item.fileId}>
              <button type="button" className="media-play-episode" disabled={busy} onClick={() => void start(selected, item)} aria-label={`播放 ${item.fileName}`}>
                <span>{item.episode === null ? '—' : formatEpisode(item.episode)}</span><span>{item.fileName}</span><Icon name="play" size={18} />
              </button>
            </li>)}</ul>
          </section>)}
          {!selected.groups.some(group => group.items.length) && <p className="media-play-hint" role="status">这部作品中没有可播放的文件，请更换作品或重新扫描媒体库。</p>}
        </>}
      </div>
    </dialog>
  </>
}
