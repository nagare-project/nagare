-- 切集：上一集 / 下一集的请求，以及播完时的「下一集」卡片。
--
-- 倒计时归 nagare 管（Go 的定时器）：界面只照着 seconds 自己倒数给人看，数到 0 也不会自己切，
-- 只显示「即将播放…」。卡片什么时候收起也听 nagare 的（upnext-clear）；新文件开始播放时
-- 界面自己也收一次，那时这张卡片一定已经过时了。
--
--   nagare → 界面：upnext {"phase":"countdown","label":"第 5 集","seconds":5}
--                 upnext {"phase":"preparing","label":"第 5 集"}
--                 upnext-clear
--   界面 → nagare：prev / next / upnext-now / upnext-cancel

local mp = require 'mp'
local utils = require 'mp.utils'
local draw = require 'draw'
local core = require 'core'

local S, P, FONT = core.S, core.P, core.FONT
local M = {}

local DEFAULT_SECONDS = 5
-- 卡片尺寸（逻辑像素，画的时候乘 display-hidpi-scale）：各行的顶边相对卡片顶边
local CARD_W, PAD, GHOST_W, BUTTON_H, RING_R = 340, 18, 96, 36, 19
local KICKER_Y, TITLE_Y, SUB_Y, BUTTONS_Y = 17, 37, 68, 100
local CARD_H = BUTTONS_Y + BUTTON_H + PAD

-- nil 表示没有卡片；有卡片时：{ phase = "countdown" | "preparing", label, seconds, started }
local card

function M.visible()
	return card ~= nil
end

-- 倒计时阶段才有「立即播放」，Enter 也只在这时接管
function M.countdown()
	return card ~= nil and card.phase == "countdown"
end

local function remaining()
	if not M.countdown() then
		return 0
	end
	return math.max(0, card.seconds - (mp.get_time() - card.started))
end

-- 还要不要定时重绘：倒数的圈在走、准备中的圈在转
function M.animating()
	if not card then
		return false
	end
	return card.phase == "preparing" or remaining() > 0
end

function M.show(json)
	local data = utils.parse_json(json or "")
	if type(data) ~= "table" or (data.phase ~= "countdown" and data.phase ~= "preparing") then
		return
	end
	local seconds = tonumber(data.seconds) or DEFAULT_SECONDS
	card = {
		phase = data.phase,
		label = type(data.label) == "string" and data.label or "",
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

-- 上一集 / 下一集（按钮、N / P、媒体键、右键菜单）：nagare 说有才发，没有就就地提示
function M.go(dir)
	if S.nav and S.nav[dir] then
		core.send(dir)
	else
		core.toast(dir == "next" and "没有下一集了" or "没有上一集了")
	end
end

-- 卡片显示时的 Esc（取消）与 Enter（立即播放）
function M.key(name)
	if not card then
		return
	end
	if name == "esc" then
		core.send("upnext-cancel")
	elseif name == "enter" and card.phase == "countdown" then
		core.send("upnext-now")
	end
end

-- 右键菜单里的「上一集 · P」「下一集 · N」：有才列出，小字带上是第几集
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
		item("prev", "上一集", "P")
	end
	if nav.next then
		item("next", "下一集", "N")
	end
	return items
end

------------------------------------------------------------------------
-- 绘制

local function button(c, id, x1, y1, x2, y2, label, primary, on_click)
	local s = core.scale()
	local r = 8 * s
	local hovered = S.hover_id == id
	local cx, cy = (x1 + x2) / 2, (y1 + y2) / 2
	if primary then
		c:rect(x1, y1, x2, y2, { color = P.accent, opacity = 1, radius = r })
		if hovered then
			c:rect(x1, y1, x2, y2, { color = "#FFFFFF", opacity = 0.12, radius = r })
		end
		-- 播放图标 + 文字，整体居中
		local icon, gap = 14 * s, 6 * s
		local left = cx - (icon + gap + draw.text_width(label, 14 * s)) / 2
		c:icon("play", left, cy - icon / 2, icon, "#FFFFFF", 1)
		c:text(left + icon + gap, cy, label, { align = 4, size = 14 * s, bold = true, color = "#FFFFFF", font = FONT })
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

local function card_texts()
	local title = card.label ~= "" and card.label or "下一集"
	if card.phase == "preparing" then
		return "正在准备", title, "准备好就开始播放"
	end
	local left = math.ceil(remaining())
	return "下一集", title, left > 0 and string.format("%d 秒后自动播放", left) or "即将播放…"
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
	-- 底边留在进度条悬停时间的上方；窗口太矮时宁可往下压住悬停时间，也不盖住顶栏的窗口按钮
	local y1 = math.max(72 * s, S.h - 160 * s - CARD_H * s)
	local y2 = y1 + CARD_H * s
	c:rect(x1, y1, x2, y2, {
		color = P.card, opacity = 0.94, radius = 12 * s,
		border = math.max(1, s), border_color = "#FFFFFF", border_opacity = 0.1,
	})

	local kicker, title, sub = card_texts()
	local tx = x1 + PAD * s
	c:text(tx, y1 + KICKER_Y * s, kicker, { size = 13 * s, bold = true, color = P.accent_text, font = FONT })
	c:text(tx, y1 + TITLE_Y * s, title, { size = 22 * s, bold = true, color = P.strong, font = FONT })
	c:text(tx, y1 + SUB_Y * s, sub, { size = 14 * s, color = P.fg, opacity = 0.7, font = FONT })
	draw_ring(c, x2 - (PAD + RING_R) * s, y1 + (KICKER_Y + SUB_Y + 18) / 2 * s)

	-- 按钮：倒计时是「立即播放」+「取消」，准备中只有一整条「取消」
	local by1 = y1 + BUTTONS_Y * s
	local by2 = by1 + BUTTON_H * s
	local bx1, bx2 = x1 + PAD * s, x2 - PAD * s
	local cancel = function() core.send("upnext-cancel") end
	if card.phase == "countdown" then
		local split = bx2 - GHOST_W * s
		button(c, "upnext-now", bx1, by1, split - 10 * s, by2, "立即播放", true, function() core.send("upnext-now") end)
		button(c, "upnext-cancel", split, by1, bx2, by2, "取消", false, cancel)
	else
		button(c, "upnext-cancel", bx1, by1, bx2, by2, "取消", false, cancel)
	end
end

return M
