// 发现页卡片上目录枚举值（类型、放送状态、季度、题材）的中文写法。
// 不认识的值原样显示（上游加了新值时总比空着好）。

const FORMAT_LABEL: Record<string, string> = {
  TV: 'TV',
  TV_SHORT: 'TV 短篇',
  MOVIE: '剧场版',
  SPECIAL: '特别篇',
  OVA: 'OVA',
  ONA: 'ONA',
  MUSIC: '音乐 MV',
}

const STATUS_LABEL: Record<string, string> = {
  RELEASING: '连载中',
  FINISHED: '已完结',
  NOT_YET_RELEASED: '未开播',
  CANCELLED: '已取消',
  HIATUS: '休载中',
}

const SEASON_LABEL: Record<string, string> = { WINTER: '冬季', SPRING: '春季', SUMMER: '夏季', FALL: '秋季' }

const GENRE_LABEL: Record<string, string> = {
  Action: '动作',
  Adventure: '冒险',
  Comedy: '喜剧',
  Drama: '剧情',
  Ecchi: '福利',
  Fantasy: '奇幻',
  Hentai: '成人',
  Horror: '恐怖',
  'Mahou Shoujo': '魔法少女',
  Mecha: '机战',
  Music: '音乐',
  Mystery: '悬疑',
  Psychological: '心理',
  Romance: '恋爱',
  'Sci-Fi': '科幻',
  'Slice of Life': '日常',
  Sports: '运动',
  Supernatural: '超自然',
  Thriller: '惊悚',
}

export const formatLabel = (format: string): string => FORMAT_LABEL[format] ?? format
export const statusLabel = (status: string): string => STATUS_LABEL[status] ?? status
export const seasonLabel = (season: string): string => SEASON_LABEL[season] ?? season
export const genreLabel = (genre: string): string => GENRE_LABEL[genre] ?? genre

/** 「2026 秋季」 */
export function seasonYearLabel(season: string, year: number | string): string {
  return `${year} ${seasonLabel(season)}`.trim()
}
