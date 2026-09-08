import { useState } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { Icon } from '../components/ui/Icon'
import type { IconName } from '../components/ui/Icon'
import type { FormEvent } from 'react'
import { AddFolderForm } from '../components/library/AddFolderForm'
import { SourcesCard } from '../components/search/SourcesCard'
import { AboutCard } from '../components/settings/AboutCard'
import { MpvCard } from '../components/settings/MpvCard'
import { QuitCard, QuitNotice } from '../components/settings/QuitCard'
import { SourcePluginCard } from '../components/settings/SourcePluginCard'
import { TorrentCard } from '../components/settings/TorrentCard'
import { UpdateCard } from '../components/settings/UpdateCard'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useLibrary } from '../hooks/useLibrary'
import { useSelfUpdateContext } from '../hooks/useSelfUpdate'
import { useSourcePlugin } from '../hooks/useSourcePlugin'
import { useSettings } from '../hooks/useSettings'
import { useSources } from '../hooks/useSources'
import { useUpdateContext } from '../hooks/useUpdate'
import { animegoLogin, animegoLogout } from '../lib/endpoints'
import type { AnimegoInfo } from '../lib/endpoints'
import type { LibraryState } from '../hooks/useLibrary'
import { errorText, formatDate } from '../lib/format'
import { label, mono } from '../theme'
import './settings.css'

const SETTINGS_GROUPS: ReadonlyArray<ReadonlyArray<{ id: string; label: string; icon: IconName }>> = [
  [{ id: 'app', label: '应用', icon: 'settings' }, { id: 'account', label: '账号', icon: 'user' }, { id: 'folders', label: '本地媒体库', icon: 'folder' }],
  [{ id: 'player', label: '媒体播放器', icon: 'monitor' }, { id: 'torrent', label: '磁力播放', icon: 'download' }, { id: 'sources', label: '来源', icon: 'extension' }],
  [{ id: 'update', label: '更新', icon: 'refresh' }, { id: 'about', label: '关于', icon: 'info' }],
]

