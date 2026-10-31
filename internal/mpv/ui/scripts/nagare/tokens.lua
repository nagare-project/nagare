-- 由 tools/gentokens 从 design/tokens.json 生成，不要手改。
-- 改 token：编辑 design/tokens.json，再在仓库根目录运行 go generate ./...（CI 检查生成文件是最新的）。
-- 规则与用法见仓库根目录的 DESIGN.md。
--
-- 用法：local T = require 'tokens'；颜色项的字段名与 draw.lua 的选项一致，
-- 例如 c:rect(x1, y1, x2, y2, { color = T.color.accent.color, opacity = T.color.accent.opacity })。
-- 尺寸是 CSS 像素，画的时候照常乘界面缩放倍数；时长是秒（mp.add_timeout 用秒）。
-- 现有界面还没有改用它（配色仍在 core.lua 的 M.P），播放器换新样式时再接。

local M = {}

-- 颜色：color 是 "#RRGGBB"，opacity 0..1
M.color = {
	bg = { color = "#000000", opacity = 1 }, -- 页面底色（Level 0）
	bg_card = { color = "#1C1C1E", opacity = 1 }, -- 卡片、面板、侧栏（Level 1）
	bg_elevated = { color = "#2C2C2E", opacity = 1 }, -- 浮层、输入框、弹窗（Level 2 / 3）
	bg_fill = { color = "#787880", opacity = 0.12 }, -- 次要填充：ghost 按钮、标签底、圆形图标按钮
	bg_fill_hover = { color = "#787880", opacity = 0.2 }, -- 次要填充的悬停态
	separator = { color = "#545458", opacity = 0.65 }, -- 半透明分隔线、三级按钮描边（系统 separator 深色是 0.6，不一致，写固定值）
	separator_opaque = { color = "#38383A", opacity = 1 }, -- 不透明分隔线、卡片边框
	hairline = { color = "#FFFFFF", opacity = 0.07 }, -- 列表行之间的发丝线、顶栏玻璃底边
	row_hover = { color = "#FFFFFF", opacity = 0.045 }, -- 列表行悬停时浮起的面
	overlay = { color = "#000000", opacity = 0.6 }, -- 模态遮罩
	accent = { color = "#0A84FF", opacity = 1 }, -- 唯一可点击色：按钮、链接、进度条、选中态
	accent_hover = { color = "#409CFF", opacity = 1 }, -- 可点击色的悬停态
	accent_active = { color = "#0070D6", opacity = 1 }, -- 可点击色的按下态
	accent_dim = { color = "#0A84FF", opacity = 0.12 }, -- 选中项的底、可点击色的轻填充
	focus_ring = { color = "#0A84FF", opacity = 0.4 }, -- 焦点环（3px box-shadow），所有可交互元素共用这一个
	teal = { color = "#5AC8FA", opacity = 1 }, -- 只读信息：信息标签、弹幕分区、只读高亮；不可点击
	teal_dim = { color = "#5AC8FA", opacity = 0.1 }, -- teal 的底色填充
	text = { color = "#FFFFFF", opacity = 1 }, -- 主文字
	text_secondary = { color = "#EBEBF5", opacity = 0.6 }, -- 要读的次要文字：说明、徽标、元信息、caption
	text_tertiary = { color = "#EBEBF5", opacity = 0.3 }, -- 只用于占位符、禁用态、纯装饰；不写要读的字
	text_quaternary = { color = "#EBEBF5", opacity = 0.18 }, -- 只用于禁用态
	nav_link = { color = "#EBEBF5", opacity = 0.66 }, -- 顶栏链接的安静态
	success = { color = "#30D158", opacity = 1 }, -- 已看完、同步成功、已完结
	success_dim = { color = "#30D158", opacity = 0.12 }, -- 成功徽标的底
	warning = { color = "#FF9F0A", opacity = 1 }, -- 连载中、待确认、需要注意
	warning_dim = { color = "#FF9F0A", opacity = 0.12 }, -- 警示徽标的底
	error = { color = "#FF453A", opacity = 1 }, -- 错误、删除确认、危险按钮
	error_hover = { color = "#FF6961", opacity = 1 }, -- 危险按钮的悬停态、提示里的失败图标
	error_dim = { color = "#FF453A", opacity = 0.12 }, -- 错误徽标的底
	info = { color = "#5AC8FA", opacity = 1 }, -- 信息提示（与 teal 同值）
	glass_nav = { color = "#09090B", opacity = 0.78 }, -- 顶栏毛玻璃（配 saturate(160%) blur(16px)）
	glass_light = { color = "#FFFFFF", opacity = 0.08 }, -- 作品页 hero 的工具按钮（玻璃·轻）
	glass_light_border = { color = "#FFFFFF", opacity = 0.13 }, -- 玻璃·轻的描边
	glass_heavy = { color = "#FFFFFF", opacity = 0.12 }, -- 作品页 hero 的次级入口（玻璃·重）
	glass_heavy_border = { color = "#FFFFFF", opacity = 0.2 }, -- 玻璃·重的描边
	popover = { color = "#18181C", opacity = 0.96 }, -- 下拉菜单、弹层的底（配 blur）
	popover_border = { color = "#FFFFFF", opacity = 0.08 }, -- 弹层描边
	toast = { color = "#121216", opacity = 0.94 }, -- 全局提示胶囊的底（配 blur(20px) saturate(150%)）
	toast_border = { color = "#FFFFFF", opacity = 0.12 }, -- 全局提示胶囊的描边
	badge_scrim = { color = "#000000", opacity = 0.75 }, -- 压在封面上的徽标底（配 blur(8px)），只让文字带颜色
}

