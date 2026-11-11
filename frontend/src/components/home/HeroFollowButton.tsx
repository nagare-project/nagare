import { RouteLink } from '../catalog/RouteLink'
import { useCollection } from '../media/CollectionContext'
import { MediaListEditor } from '../media/MediaListEditor'
import type { MediaSummary } from '../media/types'
import { CheckIcon, PlusIcon } from './icons'

// 横幅上的「追番」—— 两个按钮里玻璃的那一个，打开 nagare 自己的收藏编辑（状态、评分、进度）。
//
// 它总是画出来：它与实心按钮并排在一行里，消失的话那一行会挪位置。
// - 收藏还没读完：不可点（aria-disabled，按了不理）—— 这时按下去会被当成「没登录」，把已登录的人送去登录；
// - 没登录：一个去设置页「账号」的链接，名字说明要先登录；
// - 已登录：没追的是「+ 追番」，追了的是「✓ 已追 · 看到第 N 集」，两种都打开收藏编辑。

interface HeroFollowButtonProps {
  id: number
  title: string
  episodes: number | null
  className: string
  /** 收藏编辑开关时通知横幅：开着时横幅不切换 */
  onOpenChange?: (open: boolean) => void
}

export function HeroFollowButton({ id, title, episodes, className, onOpenChange }: HeroFollowButtonProps) {
  const collection = useCollection()
  if (collection.loading) {
    return (
      <button type="button" className={className} aria-disabled="true" aria-label={`追番：${title}`}>
        <PlusIcon />
        追番
      </button>
    )
  }
  if (!collection.loggedIn) {
    return (
      <RouteLink to="/settings#account" className={className} aria-label={`登录后追番：${title}`}>
        <PlusIcon />
        追番
      </RouteLink>
    )
  }
  const entry = collection.entries.find((e) => e.anilistId === id)
  const media: MediaSummary = { id, title, episodes, watched: entry?.currentEpisode ?? 0, genres: [] }
  const label = entry ? (entry.currentEpisode > 0 ? `已追 · 看到第 ${entry.currentEpisode} 集` : '已追') : '追番'
  return (
    <MediaListEditor
      media={media}
      onOpenChange={onOpenChange}
      trigger={(props) => (
        <button type="button" {...props} className={className} aria-label={`${entry ? `${label} · 编辑收藏` : '追番'}：${title}`}>
          {entry ? <CheckIcon /> : <PlusIcon />}
          {label}
        </button>
      )}
    />
  )
}
