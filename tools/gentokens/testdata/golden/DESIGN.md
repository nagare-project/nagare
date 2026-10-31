# 样本文档

标记之外的文字原样保留。

<!-- gentokens:begin colors -->

| Token | 值 | iOS | 用途 |
|---|---|---|---|
| `--bg` | `#000000` | `systemBackground` | 页面底色 |
| `--text-secondary` | `rgba(235, 235, 245, 0.6)` | `secondaryLabel` | 次要文字 |
| `--accent` | `#0a84ff` | `systemBlue` | 可点击（在 bg 上 5.76:1，写进 use 的对比度要与实算一致） |
| `--text-tertiary` | `rgba(235, 235, 245, 0.3)` | `tertiaryLabel` | 占位符 |
| `--glass` | `rgba(9, 9, 11, 0.78)` | 不输出 | 只给网页的毛玻璃 |
| `--teal` | `#5ac8fa` | 固定值 | 只读信息，固定值 |

<!-- gentokens:end colors -->

<!-- gentokens:begin poster-tones -->

| Token | OKLCH（h = 封面色相） | 没有色相时 | 写字 | 用途 |
|---|---|---|---|---|
| `--poster-tone` | `oklch(76% 0.085 h)` | `#b1b1b1` | 能 | 强调文字 |
| `--poster-tone-low` | `oklch(24% 0.045 h)` | `#1f1f1f` | 否 | 填充 |

<!-- gentokens:end poster-tones -->

<!-- gentokens:begin contrast -->

| 前景 | 要求 | 实测（底色 对比度） | 场景 |
|---|---|---|---|
| `text-secondary` | 文字 ≥ 4.5:1 | `bg` 6.36:1 ✓ | 说明 |
| `accent` | 文字 ≥ 4.5:1 | `bg` 5.76:1 ✓ | 链接 |
| `accent` | 界面元素 ≥ 3:1 | `bg` 5.76:1 ✓ | 进度条 |
| `text-tertiary` | 不写要读的字 | `bg` 2.25:1 | 只用于占位符 |

<!-- gentokens:end contrast -->

<!-- gentokens:begin fonts -->

| Token | 字体栈 | 用途 |
|---|---|---|
| `--font-cjk` | `"PingFang SC", system-ui, sans-serif` | 中文回落链 |
| `--font-latin` | `Sora, var(--font-cjk)` | 拉丁显示字体 |

<!-- gentokens:end fonts -->

<!-- gentokens:begin type -->

| 样式 | 字号 | 字重 | 字距 | 字体 | iOS | 用途 |
|---|---|---|---|---|---|---|
| `body` | 16px | 400 | 0 | 主字体 | `.body` | 正文 |
| `wordmark` | 20px | 700 | -0.01em | `--font-latin` | Sora（固定字号） | 字标 |
| `web-only` | 13px | 600 | 0.1em | 主字体 | — | 不进 Swift，所以不用填 iOS 样式 |

<!-- gentokens:end type -->

<!-- gentokens:begin spacing -->

| Token | 值 | 输出到 | 用途 |
|---|---|---|---|
| `--sp-sm` | 8px | 全部 | 基础单位 |
| `--sp-xxl` | 48px | css | 只给网页 |

<!-- gentokens:end spacing -->

<!-- gentokens:begin radii -->

| Token | 值 | 输出到 | 用途 |
|---|---|---|---|
| `--radius-md` | 12px | 全部 | 卡片 |

<!-- gentokens:end radii -->

<!-- gentokens:begin motion -->

| Token | 值 | 输出到 | 用途 |
|---|---|---|---|
| `--dur-toast-in` | 260ms | 全部 | 提示进场 |

| Token | 值 | 输出到 | 用途 |
|---|---|---|---|
| `--ease-standard` | `cubic-bezier(0.4, 0, 0.2, 1)` | css / swift | 默认缓动 |

<!-- gentokens:end motion -->

<!-- gentokens:begin elevation -->

| Token | 值 | 用途 |
|---|---|---|
| `--shadow-focus` | `0 0 0 3px rgba(10, 132, 255, 0.4)` | 焦点 |

<!-- gentokens:end elevation -->

<!-- gentokens:begin layout -->

| Token | 值 | 输出到 | 用途 |
|---|---|---|---|
| `--nav-h` | 64px | css | 顶栏高度 |
| `--touch-min` | 44px | 全部 | 触控目标 |
| `--bp-sm` | 600px | 只在文档 | 只进文档的断点 |

<!-- gentokens:end layout -->

结尾的文字也原样保留。
