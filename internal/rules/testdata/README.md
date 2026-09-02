# internal/rules/testdata — 六源差分测试语料（决议 CQ1）

这里是 M2 声明式规则引擎的**验收真值**：六个磁力源的真实响应 fixture、用例清单、
以及旧 Go 适配器（`internal/rules/reference/`，临时参照组）对每份 fixture 的**黄金输出**。

规则引擎的差分测试只吃这个目录，不依赖参照组。参照组在六源规则全部跑通后整包删除，
golden 留下继续当回归套件。

> fixture 的来源：animego `go-api/internal/torrents/*_test.go` 里内联在 Go 字符串常量中的
> 响应体，逐字节外提（含缩进：garden 用 tab、animetosho 用 tab+2 空格、RSS 系用空格，
> 文件末尾均无换行）。它们是手写的最小化真实响应，不是线上抓包。

---

## 目录结构

```
testdata/
├── README.md
├── garden/        animes.garden      JSON
├── acgrip/        acg.rip            RSS
├── nyaa/          nyaa.si            RSS + nyaa: 命名空间
├── dmhy/          share.dmhy.org     RSS
├── mikan/         mikanani.me        RSS + torrent 命名空间
└── animetosho/    feed.animetosho.org JSON（含 AniDB id 订阅）
    ├── cases.json              用例清单
    ├── <case>.json|.xml        fixture（响应体；坏响应按声称格式取后缀）
    ├── no_body.json|.xml       零字节：非 2xx 状态用例共用
    └── <case>.golden.json      旧适配器输出（仅 expect != error 的用例）
```

### cases.json 契约

```json
{"name": "happy", "file": "happy.xml", "query": "naruto", "status": 200,
 "kind": "search", "anidbId": 17389, "expect": "items"}
```

| 字段 | 含义 |
|---|---|
| `name` | 用例名，目录内唯一；golden 文件名 = `<name>.golden.json` |
| `file` | fixture 文件名；多个用例可共用一份（如 `empty_resources.json` 同时服务 `empty_resources` 与 `blank_query`） |
| `query` | 传给适配器的搜索词，原样；测试会断言它落在了该源约定的 URL 参数上 |
| `status` | 测试服务器返回的 HTTP 状态；`!= 200` 时 `expect` 必须为 `error` |
| `kind` | `search`（关键词）或 `anidb`（只有 animetosho 有，走 `?aid=`，此时 `query` 为空） |
| `anidbId` | 仅 `kind == anidb` |
| `expect` | 旧适配器对该输入的结论：`items`（非空）/ `empty`（非 nil 空切片，无 error）/ `error` |

`reference/golden_test.go` 里的 `loadCases` 会校验以上全部约束，另有
`TestGoldenManifestCoversDir` 保证目录里没有清单未引用的孤儿 fixture / 孤儿 golden。

### golden 的生成与比对

```sh
go test ./internal/rules/reference/            # 比对（缺 golden 即失败）
go test ./internal/rules/reference/ -update    # 重写全部 golden
```

`-update` 只在该包定义，不能从 `./...` 透传。golden 序列化规则：
`json.Encoder` + `SetIndent("", "  ")` + `SetEscapeHTML(false)`——除了 `&` 不转义成
`&` 和结尾多一个换行外，与 `json.MarshalIndent` 逐字节一致。字段序即
`TorrentItem` 结构体序。

---

## 输出契约：`TorrentItem` 的线上形状

```go
type TorrentItem struct {
	Title    string  `json:"title"`
	Magnet   string  `json:"magnet"`
	Size     string  `json:"size"`
	Fansub   *string `json:"fansub"`             // nil → null
	Date     *string `json:"date"`               // nil → null
	Source   Source  `json:"source"`             // "garden" | "acg" | "nyaa" | "dmhy" | "mikan" | "tosho"
	Provider *string `json:"provider,omitempty"` // 仅 garden；nil → 键省略
	Seeders  *int    `json:"seeders,omitempty"`  // 仅 tosho；nil → 键省略；0 是真实的 0
	Infohash string  `json:"infohash,omitempty"` // 仅 tosho 在抓取阶段填；"" → 键省略
}
```

