import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { CheckContext, emptyObject, errorEnvelope, okEnvelope, type Schema } from '../test/contract'
import * as c from '../test/apiContracts'

/**
 * Go 处理器测试写下的 API 黄金响应（仓库根 testdata/api/*.json）逐份严格解码：
 * 缺字段、类型不对、多出客户端不认识的字段都算失败 —— Go 改了响应，这里必须跟着改 TS 类型。
 * 每份黄金文件都要在下表里登记；表里登记了而文件不存在也算失败。说明见 docs/api-goldens.md。
 */

const GOLDEN_DIR = fileURLToPath(new URL('../../../testdata/api/', import.meta.url))

type Contract = Pick<Schema<unknown>, 'check'>

const CONTRACTS: Record<string, Contract> = {
  // 媒体库与作品关联
  'library-empty': okEnvelope(c.libraryData),
  'library-populated': okEnvelope(c.libraryData),
  'library-folder-add': okEnvelope(c.addFolderData),
  'library-rescan': okEnvelope(c.rescanData),
  'library-folder-remove-not-found': errorEnvelope,
  'library-association-manual': okEnvelope(c.associationData),
  'library-association-auto': okEnvelope(c.associationData),
  'library-association-dismiss-synced': okEnvelope(c.associationData),
  'fs-dirs': okEnvelope(c.dirListing),
  // 设置、mpv、播放、账号
  'settings': okEnvelope(c.settingsData),
  'settings-desktop-window': okEnvelope(c.settingsData),
  'settings-desktop-fallback': okEnvelope(c.settingsData),
  'cover-cache-clear': okEnvelope(c.coverCacheClearResult),
  'mpv-detect-found': okEnvelope(c.mpvInfo),
  'mpv-detect-missing': okEnvelope(c.mpvInfo),
  'play': okEnvelope(c.playData),
  'player-status-idle': okEnvelope(c.playerStatus),
  'player-status-idle-with-failures': okEnvelope(c.playerStatus),
  'player-status-playing': okEnvelope(c.playerStatus),
  'player-status-paused-with-failures': okEnvelope(c.playerStatus),
  'player-stop': okEnvelope(emptyObject),
  'player-sync-retry': okEnvelope(c.syncRetryResult),
  'player-sync-retry-done': okEnvelope(c.syncRetryResult),
  'player-config': okEnvelope(c.playerConfigResult),
  'player-config-apply-error': okEnvelope(c.playerConfigResult),
  'auth-login': okEnvelope(c.loginData),
  'auth-logout': okEnvelope(emptyObject),
  // 目录与收藏
  'catalog-discover': okEnvelope(c.discoverData),
  'catalog-seasonal': okEnvelope(c.seasonalData),
  'catalog-seasonal-bad-season': errorEnvelope,
  'catalog-schedule': okEnvelope(c.scheduleData),
  'catalog-detail': okEnvelope(c.summaryMedia),
  'catalog-search': okEnvelope(c.catalogSearchData),
  'catalog-unavailable': errorEnvelope,
  'catalog-discover-offline': okEnvelope(c.discoverData),
  'catalog-seasonal-offline': okEnvelope(c.seasonalData),
  'catalog-schedule-offline': okEnvelope(c.scheduleData),
  'catalog-detail-offline': okEnvelope(c.summaryMedia),
  'catalog-episodes': okEnvelope(c.episodeMetadataList),
  'catalog-episode-offset-known': okEnvelope(c.episodeOffset),
  'catalog-episode-offset-unknown': okEnvelope(c.episodeOffset),
  'lists': okEnvelope(c.collectionData),
  'lists-logged-out': okEnvelope(c.collectionData),
  'lists-save': okEnvelope(c.collectionEntry),
  'lists-delete': okEnvelope(c.collectionDelete),
  // 自更新（internal/update）
  'update-status-unchecked': okEnvelope(c.updateView),
  'update-status-available': okEnvelope(c.updateView),
  'update-status-check-failed': okEnvelope(c.updateView),
  'update-config-disabled': okEnvelope(c.updateView),
  // 桌面版专属（internal/api/sources）
  'desktop-settings': okEnvelope(c.settingsData),
  'desktop-torrent-status-idle': okEnvelope(c.torrentStatus),
  'desktop-torrent-status-active': okEnvelope(c.torrentStatus),
  'desktop-torrent-play-select': okEnvelope(c.torrentPlayData),
  'desktop-torrent-play-started': okEnvelope(c.torrentPlayData),
  'desktop-torrent-config': okEnvelope(c.torrentConfigData),
  'desktop-torrent-unavailable': errorEnvelope,
  'desktop-downloads': okEnvelope(c.downloadsData),
  'desktop-source-plugin': okEnvelope(c.sourcePluginView),
  'desktop-source-plugin-ready': okEnvelope(c.sourcePluginView),
  'desktop-source-plugin-sources-error': okEnvelope(c.sourcePluginView),
  'desktop-source-plugin-failed': okEnvelope(c.sourcePluginView),
  'desktop-sources-empty': okEnvelope(c.sourcesData),
  'desktop-sources': okEnvelope(c.sourcesData),
}

