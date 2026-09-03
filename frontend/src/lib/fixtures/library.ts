// FIXME(G1/G2/G3): 假数据。真接口缺口见仓库根 todos.md。
import type { FakeAiring, FakeMedia } from './types'

/**
 * 一份共用的作品样本。各页从这里切片，而不是各写一份 ——
 * 同一部番在「我的列表」和「发现」里必须是同一个标题同一个封面，
 * 否则演示时一眼看穿是拼的。
 */
const SHOWS: readonly FakeMedia[] = [
  { id: 154587, title: '葬送的芙莉莲', titleNative: '葬送のフリーレン', year: 2023, season: '秋', episodes: 28, watched: 12, score: 92, genres: ['奇幻', '冒险', '剧情'] },
  { id: 162804, title: '药屋少女的呢喃', titleNative: '薬屋のひとりごと', year: 2023, season: '秋', episodes: 24, watched: 24, score: 87, genres: ['悬疑', '剧情', '历史'] },
  { id: 140960, title: '孤独摇滚！', titleNative: 'ぼっち・ざ・ろっく！', year: 2022, season: '秋', episodes: 12, watched: 12, score: 88, genres: ['音乐', '喜剧', '日常'] },
  { id: 21519, title: '你的名字。', titleNative: '君の名は。', year: 2016, season: '夏', episodes: 1, watched: 1, score: 85, genres: ['剧情', '恋爱', '超自然'] },
  { id: 145064, title: '间谍过家家', titleNative: 'SPY×FAMILY', year: 2022, season: '春', episodes: 25, watched: 8, score: 86, genres: ['喜剧', '动作', '日常'] },
  { id: 101922, title: '鬼灭之刃', titleNative: '鬼滅の刃', year: 2019, season: '春', episodes: 26, watched: 0, score: 83, genres: ['动作', '超自然', '历史'] },
  { id: 113415, title: '咒术回战', titleNative: '呪術廻戦', year: 2020, season: '秋', episodes: 24, watched: 5, score: 85, genres: ['动作', '超自然'] },
  { id: 108465, title: '摇曳露营△ 第二季', titleNative: 'ゆるキャン△ SEASON2', year: 2021, season: '冬', episodes: 13, watched: 0, score: 84, genres: ['日常', '治愈'] },
  { id: 131681, title: '欢迎来到实力至上主义的教室 第二季', year: 2022, season: '夏', episodes: 13, watched: 3, score: 79, genres: ['心理', '剧情'] },
  { id: 130003, title: '别当欧尼酱了！', titleNative: 'お兄ちゃんはおしまい！', year: 2023, season: '冬', episodes: 12, watched: 0, score: 76, genres: ['喜剧', '日常'] },
  { id: 151970, title: '迷宫饭', titleNative: 'ダンジョン飯', year: 2024, season: '冬', episodes: 24, watched: 18, score: 86, genres: ['奇幻', '冒险', '喜剧'] },
  { id: 163132, title: '战场上的普通话', year: 2026, season: '冬', episodes: null, watched: 0, score: 0, genres: ['剧情'] },
]

/** 观看状态，与 seanime 的五档对应 */
export type ListStatus = 'watching' | 'planning' | 'completed' | 'paused' | 'dropped'

export const LIST_STATUS_LABEL: Record<ListStatus, string> = {
  watching: '在看',
  planning: '想看',
  completed: '看完',
  paused: '搁置',
  dropped: '弃番',
}

/** 状态 → 作品。按 id 分派，保证每次渲染一致（不要随机）。 */
export const FAKE_LISTS: Record<ListStatus, FakeMedia[]> = {
  watching: SHOWS.filter((s) => s.watched > 0 && (s.episodes === null || s.watched < s.episodes)),
  completed: SHOWS.filter((s) => s.episodes !== null && s.watched === s.episodes),
  planning: SHOWS.filter((s) => s.watched === 0 && s.id % 2 === 0),
  paused: SHOWS.filter((s) => s.watched === 0 && s.id % 2 === 1).slice(0, 2),
  dropped: [],
}

export const FAKE_DISCOVER = {
  trending: [SHOWS[10]!, SHOWS[0]!, SHOWS[6]!, SHOWS[4]!, SHOWS[1]!, SHOWS[2]!],
  popular: [SHOWS[5]!, SHOWS[0]!, SHOWS[3]!, SHOWS[1]!, SHOWS[6]!, SHOWS[4]!],
  upcoming: [SHOWS[11]!, SHOWS[7]!, SHOWS[8]!, SHOWS[9]!],
} satisfies Record<string, FakeMedia[]>

/** 本周放送。日期按「本周一起算」生成，所以页面每天看都合理。 */
export function fakeAiringThisWeek(now = new Date()): FakeAiring[] {
  const monday = new Date(now)
  monday.setHours(0, 0, 0, 0)
  // getDay()：周日是 0，换算成「距本周一几天」
  monday.setDate(monday.getDate() - ((now.getDay() + 6) % 7))

  const plan: Array<[number, number, number, number]> = [
    // [作品下标, 周几偏移, 小时, 集号]
    [0, 0, 23, 13],
    [4, 1, 22, 9],
    [6, 2, 24, 6],
    [10, 3, 23, 19],
    [1, 4, 22, 25],
    [2, 5, 21, 13],
    [5, 6, 23, 27],
    [0, 6, 25, 14],
  ]
  return plan.map(([idx, dayOffset, hour, episode], i) => {
    const at = new Date(monday)
    at.setDate(monday.getDate() + dayOffset)
    at.setHours(hour, 0, 0, 0) // hour 可以 >24，Date 会自动进位到次日
    return { id: 1000 + i, title: SHOWS[idx]!.title, episode, airingAt: at.toISOString() }
  })
}

export { SHOWS as FAKE_SHOWS }
