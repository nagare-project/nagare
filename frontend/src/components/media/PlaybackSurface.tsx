import type { ComponentPropsWithRef } from 'react'

/** The same playback controls can live on a detail page or in a card's dialog. */
export function PlaybackSurface({ inline = false, ...props }: ComponentPropsWithRef<'dialog'> & { inline?: boolean }) {
  if (inline) return <div className="media-play-inline">{props.children}</div>
  return <dialog {...props} />
}
