import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { AddFolderForm } from '../components/library/AddFolderForm'
import { SourcesCard } from '../components/search/SourcesCard'
import { AboutCard } from '../components/settings/AboutCard'
import { MpvCard } from '../components/settings/MpvCard'
import { QuitCard, QuitNotice } from '../components/settings/QuitCard'
import { UpdateCard } from '../components/settings/UpdateCard'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useLibrary } from '../hooks/useLibrary'
import { useSettings } from '../hooks/useSettings'
import { useSources } from '../hooks/useSources'
import { useUpdateContext } from '../hooks/useUpdate'
import { animegoLogin, animegoLogout } from '../lib/endpoints'
import type { AnimegoInfo } from '../lib/endpoints'
import type { LibraryState } from '../hooks/useLibrary'
import { errorText, formatDate } from '../lib/format'
import { hudPalette } from '../lib/palette'
import { label, mono } from '../tokens'
import './settings.css'

/**
 * `/settings`：animego 账号 · mpv · 更新 · 磁力源 · 库文件夹 · 关于 · 退出。
 * 设置 / 库 / 源三份数据独立加载，任一返回 401 都切到 token 提示页；
 * 更新状态来自根布局（与顶部提示条同一份）。退出成功后整页换成 QuitNotice。
 */
export function SettingsPage() {
  const settings = useSettings()
  const library = useLibrary()
  const sources = useSources()
  const update = useUpdateContext()
  const [hasQuit, setHasQuit] = useState(false)

  if (hasQuit) {
    return <QuitNotice />
  }
  if (
    settings.state.phase === 'unauthorized' ||
    library.state.phase === 'unauthorized' ||
    sources.state.phase === 'unauthorized'
  ) {
    return <UnauthorizedNotice />
  }

  return (
    <main className="settings-shell" style={hudPalette}>
      <header className="settings-head">
        <Link to="/" className="hud-link settings-back">
          ← 返回媒体库
        </Link>
        <h1 className="settings-title">设置</h1>
        <Link to="/search" className="hud-link settings-back settings-search-link">
          搜索 →
        </Link>
      </header>

      {settings.state.phase === 'loading' && (
        <p className="result result--dim" style={mono} role="status">
          正在读取设置 …
        </p>
      )}
      {settings.state.phase === 'error' && (
        <section className="panel settings-card">
          <p className="result result--err" style={mono}>
            {settings.state.message}
          </p>
          <p>
            <button
              type="button"
              className="hud-button hud-button--small"
              onClick={() => void settings.reload()}
            >
              重试
            </button>
          </p>
        </section>
      )}
      {settings.state.phase === 'ready' && (
        <>
          <AccountCard animego={settings.state.data.animego} onReload={settings.reload} />
          <MpvCard mpv={settings.state.data.mpv} onReload={settings.reload} />
        </>
      )}

      <UpdateCard update={update} />

      <SourcesCard sources={sources} />

      <FoldersCard
        state={library.state}
        onAdd={library.addFolder}
        onRemove={library.removeFolder}
        onRetry={() => void library.reload()}
      />

      {settings.state.phase === 'ready' && <AboutCard settings={settings.state.data} />}

      <QuitCard
        platform={settings.state.phase === 'ready' ? settings.state.data.platform : undefined}
        onQuit={() => setHasQuit(true)}
      />

      <footer className="colophon" style={label}>
        {settings.state.phase === 'ready'
          ? `nagare v${settings.state.data.version} · M4 · packaging`
          : 'nagare · M4 · packaging'}
      </footer>
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
      <h2 id="account-heading" className="panel-heading" style={label}>
        animego account
      </h2>
      <p className="page-notice-copy">
        登录后可拉取弹幕并回写观看进度（仅元数据 · 弹幕 · 进度三条已认证链路）。
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
              className="hud-button hud-button--small hud-button--ghost"
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
              email
            </label>
            <input
              id="animego-email"
              className="hud-input"
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
              password
            </label>
            <input
              id="animego-password"
              className="hud-input"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="current-password"
              disabled={busy}
            />
          </div>
          <div className="form-actions">
            <button type="submit" className="hud-button hud-button--small" disabled={busy}>
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
      <h2 id="folders-heading" className="panel-heading" style={label}>
        library folders
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
            <button type="button" className="hud-button hud-button--small" onClick={onRetry}>
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
                    className="hud-button hud-button--small hud-button--ghost"
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
