import { useEffect, useRef, useState } from 'react'
import { useLocation } from '@tanstack/react-router'
import { SourcesCard } from '../components/search/SourcesCard'
import { AboutCard } from '../components/settings/AboutCard'
import { CoverCacheCard } from '../components/settings/CoverCacheCard'
import { MpvCard } from '../components/settings/MpvCard'
import { PlaybackCard } from '../components/settings/PlaybackCard'
import { QuitCard, QuitNotice } from '../components/settings/QuitCard'
import { SourcePluginCard } from '../components/settings/SourcePluginCard'
import { TorrentCard } from '../components/settings/TorrentCard'
import { UpdateCard } from '../components/settings/UpdateCard'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useLibrary } from '../hooks/useLibrary'
import { useSelfUpdateContext } from '../hooks/useSelfUpdate'
import { useSourcePlugin } from '../hooks/useSourcePlugin'
import { useSettings } from '../hooks/useSettings'
import type { SettingsState } from '../hooks/useSettings'
import { useSources } from '../hooks/useSources'
import { useUpdateContext } from '../hooks/useUpdate'
import { formatVersion } from '../lib/format'
import { mono } from '../theme'
import { AccountPanel } from './settings/AccountPanel'
import { BackgroundPanel } from './settings/BackgroundPanel'
import { FoldersPanel } from './settings/FoldersPanel'
import { SettingsNav } from './settings/SettingsNav'
import { SettingsSection, sectionHeadingId } from './settings/SettingsSection'
import { TorrentShortcuts } from './settings/TorrentShortcuts'
import { SETTINGS_SECTIONS, sectionFromHash } from './settings/sections'
import type { SettingsSectionId, SettingsSectionItem } from './settings/sections'
import '../components/catalog/catalog.css'
import '../styles/panels.css'
import './settings/settings.css'

/** 按 id 取分区（表是静态的，取不到说明 id 写错了） */
function sectionItem(id: SettingsSectionId): SettingsSectionItem {
  const item = SETTINGS_SECTIONS.find((candidate) => candidate.id === id)
  if (item === undefined) throw new Error(`未知的设置分区：${id}`)
  return item
}

/**
 * `/settings` 设置页：分区写在地址的 #hash 里（安装引导、文件夹入口、搜索页可直达）。
 * 所有分区一直挂着、只用 hidden 切换，切走再回来时没提交的表单还在。
 */
export function SettingsPage() {
  const settings = useSettings()
  const library = useLibrary()
  const sources = useSources()
  const sourcePlugin = useSourcePlugin()
  const update = useUpdateContext()
  const selfUpdate = useSelfUpdateContext()
  const hash = useLocation({ select: (location) => location.hash })
  const current = sectionFromHash(hash)
  const [hasQuit, setHasQuit] = useState(false)
  // 用户从导航点进来的分区：切过去之后把焦点放到它的标题上，读屏念出换到了哪一区，下一下 Tab 就进到内容里
  const pendingFocus = useRef<SettingsSectionId | null>(null)

  useEffect(() => {
    // 分区一换就作废：点了没切过去（被拦下）的那一下，不能留到以后别的路径切回来时再抢焦点
    const wanted = pendingFocus.current
    pendingFocus.current = null
    if (wanted !== current.id) return
    document.getElementById(sectionHeadingId(current.id))?.focus()
  }, [current.id])

  function handleNavigate(id: SettingsSectionId): void {
    if (id === current.id) {
      document.getElementById(sectionHeadingId(id))?.focus()
      return
    }
    pendingFocus.current = id
  }

  if (hasQuit) return <QuitNotice />
  if ([settings.state.phase, library.state.phase, sources.state.phase, sourcePlugin.state.phase].includes('unauthorized')) return <UnauthorizedNotice />

  const data = settings.state.phase === 'ready' ? settings.state.data : null
  const section = (id: SettingsSectionId) => ({ section: sectionItem(id), active: current.id === id })

  return (
    <main className="catalog-page">
      <header className="catalog-head">
        <h1 className="catalog-title">设置</h1>
        {data !== null && (
          <p className="settings-version" style={mono}>
            nagare {formatVersion(data.version)}
          </p>
        )}
      </header>
      <div className="settings-layout">
        <SettingsNav current={current.id} onNavigate={handleNavigate} />
        <div className="settings-content">
          <SettingsStatus state={settings.state} onRetry={() => void settings.reload()} />

          <SettingsSection {...section('account')}>
            {/* 设置载荷缺了账号一项（接口数据不可全信）时不让整页崩掉：账号分区说明原因，其余分区照常 */}
            {data !== null &&
              (data.animego ? (
                <AccountPanel account={data.animego} onReload={settings.reload} />
              ) : (
                <div className="info-panel">
                  <p className="result result--warn" role="status">
                    没有读到账号状态，账号设置暂时不可用。重启 nagare 后再试。
                  </p>
                </div>
              ))}
          </SettingsSection>

          <SettingsSection {...section('folders')}>
            <FoldersPanel library={library} platform={data?.platform} />
          </SettingsSection>

          <SettingsSection {...section('player')}>
            {data !== null && <MpvCard mpv={data.mpv} desktop={data.desktop} onReload={settings.reload} />}
          </SettingsSection>

          <SettingsSection {...section('playback')}>{data !== null && <PlaybackCard player={data.player} />}</SettingsSection>

          <SettingsSection {...section('torrent')}>
            <TorrentShortcuts />
            {data?.torrent && <TorrentCard torrent={data.torrent} onReload={settings.reload} />}
          </SettingsSection>

          <SettingsSection {...section('sources')}>
            <SourcePluginCard plugin={sourcePlugin} />
            <SourcesCard sources={sources} />
          </SettingsSection>

          <SettingsSection {...section('update')}>
            <UpdateCard update={update} selfUpdate={selfUpdate} />
          </SettingsSection>

          <SettingsSection {...section('background')}>
            {data?.background && <BackgroundPanel mode={data.background.mode} platform={data.platform} />}
            <QuitCard platform={data?.platform} onQuit={() => setHasQuit(true)} />
          </SettingsSection>

          <SettingsSection {...section('about')}>
            {data !== null && <AboutCard settings={data} />}
            {data !== null && data.coverCache != null && <CoverCacheCard cache={data.coverCache} onReload={settings.reload} />}
          </SettingsSection>
        </div>
      </div>
    </main>
  )
}

/** 设置还在读 / 读失败：说在分区上面；不依赖设置的分区（文件夹、来源、更新）照常可用 */
function SettingsStatus({ state, onRetry }: { state: SettingsState; onRetry: () => void }) {
  if (state.phase === 'loading') {
    return (
      <p className="result result--dim" role="status">
        正在读取设置 …
      </p>
    )
  }
  if (state.phase !== 'error') return null
  return (
    <div className="info-panel">
      <p className="result result--err" role="alert">
        {state.message}
      </p>
      <div className="form-actions">
        <button type="button" className="btn" onClick={onRetry}>
          重试
        </button>
      </div>
    </div>
  )
}
