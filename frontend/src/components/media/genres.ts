/** 季度页类型筛选的选项（中文标签）与目录里的类型值（AniList 的英文名）一一对应 */
export const GENRES = ['全部', '动作', '冒险', '喜剧', '剧情', '卖肉', '奇幻', '恐怖', '魔法少女', '机战', '音乐', '悬疑', '心理', '恋爱', '科幻', '日常', '运动', '超自然', '惊悚']

const API_GENRES = ['', 'Action', 'Adventure', 'Comedy', 'Drama', 'Ecchi', 'Fantasy', 'Horror', 'Mahou Shoujo', 'Mecha', 'Music', 'Mystery', 'Psychological', 'Romance', 'Sci-Fi', 'Slice of Life', 'Sports', 'Supernatural', 'Thriller']
export const genreQuery = (label: string) => API_GENRES[GENRES.indexOf(label)] ?? label
export const genreLabel = (query: string) => GENRES[API_GENRES.indexOf(query)] ?? '全部'

/**
 * 作品页的类型标签链到季度页的类型筛选（`/seasonal?genre=动作`）：目录类型值换成筛选用的中文标签；
 * 认不出的类型不带筛选，落到季度页的「全部」
 */
export function genreFilter(apiGenre: string): string | undefined {
  const label = genreLabel(apiGenre)
  return label === '全部' ? undefined : label
}
