import { useState } from 'react'
import type { FormEvent } from 'react'
import type { ReloadResult, RulesConfigPatch, RulesInfo, SyncResult } from '../../lib/endpoints'
import { errorText, formatClock } from '../../lib/format'
import { label, mono } from '../../theme'
import './sources.css'

export interface RulesConfigFormProps {
  rules: RulesInfo
  onSave: (patch: RulesConfigPatch) => Promise<RulesInfo>
  onSync: () => Promise<SyncResult>
  onReload: () => Promise<ReloadResult>
}

type Action = 'save' | 'sync' | 'reload'

const ACTION_LABEL: Record<Action, string> = {
  save: '保存',
  sync: '同步规则',
  reload: '重新加载',
}

/** 动作结果行状态机；ok 时把规则级错误一并带出来展示 */
type ActionState =
  | { phase: 'idle' }
  | { phase: 'busy'; action: Action }
  | { phase: 'ok'; text: string; errors: string[] }
  | { phase: 'error'; message: string }

const HTTPS_PREFIX = /^https:\/\//i

/**
 * 规则来源配置：remoteUrl（规则仓库 HTTPS 地址）+ localDir（本机目录，开发用）+ 保存；
 * 「同步规则」从 remoteUrl 拉取，「重新加载」重读规则目录。
 * placeholder 刻意不给任何示例站点 —— 本体零内置源，也不推荐源。
 */
export function RulesConfigForm({ rules, onSave, onSync, onReload }: RulesConfigFormProps) {
  const [remoteUrl, setRemoteUrl] = useState(rules.remoteUrl)
  const [localDir, setLocalDir] = useState(rules.localDir)
  const [state, setState] = useState<ActionState>({ phase: 'idle' })
  const busy = state.phase === 'busy'

  async function run(action: Action, task: () => Promise<{ text: string; errors: string[] }>) {
    setState({ phase: 'busy', action })
    try {
      const outcome = await task()
      setState({ phase: 'ok', ...outcome })
    } catch (err) {
      console.error(`${ACTION_LABEL[action]}失败`, err)
      setState({ phase: 'error', message: errorText(err, `${ACTION_LABEL[action]}失败`) })
    }
  }

  function handleSave(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    const nextRemote = remoteUrl.trim()
    const nextLocal = localDir.trim()
    if (nextRemote !== '' && !HTTPS_PREFIX.test(nextRemote)) {
      setState({ phase: 'error', message: '规则仓库地址必须以 https:// 开头' })
      return
    }
    void run('save', async () => {
      const next = await onSave({ remoteUrl: nextRemote, localDir: nextLocal })
      return { text: `已保存 · 当前加载 ${next.loaded} 条规则`, errors: next.errors }
    })
  }

  function handleSync(): void {
    void run('sync', async () => {
      const result = await onSync()
      return {
        text: `同步完成 · 新增 ${result.added} / 更新 ${result.updated} / 移除 ${result.removed}`,
        errors: result.errors,
      }
    })
  }

  function handleReload(): void {
    void run('reload', async () => {
      const result = await onReload()
      return { text: `已重新加载 ${result.loaded} 条规则`, errors: result.errors }
    })
  }

  const canSync = rules.remoteUrl !== '' && !busy
  const view = resultView(state)

  return (
    <form className="rules-form" onSubmit={handleSave}>
      <div className="field">
        <label htmlFor="rules-remote-url" style={label}>
          规则仓库地址
        </label>
        <input
          id="rules-remote-url"
          className="input"
          type="url"
          value={remoteUrl}
          onChange={(event) => setRemoteUrl(event.target.value)}
          placeholder="规则仓库的 HTTPS 地址"
          spellCheck={false}
          autoComplete="off"
          disabled={busy}
        />
      </div>
      <div className="field">
        <label htmlFor="rules-local-dir" style={label}>
          本地规则目录
        </label>
        <input
          id="rules-local-dir"
          className="input"
          type="text"
          value={localDir}
          onChange={(event) => setLocalDir(event.target.value)}
          placeholder="本机规则目录的绝对路径（开发用，可留空）"
          spellCheck={false}
          autoComplete="off"
          disabled={busy}
        />
      </div>
      <div className="form-actions">
        <button type="submit" className="btn btn--sm" disabled={busy}>
          {state.phase === 'busy' && state.action === 'save' ? '保存中 …' : '保存'}
        </button>
        <button
          type="button"
          className="btn btn--sm"
          onClick={handleSync}
          disabled={!canSync}
          title={rules.remoteUrl === '' ? '先保存规则仓库地址' : undefined}
        >
          {state.phase === 'busy' && state.action === 'sync' ? '同步中 …' : '同步规则'}
        </button>
        <button
          type="button"
          className="btn btn--sm btn--danger"
          onClick={handleReload}
          disabled={busy}
        >
          {state.phase === 'busy' && state.action === 'reload' ? '加载中 …' : '重新加载'}
        </button>
      </div>
      <p
        className={view === null ? 'result' : `result result--${view.tone}`}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {view?.text}
      </p>
      {state.phase === 'ok' && state.errors.length > 0 && (
        <ul className="rules-action-errors" role="alert">
          {state.errors.map((error) => (
            <li key={error} style={mono}>
              {error}
            </li>
          ))}
        </ul>
      )}
      <dl className="kv-list">
        <dt>生效目录</dt>
        <dd style={mono}>{rules.dir === '' ? '（未设置）' : rules.dir}</dd>
        <dt>已加载</dt>
        <dd style={mono}>{rules.loaded} 条规则</dd>
        <dt>上次加载</dt>
        <dd style={mono}>{rules.lastLoadedAt === null ? '—' : formatClock(rules.lastLoadedAt)}</dd>
        <dt>上次同步</dt>
        <dd style={mono}>{rules.lastSyncAt === null ? '—' : formatClock(rules.lastSyncAt)}</dd>
      </dl>
    </form>
  )
}

function resultView(state: ActionState): { tone: 'dim' | 'ok' | 'err'; text: string } | null {
  switch (state.phase) {
    case 'idle':
      return null
    case 'busy':
      return { tone: 'dim', text: `${ACTION_LABEL[state.action]}中 …` }
    case 'ok':
      return { tone: 'ok', text: state.text }
    case 'error':
      return { tone: 'err', text: state.message }
  }
}
