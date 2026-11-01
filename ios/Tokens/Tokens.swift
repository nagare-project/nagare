// 由 tools/gentokens 从 design/tokens.json 生成，不要手改。
// 改 token：编辑 design/tokens.json，再在仓库根目录运行 go generate ./...（CI 检查生成文件是最新的）。
// 规则与用法见仓库根目录的 DESIGN.md。
//
// 颜色：有系统语义色对应的引用系统色（「增强对比度」由系统接管），没有的写固定 sRGB 值。
// App 固定暗色（.preferredColorScheme(.dark)），生成器只在系统色的深色值与 token 一致时才映射。
// 字号：引用系统文字样式（跟随动态字号），只有字标用打包的 Sora、固定字号。

import SwiftUI
import UIKit

/// 颜色（UIKit）
enum NagareUIColor {
    /// 页面底色（Level 0） · #000000
    static var bg: UIColor { .systemBackground }
    /// 卡片、面板、侧栏（Level 1） · #1c1c1e
    static var bgCard: UIColor { .secondarySystemBackground }
    /// 浮层、输入框、弹窗（Level 2 / 3） · #2c2c2e
    static var bgElevated: UIColor { .tertiarySystemBackground }
    /// 次要填充：ghost 按钮、标签底、圆形图标按钮 · rgba(120, 120, 128, 0.12)
    static var bgFill: UIColor { UIColor(red: 120.0 / 255, green: 120.0 / 255, blue: 128.0 / 255, alpha: 0.12) }
    /// 次要填充的悬停态 · rgba(120, 120, 128, 0.2)
    static var bgFillHover: UIColor { UIColor(red: 120.0 / 255, green: 120.0 / 255, blue: 128.0 / 255, alpha: 0.2) }
    /// 半透明分隔线、三级按钮描边（系统 separator 深色是 0.6，不一致，写固定值） · rgba(84, 84, 88, 0.65)
    static var separator: UIColor { UIColor(red: 84.0 / 255, green: 84.0 / 255, blue: 88.0 / 255, alpha: 0.65) }
    /// 不透明分隔线、卡片边框 · #38383a
    static var separatorOpaque: UIColor { .opaqueSeparator }
    /// 列表行之间的发丝线、顶栏玻璃底边 · rgba(255, 255, 255, 0.07)
    static var hairline: UIColor { UIColor(red: 255.0 / 255, green: 255.0 / 255, blue: 255.0 / 255, alpha: 0.07) }
    /// 列表行悬停时浮起的面 · rgba(255, 255, 255, 0.045)
    static var rowHover: UIColor { UIColor(red: 255.0 / 255, green: 255.0 / 255, blue: 255.0 / 255, alpha: 0.045) }
    /// 模态遮罩 · rgba(0, 0, 0, 0.6)
    static var overlay: UIColor { UIColor(red: 0.0 / 255, green: 0.0 / 255, blue: 0.0 / 255, alpha: 0.6) }
    /// 可点击色：链接、焦点环、进度条、选中态；实心按钮的底改用 accent-fill，浮层上的蓝字改用 accent-on-elevated · #0a84ff
    static var accent: UIColor { .systemBlue }
    /// 链接与蓝字的悬停态 · #409cff
    static var accentHover: UIColor { UIColor(red: 64.0 / 255, green: 156.0 / 255, blue: 255.0 / 255, alpha: 1) }
    /// 浮层（bg-elevated：弹窗、菜单、弹层）上的蓝字与链接（4.92:1） · #409cff
    static var accentOnElevated: UIColor { UIColor(red: 64.0 / 255, green: 156.0 / 255, blue: 255.0 / 255, alpha: 1) }
    /// 实心主按钮的底（白字 4.90:1） · #0070d6
    static var accentFill: UIColor { UIColor(red: 0.0 / 255, green: 112.0 / 255, blue: 214.0 / 255, alpha: 1) }
    /// 实心主按钮的悬停态（白字 4.61:1） · #0274de
    static var accentFillHover: UIColor { UIColor(red: 2.0 / 255, green: 116.0 / 255, blue: 222.0 / 255, alpha: 1) }
    /// 实心主按钮的按下态（白字 5.57:1） · #0066cc
    static var accentFillActive: UIColor { UIColor(red: 0.0 / 255, green: 102.0 / 255, blue: 204.0 / 255, alpha: 1) }
    /// 选中项的底、可点击色的轻填充 · rgba(10, 132, 255, 0.12)
    static var accentDim: UIColor { UIColor(red: 10.0 / 255, green: 132.0 / 255, blue: 255.0 / 255, alpha: 0.12) }
    /// 焦点环：不透明的实线 outline，所有可交互元素共用这一个，不随海报色变 · #0a84ff
    static var focusRing: UIColor { .systemBlue }
    /// 只读信息：信息标签、弹幕分区、只读高亮；不可点击 · #5ac8fa
    static var teal: UIColor { UIColor(red: 90.0 / 255, green: 200.0 / 255, blue: 250.0 / 255, alpha: 1) }
    /// teal 的底色填充 · rgba(90, 200, 250, 0.1)
    static var tealDim: UIColor { UIColor(red: 90.0 / 255, green: 200.0 / 255, blue: 250.0 / 255, alpha: 0.1) }
    /// 主文字 · #ffffff
    static var text: UIColor { .label }
    /// 要读的次要文字：说明、徽标、元信息、caption · rgba(235, 235, 245, 0.6)
    static var textSecondary: UIColor { .secondaryLabel }
    /// 只用于占位符、禁用态、纯装饰；不写要读的字 · rgba(235, 235, 245, 0.3)
    static var textTertiary: UIColor { .tertiaryLabel }
    /// 只用于禁用态 · rgba(235, 235, 245, 0.18)
    static var textQuaternary: UIColor { .quaternaryLabel }
    /// 顶栏链接的安静态 · rgba(235, 235, 245, 0.66)
    static var navLink: UIColor { UIColor(red: 235.0 / 255, green: 235.0 / 255, blue: 245.0 / 255, alpha: 0.66) }
    /// 已看完、同步成功、已完结 · #30d158
    static var success: UIColor { .systemGreen }
    /// 成功徽标的底 · rgba(48, 209, 88, 0.12)
    static var successDim: UIColor { UIColor(red: 48.0 / 255, green: 209.0 / 255, blue: 88.0 / 255, alpha: 0.12) }
    /// 连载中、待确认、需要注意 · #ff9f0a
    static var warning: UIColor { .systemOrange }
    /// 警示徽标的底 · rgba(255, 159, 10, 0.12)
    static var warningDim: UIColor { UIColor(red: 255.0 / 255, green: 159.0 / 255, blue: 10.0 / 255, alpha: 0.12) }
    /// 错误文字与图标；危险按钮的底改用 error-fill，浮层上的红字改用 error-on-elevated · #ff453a
    static var error: UIColor { .systemRed }
    /// 浮层上的红字、全局提示里的失败图标（4.94:1） · #ff6961
    static var errorOnElevated: UIColor { UIColor(red: 255.0 / 255, green: 105.0 / 255, blue: 97.0 / 255, alpha: 1) }
    /// 危险按钮的底（白字 4.87:1） · #d63027
    static var errorFill: UIColor { UIColor(red: 214.0 / 255, green: 48.0 / 255, blue: 39.0 / 255, alpha: 1) }
    /// 危险按钮的悬停态（白字 4.62:1） · #dc332a
    static var errorFillHover: UIColor { UIColor(red: 220.0 / 255, green: 51.0 / 255, blue: 42.0 / 255, alpha: 1) }
    /// 危险按钮的按下态（白字 5.56:1） · #c8281f
    static var errorFillActive: UIColor { UIColor(red: 200.0 / 255, green: 40.0 / 255, blue: 31.0 / 255, alpha: 1) }
    /// 错误徽标的底 · rgba(255, 69, 58, 0.12)
    static var errorDim: UIColor { UIColor(red: 255.0 / 255, green: 69.0 / 255, blue: 58.0 / 255, alpha: 0.12) }
    /// 信息提示（与 teal 同值） · #5ac8fa
    static var info: UIColor { UIColor(red: 90.0 / 255, green: 200.0 / 255, blue: 250.0 / 255, alpha: 1) }
    /// 顶栏毛玻璃（配 saturate(160%) blur(16px)） · rgba(9, 9, 11, 0.78)
    static var glassNav: UIColor { UIColor(red: 9.0 / 255, green: 9.0 / 255, blue: 11.0 / 255, alpha: 0.78) }
    /// 作品页 hero 的工具按钮（玻璃·轻） · rgba(255, 255, 255, 0.08)
    static var glassLight: UIColor { UIColor(red: 255.0 / 255, green: 255.0 / 255, blue: 255.0 / 255, alpha: 0.08) }
    /// 玻璃·轻的描边 · rgba(255, 255, 255, 0.13)
    static var glassLightBorder: UIColor { UIColor(red: 255.0 / 255, green: 255.0 / 255, blue: 255.0 / 255, alpha: 0.13) }
    /// 作品页 hero 的次级入口（玻璃·重） · rgba(255, 255, 255, 0.12)
    static var glassHeavy: UIColor { UIColor(red: 255.0 / 255, green: 255.0 / 255, blue: 255.0 / 255, alpha: 0.12) }
    /// 玻璃·重的描边 · rgba(255, 255, 255, 0.2)
    static var glassHeavyBorder: UIColor { UIColor(red: 255.0 / 255, green: 255.0 / 255, blue: 255.0 / 255, alpha: 0.2) }
    /// 下拉菜单、弹层的底（配 blur） · rgba(24, 24, 28, 0.96)
    static var popover: UIColor { UIColor(red: 24.0 / 255, green: 24.0 / 255, blue: 28.0 / 255, alpha: 0.96) }
    /// 弹层描边 · rgba(255, 255, 255, 0.08)
    static var popoverBorder: UIColor { UIColor(red: 255.0 / 255, green: 255.0 / 255, blue: 255.0 / 255, alpha: 0.08) }
    /// 全局提示胶囊的底（配 blur(20px) saturate(150%)） · rgba(18, 18, 22, 0.94)
    static var toast: UIColor { UIColor(red: 18.0 / 255, green: 18.0 / 255, blue: 22.0 / 255, alpha: 0.94) }
    /// 全局提示胶囊的描边 · rgba(255, 255, 255, 0.12)
    static var toastBorder: UIColor { UIColor(red: 255.0 / 255, green: 255.0 / 255, blue: 255.0 / 255, alpha: 0.12) }
    /// 压在封面上的徽标底（配 blur(8px)），只让文字带颜色；播放器进度条上的悬停时间也用它 · rgba(0, 0, 0, 0.75)
    static var badgeScrim: UIColor { UIColor(red: 0.0 / 255, green: 0.0 / 255, blue: 0.0 / 255, alpha: 0.75) }
}

