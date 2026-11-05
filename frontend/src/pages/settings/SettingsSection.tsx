import type { ReactNode } from 'react'
import type { SettingsSectionId, SettingsSectionItem } from './sections'

/** 分区标题的 id：导航切换后把焦点移到这里 */
export function sectionHeadingId(id: SettingsSectionId): string {
  return `settings-${id}-title`
}

/**
 * 一个设置分区：标题 + 一句说明 + 面板。
 *
 * 不是当前分区时用 hidden 藏起来而不是卸载：切到别的分区再回来，没提交的表单（登录草稿、
 * 端口、tracker）还在。hidden 的内容读屏同样读不到，所以每一区都带自己的 h2。
 * 标题 tabIndex=-1：只给脚本移焦点用，Tab 不会停在上面。
 */
export function SettingsSection({ section, active, children }: { section: SettingsSectionItem; active: boolean; children: ReactNode }) {
  const headingId = sectionHeadingId(section.id)
  return (
    <section className="settings-section" hidden={!active} aria-labelledby={headingId}>
      <div className="settings-section-head">
        <h2 id={headingId} className="settings-section-title" tabIndex={-1}>
          {section.label}
        </h2>
        <p className="settings-section-desc">{section.description}</p>
      </div>
      {children}
    </section>
  )
}
