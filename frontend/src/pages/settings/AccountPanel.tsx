import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { Icon } from '../../components/ui/Icon'
import { animegoLogin, animegoLogout } from '../../lib/endpoints'
import type { AnimegoInfo } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { handOffFocus } from '../../lib/focus'
import { mono } from '../../theme'

export interface AccountPanelProps {
  account: AnimegoInfo
  /** 登录 / 退出之后刷新设置 */
  onReload: () => Promise<void>
}

/** 账号面板：未登录 → 邮箱 / 密码表单；已登录 → 账号与服务器 + 退出登录 */
export function AccountPanel({ account, onReload }: AccountPanelProps) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const panel = useRef<HTMLDivElement>(null)
  const emailInput = useRef<HTMLInputElement>(null)
  const logoutButton = useRef<HTMLButtonElement>(null)
  // 登录 / 退出成功后表单与账号信息互换，按下的那颗按钮跟着消失：等换完（且不再禁用）再把焦点交给新的那一边
  const handOff = useRef(false)
  useEffect(() => {
    if (busy || !handOff.current) return
    handOff.current = false
    handOffFocus(account.loggedIn ? logoutButton.current : emailInput.current, panel.current)
  }, [account.loggedIn, busy])

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
      handOff.current = true
      await onReload()
    } catch (err) {
      console.error('登录失败', err)
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
      handOff.current = true
      await onReload()
    } catch (err) {
      console.error('退出登录失败', err)
      setError(errorText(err, '退出登录失败'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div ref={panel} className="info-panel">
      {account.loggedIn ? (
        <>
          <div className="account-identity">
            <span className="account-avatar" aria-hidden="true">
              <Icon name="user" size={22} />
            </span>
            <div className="account-identity-text">
              <p className="account-email" style={mono}>{account.email ?? '（未知）'}</p>
              <p className="info-panel-note">已登录 · 弹幕与观看进度会自动同步</p>
            </div>
          </div>
          <dl className="kv-list">
            <dt>服务器</dt>
            <dd style={mono}>{account.baseUrl}</dd>
          </dl>
        </>
      ) : null}

      {/* 已知限制，必须在登录【之前】就说：服务端的刷新凭证是「一个账号一份」而不是「一个登录一份」，
          所以 nagare 与网站不能同时保持登录，十几分钟内必然互相挤掉。没有任何请求会失败，
          它不会出现在任何错误提示里 —— 用户只会发现自己在网站上莫名被登出。不写出来就等于藏着。 */}
      <p className="callout" role="note">
        <Icon name="alert" size={16} />
        <span>同一个账号在 nagare 与网站上<strong>不能同时保持登录</strong>：服务端的刷新凭证按账号存一份，后登录的一方会在十几分钟内把先登录的挤下线。弹幕与进度回写不受影响，受影响的只是「另一边要重新登录一次」。</span>
      </p>

      {account.loggedIn ? (
        <div className="form-actions">
          <button ref={logoutButton} type="button" className="btn btn--destructive" onClick={() => void handleLogout()} disabled={busy}>
            {busy ? '处理中 …' : '退出登录'}
          </button>
        </div>
      ) : (
        <form className="account-form" onSubmit={(event) => void handleLogin(event)}>
          <div className="field">
            <label htmlFor="account-email" className="field-label">
              邮箱
            </label>
            <input ref={emailInput} id="account-email" className="input" type="email" value={email} onChange={(event) => setEmail(event.target.value)} autoComplete="username" spellCheck={false} disabled={busy} />
          </div>
          <div className="field">
            <label htmlFor="account-password" className="field-label">
              密码
            </label>
            <input id="account-password" className="input" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" disabled={busy} />
          </div>
          <div className="form-actions">
            <button type="submit" className="btn btn--primary" disabled={busy}>
              {busy ? '登录中 …' : '登录'}
            </button>
            <span className="info-panel-note account-server" style={mono}>
              {account.baseUrl}
            </span>
          </div>
        </form>
      )}

      <p className={error === null ? 'result' : 'result result--err'} role="status" aria-live="polite">
        {error}
      </p>
    </div>
  )
}
