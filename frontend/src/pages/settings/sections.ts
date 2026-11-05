import type { IconName } from '../../components/ui/Icon'

/**
 * 设置页的分类（DESIGN.md「「我的」与设置」）：账号在最上面，下面是三组 ——
 * 媒体库与 NAS / 播放 / 关于；桌面另多「磁力播放」「来源」「更新」「扩展」四项，
 * 前两项归在「播放」，更新归在「关于」，扩展是独立的一页（/extensions），从这里进。
 *
 * id 就是地址里的 #hash：安装引导、文件夹入口、搜索页的「去设置」都按它直达，改名要连同这些入口一起改。
 */
export type SettingsSectionId =
  | 'account'
  | 'folders'
  | 'player'
  | 'playback'
  | 'torrent'
  | 'sources'
  | 'update'
  | 'background'
  | 'about'

export interface SettingsSectionItem {
  kind: 'section'
  id: SettingsSectionId
  label: string
  icon: IconName
  /** 分区标题下的一句话：这一区管什么 */
  description: string
}

/** 不在设置页里、而是另一页的入口（扩展） */
export interface SettingsPageItem {
  kind: 'page'
  to: '/extensions'
  label: string
  icon: IconName
}

export interface SettingsGroup {
  key: string
  /** 组名；账号那一组没有组名 */
  label?: string
  items: ReadonlyArray<SettingsSectionItem | SettingsPageItem>
}

export const SETTINGS_GROUPS: readonly SettingsGroup[] = [
  {
    key: 'account',
    items: [
      { kind: 'section', id: 'account', label: '账号', icon: 'user', description: '登录后显示弹幕，看完的集数会同步到账号。不登录也能播放。' },
    ],
  },
  {
    key: 'library',
    label: '媒体库与 NAS',
    items: [
      { kind: 'section', id: 'folders', label: '媒体文件夹', icon: 'folder', description: 'nagare 扫描这些文件夹，把视频按作品整理进媒体库。外接硬盘与 NAS 上的番都按文件夹添加。' },
    ],
  },
  {
    key: 'playback',
    label: '播放',
    items: [
      { kind: 'section', id: 'player', label: '播放器', icon: 'monitor', description: 'nagare 自己不解码，本地文件与磁力都交给 mpv 播放。' },
      { kind: 'section', id: 'playback', label: '画质增强', icon: 'sparkle', description: 'Anime4K 实时超分：在 mpv 里用显卡把画面放大、去模糊。' },
      { kind: 'section', id: 'torrent', label: '磁力播放', icon: 'download', description: '边下边播怎么找分享者、整部下载放在哪里，以及缓存。' },
      { kind: 'section', id: 'sources', label: '来源', icon: 'rss', description: 'nagare 不内置任何来源：磁力与在线候选都来自你自己安装的插件和规则，在你的电脑上执行。' },
      { kind: 'page', to: '/extensions', label: '扩展', icon: 'extension' },
    ],
  },
  {
    key: 'about',
    label: '关于',
    items: [
      { kind: 'section', id: 'update', label: '更新', icon: 'refresh', description: '检查新版本；能自更新时一键下载、验证签名并替换。' },
      { kind: 'section', id: 'background', label: '后台与退出', icon: 'power', description: '关掉界面之后 nagare 在哪里继续运行，以及怎么彻底退出。' },
      { kind: 'section', id: 'about', label: '关于 nagare', icon: 'info', description: '版本、数据与日志的位置、许可。' },
    ],
  },
]

export const SETTINGS_SECTIONS: readonly SettingsSectionItem[] = SETTINGS_GROUPS.flatMap((group) =>
  group.items.filter((item): item is SettingsSectionItem => item.kind === 'section'),
)

/** 没有 #hash 或认不出来时打开的那一区 */
export const DEFAULT_SECTION: SettingsSectionItem = SETTINGS_SECTIONS[0]!

/** 地址里的 hash → 分区；认不出来的落到默认分区 */
export function sectionFromHash(hash: string): SettingsSectionItem {
  return SETTINGS_SECTIONS.find((item) => item.id === hash) ?? DEFAULT_SECTION
}
