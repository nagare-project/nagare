-- 绘制：把矩形、文字、矢量图标拼成 ASS 事件。
--
-- 坐标一律是 OSD 像素（overlay 的 res_x/res_y 设成 osd-dimensions），所以 \fs 也是像素。
-- 图标在 24×24 的格子里定义，全部是填充形状（ASS 的绘图只会填充，没有描边），画的时候缩放平移。

local M = {}

-- "#RRGGBB" → ASS 的 BBGGRR
local function hex(c)
	return c:sub(6, 7) .. c:sub(4, 5) .. c:sub(2, 3)
end

-- 不透明度 0..1 → ASS 的透明度 00（不透明）..FF（全透明）
local function alpha(opacity)
	local a = math.floor((1 - math.max(0, math.min(1, opacity))) * 255 + 0.5)
	return string.format("%02X", a)
end

local function num(v)
	return string.format("%.1f", v)
end

-- 文字转义：反斜杠后插一个零宽字符，花括号转义，换行换成 \N
function M.escape(s)
	s = tostring(s or "")
	s = s:gsub("\\", "\\\239\187\191"):gsub("{", "\\{"):gsub("}", "\\}"):gsub("\n", "\\N")
	return s
end

-- 逐个取出 UTF-8 字符：原样的字节串与码点。不用 utf8 库：mpv 常配 LuaJIT（Lua 5.1），没有它。
local function chars(s)
	local i, n = 1, #s
	return function()
		if i > n then
			return nil
		end
		local c = s:byte(i)
		local len = (c < 0x80 and 1) or (c < 0xE0 and 2) or (c < 0xF0 and 3) or 4
		local cp = c
		if len > 1 then
			cp = c % (2 ^ (7 - len))
			for j = 1, len - 1 do
				cp = cp * 64 + ((s:byte(i + j) or 0x80) % 64)
			end
		end
		local ch = s:sub(i, i + len - 1)
		i = i + len
		return ch, cp
	end
end

-- 一个字符估算多宽（em）：ASS 没有量字宽的接口。CJK 与全角记 1em，空格 0.3em，其余记 0.58em。
local function char_em(cp)
	if cp >= 0x2E80 then
		return 1
	elseif cp == 0x20 then
		return 0.3
	end
	return 0.58
end

function M.text_width(s, size)
	local w = 0
	for _, cp in chars(tostring(s or "")) do
		w = w + char_em(cp)
	end
	return w * size
end

------------------------------------------------------------------------
-- 折行（宽度同样是估算的）

local SPACE, NEWLINE = 0x20, 0x0A
-- 不能放在行首的标点：折行时连同前一个词一起挪到下一行
local NO_LINE_START = {}
for _, cp in ipairs({ 0x3001, 0x3002, 0xFF0C, 0xFF0E, 0xFF1A, 0xFF1B, 0xFF01, 0xFF1F, 0xFF09, 0x300D, 0x300F,
	0x3011, 0x3009, 0x300B, 0x2026, 0x201D, 0x2019, 0x2C, 0x2E, 0x3A, 0x3B, 0x21, 0x3F, 0x29 }) do
	NO_LINE_START[cp] = true
end