/** 分区写入 URL，安装引导与文件夹入口可直达；切换时保留未提交的表单。 */
export function SettingsPage() {
  const settings = useSettings()
  const library = useLibrary()
  const sources = useSources()
  const sourcePlugin = useSourcePlugin()
  const update = useUpdateContext()
  const selfUpdate = useSelfUpdateContext()
  const hash = useLocation({ select: (location) => location.hash })
  const section = SETTINGS_GROUPS.flat().find((item) => item.id === hash) ?? SETTINGS_GROUPS[0]![0]!
  const [hasQuit, setHasQuit] = useState(false)

  if (hasQuit) return <QuitNotice />
  if ([settings.state.phase, library.state.phase, sources.state.phase, sourcePlugin.state.phase].includes('unauthorized')) return <UnauthorizedNotice />

  return (
    <main className="settings-shell">
      <aside className="settings-nav">
        <h1 className="page-title">设置</h1>
        <nav aria-label="设置分类">
          {SETTINGS_GROUPS.map((group, i) => (
            <div className="settings-nav-group" key={i}>
              {group.map(({ id, label, icon }) => (
                <Link key={id} to="/settings" hash={id} className="settings-nav-item"
                  aria-current={section.id === id ? 'page' : undefined} activeOptions={{ includeHash: true }}>
                  <Icon name={icon} size={18} />{label}
                </Link>
              ))}
            </div>
          ))}
        </nav>
        <p className="settings-version">nagare{settings.state.phase === 'ready' ? ` v${settings.state.data.version}` : ''}</p>
      </aside>
      <div className="settings-content">
        <header className="settings-section-head">
          <Icon name={section.icon} size={26} /><h2>{section.label}</h2>
        </header>
        {settings.state.phase === 'loading' && <p className="result result--dim" role="status">正在读取设置 …</p>}
        {settings.state.phase === 'error' && <section className="panel settings-card">
          <p className="result result--err" role="alert">{settings.state.message}</p>
          <button type="button" className="btn" onClick={() => void settings.reload()}>重试</button>
        </section>}
        <div className="settings-section" hidden={section.id !== 'app'}>
          <section className="panel settings-card">
            <h3 className="panel-heading">开始使用 nagare</h3>
            <p className="page-notice-copy">管理本地动漫，或通过磁力边下边播。播放、弹幕与观看进度都在这里。</p>
            <div className="settings-shortcuts">
              <Link to="/settings" hash="folders" className="settings-shortcut"><Icon name="folder" /><span>添加媒体文件夹<small>扫描本机与外接硬盘</small></span><Icon name="right" size={18} /></Link>
              <Link to="/settings" hash="player" className="settings-shortcut"><Icon name="monitor" /><span>配置播放器<small>检测与安装 mpv</small></span><Icon name="right" size={18} /></Link>
              <Link to="/settings" hash="account" className="settings-shortcut"><Icon name="user" /><span>连接账号<small>弹幕与观看进度同步</small></span><Icon name="right" size={18} /></Link>
            </div>
          </section>
          <QuitCard platform={settings.state.phase === 'ready' ? settings.state.data.platform : undefined} onQuit={() => setHasQuit(true)} />
        </div>
        <div className="settings-section" hidden={section.id !== 'account'}>
          {settings.state.phase === 'ready' && <AccountCard animego={settings.state.data.animego} onReload={settings.reload} />}
        </div>
        <div className="settings-section" hidden={section.id !== 'player'}>
          {settings.state.phase === 'ready' && <MpvCard mpv={settings.state.data.mpv} onReload={settings.reload} />}
        </div>
        <div className="settings-section" hidden={section.id !== 'torrent'}>
          {settings.state.phase === 'ready' && <TorrentCard torrent={settings.state.data.torrent} onReload={settings.reload} />}
        </div>
        <div className="settings-section" hidden={section.id !== 'update'}><UpdateCard update={update} selfUpdate={selfUpdate} /></div>
        <div className="settings-section" hidden={section.id !== 'sources'}><SourcePluginCard plugin={sourcePlugin} /><SourcesCard sources={sources} /></div>
        <div className="settings-section" hidden={section.id !== 'folders'}>
          <FoldersCard state={library.state} onAdd={library.addFolder} onRemove={library.removeFolder} onRetry={() => void library.reload()} />
        </div>
        <div className="settings-section" hidden={section.id !== 'about'}>
          {settings.state.phase === 'ready' && <AboutCard settings={settings.state.data} />}
        </div>
      </div>
    </main>
  )
}

