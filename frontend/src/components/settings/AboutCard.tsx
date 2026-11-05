import type { SettingsData } from '../../lib/endpoints'
import { formatVersion } from '../../lib/format'
import { mono } from '../../theme'
import '../../styles/panels.css'
import './cards.css'

export interface AboutCardProps {
  settings: SettingsData
}

/** 平台名的展示写法 */
const PLATFORM_LABEL: Record<SettingsData['platform'], string> = {
  darwin: 'macOS',
  linux: 'Linux',
  windows: 'Windows',
}

/** 许可原文所在的仓库（公开仓库；nagare 本体以 AGPL-3.0 发布） */
const REPO_URL = 'https://github.com/nagare-project/nagare'

/**
 * 「版本与数据」：版本 / 平台 / 数据目录 / 日志路径 / 许可。
 * 没有远程遥测（M4 决议），出问题时用户靠这里找到日志文件自己贴出来。
 */
export function AboutCard({ settings }: AboutCardProps) {
  return (
    <section className="info-panel" aria-labelledby="about-heading">
      <h3 id="about-heading" className="info-panel-title">
        版本与数据
      </h3>
      <dl className="kv-list">
        <dt>版本</dt>
        <dd style={mono}>{formatVersion(settings.version)}</dd>
        <dt>平台</dt>
        <dd style={mono}>
          {PLATFORM_LABEL[settings.platform]} · {settings.arch}
        </dd>
        <dt>数据目录</dt>
        <dd style={mono}>{settings.dataDir === '' ? '（未知）' : settings.dataDir}</dd>
        <dt>日志文件</dt>
        <dd style={mono}>{settings.logPath === '' ? '（未知）' : settings.logPath}</dd>
        <dt>许可</dt>
        <dd>
          <a className="link" href={`${REPO_URL}/blob/main/LICENSE`} target="_blank" rel="noreferrer noopener">
            AGPL-3.0 ↗
          </a>
          <span className="about-sep" aria-hidden="true">
            ·
          </span>
          <a className="link" href={`${REPO_URL}/blob/main/THIRD_PARTY_NOTICES.md`} target="_blank" rel="noreferrer noopener">
            第三方组件的许可 ↗
          </a>
        </dd>
      </dl>
      <p className="info-panel-note">nagare 不上传任何诊断数据。遇到问题时，把日志文件的内容贴进 issue 即可。</p>
    </section>
  )
}
