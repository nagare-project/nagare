-- verify.sh 的 Lua 检查：确认脚本引擎是 LuaJIT、mp 模块可用，然后立即退出。
local engine = (jit and jit.version) or _VERSION
local arch = (jit and jit.arch) or "?"
local utils = require("mp.utils")
local encoded = utils.format_json({ ok = true })
mp.msg.info(("lua-check engine=%s arch=%s mpv=%s json=%s"):format(
	engine:gsub(" ", "_"), arch, mp.get_property("mpv-version"), encoded))
mp.commandv("quit", "0")
