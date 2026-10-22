-- 字幕、音轨与弹幕轨：认出弹幕轨、弹幕开关、字幕与音轨菜单。

local mp = require 'mp'
local core = require 'core'

local S = core.S
local M = {}

-- 弹幕轨的标题，与 internal/player/session.go 的 danmakuTrackTitle 一致。有对白字幕时它挂在
-- 第二字幕轨（secondary-sid），没有对白字幕时直接占主字幕轨（sid）。
local DANMAKU_TITLE = "nagare-danmaku"

local LANGS = {
	chi = "中文", zho = "中文", chs = "简体中文", cht = "繁体中文", zh = "中文",
	jpn = "日语", ja = "日语", eng = "英语", en = "英语", kor = "韩语", ko = "韩语",
}

local function track_label(t)
	local name = t.title
	if not name or name == "" then
		name = LANGS[t.lang or ""] or t.lang
	end
	if not name or name == "" then
		name = (t.type == "audio" and "音轨 " or "字幕 ") .. t.id
	end
	return name .. " · " .. (t.external and "外挂" or "内嵌")
end

local function danmaku_track_id()
	for _, t in ipairs(S.tracks) do
		if t.type == "sub" and t.external and t.title == DANMAKU_TITLE then
			return t.id
		end
	end
	return nil
end

-- 弹幕挂在哪条轨上："secondary" / "primary" / nil（这一集没有弹幕，或还没编排好）
local function danmaku_mode()
	local id = danmaku_track_id()
	if not id then
		return nil
	end
	if S.secondary_sid == id then
		return "secondary"
	elseif S.sid == id then
		return "primary"
	end
	return nil
end

function M.has_danmaku()
	return danmaku_mode() ~= nil
end

function M.danmaku_visible()
	local mode = danmaku_mode()
	if mode == "secondary" then
		return S.secondary_visible
	elseif mode == "primary" then
		return S.sub_visible
	end
	return false
end

function M.toggle_danmaku()
	local mode = danmaku_mode()
	if not mode then
		core.toast("这一集没有弹幕")
		return
	end
	local visible = not M.danmaku_visible()
	mp.set_property_bool(mode == "secondary" and "secondary-sub-visibility" or "sub-visibility", visible)
	core.send("danmaku", visible and "shown" or "hidden")
	core.toast(visible and "弹幕：已显示" or "弹幕：已隐藏")
end

function M.subtitle_items()
	local items = {}
	local dm = danmaku_track_id()
	-- 弹幕占着主轨时，对白字幕本来就是关着的；这时再点「关闭字幕」会把弹幕一起关掉
	local none = S.sid == false or S.sid == nil or S.sid == "no" or S.sid == dm
	items[#items + 1] = {
		label = "关闭字幕", checked = none,
		action = function()
			if S.sid ~= dm then
				mp.set_property("sid", "no")
			end
		end,
	}
	for _, t in ipairs(S.tracks) do
		-- 弹幕不出现在字幕列表里
		if t.type == "sub" and t.id ~= dm then
			items[#items + 1] = {
				label = track_label(t),
				checked = (not none) and t.id == S.sid,
				action = function() mp.set_property_number("sid", t.id) end,
			}
		end
	end
	return items
end

function M.audio_items()
	local items = {}
	for _, t in ipairs(S.tracks) do
		if t.type == "audio" then
			items[#items + 1] = {
				label = track_label(t),
				checked = t.id == S.aid,
				action = function() mp.set_property_number("aid", t.id) end,
			}
		end
	end
	return items
end

function M.audio_count()
	local n = 0
	for _, t in ipairs(S.tracks) do
		if t.type == "audio" then
			n = n + 1
		end
	end
	return n
end

return M