注意 `null` 与「键省略」是两种不同的 nil：`fansub` / `date` 永远出现（可能为 null），
`provider` / `seeders` / `infohash` 缺失时整个键不出现。golden 如实反映这一点，规则引擎
的输出结构体必须用同样的 tag 才能逐字节对上。

空结果一律是非 nil 空切片，序列化为 `[]`（不是 `null`）。所有源保持上游顺序，不排序。

---

## 六源请求形状

所有请求：`GET`，`User-Agent: AnimeGo/1.0`；JSON 源另带 `Accept: application/json`。
查询串一律经 `url.Values.Encode()`（空格 → `+`，CJK 百分号编码）。

| 源 | URL 模板 | query 参数 | `Source` 值 |
|---|---|---|---|
| garden | `https://api.animes.garden/resources?search=<q>&type=动画&pageSize=80` | `search` | `garden` |
| acg.rip | `https://acg.rip/.xml?term=<q>` | `term` | `acg` |
| nyaa | `https://nyaa.si/?page=rss&q=<q>&c=1_0&f=0` | `q` | `nyaa` |
| dmhy | `https://share.dmhy.org/topics/rss/rss.xml?keyword=<q>` | `keyword` | `dmhy` |
| mikan | `https://mikanani.me/RSS/Search?searchstr=<q>` | `searchstr` | `mikan` |
| animetosho | `https://feed.animetosho.org/json?q=<q>&only_tor=1` | `q` | `tosho` |
| animetosho (aid) | `https://feed.animetosho.org/json?aid=<id>&only_tor=1` | `aid`（不带 `q`） | `tosho` |

> ⚠️ **`AnimeGo/1.0` 是 animego 的品牌串，nagare 规则引擎不得沿用**（红线 4）。
> golden 测试的服务器不断言 UA，规则引擎自由选择自己的 UA 不影响差分结果。

### 错误契约（六源一致）

| 情况 | 旧适配器行为 |
|---|---|
| 传输层错误（拒连 / 超时） | `(nil, err)`，错误信息带源前缀 |
| 非 2xx（`< 200 \|\| >= 300`） | `(nil, err)`，**不读响应体**；错误信息 `"<src>: upstream status <code>"` |
| 解码失败 | `(nil, err)`，错误信息含 `"decode"` |
| 200 + 解出 0 条 | `([]TorrentItem{}, nil)` —— 成功空结果；garden / tosho(keyword) 另外打 Warn |

规则引擎侧：前三行是「源失败」，最后一行才是「无结果」（CQ3）。
清单里 `expect: "error"` 的用例就是前三行中能用 HTTP 表达的部分（非 2xx + 坏响应体）。

**解码器只读一个文档**（已实测）：`json.Decoder.Decode` / `xml.Decoder.Decode` 读完第一个完整值
即停，`[]garbage` 或 `</rss>` 之后的垃圾**不会**报错。规则引擎若对 JSON 用整段解析
（`json.Unmarshal("[]garbage")` → `invalid character 'g' after top-level value`）会比旧适配器
更严格；XML 的 `xml.Unmarshal` 则与流式解码一样宽松。差分时注意这不是 fixture 覆盖到的行为。

---

## 共享规则（所有源）

- **过滤**：`title == ""` 或 `magnet` 不以 `"magnet:"`（**区分大小写**）开头 → 丢弃。
  只查前缀，`magnet:?xt=abc`（没有 btih）也放行。
- **fansub**：`ParseFansub(title)` = 正则 `^[\[【]([^\]】]+)[\]】]` 的第一组；标题开头必须是
  括号；`[` 与 `】` 可以混配；`[]` 空括号不匹配；无匹配 → `null`。
  animetosho **从不调用**它（fansub 恒为 null）；garden 优先 `fansub.name`。
