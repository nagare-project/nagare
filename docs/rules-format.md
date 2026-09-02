# 磁力源规则格式（schema 1）

nagare 本体**不内置任何源**。磁力搜索的每一个源都是一个 YAML 规则文件，由用户自己提供：
在设置页填一个规则仓库的 HTTPS 地址（或本机目录），nagare 把规则拉到本地、校验后加载。
规则只能做两件事：**按模板发一个 GET 请求**、**按路径把响应解成字段**——没有脚本、没有沙箱、
没有任意代码执行（决议 A3）。

## 文件结构

```yaml
schema: 1                      # 格式版本，目前只有 1
id: example                    # 小写字母/数字/_/-，1–32 位；也是结果里的 source 字段
name: Example Source           # 展示名
homepage: https://example.org  # 展示用链接
request:
  url: "https://example.org/rss?q={{query}}"   # 必须含 {{query}}；关键词会被 URL 编码后填入
  headers:                      # 可选，附加请求头（User-Agent 由 nagare 统一设置）
    Accept: application/rss+xml
  timeout_seconds: 8            # 可选，默认 8
format: xml                     # xml | json
namespaces:                     # 仅 xml；路径里 prefix:name 的 prefix 在此声明
  nyaa: https://nyaa.si/xmlns/nyaa
items: rss/channel/item         # 条目集合的路径（xml：从根元素起；json：点路径，$ 表示根）
fields:                         # 输出字段 → 取值表达式（title 与 magnet 必填）
  title: title
  magnet: "enclosure@url"
  size: { path: "enclosure@length", transforms: [format_bytes] }
  date: pubDate
  fansub: { path: "$title", transforms: [parse_fansub] }
capabilities:                   # 可选，影响聚合排序
  seeders: false                # 该源是否提供做种数
  priority: 0                   # 同一 infohash 多源命中时，priority 高者胜
selftest:                       # 可选但强烈建议：零结果时用它探测规则是否失效
  query: "some well-known title"
```

## 输出字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `title` | 字符串 | 必填；为空的条目整条丢弃 |
| `magnet` | 字符串 | 必填；不以 `magnet:` 开头的条目整条丢弃 |
| `size` | 字符串 | 人类可读体积，如 `1.5 GB`；可为空 |
| `date` | 字符串 | 原样透传，nagare 用 RFC1123/RFC3339 尝试解析用于排序 |
| `fansub` | 字符串 | 字幕组；空 → null |
| `provider` | 字符串 | 上游子来源；空 → 省略 |
| `seeders` | 整数 | 做种数；缺失/null → 未知（与 0 不同） |
| `infohash` | 字符串 | 可选；聚合去重时会从 magnet 重新解析并覆盖 |

## 取值表达式

三种写法：

```yaml
title: title                                   # 简写：一个路径
size: { path: "enclosure@length", transforms: [format_bytes] }
magnet:                                        # any：取第一个非空候选，再施加外层 transforms
  any:
    - { path: "enclosure@url", transforms: [{ regex: "[0-9a-fA-F]{40}" }] }
    - { path: link,            transforms: [{ regex: "[0-9a-fA-F]{40}" }] }
  transforms:
    - magnet: { dn: $title, trackers: [http://tracker.example/announce] }
```

- `path` 以 `$` 开头表示引用**已算出的字段**（求值顺序固定：title → infohash → magnet → size → date → fansub → provider → seeders）。
- XML 路径：`a/b/c` 逐层匹配子元素；最后一段可用 `@attr` 取属性；`prefix:name` 匹配命名空间元素
  （默认命名空间形式 `<x xmlns="…">` 同样按 URI 匹配）。无前缀的段只匹配**无命名空间**的元素。
- JSON 路径：`a.b.c` 逐层进对象；`$` 是根。数字取字面量（`3460300`），null → 空。

## 转换词汇表

| 名称 | 作用 |
|---|---|
| `trim` / `lower` / `upper` | 去首尾空白 / 小写 / 大写 |
| `format_bytes` | 字节数 → `X.X GB` / `X MB` / `X KB`；0、负数、非数字 → 空 |
| `format_kb` | 同上，但输入单位是 KB |
| `parse_fansub` | 取标题开头 `[…]` 或 `【…】` 里的字幕组名 |
| `regex: "pattern"` | 取第一个匹配（RE2 语法，无回溯，不会 ReDoS） |
| `unix_rfc3339` | unix 秒 → RFC3339（UTC）；≤0 → 空 |
| `magnet: { dn: $title, trackers: [...] }` | 由 infohash 拼装 magnet；hash 为空 → 空（条目随后被丢弃） |

不认识的转换名、拼错的键、未声明的命名空间前缀、非法正则都会让规则**拒绝加载**并在设置页显示原因。

## 健康状态（决议 CQ3）

每次搜索每个源都会得到一个结论，界面会区分显示：

| 状态 | 含义 |
|---|---|
| `ok` | 解出 N 条 |
| `zero` | 上游正常返回，确实没有匹配 |
| `dead` | 上游有条目但规则**一条都解不出**——规则失效，界面显示「源异常」而不是「无结果」 |
| `failed` | 网络 / HTTP 非 2xx / 响应无法解析 |
| `disabled` | 用户已禁用 |

规则声明了 `selftest.query` 时，用户可在设置页一键自检。

## 规则仓库布局

```
<仓库根>/index.json         {"schema":1,"rules":[{"file":"foo.yaml","sha256":"<可选>"}]}
<仓库根>/foo.yaml
```

文件名只允许 `[a-z0-9][a-z0-9_-]*.yaml`。同步时逐个下载、校验、`rules.Parse` 通过才落盘；
清单里不再存在的文件会被删除。生成 `index.json` 的最简方式：

```bash
for f in *.yaml; do printf '{"file":"%s","sha256":"%s"}\n' "$f" "$(shasum -a 256 "$f" | cut -d' ' -f1)"; done \
  | jq -s '{schema:1, rules:.}' > index.json
```
