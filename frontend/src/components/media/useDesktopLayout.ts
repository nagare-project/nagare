import { useEffect, useState } from 'react'

/** 与 seanime 的 lg 断点一致；窄屏不挂载只服务于鼠标悬停的播放器。 */
export function useDesktopLayout() {
  const [desktop, setDesktop] = useState(() => window.matchMedia?.('(min-width: 1024px)').matches ?? true)
  useEffect(() => {
    const query = window.matchMedia?.('(min-width: 1024px)')
    if (!query) return
    const sync = () => setDesktop(query.matches)
    sync()
    query.addEventListener('change', sync)
    return () => query.removeEventListener('change', sync)
  }, [])
  return desktop
}
