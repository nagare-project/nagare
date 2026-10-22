-- 进度条：缓冲范围、章节缝、两端的时间、悬停时的时间与缩略图、拖动跳转。
--
-- 缩略图由 thumbfast（scripts/thumbfast.lua，MPL-2.0）生成，它广播 thumbfast-info 告诉我们
-- 尺寸与能不能用（网络流上它是关的）；我们只负责告诉它画在哪、画哪一秒。

local mp = require 'mp'
local utils = require 'mp.utils'
local draw = require 'draw'
local core = require 'core'

local S, P, FONT = core.S, core.P, core.FONT
local M = {}

local thumbfast = { width = 0, height = 0, disabled = true, available = false }
local thumb_wanted, thumb_shown = false, false

-- 进度条两端与中线的位置
function M.geometry()
	local s = core.scale()
	return 32 * s, S.w - 32 * s, S.h - 112 * s
end

function M.time_at(x)
	if (S.duration or 0) <= 0 then
		return nil
	end
	local x1, x2 = M.geometry()
	local frac = math.max(0, math.min(1, (x - x1) / math.max(1, x2 - x1)))
	return frac * (S.duration or 0)
end

-- 悬停：竖线 + 时间 + 缩略图
local function draw_hover(c, px, cy, t, dur)
	local s = core.scale()
	local x1, x2 = M.geometry()
	local hx = S.drag == "seek" and px or math.max(x1, math.min(x2, S.mx))
	local ht = S.drag == "seek" and t or (M.time_at(hx) or 0)
	if S.drag ~= "seek" then
		c:rect(hx - 1 * s, cy - 9 * s, hx + 1 * s, cy + 9 * s, { color = "#FFFFFF", opacity = 0.85 })
	end
	local label = core.fmt_time(ht, dur >= 3600)
	local lw = draw.text_width(label, 13 * s) + 16 * s
	local lx = math.max(lw / 2 + 4 * s, math.min(S.w - lw / 2 - 4 * s, hx))
	c:rect(lx - lw / 2, cy - 44 * s, lx + lw / 2, cy - 20 * s, { color = "#000000", opacity = 0.75, radius = 5 * s })
	c:text(lx, cy - 32 * s, label, { align = 5, size = 13 * s, color = P.fg, font = FONT })
	-- 缩略图：时间提示正上方，左右不出窗口
	if thumbfast.available and not thumbfast.disabled and thumbfast.width > 0 then
		local tw, th = thumbfast.width, thumbfast.height
		local margin = 8 * s
		local tx = math.floor(math.min(S.w - tw - margin, math.max(margin, lx - tw / 2)))
		local ty = math.floor(cy - 52 * s - th)
		local b = math.max(1, 2 * s)
		c:rect(tx - b, ty - b, tx + tw + b, ty + th + b, { color = "#FFFFFF", opacity = 0.85, radius = 4 * s })
		mp.commandv("script-message-to", "thumbfast", "thumb", tostring(ht), tostring(tx), tostring(ty))
		thumb_wanted, thumb_shown = true, true
	end
end

function M.draw(c)
	local s = core.scale()
	local x1, x2, cy = M.geometry()
	local dur = S.duration or 0
	local hovering = S.drag == "seek" or (S.hover_id == "seek")
	local th = (hovering and 8 or 6) * s
	local y1, y2 = cy - th / 2, cy + th / 2
	local width = x2 - x1
	c:rect(x1, y1, x2, y2, { color = "#FFFFFF", opacity = 0.16, radius = th / 2 })
	-- 已经可以直接拖过去的部分：本地文件是整条，网络流按 mpv 的缓存范围
	if dur > 0 then
		if not S.network then
			c:rect(x1, y1, x2, y2, { color = "#FFFFFF", opacity = 0.16, radius = th / 2 })
		else
			for _, r in ipairs(S.ranges) do
				local a = x1 + width * math.max(0, math.min(1, (r.start or 0) / dur))
				local b = x1 + width * math.max(0, math.min(1, (r["end"] or 0) / dur))
				if b - a > 1 then
					c:rect(a, y1, b, y2, { color = "#FFFFFF", opacity = 0.18, radius = th / 2 })
				end
			end
		end
	end
	local t = S.drag == "seek" and S.drag_time or S.time
	local frac = dur > 0 and math.max(0, math.min(1, t / dur)) or 0
	local px = x1 + width * frac
	if px - x1 > 0.5 then
		c:rect(x1, y1, px, y2, { color = P.accent, opacity = 1, radius = th / 2 })
	end
	-- 章节（片头、片尾）在进度条上留一道缝
	if dur > 0 then
		for _, ch in ipairs(S.chapters) do
			local tx = ch.time or 0
			if tx > 0.5 and tx < dur - 0.5 then
				local cx = x1 + width * tx / dur
				c:rect(cx - 1 * s, y1 - 0.5, cx + 1 * s, y2 + 0.5, { color = "#000000", opacity = 0.85 })
			end
		end
	end
	c:circle(px, cy, 8 * s, { color = "#FFFFFF", opacity = 1 })
	core.add_element("seek", x1 - 8 * s, cy - 14 * s, x2 + 8 * s, cy + 14 * s, nil)

	if hovering and dur > 0 then
		draw_hover(c, px, cy, t, dur)
	end

	-- 两端的时间
	local long = dur >= 3600
	local ty = S.h - 86 * s
	c:text(x1, ty, core.fmt_time(t, long), { align = 4, size = 14 * s, color = P.fg, opacity = 0.85, font = FONT })
	if dur > 0 then
		c:text(x2, ty, core.fmt_time(dur, long), { align = 6, size = 14 * s, color = P.fg, opacity = 0.85, font = FONT })
	end
end

-- 每次重绘开始时调用；重绘结束时 end_frame 看这一帧还要不要缩略图
function M.begin_frame()
	thumb_wanted = false
end

-- 离开进度条（或界面收起）：缩略图要单独清掉，它不在我们的 overlay 里
function M.end_frame()
	if thumb_shown and not thumb_wanted then
		mp.commandv("script-message-to", "thumbfast", "clear")
		thumb_shown = false
	end
end

-- 拖动中按光标位置跳转：拖的过程中只跳关键帧、限频，松手时精确跳
function M.drag(final)
	local t = M.time_at(S.mx)
	if not t then
		return -- 片长还不知道（直播流、刚打开）：拖了也不知道该跳到哪
	end
	S.drag_time = t
	local now = mp.get_time()
	if final then
		mp.commandv("seek", t, "absolute+exact")
	elseif now - S.last_drag_seek > 0.08 then
		S.last_drag_seek = now
		mp.commandv("seek", t, "absolute+keyframes")
	end
end

mp.register_script_message("thumbfast-info", function(json)
	local data = utils.parse_json(json or "")
	if type(data) == "table" and data.width and data.height then
		thumbfast = data
		core.request_render()
	end
end)

return M