/// 颜色（SwiftUI），与 NagareUIColor 一一对应
enum NagareColor {
    static let bg = Color(NagareUIColor.bg)
    static let bgCard = Color(NagareUIColor.bgCard)
    static let bgElevated = Color(NagareUIColor.bgElevated)
    static let bgFill = Color(NagareUIColor.bgFill)
    static let bgFillHover = Color(NagareUIColor.bgFillHover)
    static let separator = Color(NagareUIColor.separator)
    static let separatorOpaque = Color(NagareUIColor.separatorOpaque)
    static let hairline = Color(NagareUIColor.hairline)
    static let rowHover = Color(NagareUIColor.rowHover)
    static let overlay = Color(NagareUIColor.overlay)
    static let accent = Color(NagareUIColor.accent)
    static let accentHover = Color(NagareUIColor.accentHover)
    static let accentOnElevated = Color(NagareUIColor.accentOnElevated)
    static let accentFill = Color(NagareUIColor.accentFill)
    static let accentFillHover = Color(NagareUIColor.accentFillHover)
    static let accentFillActive = Color(NagareUIColor.accentFillActive)
    static let accentDim = Color(NagareUIColor.accentDim)
    static let focusRing = Color(NagareUIColor.focusRing)
    static let teal = Color(NagareUIColor.teal)
    static let tealDim = Color(NagareUIColor.tealDim)
    static let text = Color(NagareUIColor.text)
    static let textSecondary = Color(NagareUIColor.textSecondary)
    static let textTertiary = Color(NagareUIColor.textTertiary)
    static let textQuaternary = Color(NagareUIColor.textQuaternary)
    static let navLink = Color(NagareUIColor.navLink)
    static let success = Color(NagareUIColor.success)
    static let successDim = Color(NagareUIColor.successDim)
    static let warning = Color(NagareUIColor.warning)
    static let warningDim = Color(NagareUIColor.warningDim)
    static let error = Color(NagareUIColor.error)
    static let errorOnElevated = Color(NagareUIColor.errorOnElevated)
    static let errorFill = Color(NagareUIColor.errorFill)
    static let errorFillHover = Color(NagareUIColor.errorFillHover)
    static let errorFillActive = Color(NagareUIColor.errorFillActive)
    static let errorDim = Color(NagareUIColor.errorDim)
    static let info = Color(NagareUIColor.info)
    static let glassNav = Color(NagareUIColor.glassNav)
    static let glassLight = Color(NagareUIColor.glassLight)
    static let glassLightBorder = Color(NagareUIColor.glassLightBorder)
    static let glassHeavy = Color(NagareUIColor.glassHeavy)
    static let glassHeavyBorder = Color(NagareUIColor.glassHeavyBorder)
    static let popover = Color(NagareUIColor.popover)
    static let popoverBorder = Color(NagareUIColor.popoverBorder)
    static let toast = Color(NagareUIColor.toast)
    static let toastBorder = Color(NagareUIColor.toastBorder)
    static let badgeScrim = Color(NagareUIColor.badgeScrim)
}

