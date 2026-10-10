import { useState } from 'react'
import { updatePlayerConfig } from '../../lib/endpoints'
import type { Anime4KPreset, PlayerSettings } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { mono } from '../../theme'
import './cards.css'

const PRESETS: ReadonlyArray<{ value: Anime4KPreset; label: string; note: string }> = [
  { value: 'off', label: '关', note: '原样播放' },
  { value: 'fast', label: '标准', note: '核显、入门独显也能跑' },
  { value: 'hq', label: '高质量', note: '需要中高端独显，跑不动会掉帧' },
]

type SaveState =
  | { phase: 'idle' }
  | { phase: 'busy' }
  | { phase: 'done'; tone: 'ok' | 'warn'; text: string }
  | { phase: 'error'; message: string }

/**
 * 设置页「画质增强」卡：Anime4K 实时超分（mpv 着色器，随 nagare 附带）。
 * 点一下就保存，并立刻套到正在播放的窗口上；没在播就从下一次播放开始。
 */
export function PlaybackCard({ player }: { player: PlayerSettings }) {
  const [preset, setPreset] = useState<Anime4KPreset>(player.anime4k)
  const [state, setState] = useState<SaveState>({ phase: 'idle' })
  const busy = state.phase === 'busy'

  function choose(next: Anime4KPreset): void {
    if (next === preset || busy) return
    const previous = preset
    setPreset(next)
    setState({ phase: 'busy' })
    void updatePlayerConfig({ anime4k: next }).then(result => {
      setPreset(result.anime4k)
      if (result.applyError) setState({ phase: 'done', tone: 'warn', text: result.applyError })
      else setState({ phase: 'done', tone: 'ok', text: result.applied ? '已套到正在播放的窗口上' : '已保存，下次开始播放时生效' })
    }, (err: unknown) => {
      console.error('保存画质增强设置失败', err)
      setPreset(previous)
      setState({ phase: 'error', message: errorText(err, '保存失败') })
    })
  }

  return (
    <section className="panel settings-card" aria-labelledby="playback-heading">
      <h2 id="playback-heading" className="panel-heading">画质增强</h2>
      <p className="torrent-note">
        Anime4K 实时超分：在 mpv 里用显卡把动画画面放大、去模糊，线条更清楚。对 720p、1080p 的片源最明显。
        本地文件、磁力、在线播放都适用；用的是随 nagare 附带的官方着色器（Mode A），不联网。
      </p>
      <div className="playback-presets" role="group" aria-label="画质增强">
        {PRESETS.map(item => (
          <button key={item.value} type="button" aria-pressed={preset === item.value} disabled={busy}
            className={preset === item.value ? 'playback-preset playback-preset--active' : 'playback-preset'}
            onClick={() => choose(item.value)}>
            <strong>{item.label}</strong>
            <span>{item.note}</span>
          </button>
        ))}
      </div>
      <p className="torrent-note">播放时卡顿、掉帧就换回「标准」或关掉。需要 mpv 用显卡输出画面（默认就是）。</p>
      <p className={state.phase === 'error' ? 'result result--err' : state.phase === 'done' ? `result result--${state.tone}` : 'result result--dim'}
        style={mono} role="status" aria-live="polite">
        {state.phase === 'busy' ? '保存中 …' : state.phase === 'error' ? state.message : state.phase === 'done' ? state.text : ''}
      </p>
    </section>
  )
}
