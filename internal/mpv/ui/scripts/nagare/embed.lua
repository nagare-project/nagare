-- 嵌进 nagare 桌面窗口时（宿主模式）与宿主之间的约定，全部经 user-data 属性。
--
--   宿主 → 界面：user-data/nagare/embedded        是否嵌在 nagare 的窗口里（窗口归宿主，不归 mpv）
--               user-data/nagare/titlebar-inset  顶栏左边要让出的宽度（逻辑像素，macOS 左上角的红绿灯）
--               user-data/nagare/hidpi           屏幕缩放（libmpv 的 render API 下 display-hidpi-scale 读不到）
--   界面 → 宿主：user-data/nagare/capture         光标是否落在界面上能点的东西上（这一下按下归界面）
--               user-data/nagare/shown           控制条是否露着（宿主据此一起淡出系统的窗口按钮）
--
-- 一个都没设时（命令行 mpv：Linux、浏览器模式、今天的所有平台）界面的行为与以前一字不差：
-- 界面只读不写，capture / shown 也只在嵌入时才发布。0.36 之前的 mpv 没有 user-data：观察不存在的
-- 属性只会一直是 nil，不会报错。

local mp = require 'mp'
local core = require 'core'

local S = core.S
local M = {}

-- 宿主可能经 libmpv 设成布尔，也可能经 IPC 的 set 命令设成字符串
local function truthy(v)
	return v == true or v == "yes" or v == "true" or v == 1 or v == "1"
end

local function positive(v)
	local n = tonumber(v)
	if n and n > 0 then
		return n
	end
	return nil
end

-- 无边框：命令行 mpv 下窗口是 mpv 自己的，界面自己画标题栏与窗口按钮，所以关掉系统边框。窗口归宿主时
-- 不碰 border —— Windows 的 --wid 下 border=no 会让 mpv 子窗口的边缘变成拖动改大小的把手。--wid 加载时
-- 就看得到；embedded 是宿主连上 IPC 之后才设的，那时把已经关掉的边框还回去。
local borderless = false
local function update_border()
	local want = not S.embedded and (mp.get_property_number("wid", -1) or -1) <= 0
	if want ~= borderless then
		borderless = want
		mp.set_property_bool("border", not want)
	end
end

-- 屏幕缩放：mpv 自己读得到就用它（命令行 mpv、Windows 的 --wid），读不到用宿主给的，都没有按 1
local os_hidpi, host_hidpi
local function update_hidpi()
	S.hidpi = os_hidpi or host_hidpi or 1
end

-- 发布给宿主的值：变了才写，嵌入时才写（嵌入刚打开时把当前值补发一遍）
local published = {}
local function publish(name, value)
	if not S.embedded or published[name] == value then
		return
	end
	published[name] = value
	mp.set_property_native("user-data/nagare/" .. name, value)
end

-- 每次重绘之后由 main.lua 调：capture 是这一帧算出来的「左键归不归界面」
function M.publish(capture)
	publish("capture", capture)
	publish("shown", S.shown)
end

-- 返回媒体库：结束这次播放会话。宿主在 mpv 会话结束时换回网页，nagare 照常收尾（进度落盘、
-- 看完回写、磁力停做种），与命令行下关掉 mpv 窗口是同一条路。
function M.back()
	mp.commandv("quit")
end

-- on_change：嵌入状态、让位宽度、缩放变了之后要重绘
function M.start(on_change)
	update_border()
	mp.observe_property("user-data/nagare/embedded", "native", function(_, v)
		local on = truthy(v)
		if on ~= S.embedded then
			S.embedded = on
			published = {}
			update_border()
		end
		on_change()
	end)
	mp.observe_property("user-data/nagare/titlebar-inset", "native", function(_, v)
		S.titlebar_inset = positive(v) or 0
		on_change()
	end)
	mp.observe_property("display-hidpi-scale", "native", function(_, v)
		os_hidpi = positive(v)
		update_hidpi()
		on_change()
	end)
	mp.observe_property("user-data/nagare/hidpi", "native", function(_, v)
		host_hidpi = positive(v)
		update_hidpi()
		on_change()
	end)
end

return M
