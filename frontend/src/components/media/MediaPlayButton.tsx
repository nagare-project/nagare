import { MissingEpisodeCards } from '../library/MissingEpisodeCards'
import { LibraryEpisodeCards } from '../library/LibraryEpisodeCards'
import { displayGroups } from '../library/displayGroups'
import { PlaybackSurface } from './PlaybackSurface'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { ApiError } from '../../lib/api'
import { fetchLibrary, playFile, stopPlayer } from '../../lib/endpoints'
import type { LibraryCluster, LibraryData, LibraryItem } from '../../lib/endpoints'
import { clusterDisplayTitle, setAssociation } from '../../lib/associations'
import type { LibraryAssociation } from '../../lib/associations'
import { errorText, formatEpisode } from '../../lib/format'
import { Icon } from '../ui/Icon'
import { LibraryAnimeLink } from '../library/LibraryAnimeLink'
import { forgetLegacyChoice, isAssociatedWith, readLegacyChoice, resolveLocalCluster } from './localAssociation'
import type { LocalResolution } from './localAssociation'
import { mergeSeries } from '../../lib/librarySeries'
import type { MediaSummary } from './types'
import './media-play.css'

/** 只从实际库文件和进度选下一集，演示榜单的 watched 不参与播放。 */
export function nextLibraryEpisode(cluster: LibraryCluster): LibraryItem | undefined {
  const items = cluster.groups.flatMap(group => group.items)
  const episodes = items.filter(item => item.kind === 'main' || item.kind === 'movie' || item.kind === '')
    .sort((a, b) => (a.episode ?? Infinity) - (b.episode ?? Infinity))
  return episodes.find(item => item.progress && item.progress.positionSec > 0 && !item.progress.completed)
    ?? episodes.find(item => !item.progress?.completed) ?? episodes[0]
}

/**
 * 目录作品页的本地播放。本地作品分组与这部作品的对应关系存在后端（媒体库的作品关联）：
 * 认定过、或扫描后自动认出是这部作品的分组直接显示真实剧集 —— 同一部番的几个版本
 * （不同字幕组、不同文件夹）合并成一张剧集表，同一集的其他版本挂在那一集下面。
 * 都没有时由用户挑一个，点播放时一并认定。自动认出的只代选、不自动播放，播放时才算用户确认
 * （认定的是那一集所在的分组）。
 */