- **date**：上游字符串**原样**透传（不解析、不校验，`"t"` 也照收）；`""` → `null`。
- **`stringPtr("")` → nil**：所有可选字符串字段的空串统一变 null / 省略。
- **两个体积格式化函数**（移植自 Express，保留 JS 怪癖）：

  | | `FormatBytes(raw)`（输入字节） | `FormatKb(raw)`（输入 KB） |
  |---|---|---|
  | `>= 1e9 B` / `>= 1e6 KB` | `"X.X GB"`（`FormatFloat 'f' 1`） | 同 |
  | `>= 1e6 B` / `>= 1e3 KB` | `"X MB"`（`math.Round`，四舍五入远离零） | 同 |
  | 其余 | `"X KB"` = `math.Round(n/1e3)` → **1..499 B 得 `"0 KB"`** | `"X KB"` = 原数 |
  | `<= 0` / 空 / 非数字开头 | `""` | `""` |

  数字解析是 `parseIntJSLike`：跳过前导空白、可选正负号、贪婪取十进制数字、遇非数字停
  （`"1234abc"` → 1234；`"1.5e9"` → 1；`"abc"` → 无）。**`"0 KB"` 与 `""` 是两个不同输出**：
  前者是 1–499 字节，后者是 0 / 缺失。dmhy `length="100"`、mikan `contentLength=100`、
  tosho `total_size: 100` 在 golden 里都是 `"0 KB"`。

---

## 各源单位与怪癖（以代码为准）

### garden（JSON，`resources[]`）

- **size 单位 KB** → `FormatKb`。字段按 `json.RawMessage` 收，再 `string(raw)` 喂给解析器，
  所以：数字 `3460300` → `"3.5 GB"`；**JSON 字符串 `"3460300"` → `""`**（原始字节带引号，
  首字符 `"` 不是数字）；`null` → `""`；`1234.5` → 1234 KB。注释里说的「接受字符串」实际上不成立。
- **fansub**：`fansub.name` 非空 → 用它；`fansub: null` 或 `name: ""` → 回落 `ParseFansub(title)`。
- **provider**：`provider` 原样；`""` → 键省略。唯一填 provider 的源。
- **date**：`createdAt` 原样（RFC3339 串）。
- **seeders 恒为 nil**：解码结构体没有 seeders 字段（`types.go` 的注释说 garden 「有时」报
  seeders，代码里并没有）。
- **零结果 tripwire**：200 + 0 条 + `TrimSpace(q) != ""` → `Warn("garden: zero-result ...", "query", q)`；
  `blank_query` 用例（`"   "`）证明空白 query 不触发。结果仍是 `[]` + nil error。
- 其它字段（`id`、`pagination`）忽略。

### acg.rip（RSS，自有解码路径，不走 rss.go）

- **magnet**：`enclosure@url`；**为空则回落 `<link>`**（golden `enclosure_missing`：link 是 magnet 的那条存活）。
- **size 单位字节** → `FormatBytes(enclosure@length)`；无 enclosure → `""`。
- **date**：`pubDate` 原样（RFC1123 风格）。

### nyaa（RSS，命名空间 `https://nyaa.si/xmlns/nyaa`）

- **feed 里没有 magnet**，由 `buildNyaaMagnet(infoHash, title, link)` 合成：
  ```
  magnet:?xt=urn:btih:<TrimSpace(infoHash)>
        &dn=<url.QueryEscape(title)>
        &tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce
        &tr=http%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce
  ```
  hash **不校验、不改大小写**；`infoHash` 空 → 返回 `<link>` → 被 magnet 前缀过滤掉。
- **`dn=` 是 Go `url.QueryEscape`**：空格 → `+`，`[`/`]` → `%5B`/`%5D`，`#` → `%23`，`&` → `%26`，
  CJK 按 UTF-8 百分号编码。这与 Express 的 `encodeURIComponent`（空格 → `%20`）**已经不同**；
  golden 锁的是 Go 形式。规则引擎要么用同一编码，要么差分时对 `dn` 解码后比较。
- **size 原样透传**：`<nyaa:size>1.5 GiB</nyaa:size>` → `"1.5 GiB"`（不经 FormatBytes，单位是上游给的 GiB/MiB）。
- **seeders 恒为 nil**：nyaa RSS 其实有 `<nyaa:seeders>`，旧适配器不解码。
- **date**：`pubDate` 原样。

### dmhy（RSS，走 rss.go 共享路径）

- **magnet**：`enclosure@url` 原样（`&amp;` 由 encoding/xml 解回 `&`，不要二次 unescape）。
  **没有 `<link>` 回落**（与 acg.rip 的区别）：无 enclosure 的条目直接丢。
