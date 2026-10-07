import type { ComponentProps } from 'react'
import { useRouter } from '@tanstack/react-router'

/** 链到媒体库里的作品页（/anime/$clusterKey）；在路由里时走客户端导航，不在时退回普通链接 */
export function LibraryAnimeLink({ clusterKey, children, ...props }: Omit<ComponentProps<'a'>, 'href'> & { clusterKey: string }) {
  const router = useRouter({ warn: false })
  return (
    <a
      {...props}
      href={`/anime/${encodeURIComponent(clusterKey)}`}
      onClick={(event) => {
        props.onClick?.(event)
        if (router && !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) {
          event.preventDefault()
          void router.navigate({ to: '/anime/$clusterKey', params: { clusterKey } })
        }
      }}
    >
      {children}
    </a>
  )
}