-- 字号阶梯：size 是像素，weight 是 100–900，tracking_em 是字距（em）
M.type_scale = {
	display = { size = 48, weight = 800, tracking_em = -0.03 }, -- 发现页横幅标题（手机 32px）
	h1 = { size = 32, weight = 700, tracking_em = -0.02 }, -- 页面主标题（手机 24px）
	h2 = { size = 24, weight = 600, tracking_em = -0.02 }, -- 区块标题（手机 20px）
	h3 = { size = 18, weight = 600, tracking_em = -0.02 }, -- 子区块标题
	body = { size = 16, weight = 400, tracking_em = 0 }, -- 正文
	body_sm = { size = 15, weight = 400, tracking_em = 0 }, -- 辅助正文
	label = { size = 14, weight = 500, tracking_em = 0 }, -- UI 标签、按钮
	caption = { size = 13, weight = 400, tracking_em = 0 }, -- 次要说明（颜色用 text-secondary）
	micro = { size = 11, weight = 500, tracking_em = 0 }, -- 徽标、角标
	wordmark = { size = 20, weight = 700, tracking_em = -0.01 }, -- 字标「nagare」（iOS 也用打包的 Sora，固定字号）
	romaji = { size = 12, weight = 500, tracking_em = 0.1 }, -- 罗马音标题（大写）；iOS 用系统字体
}

-- 间距（像素）
M.space = {
	xs = 4, -- 图标间距、极小 gap
	sm = 8, -- 基础单位、元素内间距
	md = 16, -- 组件内边距
	lg = 24, -- 区块间距、容器内边距
	xl = 32, -- 大区块间距
	xxl = 48, -- 区块内部分隔
	xxxl = 64, -- 页面区块分隔
}

-- 圆角（像素）
M.radius = {
	sm = 8, -- 输入框、按钮
	md = 12, -- 卡片、封面
	lg = 16, -- 模块面板、大卡片（卡片的上限）
	xl = 20, -- 只用于大弹窗、底部面板
	full = 9999, -- 徽标、头像、标签、胶囊
}

-- 布局（像素）
M.layout = {
	touch_min = 44, -- 触控目标最小尺寸
}

-- 时长（秒）
M.duration = {
	micro = 0.1, -- 图标状态切换（50–100ms 档）
	short = 0.15, -- 按钮悬停、颜色过渡（150–250ms 档）
	medium = 0.25, -- 卡片悬停与进场、模态框（250–400ms 档）
	long = 0.5, -- 页面转场（400–700ms 档）
	toast_in = 0.26, -- 提示进场
	toast_out = 0.18, -- 提示退场
	tone = 0.9, -- 整页随海报色相过渡
}

return M
