import { useRouter } from '@tanstack/react-router'
import type { ComponentProps } from 'react'

type RouteLinkProps = Omit<ComponentProps<'a'>, 'href'> & {
  /** 站内路径（如 `/library`、`/seasonal`） */
  to: string
  /** 查询参数；只写字符串、数字与布尔值，空串与其他类型的值不写进地址 */
  search?: Record<string, unknown>
}

/** 拼出站内地址：查询参数按传入顺序，空值不写 */
export function routeHref(to: string, search?: Record<string, unknown>): string {
  const params = new URLSearchParams()
  for (const [name, value] of Object.entries(search ?? {})) {
    if ((typeof value === 'string' && value !== '') || typeof value === 'number' || typeof value === 'boolean') params.set(name, String(value))
  }
  const query = params.toString()
  return query === '' ? to : `${to}?${query}`
}

/**
 * 站内链接：真正的 <a href>（新标签页打开、复制链接照常），普通左键点击走客户端路由。
 * 与 MediaEntryLink 同一个做法；不在路由里挂载时（组件测试）退回普通链接，不会因为缺路由上下文而崩。
 */
export function RouteLink({ to, search, children, onClick, ...props }: RouteLinkProps) {
  const router = useRouter({ warn: false })
  const href = routeHref(to, search)
  return (
    <a
      {...props}
      href={href}
      onClick={(event) => {
        onClick?.(event)
        if (!router || event.defaultPrevented || event.button !== 0) return
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        event.preventDefault()
        void router.navigate({ href })
      }}
    >
      {children}
    </a>
  )
}
