-- 菜单：右键菜单与控制条按钮弹出的菜单共用这一套。
--
-- 菜单项：{ label, hint, checked, items（子菜单：表或返回表的函数）, action, separator }
-- 只有最深一层能被键盘操作；鼠标悬停到带子菜单的项上就展开下一层。

local draw = require 'draw'

local M = {}

local levels = {} -- 已展开的各层：{ items, x1, y1, x2, y2, hover, rows }
local env = { w = 0, h = 0, scale = 1, font = "", palette = nil }

local ITEM_H, SEP_H, PAD, RADIUS, GAP, MIN_W = 34, 9, 6, 10, 4, 200

function M.is_open()
	return #levels > 0
end

function M.close()
	levels = {}
end

local function resolve(items)
	if type(items) == "function" then
		return items() or {}
	end
	return items or {}
end

-- 量出一层的尺寸并定好每一行的位置
local function measure(items)
	local s = env.scale
	local size = 14 * s
	local width = MIN_W * s
	local height = PAD * 2 * s
	for _, it in ipairs(items) do
		if it.separator then
			height = height + SEP_H * s
		else
			local w = draw.text_width(it.label, size) + 24 * s
			if it.hint then
				w = w + draw.text_width(it.hint, 12 * s) + 24 * s
			end
			if it.items or it.checked ~= nil then
				w = w + 24 * s
			end
			width = math.max(width, w + PAD * 2 * s)
			height = height + ITEM_H * s
		end
	end
	return width, height
end

local function layout(level)
	local s = env.scale
	local y = level.y1 + PAD * s
	level.rows = {}
	for i, it in ipairs(level.items) do
		local h = (it.separator and SEP_H or ITEM_H) * s
		level.rows[i] = { y1 = y, y2 = y + h }
		y = y + h
	end
end

-- 把 (x, y) 起、w×h 的框挪进窗口里
local function clamp(x, y, w, h)
	local margin = 8 * env.scale
	x = math.max(margin, math.min(x, env.w - w - margin))
	y = math.max(margin, math.min(y, env.h - h - margin))
	return x, y
end

