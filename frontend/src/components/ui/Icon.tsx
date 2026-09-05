import type { CSSProperties } from 'react'

/** 固定 SVG 笔画，避免字符图标在不同平台上改变形状。 */
const paths = {
  plus: 'M12 5v14M5 12h14',
  edit: 'm15 4 5 5M4 20l5-1L21 7a2 2 0 0 0-4-4L5 15ZM4 20h16',
  trash: 'M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7',
  star: 'm12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9Z',
  broadcast: 'M5 19a10 10 0 1 1 14 0M8 16a6 6 0 1 1 8 0M12 12v9M13 11a1 1 0 1 1-2 0 1 1 0 0 1 2 0',
  home: 'm3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1Z',
  calendar: 'M8 2v4m8-4v4M3 10h18M5 4h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2',
  lists: 'm3 6 2 2 4-4M12 6h9M3 13h2m3 0h13M3 20h2m3 0h13',
  compass: 'M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0ZM16.2 7.8l-2.8 5.6-5.6 2.8 2.8-5.6Z',
  search: 'M21 21l-4.4-4.4M19 11a8 8 0 1 1-16 0 8 8 0 0 1 16 0Z',
  download: 'M7 3v12m-4-4 4 4 4-4M17 21V9m-4 4 4-4 4 4',
  rss: 'M4 11a9 9 0 0 1 9 9M4 4a16 16 0 0 1 16 16M6 19a1 1 0 1 1-2 0 1 1 0 0 1 2 0Z',
  extension: 'M9 3H4v6h1a3 3 0 1 1 0 6H4v6h6v-1a3 3 0 1 1 6 0v1h5v-6h-1a3 3 0 1 1 0-6h1V3h-6V2a3 3 0 1 0-6 0v1Z',
  server: 'M4 3h16v7H4ZM4 14h16v7H4ZM7 6h.01M7 17h.01',
  settings: 'm9 3-.6 2.2-2 .9-2.1-.6-2 3.4 1.6 1.6v2.3L2.3 15l2 3.4 2.1-.6 2 .9L9 21h4l.6-2.3 2-.9 2.1.6 2-3.4-1.6-2.2v-2.3l1.6-1.6-2-3.4-2.1.6-2-.9L13 3ZM14 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z',
  user: 'M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM4 21v-2a6 6 0 0 1 6-6h4a6 6 0 0 1 6 6v2',
  menu: 'M4 6h16M4 12h16M4 18h16',
  close: 'm6 6 12 12M6 18 18 6',
  left: 'm15 18-6-6 6-6',
  right: 'm9 18 6-6-6-6',
  refresh: 'M20 7a9 9 0 0 0-15-2L2 8m0-5v5h5m-3 9a9 9 0 0 0 15 2l3-3m0 5v-5h-5',
  folder: 'M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2ZM3 9h18',
  grid: 'M3 3h7v7H3ZM14 3h7v7h-7ZM3 14h7v7H3ZM14 14h7v7h-7Z',
  play: 'm8 4 13 8-13 8Z',
  pause: 'M8 5v14M16 5v14',
  monitor: 'M3 3h18v14H3ZM12 17v4m-5 0h10',
  info: 'M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0ZM12 11v6m0-10h.01',
  power: 'M12 2v10M5 5a9 9 0 1 0 14 0',
  chevron: 'm6 9 6 6 6-6',
  check: 'm5 12 4 4L19 6',
  flag: 'M4 22V3m0 1c5-5 11 5 16 0v10c-5 5-11-5-16 0',
  heart: 'M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z',
} as const

export type IconName = keyof typeof paths

export function Icon({ name, size = 22, style }: { name: IconName; size?: number; style?: CSSProperties }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
      focusable="false" style={{ flexShrink: 0, ...style }}>
      <path d={paths[name]} />
    </svg>
  )
}