- **size 单位字节** → `FormatBytes(enclosure@length)`；常见 `length="0"` → `""`。
- **date**：`pubDate` 原样（`+0800` 时区形式）。

### mikan（RSS，命名空间 `https://mikanani.me/0.1/`）

- **feed 里没有 magnet**：`infoHashRE = [0-9a-fA-F]{40}` 在 `enclosure@url` 找**第一个** 40-hex 串，
  没有再找 `<link>`；找到后 `buildNyaaMagnet(hash, title, link)` 合成（同 nyaa，带同样两条 tracker）。
  hash 大小写按找到的原样。两处都没有 → `<link>` 不是 magnet → 丢弃。
  真实 URL 形如 `.../Download/20260101/<hash>.torrent`、`.../Home/Episode/<hash>`；日期段 8 位不会误中。
- **size 单位字节**，来自 **`{https://mikanani.me/0.1/}torrent/contentLength`**；`enclosure@length`（恒为 0）被忽略。
  fixture 里 `<torrent xmlns="https://mikanani.me/0.1/">` 用的是默认命名空间声明形式（真实 Mikan 也是），
  规则引擎的 XML 选择器必须按 **URI** 而非前缀匹配。
- **date**：item 级 `<pubDate>`（无命名空间）优先；缺失则回落 `{ns}torrent/pubDate`；都没有 → `null`。
  torrent 级 pubDate 形如 `2026-01-01T12:00:00`（**无时区**），原样透传——参照组的 rank.go 解析不了它
  （RFC3339 要求时区），会当零时间排到最底。这是排序层的问题，不影响抓取层 golden。
- 其它：`{ns}torrent/link` 忽略。

### animetosho（JSON，顶层数组）

- **magnet**：`magnet_uri` 原样。
- **infohash**：`ToLower(TrimSpace(info_hash))`，**不校验长度/字符**（golden 里 `"dead"`、`"3333"` 都照收）。
  唯一在抓取阶段就填 `Infohash` 的源；缺失 → 键省略。
- **seeders 三态**：键缺失/`null` → nil（键省略）；`0` → 0（键存在）；`n` → n。
- **fansub 恒为 null**：不调用 ParseFansub。
- **size 单位字节**：`FormatBytes(strconv.FormatInt(total_size))`；`0`/缺失 → `""`。
  `total_size` 解码为 `int64`——上游若给浮点数（`1.5e9`）整个响应解码失败 → 源失败。
- **date**：`timestamp`（unix 秒）→ `time.Unix(ts,0).UTC().Format(RFC3339)` → `"2025-01-01T00:00:00Z"`；
  `<= 0` / 缺失 → `null`。是六源里唯一做了日期格式转换的。
- **provider 恒省略**。
- **两个入口**：关键词（`?q=`）有零结果 tripwire；AniDB（`?aid=`）没有（空 feed 是合法的「此 id 无发布」）。
- 顶层不是数组（例如上游回 `{"error": ...}`）→ 解码失败 → 源失败。
- **Capabilities**：`SupportsSeeders: true, Priority: 10`——聚合层概念，与抓取 golden 无关。

---

## 与任务说明里已知列表的出入

| 说明里的说法 | 代码实际 |
|---|---|
| garden size 是 KB | ✅ 但仅当 JSON 里是**数字**；字符串形式得 `""` |
| dmhy 常见 `length="0"` → 空串 | ✅ 另外 1–499 字节 → `"0 KB"`（不是空串） |
| mikan 从 .torrent URL/link 正则 40-hex | ✅ 顺序是 enclosure@url 先、link 后；取**第一个**匹配 |
| nyaa/mikan magnet 由 buildNyaaMagnet 拼、两条固定 tracker | ✅ 补充：`dn` 是 `url.QueryEscape`（空格 `+`），hash 不改大小写 |
| tosho 从不设 Fansub、预设 Infohash、seeders 三态 | ✅ 补充：Infohash 只 lower+trim 不校验；date 是唯一被转换格式的 |
| acg.rip enclosure 缺失回落 `<link>` | ✅ dmhy **没有**这个回落 |
| garden fansub.name 空时回落 ParseFansub | ✅ `fansub: null` 同样回落 |
| （未提）garden / nyaa seeders | 两者都恒为 nil，nyaa 的 `<nyaa:seeders>` 不解码 |
| （未提）date 校验 | 六源都不校验 date，`"t"`、`"..."` 原样透传 |