/// 海报色阶没有色相时的中性值。有色相时用服务端按 OKLCH 算好的 sRGB 值
enum NagarePosterTone {
    /// oklch(76% 0.085 h)；作品页强调文字、图标、悬停态、主按钮的底
    static let posterTone = Color(UIColor(red: 177.0 / 255, green: 177.0 / 255, blue: 177.0 / 255, alpha: 1))
    /// oklch(68% 0.07 h)；压低一档的小标签；色阶里最后一档能写字的颜色
    static let posterToneQuiet = Color(UIColor(red: 152.0 / 255, green: 152.0 / 255, blue: 152.0 / 255, alpha: 1))
    /// oklch(52% 0.07 h)；边框、分隔线（非文字；只在 --bg 上 ≥3:1，卡片与浮层上要用就先在 contrast 里声明并换更亮一档）
    static let posterToneMid = Color(UIColor(red: 105.0 / 255, green: 105.0 / 255, blue: 105.0 / 255, alpha: 1))
    /// oklch(24% 0.045 h)；tint 底、chip 填充（纯装饰）
    static let posterToneLow = Color(UIColor(red: 31.0 / 255, green: 31.0 / 255, blue: 31.0 / 255, alpha: 1))
    /// oklch(15% 0.03 h)；以 poster-tone 为底的主按钮上的字
    static let posterOnTone = Color(UIColor(red: 11.0 / 255, green: 11.0 / 255, blue: 11.0 / 255, alpha: 1))
}

