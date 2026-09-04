import { useEffect, useState } from 'react'

/** 同时控制 JS 计时器与 CSS / Motion 动效，系统设置改变后立即生效。 */
export function useReducedMotionPreference() {
  const [reduced, setReduced] = useState(() =>
    typeof window !== 'undefined' && (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false))
  useEffect(() => {
    const query = window.matchMedia?.('(prefers-reduced-motion: reduce)')
    if (!query) return
    const sync = () => setReduced(query.matches)
    sync()
    query.addEventListener('change', sync)
    return () => query.removeEventListener('change', sync)
  }, [])
  return reduced
}
