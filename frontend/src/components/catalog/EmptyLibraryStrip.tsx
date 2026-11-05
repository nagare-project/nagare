import { useEffect, useRef, useState } from 'react'
import { fetchLibrary } from '../../lib/endpoints'
import { handOffFocus, pageHeading } from '../../lib/focus'
import { Icon } from '../ui/Icon'
import { RouteLink } from './RouteLink'

/** 关掉之后记在这台浏览器里（只是本机偏好；清掉站点数据就会再出现一次） */
export const EMPTY_LIBRARY_DISMISS_KEY = 'nagare:discover-empty-library-dismissed'

function readDismissed(): boolean {
  try {
    return window.localStorage.getItem(EMPTY_LIBRARY_DISMISS_KEY) === '1'
  } catch {
    return false
  }
}

/**
 * 媒体库里一个文件都没有吗。读不到（后端没起来、没有 token）时当作「不知道」，不出提示：
 * 这条说明是给第一次打开的人的，不能在媒体库其实有东西时误报。
 */
function useLibraryEmpty(): boolean {
  const [empty, setEmpty] = useState(false)
  useEffect(() => {
    let alive = true
    fetchLibrary()
      .then((data) => {
        if (alive) setEmpty(data.clusters.length === 0)
      })
      .catch((err: unknown) => {
        if (alive) console.warn('读取媒体库失败，发现页不显示空库说明', err)
      })
    return () => {
      alive = false
    }
  }, [])
  return empty
}

/**
 * 发现页顶部的一行说明（DESIGN.md「发现」：媒体库为空时，不做欢迎页）：
 * 「nagare 播放你自己的番剧文件 · 去添加」+ 关闭。
 */
export function EmptyLibraryStrip() {
  const empty = useLibraryEmpty()
  const [dismissed, setDismissed] = useState(readDismissed)
  // 关掉之后这一条连同关闭按钮一起消失：焦点交给页面主标题，不掉到 <body> 上（与顶部提示条同一手法）
  const handOff = useRef(false)
  useEffect(() => {
    if (!dismissed || !handOff.current) return
    handOff.current = false
    handOffFocus(pageHeading())
  }, [dismissed])
  if (!empty || dismissed) return null

  function dismiss() {
    handOff.current = true
    setDismissed(true)
    try {
      window.localStorage.setItem(EMPTY_LIBRARY_DISMISS_KEY, '1')
    } catch {
      // 存储被禁用时只在这一页里关掉
    }
  }

  return (
    <div className="library-strip">
      <p className="library-strip-text">
        nagare 播放你自己的番剧文件<span aria-hidden="true"> · </span><RouteLink to="/library" className="library-strip-link">去添加</RouteLink>
      </p>
      <button type="button" className="icon-button library-strip-close" aria-label="关闭这条说明" onClick={dismiss}>
        <Icon name="close" size={16} />
      </button>
    </div>
  )
}