/// 一档文字样式。font 跟随动态字号；webSize 是网页上的像素字号（对照用）；
/// trackingEm 是字距（em），SwiftUI 的 .tracking() 要的是 pt：trackingEm × 实际字号。
struct NagareTextStyle {
    let font: Font
    let webSize: CGFloat
    let trackingEm: CGFloat
}

/// 字号阶梯
enum NagareType {
    /// 发现页横幅标题（手机 32px） · 48px / 800
    static let display = NagareTextStyle(font: Font.system(.largeTitle).weight(.heavy), webSize: 48, trackingEm: -0.03)
    /// 页面主标题（手机 24px） · 32px / 700
    static let h1 = NagareTextStyle(font: Font.system(.title).weight(.bold), webSize: 32, trackingEm: -0.02)
    /// 区块标题（手机 20px） · 24px / 600
    static let h2 = NagareTextStyle(font: Font.system(.title2).weight(.semibold), webSize: 24, trackingEm: -0.02)
    /// 子区块标题 · 18px / 600
    static let h3 = NagareTextStyle(font: Font.system(.title3).weight(.semibold), webSize: 18, trackingEm: -0.02)
    /// 正文 · 16px / 400
    static let body = NagareTextStyle(font: Font.system(.body).weight(.regular), webSize: 16, trackingEm: 0)
    /// 辅助正文 · 15px / 400
    static let bodySm = NagareTextStyle(font: Font.system(.subheadline).weight(.regular), webSize: 15, trackingEm: 0)
    /// UI 标签、按钮 · 14px / 500
    static let label = NagareTextStyle(font: Font.system(.subheadline).weight(.medium), webSize: 14, trackingEm: 0)
    /// 次要说明（颜色用 text-secondary） · 13px / 400
    static let caption = NagareTextStyle(font: Font.system(.footnote).weight(.regular), webSize: 13, trackingEm: 0)
    /// 徽标、角标 · 11px / 500
    static let micro = NagareTextStyle(font: Font.system(.caption2).weight(.medium), webSize: 11, trackingEm: 0)
    /// 字标「nagare」（iOS 也用打包的 Sora，固定字号） · 20px / 700
    static let wordmark = NagareTextStyle(font: Font.custom("Sora", fixedSize: 20).weight(.bold), webSize: 20, trackingEm: -0.01)
    /// 罗马音标题（大写）；iOS 用系统字体 · 12px / 500
    static let romaji = NagareTextStyle(font: Font.system(.caption).weight(.medium), webSize: 12, trackingEm: 0.1)
}

