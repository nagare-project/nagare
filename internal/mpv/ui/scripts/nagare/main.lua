-- nagare 播放器界面（方案 B「影院」：底部大区域、居中的大播放键、两端带时间的粗进度条）。
--
-- 和 nagare 之间的消息：
--   nagare → 界面：script-message-to nagare state <json>（标题、副标题、画质增强档位、上一集 / 下一集，
--                  见 apply_state）
--                  script-message-to nagare toast <文字> <小字>（截图结果等屏幕提示）
--                  script-message-to nagare upnext <json> / upnext-clear（「下一集」卡片，见 upnext.lua）
--   界面 → nagare：script-message nagare-event <名字> [参数]
--                  （ready / anime4k / danmaku / volume / screenshot / prev / next /
--                   upnext-now / upnext-cancel / upnext-retry / upnext-close）
-- nagare 经 JSON IPC 收到的是 client-message 事件（internal/mpv/events.go）。
-- 嵌进 nagare 桌面窗口时，宿主与界面之间另有几个 user-data/nagare/* 属性（见 embed.lua）。
--
-- 模块：core（共用状态、重绘调度、屏幕提示）、draw（ASS 绘图）、menu（菜单）、tracks（字幕与弹幕轨）、
-- seekbar（进度条与缩略图）、upnext（切集与「下一集」卡片）、embed（嵌进桌面窗口时与宿主的约定）。

local mp = require 'mp'
local utils = require 'mp.utils'
local draw = require 'draw'
local menu = require 'menu'
local core = require 'core'
local tracks = require 'tracks'
local seekbar = require 'seekbar'
local upnext = require 'upnext'
local embed = require 'embed'

local S, P, FONT = core.S, core.P, core.FONT
local send, toast, request_render, scale = core.send, core.toast, core.request_render, core.scale
local add_element, element_at, find_element = core.add_element, core.element_at, core.find_element

local HIDE_AFTER = 2.0 -- 鼠标不动多少秒后收起
local SPEEDS = { 0.5, 0.75, 1.0, 1.25, 1.5, 2.0 }
local ANIME4K = {
	{ value = "off", label = "关" },
	{ value = "fast", label = "标准" },
	{ value = "hq", label = "高质量", hint = "显卡吃力会掉帧，可换回「标准」" },
}

local overlay = mp.create_osd_overlay("ass-events")

local function anime4k_label(value)
	for _, a in ipairs(ANIME4K) do
		if a.value == value then
			return a.label
		end
	end
	return "关"
end

------------------------------------------------------------------------
-- 显示

local function ui_visible()
	-- 「下一集」卡片在的时候控制条也留着：卡片贴在进度条上方，往回拖进度条就是取消
	if menu.is_open() or S.drag or upnext.visible() then
		return true
	end
	if S.pause then
		return true
	end
	-- 键盘快进快退：光标不在窗口里也露一下，让人看见跳到了哪
	if mp.get_time() < S.flash_until then
		return true
	end
	if not S.hover then
		return false
	end
	-- 光标停在按钮、进度条上不收；停在别处两秒后收起（光标也会被 mpv 藏掉）
	if S.shown and element_at(S.mx, S.my) then
		return true
	end
	return mp.get_time() - S.last_move < HIDE_AFTER
end

-- 图标按钮：size 是点击区（正方形），icon_size 是图标本身
local function icon_button(c, id, cx, cy, size, icon, icon_size, on_click, o)
	o = o or {}
	local half = size / 2
	if S.hover_id == id then
		c:circle(cx, cy, half, { color = "#FFFFFF", opacity = 0.1 })
	end
	c:icon(icon, cx - icon_size / 2, cy - icon_size / 2, icon_size, o.color or P.fg, o.opacity or 1)
	add_element(id, cx - half, cy - half, cx + half, cy + half, on_click)
end

-- 文字按钮：返回它占的宽度
local function text_button(c, id, x2, cy, label, on_click, o)
	o = o or {}
	local s = scale()
	local size = 14 * s
	local w = draw.text_width(label, size) + 20 * s
	local x1 = x2 - w
	if S.hover_id == id then
		c:rect(x1, cy - 22 * s, x2, cy + 22 * s, { color = "#FFFFFF", opacity = 0.1, radius = 8 * s })
	end
	c:text((x1 + x2) / 2, cy, label, { align = 5, size = size, color = o.color or P.fg, font = FONT })
	add_element(id, x1, cy - 22 * s, x2, cy + 22 * s, on_click)
	return w
end

------------------------------------------------------------------------
-- 动作

local function toggle_pause()
	mp.commandv("cycle", "pause")
end

local function set_anime4k(value)
	S.anime4k = value
	send("anime4k", value)
	local hint
	for _, a in ipairs(ANIME4K) do
		if a.value == value then
			hint = a.hint
		end
	end
	toast("画质增强：" .. anime4k_label(value), hint)
end

-- 截图交给 nagare：它建好截图目录、起好文件名再让 mpv 截，结果作为屏幕提示推回来
local function screenshot()
	send("screenshot")
end

local function set_speed(v)
	mp.set_property_number("speed", v)
end

------------------------------------------------------------------------
-- 菜单内容

local function anime4k_items()
	local items = {}
	for _, a in ipairs(ANIME4K) do
		items[#items + 1] = { label = a.label, checked = S.anime4k == a.value, action = function() set_anime4k(a.value) end }
	end
	return items
end

local function speed_items()
	local items = {}
	for i = #SPEEDS, 1, -1 do
		local v = SPEEDS[i]
		items[#items + 1] = { label = core.fmt_speed(v), checked = math.abs(S.speed - v) < 0.001, action = function() set_speed(v) end }
	end
	return items
end

local function context_items()
	local items = {
		{ label = S.pause and "播放" or "暂停", hint = "空格", action = toggle_pause },
	}
	-- 上一集 / 下一集：nagare 说有才列
	for _, it in ipairs(upnext.menu_items()) do
		items[#items + 1] = it
	end
	items[#items + 1] = { separator = true }
	items[#items + 1] = { label = "字幕", items = tracks.subtitle_items }
	if tracks.audio_count() > 1 then
		items[#items + 1] = { label = "音轨", items = tracks.audio_items }
	end
	if tracks.has_danmaku() then
		local on = tracks.danmaku_visible()
		items[#items + 1] = { label = "弹幕", hint = (on and "开" or "关") .. " · D", hint_accent = on, action = tracks.toggle_danmaku }
	end
	items[#items + 1] = { label = "画质增强", items = anime4k_items }
	items[#items + 1] = { label = "倍速", items = speed_items }
	items[#items + 1] = { separator = true }
	items[#items + 1] = { label = "截图", hint = "S", action = screenshot }
	items[#items + 1] = { label = "窗口置顶", checked = S.ontop, action = function() mp.commandv("cycle", "ontop") end }
	items[#items + 1] = { label = S.fullscreen and "退出全屏" or "全屏", hint = "Enter", action = function() mp.commandv("cycle", "fullscreen") end }
	items[#items + 1] = { separator = true }
	if S.embedded then
		items[#items + 1] = { label = "返回", hint = "Esc", action = embed.back }
	else
		items[#items + 1] = { label = "关闭", hint = "Q", action = function() mp.commandv("quit") end }
	end
	return items
end

local function open_menu(items, x, y, place)
	menu.open(items, x, y, { place = place, w = S.w, h = S.h, scale = scale(), font = FONT, palette = P })
	request_render()
end

-- 控制条上的按钮弹出的菜单：贴在按钮正上方
local function open_menu_above(id, items)
	local e = find_element(id)
	if not e then
		return
	end
	open_menu(items, e.x1, e.y1 - 8 * scale(), "above")
end

local function open_context_menu()
	if S.mx < 0 then
		open_menu(context_items(), S.w / 2, S.h / 2, "cursor")
	else
		open_menu(context_items(), S.mx, S.my, "cursor")
	end
end

------------------------------------------------------------------------
-- 绘制各部分

-- 嵌进 nagare 窗口时左上角的「返回」（回到媒体库）：x 是左边缘，cy 与标题行对齐。返回右边缘
local function draw_back(c, x, cy)
	local s = scale()
	local size = 15 * s
	local w = 34 * s + draw.text_width("返回", size) + 12 * s
	if S.hover_id == "library" then
		c:rect(x, cy - 18 * s, x + w, cy + 18 * s, { color = "#FFFFFF", opacity = 0.1, radius = 8 * s })
	end
	c:icon("chevron_left", x + 8 * s, cy - 10 * s, 20 * s, P.strong, 1)
	c:text(x + 32 * s, cy, "返回", { align = 4, size = size, color = P.strong, font = FONT, outline = 0.8 * s, outline_opacity = 0.7 })
	add_element("library", x, cy - 18 * s, x + w, cy + 18 * s, embed.back)
	return x + w
end

local function draw_top(c)
	local s = scale()
	c:scrim(0, 0, S.w, 140 * s, 0.75, 0, 20)
	local x = 32 * s
	if S.embedded then
		-- 宿主的窗口按钮（macOS 左上角的红绿灯）占着左边：「返回」与标题从让出的宽度后面开始
		x = draw_back(c, (S.titlebar_inset + 16) * s, 33 * s) + 12 * s
	end
	local title = S.nagare_title or (S.title ~= "" and S.title) or "nagare"
	c:text(x, 22 * s, title, { size = 22 * s, bold = true, color = P.strong, font = FONT, outline = 1 * s, outline_opacity = 0.7 })
	if S.info ~= "" then
		-- 弹幕密的时候顶部全是字：副标题也要描边，否则会被弹幕盖得看不清
		c:text(x, 54 * s, S.info, { size = 14 * s, color = P.fg, opacity = 0.85, font = FONT, outline = 0.8 * s, outline_opacity = 0.7 })
	end
	-- 最小化 / 最大化 / 关闭：全屏时不画；嵌进 nagare 窗口时也不画，窗口按钮是宿主的（同一个窗口不要两套）
	if not S.fullscreen and not S.embedded then
		local cy = 20 * s + 22 * s
		local x = S.w - 16 * s - 22 * s
		icon_button(c, "close", x, cy, 44 * s, "close", 18 * s, function() mp.commandv("quit") end)
		x = x - 48 * s
		icon_button(c, "maximize", x, cy, 44 * s, "maximize", 16 * s, function() mp.commandv("cycle", "window-maximized") end)
		x = x - 48 * s
		icon_button(c, "minimize", x, cy, 44 * s, "minimize", 18 * s, function() mp.set_property_bool("window-minimized", true) end)
	end
end

-- 返回音量这一组的右边缘
local function draw_volume(c, cy, compact)
	local s = scale()
	local icon = (S.mute or S.volume <= 0) and "mute" or (S.volume < 50 and "volume_low" or "volume")
	local bx = 20 * s + 22 * s
	icon_button(c, "mute", bx, cy, 44 * s, icon, 22 * s, function() mp.commandv("cycle", "mute") end)
	if compact then
		return bx + 22 * s
	end
	local x1, x2 = bx + 28 * s, bx + 28 * s + 96 * s
	local th = (S.hover_id == "volume" or S.drag == "volume") and 6 * s or 4 * s
	c:rect(x1, cy - th / 2, x2, cy + th / 2, { color = "#FFFFFF", opacity = 0.22, radius = th / 2 })
	local frac = S.mute and 0 or math.max(0, math.min(1, S.volume / 100))
	if frac > 0 then
		c:rect(x1, cy - th / 2, x1 + (x2 - x1) * frac, cy + th / 2, { color = P.strong, opacity = 1, radius = th / 2 })
	end
	if S.hover_id == "volume" or S.drag == "volume" then
		c:circle(x1 + (x2 - x1) * frac, cy, 6 * s, { color = "#FFFFFF", opacity = 1 })
	end
	add_element("volume", x1 - 6 * s, cy - 14 * s, x2 + 6 * s, cy + 14 * s, nil)
	return x2 + 6 * s
end

local function draw_transport(c, cy, compact)
	local s = scale()
	local cx = S.w / 2
	-- 大播放键：白色实心圆 + 深色图标
	local r = 30 * s
	c:circle(cx, cy, r, { color = S.hover_id == "play" and "#FFFFFF" or P.strong, opacity = 1 })
	c:icon(S.pause and "play" or "pause", cx - 13 * s, cy - 13 * s, 26 * s, "#0B0B0B", 1)
	add_element("play", cx - r, cy - r, cx + r, cy + r, toggle_pause)
	if compact then
		return
	end
	icon_button(c, "back", cx - 68 * s, cy, 48 * s, "back", 28 * s, function() mp.commandv("seek", -10) end)
	c:text(cx - 68 * s, cy + 1.2 * s, "10", { align = 5, size = 9 * s, bold = true, color = P.fg, font = FONT })
	icon_button(c, "forward", cx + 68 * s, cy, 48 * s, "forward", 28 * s, function() mp.commandv("seek", 10) end)
	c:text(cx + 68 * s, cy + 1.2 * s, "10", { align = 5, size = 9 * s, bold = true, color = P.fg, font = FONT })
end

-- 上一集 / 下一集：nagare 告诉界面有才画；两边放不下（窄窗口、右边按钮多）就不画，
-- 右键菜单与 Shift+N / Shift+P 照样能用
local function draw_nav(c, cy, left_limit, right_limit)
	local s = scale()
	local cx = S.w / 2
	local reach = (130 + 24 + 8) * s
	if cx - reach < left_limit or cx + reach > right_limit then
		return
	end
	if S.nav.prev then
		icon_button(c, "prev", cx - 130 * s, cy, 48 * s, "prev", 24 * s, function() upnext.go("prev") end)
	end
	if S.nav.next then
		icon_button(c, "next", cx + 130 * s, cy, 48 * s, "next", 24 * s, function() upnext.go("next") end)
	end
end

-- 返回右边这一组的左边缘
local function draw_right(c, cy, compact)
	local s = scale()
	local x = S.w - 20 * s
	icon_button(c, "fullscreen", x - 22 * s, cy, 44 * s, S.fullscreen and "exit_fullscreen" or "fullscreen", 20 * s,
		function() mp.commandv("cycle", "fullscreen") end)
	x = x - 46 * s
	icon_button(c, "more", x - 22 * s, cy, 44 * s, "more", 20 * s, function() open_menu_above("more", context_items) end)
	x = x - 46 * s
	if not compact then
		x = x - text_button(c, "speed", x, cy, core.fmt_speed(S.speed), function() open_menu_above("speed", speed_items) end) - 2 * s
	end
	icon_button(c, "anime4k", x - 22 * s, cy, 44 * s, "sparkle", 22 * s, function() open_menu_above("anime4k", anime4k_items) end,
		{ color = S.anime4k ~= "off" and P.accent_text or P.fg })
	x = x - 46 * s
	icon_button(c, "subtitles", x - 22 * s, cy, 44 * s, "subtitles", 22 * s, function() open_menu_above("subtitles", tracks.subtitle_items) end)
	local left = x - 44 * s
	x = x - 46 * s
	if tracks.has_danmaku() then
		local id = "danmaku"
		local bx1, bx2 = x - 44 * s, x
		if S.hover_id == id then
			c:rect(bx1, cy - 22 * s, bx2, cy + 22 * s, { color = "#FFFFFF", opacity = 0.1, radius = 8 * s })
		end
		local px1, px2 = bx1 + 9 * s, bx2 - 9 * s
		if tracks.danmaku_visible() then
			c:rect(px1, cy - 11 * s, px2, cy + 11 * s, { color = P.accent, opacity = 1, radius = 5 * s })
			c:text((px1 + px2) / 2, cy, "弹", { align = 5, size = 13 * s, bold = true, color = "#FFFFFF", font = FONT })
		else
			c:stroke_rect(px1, cy - 11 * s, px2, cy + 11 * s, { width = 1.5 * s, color = "#FFFFFF", opacity = 0.6, radius = 5 * s })
			c:text((px1 + px2) / 2, cy, "弹", { align = 5, size = 13 * s, bold = true, color = P.fg, opacity = 0.7, font = FONT })
		end
		add_element(id, bx1, cy - 22 * s, bx2, cy + 22 * s, tracks.toggle_danmaku)
		left = bx1
	end
	return left
end

local function draw_bottom(c)
	local s = scale()
	c:scrim(0, S.h - 240 * s, S.w, S.h, 0, 0.85, 24)
	seekbar.draw(c)
	local cy = S.h - 44 * s
	local compact = S.w < 760 * s
	local volume_right = draw_volume(c, cy, compact)
	draw_transport(c, cy, compact)
	local right_left = draw_right(c, cy, compact)
	if not compact then
		draw_nav(c, cy, volume_right, right_left)
	end
end

local function draw_buffering(c)
	local s = scale()
	local cx, cy = S.w / 2, S.h / 2 - 16 * s
	c:shape(draw.arc_path(cx, cy, 18 * s, 15 * s, 0, 360, 48), "#FFFFFF", 0.16)
	local a = (S.spin * 360) % 360
	c:shape(draw.arc_path(cx, cy, 18 * s, 15 * s, a, a + 90, 16), P.strong, 1)
	c:text(cx, cy + 36 * s, "正在缓冲", { align = 5, size = 15 * s, color = P.fg, font = FONT, outline = 0.8 * s })
	local rate = core.fmt_rate(S.cache_speed)
	if rate then
		c:text(cx, cy + 60 * s, rate, { align = 5, size = 13 * s, color = P.fg, opacity = 0.7, font = FONT, outline = 0.8 * s })
	end
end

local function draw_toast(c, visible)
	local t = S.toast
	if not t or mp.get_time() > t["until"] then
		S.toast = nil
		return
	end
	local s = scale()
	local x, y = 32 * s, (visible and 92 or 32) * s
	local w = math.max(draw.text_width(t.text, 14 * s), t.sub and draw.text_width(t.sub, 12 * s) or 0) + 28 * s
	local h = (t.sub and 58 or 40) * s
	c:rect(x, y, x + w, y + h, {
		color = P.card, opacity = 0.92, radius = 10 * s,
		border = math.max(1, s), border_color = "#FFFFFF", border_opacity = 0.1,
	})
	c:text(x + 14 * s, y + 10 * s, t.text, { size = 14 * s, color = P.fg, font = FONT })
	if t.sub then
		c:text(x + 14 * s, y + 32 * s, t.sub, { size = 12 * s, color = P.fg, opacity = 0.62, font = FONT })
	end
end

------------------------------------------------------------------------
-- 鼠标与按键分区

-- 只在鼠标落在按钮、进度条上（或菜单开着、正在拖）时接管左键；其余时候左键交还 mpv，
-- 拖动窗口与双击全屏才照常可用。「下一集」卡片显示时接管 Esc（取消）与 Enter（立即播放 / 重试），
-- 菜单开着时让给菜单。嵌进 nagare 窗口时，菜单与卡片都不要 Esc 的时候由界面接管 Esc（见 embedded_esc）。
local bound = { mouse = false, menu = false, upnext_esc = false, upnext_enter = false, embedded_esc = false }

local function set_section(key, section, on)
	if bound[key] == on then
		return
	end
	bound[key] = on
	if on then
		mp.enable_key_bindings(section)
	else
		mp.disable_key_bindings(section)
	end
end

local function update_bindings()
	local over = S.shown and S.hover and element_at(S.mx, S.my) ~= nil
	local capture = menu.is_open() or S.drag ~= nil or over
	set_section("mouse", "nagare-mouse", capture)
	set_section("menu", "nagare-menu-keys", menu.is_open())
	local card_keys = upnext.visible() and not menu.is_open()
	set_section("upnext_esc", "nagare-upnext-esc", card_keys)
	set_section("upnext_enter", "nagare-upnext-enter", card_keys and upnext.takes_enter())
	set_section("embedded_esc", "nagare-embedded-esc", S.embedded and not menu.is_open() and not card_keys)
	-- 宿主据 capture 决定这一下按下是交给界面还是拖动窗口（mpv 自己的拖窗在宿主模式下不生效）
	embed.publish(capture)
end

local function render()
	if S.w <= 0 or S.h <= 0 then
		return
	end
	local c = draw.canvas()
	local visible = ui_visible()
	S.shown = visible
	S.elements = {}
	seekbar.begin_frame()
	if visible then
		draw_top(c)
		draw_bottom(c)
	end
	if S.buffering then
		draw_buffering(c)
	end
	draw_toast(c, visible)
	upnext.draw(c)
	if menu.is_open() then
		menu.render(c)
	end
	overlay.res_x, overlay.res_y = S.w, S.h
	overlay.data = c:result()
	overlay:update()
	seekbar.end_frame()
	if not visible then
		S.hover_id = nil
	end
	update_bindings()
end
core.set_renderer(render)

local function drag_volume()
	local e = find_element("volume")
	if not e then
		return
	end
	local x1, x2 = e.x1 + 6 * scale(), e.x2 - 6 * scale()
	local v = math.floor(math.max(0, math.min(1, (S.mx - x1) / math.max(1, x2 - x1))) * 100 + 0.5)
	if S.mute then
		mp.set_property_bool("mute", false)
	end
	mp.set_property_number("volume", v)
end

local function on_mouse_down()
	if menu.is_open() then
		return -- 菜单在松开时处理
	end
	local e = element_at(S.mx, S.my)
	if not e then
		return
	end
	S.pressed = e.id
	if e.id == "seek" then
		if (S.duration or 0) <= 0 then
			return
		end
		S.drag = "seek"
		seekbar.drag(false)
	elseif e.id == "volume" then
		S.drag = "volume"
		drag_volume()
	end
	request_render()
end

local function on_mouse_up()
	if menu.is_open() then
		menu.click(S.mx, S.my)
		request_render()
		return
	end
	if S.drag == "seek" then
		seekbar.drag(true)
	end
	local was_drag = S.drag ~= nil
	S.drag = nil
	local pressed = S.pressed
	S.pressed = nil
	local e = element_at(S.mx, S.my)
	-- 有的输入只发一个「按下即松开」的事件（模拟点击、部分触控板轻点）：没有按下的记录时，
	-- 按光标下的元素算一次点击
	if not was_drag and not pressed and e then
		pressed = e.id
		if e.id == "seek" then
			local t = seekbar.time_at(S.mx)
			if t then
				mp.commandv("seek", t, "absolute+exact")
			end
		elseif e.id == "volume" then
			drag_volume()
		end
	end
	if not was_drag and pressed and e and e.id == pressed and e.click then
		e.click()
	end
	request_render()
end

mp.set_key_bindings({
	{ "mbtn_left", on_mouse_up, on_mouse_down },
	{ "mbtn_left_dbl", function() end },
}, "nagare-mouse", "force")

local function menu_key(name)
	return function()
		menu.key(name)
		request_render()
	end
end

mp.set_key_bindings({
	{ "esc", menu_key("esc") },
	{ "up", menu_key("up") },
	{ "down", menu_key("down") },
	{ "left", menu_key("left") },
	{ "right", menu_key("right") },
	{ "enter", menu_key("enter") },
	{ "kp_enter", menu_key("enter") },
}, "nagare-menu-keys", "force")

-- 卡片的 Esc 与 Enter 分开两区：准备中只有「取消」，Enter 照旧是全屏（倒计时与出错时 Enter 归卡片）
mp.set_key_bindings({
	{ "esc", function() upnext.key("esc") end },
}, "nagare-upnext-esc", "force")
mp.set_key_bindings({
	{ "enter", function() upnext.key("enter") end },
	{ "kp_enter", function() upnext.key("enter") end },
}, "nagare-upnext-enter", "force")

-- 嵌进 nagare 窗口时的 Esc：全屏时照旧退出全屏（同 input.conf），不全屏时 = 返回媒体库。
-- 全屏与否当场读属性，不用观察来的 S.fullscreen（那个可能晚一拍）。按住不放的重复不触发，
-- 退出全屏的那一下不会接着连成返回。
mp.set_key_bindings({
	{ "esc", function()
		if mp.get_property_bool("fullscreen") then
			mp.set_property_bool("fullscreen", false)
		else
			embed.back()
		end
	end },
}, "nagare-embedded-esc", "force")

mp.observe_property("mouse-pos", "native", function(_, pos)
	if not pos then
		return
	end
	S.mx, S.my, S.hover = pos.x, pos.y, pos.hover ~= false
	S.last_move = mp.get_time()
	if menu.is_open() then
		menu.mouse_move(S.mx, S.my)
	end
	if S.drag == "seek" then
		seekbar.drag(false)
	elseif S.drag == "volume" then
		drag_volume()
	end
	local e = S.hover and element_at(S.mx, S.my)
	S.hover_id = e and e.id or nil
	request_render()
end)

------------------------------------------------------------------------
-- 播放状态

local function observe(name, kind, fn)
	mp.observe_property(name, kind, function(_, v)
		fn(v)
		request_render()
	end)
end

observe("osd-dimensions", "native", function(v)
	if v then
		S.w, S.h = v.w or 0, v.h or 0
	end
end)
-- 屏幕缩放（display-hidpi-scale，嵌入时还有宿主给的）在 embed.lua 里观察
observe("pause", "bool", function(v) S.pause = v or false end)
observe("duration", "number", function(v) S.duration = v or 0 end)
observe("volume", "number", function(v) S.volume = v or 100 end)
observe("mute", "bool", function(v) S.mute = v or false end)
observe("speed", "number", function(v) S.speed = v or 1 end)
observe("fullscreen", "bool", function(v) S.fullscreen = v or false end)
observe("window-maximized", "bool", function(v) S.maximized = v or false end)
observe("ontop", "bool", function(v) S.ontop = v or false end)
observe("media-title", "string", function(v) S.title = v or "" end)
observe("track-list", "native", function(v) S.tracks = v or {} end)
observe("sid", "native", function(v) S.sid = v end)
observe("aid", "native", function(v) S.aid = v end)
observe("secondary-sid", "native", function(v) S.secondary_sid = v end)
observe("secondary-sub-visibility", "bool", function(v) S.secondary_visible = v ~= false end)
observe("sub-visibility", "bool", function(v) S.sub_visible = v ~= false end)
observe("demuxer-via-network", "bool", function(v) S.network = v or false end)
observe("chapter-list", "native", function(v) S.chapters = v or {} end)

-- 这两个一秒变好几次：只在界面露着、或正在缓冲（要显示速度）时才值得重绘
local function observe_quiet(name, kind, fn)
	mp.observe_property(name, kind, function(_, v)
		fn(v)
		if S.shown or S.buffering then
			request_render()
		end
	end)
end
observe_quiet("cache-speed", "number", function(v) S.cache_speed = v or 0 end)
observe_quiet("demuxer-cache-state", "native", function(v) S.ranges = (v and v["seekable-ranges"]) or {} end)
observe("paused-for-cache", "bool", function(v) S.buffering = v or false end)

-- 进度只在界面露着、且秒数变了时才重绘：每帧整屏重绘会让 mpv 的 CPU 翻倍
local last_second
mp.observe_property("time-pos", "number", function(_, v)
	S.time = v or 0
	local sec = math.floor(S.time)
	if S.shown and sec ~= last_second then
		request_render()
	end
	last_second = sec
end)

-- 键盘快进快退时露一下界面（拖进度条时界面本来就开着）
mp.observe_property("seeking", "bool", function(_, v)
	if v and not S.drag then
		S.flash_until = mp.get_time() + HIDE_AFTER
		request_render()
	end
end)

-- 缓冲转圈、「下一集」卡片的倒数圈、界面到点收起：都靠这一个定时器推
mp.add_periodic_timer(0.05, function()
	if S.buffering then
		S.spin = S.spin + 0.05
		request_render()
	elseif upnext.animating() or S.shown ~= ui_visible() then
		request_render()
	end
end)

-- 屏幕提示：键盘调音量、倍速、置顶时界面可能是收着的
local last_volume, last_speed
mp.observe_property("volume", "number", function(_, v)
	if v and last_volume and math.abs(v - last_volume) >= 0.5 and S.drag ~= "volume" and not S.shown then
		toast(string.format("音量 %d%%", math.floor(v + 0.5)))
	end
	last_volume = v
end)
mp.observe_property("speed", "number", function(_, v)
	if v and last_speed and math.abs(v - last_speed) > 0.001 then
		toast("倍速 " .. core.fmt_speed(v))
	end
	last_speed = v
end)

-- 音量调完一秒后告诉 nagare，下次起播带回来。起播时的那个值（nagare 自己传的）不回写。
local volume_timer, startup_volume
mp.observe_property("volume", "number", function(_, v)
	if not v then
		return
	end
	if startup_volume == nil then
		startup_volume = v
		return
	end
	if volume_timer then
		volume_timer:kill()
	end
	volume_timer = mp.add_timeout(1.0, function()
		send("volume", tostring(math.floor(v + 0.5)))
	end)
end)

------------------------------------------------------------------------
-- nagare 发来的状态

-- nav：{"prev": bool, "next": bool, "prevLabel": "第 3 集", "nextLabel": "第 5 集"}，没有的那一边不带 label
local function apply_state(json)
	local st = utils.parse_json(json or "")
	if type(st) ~= "table" then
		return
	end
	if type(st.title) == "string" and st.title ~= "" then
		S.nagare_title = st.title
	end
	if type(st.info) == "string" then
		S.info = st.info
	end
	if type(st.anime4k) == "string" then
		S.anime4k = st.anime4k
	end
	if type(st.nav) == "table" then
		S.nav = st.nav
	end
	request_render()
end

mp.register_script_message("state", apply_state)
mp.register_script_message("toast", function(text, sub)
	if text and text ~= "" then
		toast(text, (sub and sub ~= "") and sub or nil)
	end
end)
mp.register_script_message("upnext", upnext.show)
mp.register_script_message("upnext-clear", upnext.clear)
-- 同一个窗口里换到了别的文件：上一集的卡片一定过时了
mp.register_event("start-file", function() upnext.clear() end)

mp.add_key_binding(nil, "menu", open_context_menu)
mp.add_key_binding(nil, "toggle-danmaku", tracks.toggle_danmaku)
mp.add_key_binding(nil, "screenshot", screenshot)
mp.add_key_binding(nil, "next", function() upnext.go("next") end)
mp.add_key_binding(nil, "prev", function() upnext.go("prev") end)

-- 无边框窗口由界面自己画标题栏与窗口按钮（embed.start 关掉 border；窗口归宿主时不碰）；mpv 自带的
-- 控制条也由这里关掉。都放在脚本里做而不是写进 mpv.conf：万一脚本没加载起来，窗口还有系统边框，
-- 手上还有自带的控制条
embed.start(request_render)
mp.commandv("script-message", "osc-visibility", "never", "no_osd")

send("ready")
