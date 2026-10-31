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
    /// 页面底色 · #000000
    static var bg: UIColor { .systemBackground }
    /// 次要文字 · rgba(235, 235, 245, 0.6)
    static var textSecondary: UIColor { .secondaryLabel }
    /// 可点击（在 bg 上 5.76:1，写进 use 的对比度要与实算一致） · #0a84ff
    static var accent: UIColor { .systemBlue }
    /// 占位符 · rgba(235, 235, 245, 0.3)
    static var textTertiary: UIColor { .tertiaryLabel }
    /// 只读信息，固定值 · #5ac8fa
    static var teal: UIColor { UIColor(red: 90.0 / 255, green: 200.0 / 255, blue: 250.0 / 255, alpha: 1) }
}

/// 颜色（SwiftUI），与 NagareUIColor 一一对应
enum NagareColor {
    static let bg = Color(NagareUIColor.bg)
    static let textSecondary = Color(NagareUIColor.textSecondary)
    static let accent = Color(NagareUIColor.accent)
    static let textTertiary = Color(NagareUIColor.textTertiary)
    static let teal = Color(NagareUIColor.teal)
}

/// 海报色阶没有色相时的中性值。有色相时用服务端按 OKLCH 算好的 sRGB 值
enum NagarePosterTone {
    /// oklch(76% 0.085 h)；强调文字
    static let posterTone = Color(UIColor(red: 177.0 / 255, green: 177.0 / 255, blue: 177.0 / 255, alpha: 1))
    /// oklch(24% 0.045 h)；填充
    static let posterToneLow = Color(UIColor(red: 31.0 / 255, green: 31.0 / 255, blue: 31.0 / 255, alpha: 1))
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
    /// 正文 · 16px / 400
    static let body = NagareTextStyle(font: Font.system(.body).weight(.regular), webSize: 16, trackingEm: 0)
    /// 字标 · 20px / 700
    static let wordmark = NagareTextStyle(font: Font.custom("Sora", fixedSize: 20).weight(.bold), webSize: 20, trackingEm: -0.01)
}

/// 间距（pt）
enum NagareSpacing {
    /// 基础单位
    static let sm: CGFloat = 8
}

/// 圆角（pt）
enum NagareRadius {
    /// 卡片
    static let md: CGFloat = 12
}

/// 布局（pt）
enum NagareLayout {
    /// 触控目标
    static let touchMin: CGFloat = 44
}

/// 时长（秒）
enum NagareDuration {
    /// 提示进场
    static let toastIn: TimeInterval = 0.26
}

/// 缓动：cubic-bezier 的四个控制点，用法 Animation.timingCurve(e.0, e.1, e.2, e.3, duration:)
enum NagareEasing {
    /// 默认缓动
    static let standard: (Double, Double, Double, Double) = (0.4, 0, 0.2, 1)
}
