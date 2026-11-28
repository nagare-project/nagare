// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MediaSummary } from '../../components/media/types'
import { resetNearViewportForTests } from '../../components/media/artworkLoading'
import { resetArtworkCacheForTests } from '../../lib/imageCache'
import type { PosterTones } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { getByRole, getByText, queryByRole } from '../../test/queries'
import { installFakeIntersectionObserver } from '../../test/intersectionObserver'
import type { FakeIntersectionObservers } from '../../test/intersectionObserver'
import { EntryInfo, EntryRelations } from './EntryInfo'

const collectionState = vi.hoisted(() => ({
  loggedIn: false,
  entries: [] as { anilistId: number; status: string; currentEpisode: number; score: number | null }[],
}))

vi.mock('../../components/media/CollectionContext', () => ({
  useCollection: () => ({
    loggedIn: collectionState.loggedIn,
    entries: collectionState.entries,
    loading: false,
    error: null,
    reload: vi.fn(),
    save: vi.fn(),
    remove: vi.fn(),
  }),
}))

const TONES_A: PosterTones = { hue: 140, tone: '#88c197', quiet: '#77a583', mid: '#497555', low: '#0c2514', onTone: '#020f06' }
const TONES_B: PosterTones = { hue: 35, tone: '#c9a97d', quiet: '#b09166', mid: '#7d623b', low: '#261b0c', onTone: '#120d05' }

let io: FakeIntersectionObservers
beforeEach(() => {
  collectionState.loggedIn = false
  collectionState.entries = []
  resetArtworkCacheForTests()
  resetNearViewportForTests()
  io = installFakeIntersectionObserver()
})
afterEach(() => {
  resetNearViewportForTests()
  io.restore()
})

describe('作品信息（EntryInfo）', () => {
  it('渲染无框资料表（含日文原名 lang="ja"）、类型胶囊与长简介折叠/展开', async () => {
    const longDesc = '这是一段足够长的简介内容，用来验证超过一百六十个字符后会自动进入折叠状态并显示展开按钮。'.repeat(5)
    const media: MediaSummary = {
      id: 154587,
      title: '葬送的芙莉莲',
      titleNative: '葬送のフリーレン',
      format: 'TV',
      source: 'MANGA',
      episodes: 28,
      duration: 24,
      startDate: '2026-10-03',
      studios: ['Madhouse'],
      description: longDesc,
      genres: ['奇幻', '冒险'],
      watched: 0,
    }
    const { container, unmount } = await mount(<EntryInfo media={media} />)

    const region = getByRole(container, 'region', { name: '作品信息' })
    const desc = region.querySelector('.entry-description-text')!
    expect(desc.className).toContain('entry-description-text--folded')

    const toggle = getByRole(region, 'button', { name: '展开简介' })
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(toggle.getAttribute('aria-controls')).toBe(desc.id)
    await act(async () => toggle.click())
    expect(toggle.textContent).toBe('收起简介')
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(desc.className).not.toContain('entry-description-text--folded')

    const tags = region.querySelectorAll('.entry-tag')
    expect([...tags].map((el) => el.textContent)).toEqual(['奇幻', '冒险'])

    const rows = [...region.querySelectorAll('.entry-facts > div')]
    expect(rows.map((row) => [row.querySelector('dt')?.textContent, row.querySelector('dd')?.textContent])).toEqual([
      ['类型', 'TV'],
      ['原作', '漫画'],
      ['集数', '28 集'],
      ['单集时长', '24 分钟'],
      ['首播日期', '2026-10-03'],
      ['制作公司', 'Madhouse'],
      ['原名', '葬送のフリーレン'],
    ])
    const nativeDd = rows[rows.length - 1]!.querySelector('dd')!
    expect(nativeDd.getAttribute('lang')).toBe('ja')
    expect(nativeDd.className).toContain('entry-facts-native')
    await unmount()
  })

  it('短简介不显示展开按钮，无简介时回落为「暂无简介。」', async () => {
    const media: MediaSummary = { id: 1, title: '短番', episodes: null, watched: 0, genres: [] }
    const { container, unmount } = await mount(<EntryInfo media={media} />)
    expect(getByText(container, '暂无简介。')).toBeDefined()
    expect(queryByRole(container, 'button', { name: /简介/ })).toBeNull()
    expect(container.querySelector('.entry-facts')).toBeNull()
    await unmount()
  })
})

