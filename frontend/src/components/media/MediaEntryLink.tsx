import { useRouter } from '@tanstack/react-router'
import type { ComponentProps } from 'react'

/** 保留新标签页与复制链接行为，普通点击用客户端路由过渡。 */
export function MediaEntryLink({ id, children, ...props }: Omit<ComponentProps<'a'>, 'id' | 'href'> & { id: number }) {
  const router = useRouter({ warn: false })
  return <a {...props} href={`/entry?id=${id}`} onClick={event => {
    props.onClick?.(event)
    if (router && !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) {
      event.preventDefault(); void router.navigate({ to: '/entry', search: { id } })
    }
  }}>{children}</a>
}