local function push_level(items, x, y, place)
	items = resolve(items)
	if #items == 0 then
		return
	end
	local w, h = measure(items)
	if place == "above" then
		y = y - h
	elseif place == "submenu" then
		-- 右边放不下就放到上一层左边
		local parent = levels[#levels]
		if x + w > env.w - 8 * env.scale and parent then
			x = parent.x1 - GAP * env.scale - w
		end
	end
	x, y = clamp(x, y, w, h)
	local level = { items = items, x1 = x, y1 = y, x2 = x + w, y2 = y + h, hover = nil }
	layout(level)
	levels[#levels + 1] = level
end

-- opts.place："cursor"（左上角落在 x, y）或 "above"（底边落在 y，用于控制条按钮）
function M.open(items, x, y, opts)
	opts = opts or {}
	env.w, env.h = opts.w or env.w, opts.h or env.h
	env.scale, env.font, env.palette = opts.scale or 1, opts.font or "", opts.palette
	levels = {}
	push_level(items, x, y, opts.place or "cursor")
end

local function row_at(level, x, y)
	if x < level.x1 or x > level.x2 or y < level.y1 or y > level.y2 then
		return nil
	end
	for i, r in ipairs(level.rows) do
		if y >= r.y1 and y < r.y2 and not level.items[i].separator then
			return i
		end
	end
	return nil
end

-- 第 depth 层第 i 项是带子菜单的项：展开它（已经展开同一项就不动）
local function expand(depth, i)
	while #levels > depth do
		table.remove(levels)
	end
	local level = levels[depth]
	local it = level.items[i]
	if it and it.items then
		push_level(it.items, level.x2 + GAP * env.scale, level.rows[i].y1 - PAD * env.scale, "submenu")
		levels[depth].open_index = i
	else
		levels[depth].open_index = nil
	end
end

-- 返回鼠标是否在任何一层菜单上
function M.mouse_move(x, y)
	for depth = #levels, 1, -1 do
		local level = levels[depth]
		local i = row_at(level, x, y)
		if x >= level.x1 and x <= level.x2 and y >= level.y1 and y <= level.y2 then
			level.hover = i
			-- 悬停到另一项：它有子菜单就展开，没有就收起更深的层
			if i and level.open_index ~= i then
				expand(depth, i)
			end
			return true
		end
	end
	return false
end

local function activate(it)
	if it.items then
		return false
	end
	M.close()
	if it.action then
		it.action()
	end
	return true
end

-- 点击：落在菜单上就执行，落在外面就关掉。返回是否落在菜单上。
function M.click(x, y)
	for depth = #levels, 1, -1 do
		local level = levels[depth]
		if x >= level.x1 and x <= level.x2 and y >= level.y1 and y <= level.y2 then
			local i = row_at(level, x, y)
			if i then
				local it = level.items[i]
				if it.items then
					expand(depth, i)
				else
					activate(it)
				end
			end
			return true
		end
	end
	M.close()
	return false
end

local function step(level, dir)
	local n = #level.items
	local i = level.hover or (dir > 0 and 0 or n + 1)
	for _ = 1, n do
		i = i + dir
		if i < 1 then
			i = n
		elseif i > n then
			i = 1
		end
		if not level.items[i].separator then
			level.hover = i
			return
		end
	end
end

function M.key(name)
	local level = levels[#levels]
	if not level then
		return
	end
	if name == "esc" then
		M.close()
	elseif name == "up" then
		step(level, -1)
	elseif name == "down" then
		step(level, 1)
	elseif name == "left" then
		if #levels > 1 then
			table.remove(levels)
			levels[#levels].open_index = nil
		end
	elseif name == "right" or name == "enter" then
		local i = level.hover
		local it = i and level.items[i]
		if it then
			if it.items then
				expand(#levels, i)
				local child = levels[#levels]
				if child ~= level then
					step(child, 1)
				end
			elseif name == "enter" then
				activate(it)
			end
		end
	end
end

function M.render(canvas)
	local s, p = env.scale, env.palette
	for _, level in ipairs(levels) do
		canvas:rect(level.x1, level.y1, level.x2, level.y2, {
			color = p.panel, opacity = 0.97, radius = RADIUS * s,
			border = math.max(1, s), border_color = "#FFFFFF", border_opacity = 0.1,
		})
		for i, it in ipairs(level.items) do
			local r = level.rows[i]
			if it.separator then
				local cy = (r.y1 + r.y2) / 2
				canvas:rect(level.x1 + 12 * s, cy, level.x2 - 12 * s, cy + math.max(1, s), { color = "#FFFFFF", opacity = 0.08 })
			else
				local x1, x2 = level.x1 + PAD * s, level.x2 - PAD * s
				local hovered = level.hover == i or level.open_index == i
				local color = p.fg
				if it.checked then
					canvas:rect(x1, r.y1, x2, r.y2, { color = p.accent, opacity = 0.16, radius = 6 * s })
					color = p.accent_text
				elseif hovered then
					canvas:rect(x1, r.y1, x2, r.y2, { color = "#FFFFFF", opacity = 0.08, radius = 6 * s })
				end
				local cy = (r.y1 + r.y2) / 2
				canvas:text(x1 + 12 * s, cy, it.label, { align = 4, size = 14 * s, color = color, font = env.font })
				local right = x2 - 12 * s
				if it.items then
					canvas:text(right, cy, "›", { align = 6, size = 16 * s, color = p.fg, opacity = 0.55, font = env.font })
				elseif it.checked then
					canvas:icon("check", right - 16 * s, cy - 8 * s, 16 * s, p.accent_text, 1)
				elseif it.hint then
					canvas:text(right, cy, it.hint, {
						align = 6, size = 12 * s, font = env.font,
						color = it.hint_accent and p.accent_text or p.fg, opacity = it.hint_accent and 1 or 0.55,
					})
				end
			end
		end
	end
end

return M
