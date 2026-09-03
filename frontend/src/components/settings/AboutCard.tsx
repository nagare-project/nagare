import type { SettingsData } from '../../lib/endpoints'
import { formatVersion } from '../../lib/format'
import { mono } from '../../theme'
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

/**
 * 「关于」卡：版本 / 平台 / 数据目录 / 日志路径。
 * 没有远程遥测（M4 决议），出问题时用户靠这里找到日志文件自己贴出来。
 */
export function AboutCard({ settings }: AboutCardProps) {
  return (
    <section className="panel settings-card" aria-labelledby="about-heading">
      <h2 id="about-heading" className="panel-heading">
        关于
      </h2>
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
      </dl>
      <p className="page-notice-copy">
        nagare 不上传任何诊断数据。遇到问题时，把日志文件的内容贴进 issue 即可。
      </p>
    </section>
  )
}
