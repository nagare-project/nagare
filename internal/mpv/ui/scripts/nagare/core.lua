-- 界面各模块共用的部分：配色、字体、状态表、可点区域的登记、重绘调度、屏幕提示、发给 nagare 的消息。
--
-- 各模块 require 'core' 拿到的是同一张表，状态 S 也就只有一份。

local mp = require 'mp'

local M = {}

-- 配色照 nagare 网页（frontend/src/styles/global.css）：近黑表面 + 靛紫品牌色
M.P = {
	fg = "#E6E6E6",
	strong = "#F2F2F2",
	panel = "#101010",
	card = "#0B0B0B",
	accent = "#6152DF",
	accent_text = "#C7C2FF",
	warn = "#FDBA74",
}

local function detect_font()
	local platform = mp.get_property("platform")
	if platform == nil then
		platform = package.config:sub(1, 1) == "\\" and "windows" or "unix"
	end
	if platform == "windows" then
		return "Microsoft YaHei UI"
	elseif platform == "darwin" then
		return "PingFang SC"
	end
	return "Noto Sans CJK SC"
end
M.FONT = detect_font()

local TOAST_SECONDS = 2.2

M.S = {
	w = 0, h = 0, hidpi = 1,
	mx = -1, my = -1, hover = false, last_move = 0,
	pause = false, time = 0, duration = 0, volume = 100, mute = false, speed = 1,
	fullscreen = false, maximized = false, ontop = false,
	title = "", info = "", anime4k = "off", nav = {},
	network = false, buffering = false, cache_speed = 0, ranges = {}, chapters = {},
	tracks = {}, sid = false, aid = false, secondary_sid = false, secondary_visible = true, sub_visible = true,
	elements = {}, hover_id = nil, pressed = nil, drag = nil, drag_time = nil, last_drag_seek = 0,
	toast = nil, spin = 0, shown = false, flash_until = 0,
}
local S = M.S

function M.send(...)
	mp.commandv("script-message", "nagare-event", ...)
end

function M.scale()
	return S.hidpi
end

------------------------------------------------------------------------
-- 格式化

function M.fmt_time(t, long)
	t = math.max(0, math.floor(t or 0))
	local hh, mm, ss = math.floor(t / 3600), math.floor(t % 3600 / 60), t % 60
	if long or hh > 0 then
		return string.format("%d:%02d:%02d", hh, mm, ss)
	end
	return string.format("%02d:%02d", mm, ss)
end

function M.fmt_speed(v)
	if math.abs(v * 10 - math.floor(v * 10 + 0.5)) < 0.001 then
		return string.format("%.1f×", v)
	end
	return string.format("%.2f×", v)
end

function M.fmt_rate(bps)
	if not bps or bps <= 0 then
		return nil
	end
	if bps >= 1024 * 1024 then
		return string.format("%.1f MB/s", bps / 1024 / 1024)
	end
	return string.format("%d KB/s", math.floor(bps / 1024))
end

------------------------------------------------------------------------
-- 元素：每次重绘重新登记一遍可点的区域，鼠标事件按上一次的登记命中

function M.add_element(id, x1, y1, x2, y2, on_click)
	S.elements[#S.elements + 1] = { id = id, x1 = x1, y1 = y1, x2 = x2, y2 = y2, click = on_click }
end

-- 后登记的在上层：从后往前找
function M.element_at(x, y)
	for i = #S.elements, 1, -1 do
		local e = S.elements[i]
		if x >= e.x1 and x <= e.x2 and y >= e.y1 and y <= e.y2 then
			return e
		end
	end
	return nil
end

function M.find_element(id)
	for _, e in ipairs(S.elements) do
		if e.id == id then
			return e
		end
	end
	return nil
end

------------------------------------------------------------------------
-- 重绘与屏幕提示

local render_pending = false
local renderer

-- main.lua 把真正的重绘函数交进来；各模块只管 request_render，30 毫秒内的多次请求合成一次
function M.set_renderer(fn)
	renderer = fn
end

function M.request_render()
	if render_pending or not renderer then
		return
	end
	render_pending = true
	mp.add_timeout(0.03, function()
		render_pending = false
		renderer()
	end)
end

function M.toast(text, sub)
	S.toast = { text = text, sub = sub, ["until"] = mp.get_time() + TOAST_SECONDS }
	M.request_render()
	mp.add_timeout(TOAST_SECONDS + 0.05, M.request_render)
end

return M
