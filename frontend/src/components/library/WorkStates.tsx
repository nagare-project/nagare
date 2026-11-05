import { useState } from 'react'
import { RouteLink } from '../catalog/RouteLink'
import { useCollection } from '../media/CollectionContext'
import { Icon } from '../ui/Icon'

/**
 * 作品页的两种状态（DESIGN.md「状态表 · 作品页 / 账号」），/entry 与 /anime/$clusterKey 共用。
 */

/** 读取中：hero 的骨架（海报 + 几行字 + 动作行），读完原地换成真的 hero，不跳版 */
export function WorkHeroSkeleton({ label }: { label: string }) {
  return (
    <div className="work-hero work-hero--skeleton" role="status" aria-label={label}>
      <div className="work-hero-inner">
        <div className="work-hero-poster work-skeleton-block" />
        <div className="work-hero-text">
          <span className="work-skeleton-line work-skeleton-line--title" />
          <span className="work-skeleton-line" />
        </div>
        <div className="work-hero-actions">
          <span className="work-skeleton-line work-skeleton-line--button" />
          <span className="work-skeleton-line work-skeleton-line--button" />
        </div>
      </div>
    </div>
  )
}

/** 关掉之后记在这台浏览器里：两个作品页共用一个记号，「提示一次」 */
export const LOGIN_HINT_DISMISS_KEY = 'nagare:work-login-hint-dismissed'

function readDismissed(): boolean {
  try {
    return window.localStorage.getItem(LOGIN_HINT_DISMISS_KEY) === '1'
  } catch {
    return false
  }
}

/**
 * 没登录账号时的一句说明：「登录后显示弹幕、同步进度」，不挡播放。
 * 账号状态读不出来（还在读、读失败）时不说 —— 不能对已经登录的人说「去登录」。
 */
export function LoginHint() {
  const collection = useCollection()
  const [dismissed, setDismissed] = useState(readDismissed)
  if (collection.loading || collection.error !== null || collection.loggedIn || dismissed) return null

  function dismiss() {
    setDismissed(true)
    try {
      window.localStorage.setItem(LOGIN_HINT_DISMISS_KEY, '1')
    } catch {
      // 存储被禁用时只在这一页里关掉
    }
  }

  return (
    <div className="work-strip work-strip--hint">
      <p>登录后显示弹幕、同步进度<span aria-hidden="true"> · </span><RouteLink to="/settings#account" className="link">去登录</RouteLink></p>
      <button type="button" className="icon-button work-strip-close" aria-label="关闭这条说明" onClick={dismiss}>
        <Icon name="close" size={16} />
      </button>
    </div>
  )
}
