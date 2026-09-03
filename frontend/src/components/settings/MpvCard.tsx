import { useEffect, useRef, useState } from 'react'
import { copyText } from '../../lib/clipboard'
import { redetectMpv } from '../../lib/endpoints'
import type { MpvInfo, MpvInstallGuide, MpvSource } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { isHttpUrl } from '../../lib/url'
import { label, mono } from '../../theme'
import './cards.css'

/** 「已复制」提示停留多久后恢复按钮文案 */
const COPY_FEEDBACK_MS = 2000

/** mpv 来源的中文（后端 source 字段） */
const SOURCE_LABEL: Record<MpvSource, string> = {
  explicit: '显式路径（配置指定）',
  bundled: '内置（随 nagare 附带）',
  path: 'PATH 环境变量',
  known: '常见安装位置',
}

type CopyState = 'idle' | 'copied' | 'failed'

const COPY_LABEL: Record<CopyState, string> = {
  idle: '复制',
  copied: '已复制 ✓',
  failed: '复制失败',
}

/** 「重新检测」的结果行状态机 */
type DetectState =
  | { phase: 'idle' }
  | { phase: 'busy' }
  | { phase: 'done'; info: MpvInfo }
  | { phase: 'error'; message: string }

export interface MpvCardProps {
  mpv: MpvInfo
  /** 重新检测成功后刷新设置（useSettings.reload），让状态点与本卡同步 */
  onReload: () => Promise<void>
}

/**
 * mpv 信息卡。找到：版本 / 路径 / 来源；未找到：提示 + 按平台的安装指引
 * （命令块 + 一键复制 + 外链）+「重新检测」—— macOS 不捆绑 mpv，用户装完不用重启。
 */
export function MpvCard({ mpv, onReload }: MpvCardProps) {
  const [detect, setDetect] = useState<DetectState>({ phase: 'idle' })
  const busy = detect.phase === 'busy'

  async function handleDetect(): Promise<void> {
    setDetect({ phase: 'busy' })
    try {
      const info = await redetectMpv()
      await onReload()
      setDetect({ phase: 'done', info })
    } catch (err) {
      console.error('重新检测 mpv 失败', err)
      setDetect({ phase: 'error', message: errorText(err, '重新检测失败') })
    }
  }

  const view = detectView(detect)

  return (
    <section className="panel settings-card" aria-labelledby="mpv-heading">
      <h2 id="mpv-heading" className="panel-heading">
        播放器 mpv
      </h2>

      {mpv.found ? (
        <dl className="kv-list">
          <dt>状态</dt>
          <dd className="result--ok">已找到 ✓</dd>
          <dt>版本</dt>
          <dd style={mono}>{mpv.version ?? '（未知）'}</dd>
          <dt>路径</dt>
          <dd style={mono}>{mpv.path ?? '（未知）'}</dd>
          <dt>来源</dt>
          <dd>{mpv.source === undefined ? '（未知）' : SOURCE_LABEL[mpv.source]}</dd>
        </dl>
      ) : (
        <>
          <dl className="kv-list">
            <dt>状态</dt>
            <dd className="result--err">未找到</dd>
          </dl>
          <p className="alert-warn" role="alert">
            {mpv.hint ?? '未检测到 mpv，请先安装 mpv。'}
          </p>
          {mpv.install !== undefined && <InstallGuide install={mpv.install} />}
        </>
      )}

      <div className="form-actions">
        <button
          type="button"
          className="btn btn--sm"
          onClick={() => void handleDetect()}
          disabled={busy}
        >
          {busy ? '检测中 …' : '重新检测'}
        </button>
        {!mpv.found && (
          <span className="result result--dim mpv-detect-hint">
            安装完成后点「重新检测」即可，无需重启 nagare
          </span>
        )}
      </div>
      <p
        className={view === null ? 'result' : `result result--${view.tone}`}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {view?.text}
      </p>
    </section>
  )
}

/** 安装指引：一行说明 + 等宽命令块（可复制；windows 没有命令）+ 外链 */
function InstallGuide({ install }: { install: MpvInstallGuide }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const timerRef = useRef<number | null>(null)

  // 卸载时清掉复位定时器，别对着已卸载的卡片 setState
  useEffect(() => {
    return () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    }
  }, [])

  async function handleCopy(): Promise<void> {
    setCopyState((await copyText(install.command)) ? 'copied' : 'failed')
    if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => setCopyState('idle'), COPY_FEEDBACK_MS)
  }

  const hasCommand = install.command.trim() !== ''
  const hasUrl = isHttpUrl(install.url)

  return (
    <div className="mpv-install">
      <h3 className="mpv-install-heading" style={label}>
        安装方法
      </h3>
      {install.note !== '' && <p className="mpv-install-note">{install.note}</p>}
      {hasCommand && (
        <div className="mpv-command">
          <code className="mpv-command-text" style={mono}>
            {install.command}
          </code>
          <button
            type="button"
            className={
              copyState === 'failed'
                ? 'btn btn--sm mpv-copy mpv-copy--failed'
                : 'btn btn--sm mpv-copy'
            }
            onClick={() => void handleCopy()}
            aria-label="复制安装命令"
          >
            {COPY_LABEL[copyState]}
          </button>
          <span className="visually-hidden" role="status" aria-live="polite">
            {copyState === 'idle' ? '' : COPY_LABEL[copyState]}
          </span>
        </div>
      )}
      {hasUrl && (
        <a
          className="link mpv-install-link"
          href={install.url}
          target="_blank"
          rel="noreferrer noopener"
        >
          安装说明 ↗
        </a>
      )}
    </div>
  )
}

function detectView(state: DetectState): { tone: 'dim' | 'ok' | 'err'; text: string } | null {
  switch (state.phase) {
    case 'idle':
      return null
    case 'busy':
      return { tone: 'dim', text: '正在重新检测 mpv …' }
    case 'done':
      return state.info.found
        ? {
            tone: 'ok',
            text: `已找到 mpv${state.info.version !== undefined ? ` ${state.info.version}` : ''}`,
          }
        : { tone: 'err', text: '仍未找到 mpv，请确认安装已完成' }
    case 'error':
      return { tone: 'err', text: state.message }
  }
}