/// 间距（pt）
enum NagareSpacing {
    /// 图标间距、极小 gap
    static let xs: CGFloat = 4
    /// 基础单位、元素内间距
    static let sm: CGFloat = 8
    /// 组件内边距
    static let md: CGFloat = 16
    /// 区块间距、容器内边距
    static let lg: CGFloat = 24
    /// 大区块间距
    static let xl: CGFloat = 32
    /// 区块内部分隔
    static let xxl: CGFloat = 48
    /// 页面区块分隔
    static let xxxl: CGFloat = 64
}

/// 圆角（pt）
enum NagareRadius {
    /// 输入框、按钮
    static let sm: CGFloat = 8
    /// 卡片、封面
    static let md: CGFloat = 12
    /// 模块面板、大卡片（卡片的上限）
    static let lg: CGFloat = 16
    /// 只用于大弹窗、底部面板
    static let xl: CGFloat = 20
    /// 徽标、头像、标签、胶囊
    static let full: CGFloat = 9999
}

/// 布局（pt）
enum NagareLayout {
    /// 触控目标最小尺寸
    static let touchMin: CGFloat = 44
    /// iOS 播放器屏幕边缘不接手势的宽度（pt）
    static let edgeGesture: CGFloat = 20
}

/// 时长（秒）
enum NagareDuration {
    /// 图标状态切换（50–100ms 档）
    static let micro: TimeInterval = 0.1
    /// 按钮悬停、颜色过渡（150–250ms 档）
    static let short: TimeInterval = 0.15
    /// 卡片悬停与进场、模态框（250–400ms 档）
    static let medium: TimeInterval = 0.25
    /// 页面转场（400–700ms 档）
    static let long: TimeInterval = 0.5
    /// 提示进场
    static let toastIn: TimeInterval = 0.26
    /// 提示退场
    static let toastOut: TimeInterval = 0.18
    /// 整页随海报色相过渡
    static let tone: TimeInterval = 0.9
    /// 发现页横幅自动切换的间隔（有暂停规则）
    static let banner: TimeInterval = 8
}

/// 缓动：cubic-bezier 的四个控制点，用法 Animation.timingCurve(e.0, e.1, e.2, e.3, duration:)
enum NagareEasing {
    /// 所有状态转换的默认缓动
    static let standard: (Double, Double, Double, Double) = (0.4, 0, 0.2, 1)
    /// 元素进场（ease-out）
    static let enter: (Double, Double, Double, Double) = (0, 0, 0.58, 1)
    /// 元素退场（ease-in）
    static let exit: (Double, Double, Double, Double) = (0.42, 0, 1, 1)
    /// 提示进场
    static let emphasized: (Double, Double, Double, Double) = (0.16, 1, 0.3, 1)
}
