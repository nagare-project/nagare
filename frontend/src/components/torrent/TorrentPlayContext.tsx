import { createContext, useCallback, useContext, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useTorrentPlay } from '../../hooks/useTorrentPlay'
import type { TorrentPlayState, UseTorrentPlayResult } from '../../hooks/useTorrentPlay'
import type { TorrentPlayRequest } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { EpisodePicker } from './EpisodePicker'
import { TorrentStatusBar } from './TorrentStatusBar'
import './torrent.css'

const STOP_FAILED_SUFFIX = '（后端可能还留着这个种子，可到设置页清空磁力缓存）'

type RestoreFocus = () => void

export interface SharedTorrentPlay extends Omit<UseTorrentPlayResult, 'play' | 'cancel'> {
  play: (request: TorrentPlayRequest | string, title: string, restoreFocus?: RestoreFocus) => void
  cancel: () => Promise<void>
}

const idleState: TorrentPlayState = { phase: 'idle' }
const fallback: SharedTorrentPlay = {
  state: idleState,
  status: null,
  zeroPeerSeconds: 0,
  busy: false,
  play: () => {},
  selectFile: () => {},
  retry: () => {},
  cancel: async () => {},
}

const TorrentPlayContext = createContext<SharedTorrentPlay | null>(null)

/**
 * 一份挂在根布局上的磁力播放会话。页面切换只会卸载入口，不会掐断正在读取
 * 元数据、等待用户选集或下载中的种子；状态条与种子内选集窗口也始终只有一份。
 */
export function TorrentPlayProvider({ children }: { children: ReactNode }) {
  const torrent = useTorrentPlay()
  const restoreFocus = useRef<RestoreFocus | undefined>(undefined)
  const [stopError, setStopError] = useState<string | null>(null)

  const play = useCallback((request: TorrentPlayRequest | string, title: string, restore?: RestoreFocus) => {
    restoreFocus.current = restore
    setStopError(null)
    torrent.play(request, title)
  }, [torrent.play])

  const cancel = useCallback(async () => {
    try {
      await torrent.cancel()
      setStopError(null)
    } catch (err) {
      console.error('停止磁力播放失败', err)
      setStopError(`${errorText(err, '停止失败')}${STOP_FAILED_SUFFIX}`)
    }
  }, [torrent.cancel])

  const value: SharedTorrentPlay = { ...torrent, play, cancel }
  return <TorrentPlayContext.Provider value={value}>
    {children}
    <TorrentStatusBar
      state={torrent.state}
      status={torrent.status}
      zeroPeerSeconds={torrent.zeroPeerSeconds}
      onCancel={() => { void cancel() }}
      onRetry={torrent.retry}
    />
    {torrent.state.phase === 'selecting' && <EpisodePicker
      title={torrent.state.title}
      files={torrent.state.files}
      onSelect={torrent.selectFile}
      onCancel={() => { void cancel() }}
      restoreFocus={() => {
        const restore = restoreFocus.current
        restoreFocus.current = undefined
        restore?.()
      }}
    />}
    {stopError && <div className="torrent-global-error" role="alert">
      <span>{stopError}</span>
      <button type="button" className="btn btn--sm" onClick={() => setStopError(null)}>关闭</button>
    </div>}
  </TorrentPlayContext.Provider>
}

/** 测试中的独立媒体组件没有根布局时保持不可用空态；产品路由始终由 Provider 包裹。 */
export function useTorrentPlayback(): SharedTorrentPlay {
  return useContext(TorrentPlayContext) ?? fallback
}
