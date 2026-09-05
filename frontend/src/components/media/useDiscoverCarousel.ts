import { useEffect, useState } from 'react'

export const DISCOVER_INTERVAL_MS = 12000
export const DISCOVER_EXIT_MS = 900

/** 先退出旧内容，再提交新作品；连点只保留最后一个目标，卸载时清除计时器。 */
export function useDiscoverCarousel(count: number, stopped: boolean, reducedMotion: boolean) {
  const [current, setCurrent] = useState(0)
  const [target, setTarget] = useState(0)
  const [manualChange, setManualChange] = useState(0)
  const activeIndex = count ? current % count : 0
  const selectedIndex = count ? target % count : 0
  const transitioning = activeIndex !== selectedIndex && !reducedMotion

  useEffect(() => {
    if (activeIndex === selectedIndex) return
    const timer = setTimeout(() => setCurrent(selectedIndex), reducedMotion ? 0 : DISCOVER_EXIT_MS)
    return () => clearTimeout(timer)
  }, [activeIndex, selectedIndex, reducedMotion])

  useEffect(() => {
    if (stopped || reducedMotion || count < 2) return
    const timer = setInterval(() => setTarget((i) => (i + 1) % count), DISCOVER_INTERVAL_MS)
    return () => clearInterval(timer)
  }, [count, stopped, reducedMotion, manualChange])

  function select(index: number) {
    if (index < 0 || index >= count) return
    setTarget(index)
    setManualChange((n) => n + 1)
    if (reducedMotion) setCurrent(index)
  }

  return { activeIndex: reducedMotion ? selectedIndex : activeIndex, selectedIndex, transitioning, select }
}