describe('角色（EntryRelations）', () => {
  it('镜像双侧竖图行：角色与声优（有图与缺图占位）、标题带人数、离视口近即切 eager', async () => {
    const media: MediaSummary = {
      id: 1,
      title: '作品',
      episodes: 12,
      watched: 0,
      genres: [],
      characters: [
        { name: '芙莉莲', image: '/char/frieren.jpg', role: 'MAIN', actor: '種﨑敦美', actorImage: '/va/tanezaki.jpg' },
        { name: '菲伦', image: '/char/fern.jpg', role: 'MAIN', actor: '市ノ瀬加那' },
        { name: '辛美尔', image: '', role: 'SUPPORTING' },
      ],
    }
    const { container, unmount } = await mount(<EntryRelations media={media} />)
    const region = getByRole(container, 'region', { name: '角色' })
    expect(region.querySelector('.work-section-count')?.textContent).toBe('3')

    const rows = [...region.querySelectorAll('.entry-characters .person-row')]
    expect(rows).toHaveLength(3)

    // 第 1 行：角色 + 声优均有图，通过 useArtworkImage 懒切 eager
    const imgs = [...region.querySelectorAll('.entry-characters img')]
    expect(imgs).toHaveLength(3)
    for (const img of imgs) {
      expect(img.getAttribute('loading')).toBe('lazy')
      expect(io.isObserved(img)).toBe(true)
    }
    await act(async () => {
      for (const img of imgs) io.intersect(img)
    })
    for (const img of imgs) {
      expect(img.getAttribute('loading')).toBe('eager')
    }

    const row1Va = rows[0]!.querySelector('.person-side--va')!
    expect(row1Va.querySelector('.person-role')?.textContent).toBe('日语')
    expect(row1Va.querySelector('.person-name')?.textContent).toBe('種﨑敦美')
    expect(row1Va.querySelector('.person-name')?.getAttribute('lang')).toBe('ja')
    expect(row1Va.querySelector('img')?.getAttribute('src')).toBe('/va/tanezaki.jpg')

    // 第 2 行：声优缺 actorImage 时画首字竖图占位框
    const row2Va = rows[1]!.querySelector('.person-side--va')!
    expect(row2Va.querySelector('.person-portrait--va')?.textContent).toBe('市')
    expect(row2Va.querySelector('.person-portrait--va')?.getAttribute('aria-hidden')).toBe('true')

    // 第 3 行：无声优不渲染右侧；角色缺图画首字竖图占位框
    expect(rows[2]!.querySelector('.person-side--va')).toBeNull()
    expect(rows[2]!.querySelector('.person-role--char')?.textContent).toBe('配角')
    expect(rows[2]!.querySelector('.person-portrait--blank')?.textContent).toBe('辛')
    await unmount()
  })
})

