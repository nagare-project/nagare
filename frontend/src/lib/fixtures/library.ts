// FIXME(G1/G2/G3): 假数据，真接口尚未接通。
import type { MediaSummary } from '../../components/media/types'
import type { FakeAiring } from './types'

/**
 * 一份共用的作品样本。各页从这里切片，而不是各写一份 ——
 * 同一部番在「我的列表」和「发现」里必须是同一个标题同一个封面，
 * 否则演示时一眼看穿是拼的。
 */
const SHOWS: readonly MediaSummary[] = [
  { id: 154587, title: '葬送的芙莉莲', titleNative: '葬送のフリーレン', year: 2023, season: '秋', episodes: 28, watched: 12, score: 92, genres: ['奇幻', '冒险', '剧情'], description: '打倒魔王之后，勇者一行人各奔东西。身为精灵的魔法使芙莉莲，用漫长的寿命重新丈量那段短暂的旅程，以及人类留下的东西。' },
  { id: 162804, title: '药屋少女的呢喃', titleNative: '薬屋のひとりごと', year: 2023, season: '秋', episodes: 24, watched: 24, score: 87, genres: ['悬疑', '剧情', '历史'], description: '在花街长大的药师少女猫猫，被卖进后宫当下女。她凭着对毒与药的执念，一头撞进宫廷深处那些没人想说破的事。' },
  { id: 140960, title: '孤独摇滚！', titleNative: 'ぼっち・ざ・ろっく！', year: 2022, season: '秋', episodes: 12, watched: 12, score: 88, genres: ['音乐', '喜剧', '日常'], description: '一个连搭话都费劲的吉他少女，被硬拽进了乐队。社恐没治好，但台上那几分钟是真的。' },
  { id: 21519, title: '你的名字。', titleNative: '君の名は。', year: 2016, season: '夏', episodes: 1, watched: 1, score: 85, genres: ['剧情', '恋爱', '超自然'], description: '两个素不相识的高中生开始不定期互换身体。等他们想见面时，才发现隔着的不只是距离。' },
  { id: 145064, title: '间谍过家家', titleNative: 'SPY×FAMILY', year: 2022, season: '春', episodes: 25, watched: 8, score: 86, genres: ['喜剧', '动作', '日常'], description: '为了任务，间谍临时组了个家：一个会读心的女儿，一个是杀手的妻子。三个人都在瞒着彼此，却意外地像一家人。' },
  { id: 101922, title: '鬼灭之刃', titleNative: '鬼滅の刃', year: 2019, season: '春', episodes: 26, watched: 0, score: 83, genres: ['动作', '超自然', '历史'], description: '全家被杀，只剩下变成鬼的妹妹。少年拿起刀去找把她变回人的办法，代价是从此活在夜里。' },
  { id: 113415, title: '咒术回战', titleNative: '呪術廻戦', year: 2020, season: '秋', episodes: 24, watched: 5, score: 85, genres: ['动作', '超自然'], description: '人的负面情绪会凝成咒灵。少年吞下了最强咒灵的一部分，从此在「什么时候被处决」的倒计时里战斗。' },
  { id: 108465, title: '摇曳露营△ 第二季', titleNative: 'ゆるキャン△ SEASON2', year: 2021, season: '冬', episodes: 13, watched: 0, score: 84, genres: ['日常', '治愈'], description: '几个女生在冬天的湖边扎营、煮东西、看星星。没有冲突，也不需要冲突。' },
  { id: 131681, title: '欢迎来到实力至上主义的教室 第二季', year: 2022, season: '夏', episodes: 13, watched: 3, score: 79, genres: ['心理', '剧情'], description: '一所把学生按「实力」分级的学校，规则写得漂亮，玩法全在字缝里。有人从一开始就在装弱。' },
  { id: 130003, title: '别当欧尼酱了！', titleNative: 'お兄ちゃんはおしまい！', year: 2023, season: '冬', episodes: 12, watched: 0, score: 76, genres: ['喜剧', '日常'], description: '闭门不出的哥哥一觉醒来变成了妹妹。身体换了，生活也被迫重新过一遍。' },
  { id: 151970, title: '迷宫饭', titleNative: 'ダンジョン飯', year: 2024, season: '冬', episodes: 24, watched: 18, score: 86, genres: ['奇幻', '冒险', '喜剧'], description: '妹妹被龙吃掉了，队伍散了，补给也没了。莱欧斯决定一边吃魔物一边往地牢深处走 —— 反正食材遍地都是。' },
  { id: 163132, title: '战场上的普通话', year: 2026, season: '冬', episodes: null, watched: 0, score: 0, genres: ['剧情'], description: '一部编造出来的作品，用来占「即将播出、集数与评分都还未知」这一格 —— 界面必须扛得住这种半空的条目。' },
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
export const FAKE_LISTS: Record<ListStatus, MediaSummary[]> = {
  watching: SHOWS.filter((s) => s.watched > 0 && (s.episodes === null || s.watched < s.episodes)),
  completed: SHOWS.filter((s) => s.episodes !== null && s.watched === s.episodes),
  planning: SHOWS.filter((s) => s.watched === 0 && s.id % 2 === 0),
  paused: SHOWS.filter((s) => s.watched === 0 && s.id % 2 === 1).slice(0, 2),
  dropped: [],
}

/**
 * 发现页的板块，顺序与 seanime 的 anime 标签页一致：
 * 热门 → 最近更新 → 本季 → 上季 → 补番 → 即将播出 → 剧场版。
 */
export const FAKE_DISCOVER = {
  trending: [SHOWS[10]!, SHOWS[0]!, SHOWS[6]!, SHOWS[4]!, SHOWS[1]!, SHOWS[2]!],
  recent: [SHOWS[0]!, SHOWS[10]!, SHOWS[1]!, SHOWS[4]!, SHOWS[8]!],
  thisSeason: [SHOWS[11]!, SHOWS[10]!, SHOWS[0]!, SHOWS[1]!],
  pastSeason: [SHOWS[2]!, SHOWS[9]!, SHOWS[7]!, SHOWS[8]!],
  missedSequels: [SHOWS[6]!, SHOWS[5]!, SHOWS[8]!],
  upcoming: [SHOWS[11]!, SHOWS[7]!, SHOWS[8]!, SHOWS[9]!],
  movies: [SHOWS[3]!],
} satisfies Record<string, MediaSummary[]>

/** hero 轮播的候选：有简介的那几部（没简介的 hero 是空的，不好看） */
export const FAKE_FEATURED: MediaSummary[] = SHOWS.filter((s) => s.description !== undefined)

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
