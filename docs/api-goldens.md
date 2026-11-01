# API 黄金响应（testdata/api）

nagare 的接口由 Go 后端（`internal/api` 等）提供，客户端各自手写类型：网页端在 `frontend/src/lib/endpoints.ts`，
以后还有 iOS（Swift）与安卓。后端改了一个字段名、客户端没跟上，客户端不会报错，只会静默解出一堆零值 ——
界面上表现为「封面没了」「进度不动了」，单看哪一边都是对的。

黄金响应把这条线钉住：

- **Go 测试**用真实的处理器（替身只换掉外部依赖）请求各接口，把响应排成规范格式，与仓库根 `testdata/api/<名字>.json`
  **逐字节**比对。处理器输出变了，Go 测试先红。
- **每个客户端**逐份**严格解码**这些文件：缺必填字段、类型不对、多出客户端不认识的字段，都算失败。
  后端加了字段，客户端必须跟着加类型，测试才会绿。

两边都挂在现有的 CI 里（`go test ./...` 与前端的 `bun run typecheck` / `bun run test`），不需要额外的 job。

## 改了接口之后：重新生成

确认后端的改动是有意的，在仓库根运行一行：

```bash
CGO_ENABLED=0 go test ./internal/api/... ./internal/update/ -run Golden -update
```

它重写 `testdata/api/` 里的全部黄金文件，并删掉已经没有对应用例的旧文件。然后：

1. `git diff testdata/api` 看一眼变化是不是预期的那样；
2. 同步改客户端类型（`frontend/src/lib/endpoints.ts` 等）和 `frontend/src/test/apiContracts.ts` 里对应的 schema；
3. `cd frontend && bun run typecheck && bun run test`。

新增一个用例：在 Go 的用例表里加一行（见下表），重新生成，再在 `frontend/src/lib/apiGoldens.test.ts` 的
`CONTRACTS` 里登记它用哪个 schema 解码。没登记的黄金文件会让前端测试失败。

## 文件在哪、谁生成

| 文件名 | 生成它的测试 | 谁要解码 |
|---|---|---|
| `desktop-*.json` | `internal/api/sources/golden_test.go` | 只有桌面版（磁力、来源插件、规则，手机版没有这些接口） |
| `update-*.json` | `internal/update/golden_test.go` | 带自更新的客户端（桌面版） |
| 其余 | `internal/api/golden_test.go`（用例分在 `golden_*_test.go`） | 所有客户端 |

每个文件就是一次完整的响应体：统一信封 `{"success": true, "data": …}` 或 `{"success": false, "data": null, "error": "…"}`。
普通 JSON，没有注释；时间沿用接口本来的写法（毫秒 / 秒时间戳，或 RFC 3339 字符串）。Swift 的 `Codable` 可以直接读。

## 怎么做到每次都一样

- 固定时钟（2026-10-15 12:00 UTC）、固定的封面 / 媒体能力段（`/art/cap-art`、`/media/cap-media`）、
  固定的平台（linux/amd64；桌面窗口那份是 darwin/arm64、Windows 回退那份是 windows/amd64）。
- 目录、收藏、账号、mpv、播放器都是替身，不联网；逐集资料的上游用本机 httptest 服务器。
- 海报取色（`posterTones`）用替身：两张封面算出了色阶，其余是 `null`，两种形态都在黄金文件里。
- `catalog-*-offline` 是「离线启动」：先在固定时钟下把目录取一遍写进临时目录里的磁盘缓存，再换一个全新的 Handler
  （同一个缓存目录、上游全挂、两小时后）请求两次，等后台刷新失败收场，第二次的 `freshness` 带上 `stale` 与 `offline`。
- 会话凭证只是占位串，响应里本来也不带；不读写真实的配置目录，状态文件放在临时目录。
- 实在没有注入点的几样，比对前换成固定值：媒体库的临时目录路径（换成 `/media/anime`）、
  store 生成的库目录 ID 与添加时间、规则目录的加载时间。
- 生成用的是 POSIX 路径与 linux 的报错措辞，所以这几个测试在 Windows 上跳过（CI 在 linux 上比对）。

## 前端的严格解码

`frontend/src/test/contract.ts` 是一个几十行的小解码器（没引入 zod 之类的运行时依赖），
`frontend/src/test/apiContracts.ts` 为每个接口类型写一个 schema：

- **编译期**：`exact<LibraryItem>()(obj({...}))` 要求 schema 推出的类型与 `LibraryItem` 完全一致
  （字段一个不多、一个不少，可选性、字面量取值范围都一样），否则 `bun run typecheck` 报错。schema 因此不会和
  `endpoints.ts` 各说各话。
- **运行时**：`lib/apiGoldens.test.ts` 逐份解码黄金文件。可选字段为 `null` 与缺席是两回事；计数、ID、时间戳按整数检查
  （TS 里同是 `number`，但 Swift / Kotlin 会按整数解码）。
- **覆盖**：TS 类型里的每个可选字段、联合类型的每个分支，都要在至少一份黄金文件里出现过。
  从没出现过的，要么补一个用例，要么是 TS 里留着一个后端早就不发的字段。

以后的 Swift / Kotlin 客户端照同样的规则各写一份测试：读 `testdata/api/` 下的全部文件（手机版跳过 `desktop-*`
与 `update-*`），用严格模式解码（遇到不认识的字段就失败），并检查没有漏掉的文件。