const goldenNames = readdirSync(GOLDEN_DIR)
  .filter(name => name.endsWith('.json'))
  .map(name => name.slice(0, -'.json'.length))
  .sort()

function readGolden(name: string): unknown {
  return JSON.parse(readFileSync(join(GOLDEN_DIR, `${name}.json`), 'utf8'))
}

describe('API 黄金响应', () => {
  it('每份黄金文件都登记了解码契约，登记的也都有文件', () => {
    expect(goldenNames.filter(name => !(name in CONTRACTS))).toEqual([])
    expect(Object.keys(CONTRACTS).filter(name => !goldenNames.includes(name)).sort()).toEqual([])
  })

  it.each(goldenNames.filter(name => name in CONTRACTS))('%s 能被严格解码', name => {
    const ctx = new CheckContext()
    CONTRACTS[name]?.check(readGolden(name), name, ctx)
    expect(ctx.errors).toEqual([])
  })

  it('TS 类型里的每个可选字段、每个联合分支都至少出现在一份黄金文件里', () => {
    // 从没出现过的可选字段：要么补一份黄金用例，要么是 TS 里留着一个后端早就不发的字段
    const ctx = new CheckContext()
    for (const name of goldenNames) CONTRACTS[name]?.check(readGolden(name), name, ctx)
    expect(ctx.errors).toEqual([])
    expect(ctx.uncovered()).toEqual([])
  })
})

describe('严格解码器本身', () => {
  const schema = okEnvelope(c.episodeOffset)
  const errorsOf = (value: unknown): string[] => {
    const ctx = new CheckContext()
    schema.check(value, 'x', ctx)
    return ctx.errors
  }

  it('缺必填字段、类型不对、多出字段都报错', () => {
    expect(errorsOf({ success: true, data: { known: true, offset: 12 } })).toEqual([])
    expect(errorsOf({ success: true, data: { known: true } })).toEqual(['x.data.offset: 缺少必填字段'])
    expect(errorsOf({ success: true, data: { known: 'yes', offset: 12 } })[0]).toMatch(/^x\.data\.known: 应为 boolean/)
    expect(errorsOf({ success: true, data: { known: true, offset: 1.5 } })[0]).toMatch(/^x\.data\.offset: 应为 整数/)
    expect(errorsOf({ success: true, data: { known: true, offset: 12, extra: 1 } })[0]).toMatch(/^x\.data\.extra: 客户端类型里没有这个字段/)
  })

  it('可选字段为 null 与缺席不是一回事', () => {
    const ctx = new CheckContext()
    c.playData.check({ title: 't', danmaku: { state: 'ok' }, fileId: null }, 'p', ctx)
    expect(ctx.errors[0]).toMatch(/^p\.fileId: 应为 string/)
  })
})
