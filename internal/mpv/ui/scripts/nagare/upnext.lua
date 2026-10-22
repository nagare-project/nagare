-- 切集：上一集 / 下一集的请求，以及播完时的「下一集」卡片。
--
-- 倒计时归 nagare 管（Go 的定时器）：界面只照着 seconds 自己倒数给人看，数到 0 也不会自己切，
-- 只显示「即将播放…」。卡片什么时候收起也听 nagare 的（upnext-clear）；新文件开始播放时
-- 界面自己也收一次，那时这张卡片一定已经过时了。下一集没能打开时 nagare 发 error：卡片不倒数，
-- 一直留着，等人点「重试」或「关闭窗口」。
--
--   nagare → 界面：upnext {"phase":"countdown","label":"第 5 集","seconds":5}
--                 upnext {"phase":"preparing","label":"第 5 集"}
--                 upnext {"phase":"error","label":"第 5 集","title":"下一集没能打开","reason":"原因与恢复动作"}
--                 upnext-clear
--   界面 → nagare：prev / next / upnext-now / upnext-cancel / upnext-retry / upnext-close

local mp = require 'mp'
local utils = require 'mp.utils'
local draw = require 'draw'
local core = require 'core'

local S, P, FONT = core.S, core.P, core.FONT
local M = {}

local PHASES = { countdown = true, preparing = true, error = true }
-- 卡片显示时 Enter 发什么：准备中不接管，Enter 照旧是全屏
local ENTER_ACTION = { countdown = "upnext-now", error = "upnext-retry" }
local DEFAULT_SECONDS = 5
local ERROR_TITLE = "下一集没能打开"
-- 卡片尺寸（逻辑像素，画的时候乘 display-hidpi-scale）：各行的顶边相对卡片顶边
local CARD_W, PAD, GHOST_W, BUTTON_H, RING_R = 340, 18, 96, 36, 19
local KICKER_Y, TITLE_Y, SUB_Y, LINE_H, BUTTONS_GAP = 17, 37, 68, 18, 14
-- 出错卡片的原因最多几行（窗口矮时更少），再长以「…」收尾
local REASON_LINES = 5
-- 卡片顶边不高过顶栏的窗口按钮；底边平时留在进度条悬停时间的上方，窗口矮时最多压到进度条上方
local TOP_MIN, BOTTOM_ROOM, BOTTOM_MIN = 72, 160, 120

-- nil 表示没有卡片；有卡片时：{ phase = "countdown" | "preparing" | "error", label, title, reason, seconds, started }
local card

local function str(v)
	return type(v) == "string" and v or ""
end

function M.visible()
	return card ~= nil
end

-- 卡片显示时 Enter 归不归卡片：倒计时是「立即播放」，出错是「重试」
function M.takes_enter()
	return card ~= nil and ENTER_ACTION[card.phase] ~= nil
end

local function remaining()
	if not card or card.phase ~= "countdown" then
		return 0
	end
	return math.max(0, card.seconds - (mp.get_time() - card.started))
end

-- 还要不要定时重绘：倒数的圈在走、准备中的圈在转（出错卡片是静止的）
function M.animating()
	if not card then
		return false
	end
	return card.phase == "preparing" or remaining() > 0
end

function M.show(json)
	local data = utils.parse_json(json or "")
	if type(data) ~= "table" or not PHASES[data.phase] then
		return
	end
	local seconds = tonumber(data.seconds) or DEFAULT_SECONDS
	card = {
		phase = data.phase,
		label = str(data.label),
		title = str(data.title),
		reason = str(data.reason),
		seconds = math.max(0, seconds),
		started = mp.get_time(),
	}
	core.request_render()
end

function M.clear()
	if card then
		card = nil
		core.request_render()
	end
end

-- 上一集 / 下一集（按钮、Shift+N / Shift+P、PgDn / PgUp、媒体键、右键菜单）：nagare 说有才发，没有就就地提示
function M.go(dir)
	if S.nav and S.nav[dir] then
		core.send(dir)
	else
		core.toast(dir == "next" and "没有下一集了" or "没有上一集了")
	end
