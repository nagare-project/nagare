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
	bg = { color = "#000000", opacity = 1 }, -- 页面底色
	text_secondary = { color = "#EBEBF5", opacity = 0.6 }, -- 次要文字
	accent = { color = "#0A84FF", opacity = 1 }, -- 可点击（在 bg 上 5.76:1，写进 use 的对比度要与实算一致）
	text_tertiary = { color = "#EBEBF5", opacity = 0.3 }, -- 占位符
	teal = { color = "#5AC8FA", opacity = 1 }, -- 只读信息，固定值
}

-- 字号阶梯：size 是像素，weight 是 100–900，tracking_em 是字距（em）
M.type_scale = {
	body = { size = 16, weight = 400, tracking_em = 0 }, -- 正文
	wordmark = { size = 20, weight = 700, tracking_em = -0.01 }, -- 字标
	web_only = { size = 13, weight = 600, tracking_em = 0.1 }, -- 不进 Swift，所以不用填 iOS 样式
}

-- 间距（像素）
M.space = {
	sm = 8, -- 基础单位
}

-- 圆角（像素）
M.radius = {
	md = 12, -- 卡片
}

-- 布局（像素）
M.layout = {
	touch_min = 44, -- 触控目标
}

-- 时长（秒）
M.duration = {
	toast_in = 0.26, -- 提示进场
}

return M