export function MediaPlayButton({ media, onOpenChange = () => {}, inline = false }: { media: MediaSummary; onOpenChange?: (open: boolean) => void; inline?: boolean }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const requestVersion = useRef(0)
  const playing = useRef(false)
  const headingId = useId()
  const [library, setLibrary] = useState<LibraryData | null>(null)
  /** 选中的本地分组（同一部作品的多个版本一起选中） */
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const [query, setQuery] = useState(media.title)
  const [loading, setLoading] = useState(false)
  const [pending, setPending] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [launched, setLaunched] = useState(false)
  const [resume, setResume] = useState(false)
  /** 不挡播放的提示（比如对应关系没能保存） */
  const [notice, setNotice] = useState<string | null>(null)
  const [via, setVia] = useState<LocalResolution['via']>()
  useEffect(() => () => { requestVersion.current++ }, [])

  /**
   * 认定 cluster 就是这部作品。只发请求、不碰界面状态（调用方核对过请求版本再改界面）。
   * 后端在这个分组有文件正在播放时会拒绝（409）：用户点了播放时（stopIfPlaying）先停掉再试一次，
   * 反正马上就要换成新的这一集；只是打开页面时的迁移不去动正在放的东西。
   */
  async function associate(cluster: LibraryCluster, stopIfPlaying: boolean): Promise<{ association: LibraryAssociation | null } | { problem: string }> {
    const send = () => setAssociation(cluster.clusterKey, { mode: 'manual', anilistId: media.id })
    let failure: unknown
    try {
      return { association: await send() }
    } catch (err) {
      failure = err
    }
    if (stopIfPlaying && failure instanceof ApiError && failure.status === 409) {
      try {
        await stopPlayer()
        return { association: await send() }
      } catch (err) {
        failure = err
      }
    }
    console.error('保存作品对应关系失败', failure)
    return { problem: errorText(failure, '保存失败') }
  }

  /** 认定成功：删掉本机旧记录，把列表里那一项换成已认定（在当前列表上改，不拿旧快照整个盖回去） */
  function markAssociated(clusterKey: string, association: LibraryAssociation | null) {
    forgetLegacyChoice(media.id)
    setLibrary(current => current && { ...current, clusters: current.clusters.map(c => c.clusterKey === clusterKey ? { ...c, association: association ?? undefined, matched: undefined } : c) })
    setVia('manual')
  }

  /** skipAssociate：这次加载里已经试过认定、失败了，别再等一轮 */
  async function start(cluster: LibraryCluster, item: LibraryItem, version = requestVersion.current, skipAssociate = false) {
    if (playing.current) return
    playing.current = true
    setPending(item.fileId)
    setError(null)
    setLaunched(false)
    try {
      // 在这部作品的页面上播一个还没认定的本地作品，就是在确认它们是同一部：先认定再播放，
      // 弹幕与「看完」才会记到这部作品上。已经对应了别的作品的不在这里改（界面上也选不到，
      // 这里再挡一次过期的列表）。认定失败不挡播放，但要说清这一集会怎样。
      if (cluster.association === undefined && !skipAssociate) {
        const result = await associate(cluster, true)
        // 对话框关了、换了作品：不再启动播放
        if (version !== requestVersion.current) return
        if ('problem' in result) {
          setNotice(`没能记住这部作品对应的本地文件夹（${result.problem}）。这一集仍按文件名自动匹配，弹幕和看完的集数可能记到别的作品上。`)
        } else {
          markAssociated(cluster.clusterKey, result.association)
          setNotice(null)
        }
      }
      await playFile(item.fileId)
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
    setSelectedKeys([])
    setNotice(null)
    setVia(undefined)
    try {
      const data = await fetchLibrary()
      if (version !== requestVersion.current) return
      const found = resolveLocalCluster(data, media.id, readLegacyChoice(media.id))
      if (found.dropLegacy) forgetLegacyChoice(media.id)
      let cluster = found.via === 'legacy' ? found.selected[0] : undefined
      let shown = data
      let migrationFailed = false
      if (cluster && found.via === 'legacy') {
        // 以前只记在这个浏览器里的选择：同步到后端。失败就先照旧用着，下次再试
        const result = await associate(cluster, false)
        if (version !== requestVersion.current) return
        if ('problem' in result) {
          migrationFailed = true
          setNotice(`以前在这个浏览器里选的本地作品还没能同步到媒体库（${result.problem}），下次打开时再试。`)
        } else {
          forgetLegacyChoice(media.id)
          const migrated: LibraryCluster = { ...cluster, association: result.association ?? undefined, matched: undefined }
          cluster = migrated
          shown = { ...data, clusters: data.clusters.map(c => c.clusterKey === migrated.clusterKey ? migrated : c) }
        }
      }
      // 迁移结束后才把列表摆出来：迁移期间列表可点的话，用户点了别的，选中项会在他手里跳回去
      setLibrary(shown)
      const keys = found.selected.map(c => c.clusterKey)
      const chosen = keys.map(key => shown.clusters.find(c => c.clusterKey === key)).filter((c): c is LibraryCluster => c !== undefined)
      if (chosen.length > 0) {
        setSelectedKeys(keys)
        setVia(found.via === 'legacy' && cluster?.association ? 'manual' : found.via)
        const merged = mergeSeries(chosen)
        const next = nextLibraryEpisode(merged.cluster)
        setResume(!!next?.progress?.positionSec && !next.progress.completed)
        // 自动认出的不算用户确认过：只代选，不自动播放。看的是下一集【所在的分组】认定过没有 ——
        // 认定过的版本旁边多了个自动认出的版本，不该让一键续播失效
        const owner = next ? merged.owner.get(next.fileId) ?? chosen[0]! : undefined
        if (autoplay && next && owner && (found.via === 'legacy' || isAssociatedWith(owner, media.id))) await start(owner, next, version, migrationFailed)
      } else if (found.legacyMissing) {
        setQuery('')
        setError('之前选择的本地作品已不在媒体库中，请重新选择。')
      }
    } catch (err) {
      if (version === requestVersion.current) setError(errorText(err, '读取媒体库失败'))
    } finally {
      if (version === requestVersion.current) setLoading(false)
    }
  }

  useEffect(() => {
    if (inline) void load(false)
  }, [inline, media.id])

  const chosen = useMemo(() => selectedKeys.map(key => library?.clusters.find(c => c.clusterKey === key)).filter((c): c is LibraryCluster => c !== undefined), [library, selectedKeys])
  const merged = useMemo(() => (chosen.length > 0 ? mergeSeries(chosen) : undefined), [chosen])
  const selected = merged?.cluster
  /** 播放某个文件时要认定的是它所在的分组（合并显示时不一定是第一个） */
  const ownerOf = (item: LibraryItem) => merged?.owner.get(item.fileId) ?? chosen[0]!
  // 选中的里面有自动认出、还没确认的分组（用户自己从列表里挑的不算「自动认出」）
  const autoMatched = chosen.some(c => c.association === undefined && c.matched?.anilistId === media.id)
  const next = selected ? nextLibraryEpisode(selected) : undefined
  const q = query.trim().toLocaleLowerCase()
  // 认定为这部作品的排在前面；文件夹名与认定的作品名都能搜到
  const visible = (library?.clusters ?? [])
    .filter(cluster => [cluster.title, clusterDisplayTitle(cluster)].some(t => t.toLocaleLowerCase().includes(q)))
    .sort((a, b) => Number(isAssociatedWith(b, media.id)) - Number(isAssociatedWith(a, media.id)))
  const busy = loading || pending !== null

  return <>
    {!inline && <button type="button" className="discover-card-play" ref={trigger} aria-label={`${resume ? '继续观看' : '播放'} ${media.title}`}
      onClick={() => {
        if (playing.current) return
        dialog.current?.showModal()
        onOpenChange(true)
        setQuery(media.title)
        void load(true)
      }}><Icon name="play" size={24} style={{ fill: 'currentColor', strokeWidth: 0 }} />{resume ? '继续观看' : '播放'}</button>}
    <PlaybackSurface inline={inline} className="media-play-dialog" ref={dialog} aria-labelledby={!inline || selected ? headingId : undefined} aria-label={inline && !selected ? '本地媒体库' : undefined}
      onClose={() => { requestVersion.current++; setPending(null); trigger.current?.focus(); onOpenChange(false) }}
      onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <div className="media-play-body">
        {!inline && <button type="button" className="icon-button media-play-close" aria-label="关闭播放选集" onClick={() => dialog.current?.close()}><Icon name="close" /></button>}
        <p className="media-play-kicker">本地播放</p>
        {(!inline || selected) && <h2 id={headingId}>{inline ? '本地剧集' : media.title}</h2>}
        {error && <p className="result result--err" role="alert">{error}</p>}
        {loading && !pending && <p className="result result--dim" role="status">正在读取媒体库…</p>}
        {pending && <p className="result result--dim" role="status">正在启动播放器…</p>}
        {launched && <p className="result result--ok" role="status">已交给 mpv 播放。</p>}
        {notice && <p className="result result--warn" role="status">{notice}</p>}
        {!library && !loading && <button type="button" className="btn" onClick={() => void load(false)}>重新读取</button>}

        {library && !selected && (!inline || library.clusters.length > 0) && <>
          <p className="media-play-hint">选择这部作品在媒体库中的版本，点播放时会记住你的选择（之后也可以在媒体库的作品页更改）。</p>
          <div className="media-play-search">
            <input type="search" className="input" aria-label="查找本地作品" placeholder="输入本地作品名…" value={query} onChange={event => setQuery(event.target.value)} />
            {query && <button type="button" className="btn btn--sm" onClick={() => setQuery('')}>显示全部</button>}
          </div>
          {visible.length ? <ul className="media-play-choices">{visible.map(cluster => <li key={cluster.clusterKey}>
            <LocalChoice cluster={cluster} mediaId={media.id} onChoose={() => { setSelectedKeys([cluster.clusterKey]); setVia(isAssociatedWith(cluster, media.id) ? 'manual' : undefined); setError(null); setNotice(null) }} />
          </li>)}</ul> : <p className="media-play-hint" role="status">{library.clusters.length ? '没有找到同名的本地作品。可以显示全部，按文件夹中的名称选择。' : '媒体库里还没有视频，请先导入本地文件。'}</p>}
          <a className="link" href="/">管理媒体库</a>
        </>}
        {selected && <>
          <div className="media-play-selection">
            <div className="media-play-selection-title"><h3>{clusterDisplayTitle(selected)}</h3>{autoMatched && via !== 'legacy' && <p className="media-play-via">自动认出的本地作品，播放时会记住</p>}</div>
            <LibraryAnimeLink clusterKey={selected.clusterKey} className="btn btn--sm">在媒体库中打开</LibraryAnimeLink>
            <button type="button" className="btn btn--sm" disabled={busy} onClick={() => { setSelectedKeys([]); setQuery(''); setLaunched(false); setNotice(null) }}>更换作品</button>
          </div>
          {chosen.length > 1 && <p className="media-play-versions">本地有 {chosen.length} 个版本，同一集的其他版本列在那一集下面：{chosen.map((c, i) => <span key={c.clusterKey}>{i > 0 && '、'}<LibraryAnimeLink clusterKey={c.clusterKey} className="link">{c.title}（{c.episodeCount} 集）</LibraryAnimeLink></span>)}</p>}
          {next && <button type="button" className="btn btn--primary" disabled={busy} onClick={() => void start(ownerOf(next), next)}>
            <Icon name="play" size={18} />{next.progress?.positionSec && !next.progress.completed ? '继续观看' : '播放'} {next.episode === null ? next.fileName : `第 ${formatEpisode(next.episode)} 集`}
          </button>}
          {inline ? <LibraryEpisodeCards cluster={selected} next={next} totalEpisodes={media.episodes} titles={media.episodeTitles} banner={media.banner} cover={media.cover} pending={busy} versions={merged?.versions} onPlay={item => void start(ownerOf(item), item)} /> : displayGroups(selected).map(group => <section className="media-play-group" key={group.groupKey}>
            <h4>{group.label || '剧集'}</h4><ul className="media-play-choices">{group.items.flatMap(item => [{ item, label: '' }, ...(merged?.versions.get(item.fileId) ?? [])]).map(({ item, label }) => <li key={item.fileId}>
              <button type="button" className="media-play-episode" disabled={busy} onClick={() => void start(ownerOf(item), item)} aria-label={`播放 ${item.fileName}`}>
                <span>{item.episode === null ? '—' : formatEpisode(item.episode)}</span><span>{label ? `其他版本（${label}）：${item.fileName}` : item.fileName}</span><Icon name="play" size={18} />
              </button>
            </li>)}</ul>
          </section>)}
          {!selected.groups.some(group => group.items.length) && <p className="media-play-hint" role="status">这部作品中没有可播放的文件，请更换作品或重新扫描媒体库。</p>}
        </>}
        {inline && library && <MissingEpisodeCards media={media} cluster={selected} unassociated={!selected && library.clusters.length > 0} />}
      </div>
    </PlaybackSurface>
  </>
}

/** 选本地作品的一项。已经对应了别的作品（或标为不是目录作品）的分组不在这里改，指到它的作品页去 */
function LocalChoice({ cluster, mediaId, onChoose }: { cluster: LibraryCluster; mediaId: number; onChoose: () => void }) {
  const meta = `${cluster.season === null ? '' : `第 ${cluster.season} 季 · `}${cluster.episodeCount} 集`
  const { association } = cluster
  const title = clusterDisplayTitle(cluster)
  // 认出了作品时标题是作品名：两个文件夹认成同一部会长得一模一样，附上文件夹原名才分得清
  const folder = title !== cluster.title ? `文件夹：${cluster.title} · ` : ''
  if (association && !isAssociatedWith(cluster, mediaId)) {
    const taken = association.mode === 'none' ? '已标为不是目录里的作品' : `已对应「${association.title || `作品 ${association.anilistId}`}」`
    return <div className="media-play-choice media-play-choice--taken">
      <Icon name="folder" size={20} /><span>{title}<small>{folder}{meta} · {taken}</small></span>
      <LibraryAnimeLink clusterKey={cluster.clusterKey} className="link" aria-label={`去「${title}」的作品页更改对应作品`}>去作品页更改</LibraryAnimeLink>
    </div>
  }
  // 没认过的：说出自动匹配到了谁 —— 同名文件夹分属不同季时，这是挑对的唯一线索
  const hint = isAssociatedWith(cluster, mediaId) ? '已对应这部作品'
    : cluster.matched?.anilistId === mediaId ? '自动匹配到这部作品'
    : cluster.matched ? `自动匹配到「${cluster.matched.title || `作品 ${cluster.matched.anilistId}`}」` : ''
  return <button type="button" className="media-play-choice" onClick={onChoose}>
    <Icon name="folder" size={20} /><span>{title}<small>{folder}{meta}{hint && ` · ${hint}`}</small></span><Icon name="right" size={18} />
  </button>
}
