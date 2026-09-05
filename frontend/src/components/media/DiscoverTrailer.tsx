import { useEffect, useId, useRef, useState } from 'react'

const YOUTUBE_ORIGIN = 'https://www.youtube-nocookie.com'
export const TRAILER_HOVER_MS = 1000
const TRAILER_TIMEOUT_MS = 15000

type TrailerState = 'waiting' | 'loading' | 'playing' | 'failed'

/** 仅加载经过校验的 YouTube 预告片；确认开始播放后才把图片背景盖住。 */
export function DiscoverTrailer({ videoId, active, title, variant = 'hero' }: { videoId?: string; active: boolean; title: string; variant?: 'hero' | 'card' }) {
  const frame = useRef<HTMLIFrameElement>(null)
  const playerId = useId()
  const failRef = useRef(() => {})
  const [state, setState] = useState<TrailerState>('waiting')
  const [revealed, setRevealed] = useState(false)
  const revealTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const valid = !!videoId && /^[A-Za-z0-9_-]{11}$/.test(videoId)

  useEffect(() => {
    setState('waiting')
    setRevealed(false)
    if (!active || !valid) return
    let listening: ReturnType<typeof setInterval> | undefined
    let timeout: ReturnType<typeof setTimeout> | undefined
    function fail() {
      setState('failed')
      clearInterval(listening)
      clearTimeout(timeout)
      clearTimeout(revealTimer.current)
      setRevealed(false)
    }
    failRef.current = fail
    function send(payload: object) {
      frame.current?.contentWindow?.postMessage(JSON.stringify(payload), YOUTUBE_ORIGIN)
    }
    function onMessage(event: MessageEvent) {
      if (event.origin !== YOUTUBE_ORIGIN || !frame.current?.contentWindow || event.source !== frame.current.contentWindow) return
      let data: { event?: string; info?: number | { playerState?: number } }
      try { data = typeof event.data === 'string' ? JSON.parse(event.data) : event.data } catch { return }
      if (!data || typeof data !== 'object') return
      if (data.event === 'onError') { fail(); return }
      const playerState = data.event === 'onStateChange' ? data.info
        : data.event === 'infoDelivery' && typeof data.info === 'object' ? data.info?.playerState : undefined
      if (playerState === 1) {
        setState('playing')
        clearInterval(listening)
        clearTimeout(timeout)
      }
    }
    window.addEventListener('message', onMessage)
    const hover = setTimeout(() => {
      setState('loading')
      timeout = setTimeout(fail, TRAILER_TIMEOUT_MS)
      // 与嵌入播放器建立事件通道，无需在 nagare 顶层执行第三方脚本。
      listening = setInterval(() => {
        send({ event: 'listening', id: playerId })
        send({ event: 'command', func: 'addEventListener', args: ['onStateChange'], id: playerId })
        send({ event: 'command', func: 'addEventListener', args: ['onError'], id: playerId })
      }, 250)
    }, variant === 'card' ? 0 : TRAILER_HOVER_MS)
    return () => {
      clearTimeout(hover)
      clearTimeout(timeout)
      clearInterval(listening)
      clearTimeout(revealTimer.current)
      window.removeEventListener('message', onMessage)
    }
  }, [active, valid, videoId, playerId, variant])

  if (!active || !valid || state === 'waiting') return null
  if (state === 'failed') return <span className="hero-trailer-notice" role="status">预告片暂不可用</span>
  const params = new URLSearchParams({ autoplay: '1', controls: '0', mute: '1', disablekb: '1', loop: '1',
    playlist: videoId!, playsinline: '1', enablejsapi: '1', origin: window.location.origin, rel: '0' })
  return <div className={state === 'playing' && revealed ? 'hero-trailer hero-trailer--playing' : 'hero-trailer'} data-variant={variant} aria-hidden="true">
    <iframe ref={frame} id={playerId} title={`${title}预告片（静音）`} src={`${YOUTUBE_ORIGIN}/embed/${videoId}?${params}`}
      allow="autoplay; encrypted-media" sandbox="allow-scripts allow-same-origin allow-presentation" referrerPolicy="origin" tabIndex={-1}
      onLoad={() => { clearTimeout(revealTimer.current); revealTimer.current = setTimeout(() => setRevealed(true), 1000) }}
      onError={() => failRef.current()} />
  </div>
}
