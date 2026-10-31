-- verify.sh --simulator 的 Lua 检查：脚本引擎是 LuaJIT、mp 模块可用，并且 JIT 编译器不存在
-- （iOS 不许运行时生成代码：jit.status() 必须是 false，jit.on() 必须被拒绝）。打一行日志就退出。
local engine = (jit and jit.version) or _VERSION
local arch = (jit and jit.arch) or "?"
local jit_status = jit and jit.status() or false
local jit_on_accepted = jit ~= nil and pcall(jit.on)
local utils = require("mp.utils")
mp.msg.info(("lua-check engine=%s arch=%s jit=%s jit-on=%s json=%s"):format(
	engine:gsub(" ", "_"), arch, jit_status and "on" or "off",
	jit_on_accepted and "accepted" or "refused", utils.format_json({ ok = true })))
mp.commandv("quit", "0")