-- 拆成折行用的词：CJK 一字一词，空格、换行各自一个（single），连在一起的其余字符（拉丁字母、数字、
-- 路径）算一个词
local function words(s)
	local out, word = {}, nil
	for ch, cp in chars(s) do
		if cp >= 0x2E80 or cp == SPACE or cp == NEWLINE then
			if word then
				out[#out + 1] = word
				word = nil
			end
			out[#out + 1] = { text = ch, em = char_em(cp), cp = cp, single = true }
		elseif word then
			word = { text = word.text .. ch, em = word.em + char_em(cp), cp = word.cp }
		else
			word = { text = ch, em = char_em(cp), cp = cp }
		end
	end
	if word then
		out[#out + 1] = word
	end
	return out
end

-- 去掉行尾空格后拼成一行
local function join(line)
	local last = #line
	while last > 0 and line[last].cp == SPACE do
		last = last - 1
	end
	local parts = {}
	for i = 1, last do
		parts[i] = line[i].text
	end
	return table.concat(parts)
end

-- 去掉最后一个 UTF-8 字符
local function drop_last_char(s)
	local i = #s
	while i > 1 and s:byte(i) >= 0x80 and s:byte(i) < 0xC0 do
		i = i - 1
	end
	return s:sub(1, i - 1)
end

-- 按 max_w 折行，最多 max_lines 行：放不下的，最后一行以「…」结尾。
-- 一整行都放不下的词（路径、网址）按字切开；文字里的换行照样换行。
function M.wrap(s, size, max_w, max_lines)
	local limit = max_w / size
	local lines, line, em = {}, {}, 0
	local function newline()
		lines[#lines + 1] = join(line)
		line, em = {}, 0
	end
	local add
	add = function(w)
		if w.cp == NEWLINE then
			newline()
		elseif w.cp == SPACE and #line == 0 then
			return -- 行首不留空格
		elseif em + w.em <= limit or (#line == 0 and w.single) then
			-- 窄到一个字都放不下时照样放：宁可出界也不丢字
			line[#line + 1] = w
			em = em + w.em
		elseif w.cp == SPACE then
			newline()
		elseif w.em > limit and not w.single then
			for ch, cp in chars(w.text) do
				add({ text = ch, em = char_em(cp), cp = cp, single = true })
			end
		else
			local carry = (NO_LINE_START[w.cp] and #line > 1) and table.remove(line) or nil
			newline()
			if carry then
				add(carry)
			end
			add(w)
		end
	end
	for _, w in ipairs(words(tostring(s or ""))) do
		add(w)
	end
	if #line > 0 then
		newline()
	end
	if #lines <= max_lines then
		return lines
	end
	local out = {}
	for i = 1, max_lines do
		out[i] = lines[i]
	end
	-- 「…」按 1em 留位置：多数中文字体里它是全角宽
	local last = out[max_lines]
	while last ~= "" and M.text_width(last, size) + size > max_w do
		last = drop_last_char(last)
	end
	out[max_lines] = last .. "…"
	return out
end

------------------------------------------------------------------------
-- 路径

-- 圆角矩形（三次贝塞尔近似四分之一圆，常数 0.5523）
local function rrect_path(x1, y1, x2, y2, r)
	r = math.max(0, math.min(r or 0, (x2 - x1) / 2, (y2 - y1) / 2))
	if r == 0 then
		return table.concat({ "m", num(x1), num(y1), "l", num(x2), num(y1), "l", num(x2), num(y2), "l", num(x1), num(y2) }, " ")
	end
	local k = r * 0.4477
	return table.concat({
		"m", num(x1 + r), num(y1),
		"l", num(x2 - r), num(y1),
		"b", num(x2 - k), num(y1), num(x2), num(y1 + k), num(x2), num(y1 + r),
		"l", num(x2), num(y2 - r),
		"b", num(x2), num(y2 - k), num(x2 - k), num(y2), num(x2 - r), num(y2),
		"l", num(x1 + r), num(y2),
		"b", num(x1 + k), num(y2), num(x1), num(y2 - k), num(x1), num(y2 - r),
		"l", num(x1), num(y1 + r),
		"b", num(x1), num(y1 + k), num(x1 + k), num(y1), num(x1 + r), num(y1),
	}, " ")
end

-- 环形扇区（角度制，0° 朝右、顺时针为正，与屏幕坐标一致）
local function arc_path(cx, cy, ro, ri, a1, a2, steps)
	steps = steps or 24
	local parts = {}
	for i = 0, steps do
		local a = math.rad(a1 + (a2 - a1) * i / steps)
		parts[#parts + 1] = (i == 0 and "m " or "l ") .. num(cx + ro * math.cos(a)) .. " " .. num(cy + ro * math.sin(a))
	end
	for i = steps, 0, -1 do
		local a = math.rad(a1 + (a2 - a1) * i / steps)
		parts[#parts + 1] = "l " .. num(cx + ri * math.cos(a)) .. " " .. num(cy + ri * math.sin(a))
	end
	return table.concat(parts, " ")
end
M.arc_path = arc_path

-- 粗线段（四边形）
local function line_path(x1, y1, x2, y2, w)
	local dx, dy = x2 - x1, y2 - y1
	local len = math.sqrt(dx * dx + dy * dy)
	local nx, ny = -dy / len * w / 2, dx / len * w / 2
	return table.concat({
		"m", num(x1 + nx), num(y1 + ny), "l", num(x2 + nx), num(y2 + ny),
		"l", num(x2 - nx), num(y2 - ny), "l", num(x1 - nx), num(y1 - ny),
	}, " ")
end

local function circle_path(cx, cy, r)
	return rrect_path(cx - r, cy - r, cx + r, cy + r, r)
end

-- 矩形框（四条边），给字幕、最大化图标用
local function frame_path(x1, y1, x2, y2, t)
	return table.concat({
		rrect_path(x1, y1, x2, y1 + t, 0),
		rrect_path(x1, y2 - t, x2, y2, 0),
		rrect_path(x1, y1, x1 + t, y2, 0),
		rrect_path(x2 - t, y1, x2, y2, 0),
	}, " ")
end

------------------------------------------------------------------------
-- 图标（24×24）

local speaker = "m 3 9 l 7 9 l 12 5 l 12 19 l 7 15 l 3 15"

local icons = {
	play = "m 8 5 l 19 12 l 8 19",
	pause = "m 6 5 l 10 5 l 10 19 l 6 19 m 14 5 l 18 5 l 18 19 l 14 19",
	prev = "m 5 5 l 7 5 l 7 19 l 5 19 m 19 6 l 19 18 l 9 12",
	next = "m 17 5 l 19 5 l 19 19 l 17 19 m 5 6 l 15 12 l 5 18",
	volume = speaker .. " " .. arc_path(12, 12, 4.6, 3, -55, 55, 12) .. " " .. arc_path(12, 12, 8.4, 6.8, -55, 55, 16),
	volume_low = speaker .. " " .. arc_path(12, 12, 4.6, 3, -55, 55, 12),
	mute = speaker .. " " .. line_path(15, 9, 21, 15, 1.8) .. " " .. line_path(21, 9, 15, 15, 1.8),
	fullscreen = "m 4 4 l 10 4 l 10 6 l 6 6 l 6 10 l 4 10 "
		.. "m 20 4 l 20 10 l 18 10 l 18 6 l 14 6 l 14 4 "
		.. "m 4 20 l 4 14 l 6 14 l 6 18 l 10 18 l 10 20 "
		.. "m 20 20 l 14 20 l 14 18 l 18 18 l 18 14 l 20 14",
	exit_fullscreen = "m 10 4 l 10 10 l 4 10 l 4 8 l 8 8 l 8 4 "
		.. "m 14 4 l 16 4 l 16 8 l 20 8 l 20 10 l 14 10 "
		.. "m 4 14 l 10 14 l 10 20 l 8 20 l 8 16 l 4 16 "
		.. "m 14 20 l 14 14 l 20 14 l 20 16 l 16 16 l 16 20",
	subtitles = frame_path(3, 5, 21, 19, 1.8) .. " "
		.. rrect_path(7, 10.2, 10, 11.8, 0) .. " " .. rrect_path(12, 10.2, 17, 11.8, 0) .. " "
		.. rrect_path(7, 13.6, 13, 15.2, 0) .. " " .. rrect_path(15, 13.6, 17, 15.2, 0),
	sparkle = "m 12 3 b 12.7 7.4 14.6 9.3 19 10 b 14.6 10.7 12.7 12.6 12 17 b 11.3 12.6 9.4 10.7 5 10 b 9.4 9.3 11.3 7.4 12 3 "
		.. "m 18.5 14.5 b 18.8 16.2 19.3 16.7 21 17 b 19.3 17.3 18.8 17.8 18.5 19.5 b 18.2 17.8 17.7 17.3 16 17 b 17.7 16.7 18.2 16.2 18.5 14.5",
	more = circle_path(5, 12, 1.9) .. " " .. circle_path(12, 12, 1.9) .. " " .. circle_path(19, 12, 1.9),
	minimize = rrect_path(5, 11.1, 19, 12.9, 0),
	maximize = frame_path(5, 5, 19, 19, 1.8),
	close = line_path(6, 6, 18, 18, 1.8) .. " " .. line_path(18, 6, 6, 18, 1.8),
	check = line_path(5, 12.5, 9.8, 17.3, 2.2) .. " " .. line_path(8.3, 17.3, 19, 6.6, 2.2),
	-- 快退 / 快进 10 秒：一圈缺个角的环 + 箭头，「10」另用文字画
	back = arc_path(12, 12.5, 8.6, 7, -90, 180, 28) .. " m 12 1.9 l 12 6 l 8.6 4",
	forward = arc_path(12, 12.5, 8.6, 7, 0, 270, 28) .. " m 12 1.9 l 12 6 l 15.4 4",
	-- 重试：顺时针一圈、左上留个口，口上一个朝右上的箭头
	retry = arc_path(12, 12.5, 8.2, 6.2, -50, 220, 28) .. " m 3.7 5.6 l 9.4 4.4 l 9.2 10.2",
}

-- 把 24×24 格子里的路径缩放平移到 (x, y) 起、边长 size 的方块
local function place(path, x, y, size)
	local k = size / 24
	local out, axis = {}, 0
	for tok in path:gmatch("%S+") do
		local v = tonumber(tok)
		if v == nil then
			out[#out + 1] = tok
			axis = 0
		else
			if axis == 0 then
				out[#out + 1] = num(x + v * k)
			else
				out[#out + 1] = num(y + v * k)
			end
			axis = 1 - axis
		end
	end
	return table.concat(out, " ")
end

------------------------------------------------------------------------
-- 画布

local Canvas = {}
Canvas.__index = Canvas

function M.canvas()
	return setmetatable({ events = {} }, Canvas)
end

function Canvas:push(s)
	self.events[#self.events + 1] = s
end

function Canvas:result()
	return table.concat(self.events, "\n")
end

local function fill_tags(color, opacity)
	return "{\\an7\\pos(0,0)\\bord0\\shad0\\blur0\\1c&H" .. hex(color) .. "&\\1a&H" .. alpha(opacity) .. "&\\p1}"
end

-- 原样画一段已经在屏幕坐标里的路径
function Canvas:shape(path, color, opacity)
	self:push(fill_tags(color or "#FFFFFF", opacity or 1) .. path .. "{\\p0}")
end

function Canvas:rect(x1, y1, x2, y2, o)
	o = o or {}
	if o.border_color then
		local b = o.border or 1
		self:shape(rrect_path(x1, y1, x2, y2, o.radius), o.border_color, o.border_opacity or 1)
		self:shape(rrect_path(x1 + b, y1 + b, x2 - b, y2 - b, math.max(0, (o.radius or 0) - b)), o.color, o.opacity)
	else
		self:shape(rrect_path(x1, y1, x2, y2, o.radius), o.color, o.opacity)
	end
end

-- 只画边框：填充全透明，靠 ASS 的 \bord 描边（叠一层透明形状盖不掉下面的填充）
function Canvas:stroke_rect(x1, y1, x2, y2, o)
	o = o or {}
	self:push("{\\an7\\pos(0,0)\\bord" .. num(o.width or 1) .. "\\shad0\\blur0\\1a&HFF&\\3c&H"
		.. hex(o.color or "#FFFFFF") .. "&\\3a&H" .. alpha(o.opacity or 1) .. "&\\p1}"
		.. rrect_path(x1, y1, x2, y2, o.radius) .. "{\\p0}")
end

function Canvas:circle(cx, cy, r, o)
	o = o or {}
	self:shape(circle_path(cx, cy, r), o.color, o.opacity)
end

-- 竖直方向的渐变遮罩：ASS 画不了渐变，切成若干条叠出来
function Canvas:scrim(x1, y1, x2, y2, top_opacity, bottom_opacity, bands)
	bands = bands or 24
	local h = (y2 - y1) / bands
	for i = 0, bands - 1 do
		local t = (i + 0.5) / bands
		local op = top_opacity + (bottom_opacity - top_opacity) * t
		if op > 0.004 then
			self:shape(rrect_path(x1, y1 + i * h, x2, y1 + (i + 1) * h + 0.6, 0), "#000000", op)
		end
	end
end

function Canvas:icon(name, x, y, size, color, opacity)
	local path = icons[name]
	if path then
		self:shape(place(path, x, y, size), color, opacity)
	end
end

-- 文字。o.align 用 ASS 的数字键盘方位（7 左上、5 居中、9 右上、4 左中、6 右中……）
function Canvas:text(x, y, s, o)
	o = o or {}
	local tags = {
		"{\\an", tostring(o.align or 7),
		"\\pos(", num(x), ",", num(y), ")",
		"\\fs", num(o.size or 16),
		"\\b", o.bold and "1" or "0",
		"\\bord", num(o.outline or 0),
		"\\3c&H000000&\\3a&H", alpha(o.outline_opacity or 0.5), "&",
		"\\shad0\\blur", num(o.blur or 0),
		"\\1c&H", hex(o.color or "#E6E6E6"), "&",
		"\\1a&H", alpha(o.opacity or 1), "&",
	}
	if o.font and o.font ~= "" then
		tags[#tags + 1] = "\\fn" .. o.font
	end
	tags[#tags + 1] = "}"
	self:push(table.concat(tags) .. M.escape(s))
end

return M