end

-- 卡片显示时的 Esc（取消）与 Enter（立即播放 / 重试）
function M.key(name)
	if not card then
		return
	end
	if name == "esc" then
		core.send("upnext-cancel")
	elseif name == "enter" and ENTER_ACTION[card.phase] then
		core.send(ENTER_ACTION[card.phase])
	end
end

-- 右键菜单里的「上一集 · Shift+P」「下一集 · Shift+N」：有才列出，小字带上是第几集
function M.menu_items()
	local items = {}
	local nav = S.nav or {}
	local function item(dir, label, key)
		local ep = nav[dir .. "Label"]
		items[#items + 1] = {
			label = label,
			hint = (type(ep) == "string" and ep ~= "") and (ep .. " · " .. key) or key,
			action = function() M.go(dir) end,
		}
	end
	if nav.prev then
		item("prev", "上一集", "Shift+P")
	end
	if nav.next then
		item("next", "下一集", "Shift+N")
	end
	return items
end

------------------------------------------------------------------------
-- 绘制

-- icon：主按钮（品牌色实心）左边的图标；nil 是次按钮
local function button(c, id, x1, y1, x2, y2, label, icon, on_click)
	local s = core.scale()
	local r = 8 * s
	local hovered = S.hover_id == id
	local cx, cy = (x1 + x2) / 2, (y1 + y2) / 2
	if icon then
		c:rect(x1, y1, x2, y2, { color = P.accent, opacity = 1, radius = r })
		if hovered then
			c:rect(x1, y1, x2, y2, { color = "#FFFFFF", opacity = 0.12, radius = r })
		end
		-- 图标 + 文字，整体居中
		local size, gap = 14 * s, 6 * s
		local left = cx - (size + gap + draw.text_width(label, 14 * s)) / 2
		c:icon(icon, left, cy - size / 2, size, "#FFFFFF", 1)
		c:text(left + size + gap, cy, label, { align = 4, size = 14 * s, bold = true, color = "#FFFFFF", font = FONT })
	else
		c:rect(x1, y1, x2, y2, { color = "#FFFFFF", opacity = hovered and 0.16 or 0.08, radius = r })
		c:text(cx, cy, label, { align = 5, size = 14 * s, color = P.fg, font = FONT })
	end
	core.add_element(id, x1, y1, x2, y2, on_click)
end

-- 右上角的圈：倒计时是剩下的那一段弧 + 秒数，准备中是转圈
local function draw_ring(c, cx, cy)
	local s = core.scale()
	local ro, ri = RING_R * s, (RING_R - 2.5) * s
	c:shape(draw.arc_path(cx, cy, ro, ri, 0, 360, 48), "#FFFFFF", 0.14)
	if card.phase == "preparing" then
		local a = (mp.get_time() * 300) % 360
		c:shape(draw.arc_path(cx, cy, ro, ri, a, a + 90, 16), P.strong, 1)
		return
	end
	local left = remaining()
	local frac = card.seconds > 0 and left / card.seconds or 0
	if frac > 0.005 then
		c:shape(draw.arc_path(cx, cy, ro, ri, -90, -90 + 360 * frac, math.max(2, math.floor(48 * frac))), P.accent_text, 1)
	end
	c:text(cx, cy, tostring(math.ceil(left)), { align = 5, size = 15 * s, bold = true, color = P.strong, font = FONT })
end

local function one_line(text, size, max_w)
	return draw.wrap(text, size, max_w, 1)[1] or ""
end