describe('关联作品与相关推荐（EntryRelations）', () => {
  it('关联作品：横向关系条卡（关系胶囊、标题、元信息与各自海报色）', async () => {
    const media: MediaSummary = {
      id: 154587,
      title: '葬送的芙莉莲',
      episodes: 28,
      watched: 0,
      genres: [],
      relations: [
        {
          type: 'SEQUEL',
          media: { id: 201, title: '葬送的芙莉莲 第二季', format: 'TV', status: 'NOT_YET_RELEASED', episodes: null, watched: 0, genres: [], cover: '/cover/s2.jpg', posterTones: TONES_A },
        },
        {
          type: 'SIDE_STORY',
          media: { id: 202, title: '葬送的芙莉莲 番外', format: 'ONA', status: 'FINISHED', episodes: 11, watched: 0, genres: [], cover: '' },
        },
      ],
    }
    const { container, unmount } = await mount(<EntryRelations media={media} />)
    const region = getByRole(container, 'region', { name: '关联作品' })
    expect(region.querySelector('.work-section-count')?.textContent).toBe('2')

    const cards = [...region.querySelectorAll<HTMLAnchorElement>('.relations-grid .relation-card')]
    expect(cards).toHaveLength(2)
    expect(cards[0]!.getAttribute('href')).toBe('/entry?id=201')
    expect(cards[0]!.style.getPropertyValue('--poster-tone')).toBe('#88c197')
    expect(cards[0]!.querySelector('.relation-kind')?.textContent).toBe('续作')
    expect(cards[0]!.querySelector('.relation-title')?.textContent).toBe('葬送的芙莉莲 第二季')
    expect(cards[0]!.querySelector('.relation-meta')?.textContent).toBe('TV · 尚未播出')

    expect(cards[1]!.getAttribute('href')).toBe('/entry?id=202')
    expect(cards[1]!.querySelector('.relation-kind')?.textContent).toBe('番外')
    expect(cards[1]!.querySelector('.relation-meta')?.textContent).toBe('网络动画 · 11 集 · 已完结')
    expect(cards[1]!.querySelector('.poster-art-mark')?.textContent).toBe('葬')
    await unmount()
  })

  it('相关推荐：无框影院海报卡（各自海报色、评分角标、年份季度元信息与登录后快速追番按钮）', async () => {
    collectionState.loggedIn = true
    collectionState.entries = [{ anilistId: 302, status: 'watching', currentEpisode: 5, score: null }]
    const media: MediaSummary = {
      id: 154587,
      title: '葬送的芙莉莲',
      episodes: 28,
      watched: 0,
      genres: [],
      recommendations: [
        { id: 301, title: '药屋少女的呢喃', cover: '/cover/kusuriya.jpg', score: 87, year: 2023, season: 'FALL', episodes: 24, watched: 0, genres: [], posterTones: TONES_B },
        { id: 302, title: '迷宫饭', cover: '/cover/dungeon.jpg', score: 0, year: 2024, season: 'WINTER', episodes: 24, watched: 0, genres: [] },
      ],
    }
    const { container, unmount } = await mount(<EntryRelations media={media} />)
    const region = getByRole(container, 'region', { name: '相关推荐' })

    const cards = [...region.querySelectorAll<HTMLLIElement>('.recs-grid .rec-card')]
    expect(cards).toHaveLength(2)

    expect(cards[0]!.style.getPropertyValue('--poster-tone')).toBe('#c9a97d')
    expect(cards[0]!.querySelector('.rec-score')?.textContent).toBe('8.7')
    expect(cards[0]!.querySelector('.rec-meta')?.textContent).toBe('2023 秋 · 24 集')
    expect(getByRole(region, 'link', { name: '药屋少女的呢喃' }).getAttribute('href')).toBe('/entry?id=301')
    const addBtn = getByRole(cards[0]!, 'button', { name: '加入追番 药屋少女的呢喃' })
    expect(addBtn.getAttribute('data-follow')).toBe('off')

    // score 为 0 时不画评分角标；已在追番列表中时按钮高亮为已追并显示进度
    expect(cards[1]!.querySelector('.rec-score')).toBeNull()
    expect(cards[1]!.querySelector('.rec-meta')?.textContent).toBe('2024 冬 · 24 集 · 已看 5')
    const editBtn = getByRole(cards[1]!, 'button', { name: '编辑收藏 迷宫饭' })
    expect(editBtn.getAttribute('data-follow')).toBe('on')
    await unmount()
  })

  it('未登录时不渲染推荐卡上的快速追番按钮', async () => {
    collectionState.loggedIn = false
    const media: MediaSummary = {
      id: 1,
      title: '作品',
      episodes: 12,
      watched: 0,
      genres: [],
      recommendations: [{ id: 301, title: '推荐一', cover: '/a.jpg', episodes: 12, watched: 0, genres: [] }],
    }
    const { container, unmount } = await mount(<EntryRelations media={media} />)
    const region = getByRole(container, 'region', { name: '相关推荐' })
    expect(queryByRole(region, 'button', { name: /追番|收藏/ })).toBeNull()
    await unmount()
  })
})