/** animego 账号卡：未登录 → 邮箱/密码表单；已登录 → 账号信息 + 退出 */
function AccountCard({
  animego,
  onReload,
}: {
  animego: AnimegoInfo
  onReload: () => Promise<void>
}) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleLogin(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault()
    if (email.trim() === '' || password === '') {
      setError('请输入邮箱和密码')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await animegoLogin(email.trim(), password)
      window.dispatchEvent(new Event('nagare:account-changed'))
      setPassword('')
      await onReload()
    } catch (err) {
      console.error('animego 登录失败', err)
      setError(errorText(err, '登录失败'))
    } finally {
      setBusy(false)
    }
  }

  async function handleLogout(): Promise<void> {
    setBusy(true)
    setError(null)
    try {
      await animegoLogout()
      window.dispatchEvent(new Event('nagare:account-changed'))
      await onReload()
    } catch (err) {
      console.error('animego 退出登录失败', err)
      setError(errorText(err, '退出登录失败'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="panel settings-card" aria-labelledby="account-heading">
      <h2 id="account-heading" className="panel-heading">
        animego 账号
      </h2>
      <p className="page-notice-copy">
        登录后获取弹幕，并把看完的集数同步到账号。
      </p>
      {/* 已知限制，必须在登录【之前】就说：服务端的刷新凭证是「一个账号一份」而不是
          「一个登录一份」，所以 nagare 与网站不能同时保持登录，十几分钟内必然互相挤掉。
          这不是错误、没有任何请求会失败，因此它不会出现在任何错误提示里 ——
          用户只会发现自己在网站上莫名被登出，然后无从解释。不写出来就等于藏着。 */}
      <p className="alert-warn account-note">
        同一个账号在 nagare 与网站上<strong>不能同时保持登录</strong>：服务端的刷新凭证按账号
        存一份，后登录的一方会在十几分钟内把先登录的挤下线。弹幕与进度回写不受影响，
        受影响的只是「另一边要重新登录一次」。
      </p>

      {animego.loggedIn ? (
        <>
          <dl className="kv-list">
            <dt>账号</dt>
            <dd style={mono}>{animego.email ?? '（未知）'}</dd>
            <dt>服务器</dt>
            <dd style={mono}>{animego.baseUrl}</dd>
          </dl>
          <div className="form-actions">
            <button
              type="button"
              className="btn btn--sm btn--danger"
              onClick={() => void handleLogout()}
              disabled={busy}
            >
              {busy ? '处理中 …' : '退出登录'}
            </button>
          </div>
        </>
      ) : (
        <form className="account-form" onSubmit={(event) => void handleLogin(event)}>
          <div className="field">
            <label htmlFor="animego-email" style={label}>
              邮箱
            </label>
            <input
              id="animego-email"
              className="input"
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              autoComplete="email"
              spellCheck={false}
              disabled={busy}
            />
          </div>
          <div className="field">
            <label htmlFor="animego-password" style={label}>
              密码
            </label>
            <input
              id="animego-password"
              className="input"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="current-password"
              disabled={busy}
            />
          </div>
          <div className="form-actions">
            <button type="submit" className="btn btn--sm" disabled={busy}>
              {busy ? '登录中 …' : '登录'}
            </button>
            <span className="result result--dim" style={mono}>
              {animego.baseUrl}
            </span>
          </div>
        </form>
      )}

      <p
        className={error === null ? 'result' : 'result result--err'}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {error}
      </p>
    </section>
  )
}

/** 文件夹管理卡：列表（含扫描错误展示）+ 删除 + 添加 */
function FoldersCard({
  state,
  onAdd,
  onRemove,
  onRetry,
}: {
  state: LibraryState
  onAdd: ReturnType<typeof useLibrary>['addFolder']
  onRemove: (id: string) => Promise<void>
  onRetry: () => void
}) {
  const [removingId, setRemovingId] = useState<string | null>(null)
  const [removeError, setRemoveError] = useState<string | null>(null)

  async function handleRemove(id: string): Promise<void> {
    setRemovingId(id)
    setRemoveError(null)
    try {
      await onRemove(id)
    } catch (err) {
      console.error('删除文件夹失败', err)
      setRemoveError(errorText(err, '删除文件夹失败'))
    } finally {
      setRemovingId(null)
    }
  }

  return (
    <section className="panel settings-card" aria-labelledby="folders-heading">
      <h2 id="folders-heading" className="panel-heading">
        媒体文件夹
      </h2>

      {state.phase === 'loading' && (
        <p className="result result--dim" style={mono} role="status">
          正在读取文件夹列表 …
        </p>
      )}
      {state.phase === 'error' && (
        <>
          <p className="result result--err" style={mono}>
            {state.message}
          </p>
          <p>
            <button type="button" className="btn btn--sm" onClick={onRetry}>
              重试
            </button>
          </p>
        </>
      )}
      {state.phase === 'ready' && (
        <>
          {state.data.folders.length === 0 ? (
            <p className="result result--dim" style={mono}>
              还没有添加任何文件夹
            </p>
          ) : (
            <ul className="folder-list">
              {state.data.folders.map((folder) => (
                <li key={folder.id} className="folder-row">
                  <span className="folder-path" style={mono} title={folder.path}>
                    {folder.path}
                  </span>
                  <span className="folder-date" style={mono}>
                    {formatDate(folder.addedAt)}
                  </span>
                  <button
                    type="button"
                    className="btn btn--sm btn--danger"
                    onClick={() => void handleRemove(folder.id)}
                    disabled={removingId !== null}
                    aria-label={`删除文件夹 ${folder.path}`}
                  >
                    {removingId === folder.id ? '删除中 …' : '删除'}
                  </button>
                  {folder.error !== undefined && folder.error !== '' && (
                    <span className="folder-error" role="alert">
                      扫描出错：{folder.error}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          )}
          {removeError !== null && (
            <p className="result result--err" style={mono} role="alert">
              {removeError}
            </p>
          )}
          <AddFolderForm onAdd={onAdd} />
        </>
      )}
    </section>
  )
}