-- 卡片上的文字：小标题、大标题、下面的几行小字，以及按钮行的顶边。inner 是卡片内容区的宽度。
-- 出错卡片的原因按内容区宽度折行，窗口矮时少几行，保证卡片不压到进度条
local function layout(inner)
	local s = core.scale()
	local label = card.label ~= "" and card.label or "下一集"
	local L = { kicker_color = P.accent_text, ring = card.phase ~= "error" }
	if card.phase == "error" then
		local fit = math.floor((S.h / s - TOP_MIN - BOTTOM_MIN - (SUB_Y + BUTTONS_GAP + BUTTON_H + PAD)) / LINE_H)
		L.kicker = card.title ~= "" and card.title or ERROR_TITLE
		L.kicker_color = P.warn
		L.title = label
		L.lines = card.reason ~= "" and draw.wrap(card.reason, 14 * s, inner, math.max(1, math.min(REASON_LINES, fit))) or {}
	elseif card.phase == "preparing" then
		L.kicker, L.title, L.lines = "正在准备", label, { "准备好就开始播放" }
	else
		local left = math.ceil(remaining())
		L.kicker, L.title = "下一集", label
		L.lines = { left > 0 and string.format("%d 秒后自动播放", left) or "即将播放…" }
	end
	-- 右上角有圈时，标题让出圈的位置
	local title_w = L.ring and inner - (2 * RING_R + 12) * s or inner
	L.kicker = one_line(L.kicker, 13 * s, title_w)
	L.title = one_line(L.title, 22 * s, title_w)
	L.buttons_y = SUB_Y + #L.lines * LINE_H + BUTTONS_GAP
	return L
end

-- 按钮：倒计时是「立即播放」+「取消」，准备中只有一整条「取消」，出错是「重试」+「关闭窗口」
-- （嵌进 nagare 窗口时叫「返回」：关掉的是这次播放，窗口回到媒体库）
local function draw_buttons(c, bx1, by1, bx2)
	local s = core.scale()
	local by2 = by1 + BUTTON_H * s
	local split = bx2 - GHOST_W * s
	local function send(name)
		return function() core.send(name) end
	end
	if card.phase == "countdown" then
		button(c, "upnext-now", bx1, by1, split - 10 * s, by2, "立即播放", "play", send("upnext-now"))
		button(c, "upnext-cancel", split, by1, bx2, by2, "取消", nil, send("upnext-cancel"))
	elseif card.phase == "error" then
		button(c, "upnext-retry", bx1, by1, split - 10 * s, by2, "重试", "retry", send("upnext-retry"))
		button(c, "upnext-close", split, by1, bx2, by2, S.embedded and "返回" or "关闭窗口", nil, send("upnext-close"))
	else
		button(c, "upnext-cancel", bx1, by1, bx2, by2, "取消", nil, send("upnext-cancel"))
	end
end

-- 卡片放在右下角、进度条上方，右边与进度条对齐；窄窗口时收窄
function M.draw(c)
	if not card then
		return
	end
	local s = core.scale()
	local w = math.min(CARD_W * s, S.w - 64 * s)
	local x2 = S.w - 32 * s
	local x1 = x2 - w
	local L = layout(w - 2 * PAD * s)
	local h = (L.buttons_y + BUTTON_H + PAD) * s
	-- 窗口太矮时宁可往下压住悬停时间，也不盖住顶栏的窗口按钮
	local y1 = math.max(TOP_MIN * s, S.h - BOTTOM_ROOM * s - h)
	c:rect(x1, y1, x2, y1 + h, {
		color = P.card, opacity = 0.94, radius = 12 * s,
		border = math.max(1, s), border_color = "#FFFFFF", border_opacity = 0.1,
	})

	local tx = x1 + PAD * s
	c:text(tx, y1 + KICKER_Y * s, L.kicker, { size = 13 * s, bold = true, color = L.kicker_color, font = FONT })
	c:text(tx, y1 + TITLE_Y * s, L.title, { size = 22 * s, bold = true, color = P.strong, font = FONT })
	for i, line in ipairs(L.lines) do
		c:text(tx, y1 + (SUB_Y + (i - 1) * LINE_H) * s, line, { size = 14 * s, color = P.fg, opacity = 0.7, font = FONT })
	end
	if L.ring then
		draw_ring(c, x2 - (PAD + RING_R) * s, y1 + (KICKER_Y + SUB_Y + 18) / 2 * s)
	end
	draw_buttons(c, x1 + PAD * s, y1 + L.buttons_y * s, x2 - PAD * s)
end

return M