---

## 未收进清单的用例

- **传输层错误**（`*_TransportError`：服务器已关闭 → 拒连）——不是 HTTP 状态，fixture 表达不了。
  参照组自己的 `*_test.go` 覆盖了；规则引擎侧应自行加「拒连 / 超时 → 源失败」的测试。
- **tripwire 的日志断言**（garden `empty_resources`、tosho `empty_array_tripwire`）——golden 测试传
  nil logger，只锁「结果为 `[]` 且无 error」；是否 Warn 由参照组原测试锁。规则引擎侧这是 CQ3 的
  失效检测策略，不是字段映射。
- 纯函数单测（`FormatBytes` / `FormatKb` / `ParseFansub` / `parseInfohash` / `mapRSSItems` / rank / registry）
  ——没有 HTTP 响应体，留在参照组测试里；上面「共享规则」一节把表测里的边界值都写进了文字。

---

## 用声明式规则会难表达的点

按「难度 × 差分时一定会撞上」排序：

1. **两套体积格式化 + JS 式 parseInt**（`FormatBytes` / `FormatKb`、`math.Round` vs `FormatFloat 'f' 1`、
   `"0 KB"` vs `""`）。纯 YAML 映射写不出来；需要引擎内置 `format_size(unit: bytes|kb)`，且实现必须
   逐字节复刻上面的表——golden 里 `"1.2 GB"`、`"500 KB"`、`"0 KB"`、`"40 MB"` 都是这张表的边界样本。
2. **magnet 合成**（nyaa / mikan）：`urn:btih:` + hash + `url.QueryEscape(title)` + 两条固定 tracker。
   需要内置 `magnet_from_hash`，并且 **`dn` 的编码方式要可指定**（Go `QueryEscape` 的 `+`）。
3. **多字段优先级回落**：mikan hash（enclosure@url → link）、mikan date（item pubDate → ns pubDate）、
   acg magnet（enclosure@url → link）、garden fansub（`fansub.name` → 括号解析）。需要 `coalesce` 语义，
   且 dmhy **故意没有** link 回落——规则要能表达「不回落」。
4. **正则提取 + 首个匹配**：mikan 的 `[0-9a-fA-F]{40}`；需要 `regex_first` 转换。
5. **XML 命名空间按 URI 解析**（nyaa、mikan），且要接受 `<torrent xmlns="...">` 默认命名空间形式。
   选择器如果只认 `torrent:contentLength` 前缀，mikan 直接全 miss。
6. **garden `size` 的 RawMessage 语义**：数字 OK、字符串 `""`。JSONPath 引擎通常会把两者都取成值；
   要么内置一个「原始字节再 parseInt」的转换，要么接受这一点与旧适配器不一致并记录。
7. **tosho seeders 三态**：规则输出必须保留「缺失」与「0」的区别，落到 `*int`。YAML 映射 `seeders: $.seeders`
   若把缺失当 0 就错。
8. **tosho `timestamp` unix → RFC3339 UTC**：需要 `unix_to_rfc3339` 转换；`<= 0 → null`。
9. **tosho `infohash` 只 lower+trim 不校验** vs. 聚合层 `parseInfohash` 严格校验（40/64 hex、32 base32）：
   两种规范化并存。规则引擎最好统一到严格版，但那样 golden 里 `"dead"`/`"3333"` 就对不上——
   需要在差分时决定是「按旧行为」还是「记录为有意的差异」。
10. **过滤条件是「前缀 `magnet:` 区分大小写」**，不是「有 btih」；`magnet:?xt=abc` 放行。规则里的
    `starts_with` 必须区分大小写。
11. **`""` → null / 键省略的双轨**：输出结构体 tag 决定，规则本身无需表达，但引擎的输出类型必须
    与 `TorrentItem` 完全一致（含 omitempty 分布）才能逐字节对上 golden。
12. **零结果 tripwire 的 gate**（`TrimSpace(q) != ""`）与 aid 入口无 tripwire——这是失效检测策略（CQ3），
    不是映射；建议规则里用 `expect_nonempty_for_query: true` 一类的声明，而不是硬编码。
