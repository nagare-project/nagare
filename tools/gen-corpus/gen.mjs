// 语料生成器（决议 CQ2）：跑 animego 的【真实 JS 实现】生成 internal/library/testdata/parse.jsonl。
// Go 移植以此文件为唯一行为基准 —— 两边测试吃同一份语料，漂移在 CI 里直接爆红。
//
// 用法：bun tools/gen-corpus/gen.mjs
// 环境：ANIMEGO_DIR 指向 animego 检出（默认 ~/animego）。仅维护者再生语料时需要；
//       生成产物 parse.jsonl 已提交进仓库，日常测试不依赖 animego。
import { readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'

const ANIMEGO = process.env.ANIMEGO_DIR ?? path.join(process.env.HOME, 'animego')
const src = (p) => path.join(ANIMEGO, 'next-app/src', p)

const { parseEpisodeMeta, parseEpisodeNumber, parseAnimeKeyword, isVideoFile } = await import(
  src('app/[lang]/library/_hooks/episodeParser.js')
)
const { normalizeTokens } = await import(src('lib/library/normalize.js'))
const { groupByFolder } = await import(src('lib/library/grouping.js'))
const { clusterize } = await import(src('lib/library/clusterizer.js'))
const { matchSingleCluster } = await import(src('lib/library/seriesMatcher.js'))

const here = path.dirname(new URL(import.meta.url).pathname)
const outPath = path.join(here, '../../internal/library/testdata/parse.jsonl')

// undefined 在 JSON.stringify 里会整键消失，统一压成 null 保证 Go 侧形状稳定。
const nn = (v) => (v === undefined ? null : v)

const lines = []

// ── 1. 单文件名解析（parseEpisodeMeta 全字段）──────────────────────────────
const names = readFileSync(path.join(here, 'filenames.txt'), 'utf8')
  .split('\n')
  .map((s) => s.trim())
  .filter((s) => s && !s.startsWith('#'))

for (const name of names) {
  const m = parseEpisodeMeta(name)
  lines.push(
    JSON.stringify({
      kind: 'meta',
      in: name,
      out: {
        title: nn(m.title),
        number: nn(m.number),
        kind: nn(m.kind),
        group: nn(m.group),
        resolution: nn(m.resolution),
        season: nn(m.season),
        episodeAlt: nn(m.episodeAlt),
      },
    }),
  )
}

// ── 2. 标题归一化（normalizeTokens）────────────────────────────────────────
const titles = readFileSync(path.join(here, 'titles.txt'), 'utf8')
  .split('\n')
  .map((s) => s.trim())
  .filter((s) => s && !s.startsWith('#'))

for (const t of titles) {
  lines.push(JSON.stringify({ kind: 'tokens', in: t, out: normalizeTokens(t) }))
}

// ── 3. 视频扩展名判定 ─────────────────────────────────────────────────────
const videoProbes = [
  'a.mkv', 'b.MP4', 'c.avi', 'd.webm', 'e.rmvb', 'f.ts', 'g.m4v', 'h.MOV',
  'x.ass', 'y.srt', 'z.txt', 'noext', 'trap.mkv.jpg', 'upper.MKV',
]
for (const p of videoProbes) {
  lines.push(JSON.stringify({ kind: 'video', in: p, out: isVideoFile(p) }))
}

// ── 4. 端到端管线（items → groupByFolder → clusterize → verdict）──────────
// 逐字复刻 useVideoFiles.processFiles 的 item 构造：
//   episode = parseEpisodeNumber(fileName)
//   parsedTitle = (路径多段时 parseAnimeKeyword(首段)) || meta.title   ← 目录名优先
//   软 id = name|size|mtime（生成器里 size/mtime 取确定值）
function buildItems(relPaths) {
  const files = relPaths.map((rel, i) => ({
    name: rel.split('/').filter(Boolean).pop(),
    rel,
    size: 1000 + i,
    mtime: 1700000000000 + i,
  }))
  const parsed = files
    .filter((f) => isVideoFile(f.name))
    .map((f) => {
      const episode = parseEpisodeNumber(f.name)
      const meta = parseEpisodeMeta(f.name)
      const segments = f.rel.split('/').filter(Boolean)
      const folderTitle = segments.length > 1 ? parseAnimeKeyword(segments[0]) : null
      return {
        fileId: `${f.name}|${f.size}|${f.mtime}`,
        fileName: f.name,
        relativePath: f.rel,
        episode,
        subtitle: null,
        parsedTitle: folderTitle || meta.title,
        parsedNumber: meta.number,
        parsedKind: meta.kind,
        parsedGroup: meta.group,
        parsedResolution: meta.resolution,
        parsedSeason: meta.season,
        parsedEpisodeAlt: meta.episodeAlt,
      }
    })
  parsed.sort((a, b) => (a.episode ?? 999) - (b.episode ?? 999))
  return parsed
}

const pipelines = JSON.parse(readFileSync(path.join(here, 'pipelines.json'), 'utf8'))
for (const pc of pipelines) {
  const items = buildItems(pc.files)
  const groups = groupByFolder(items)
  const clusters = clusterize(groups)
  const out = {
    items: items.map((i) => ({
      fileId: i.fileId,
      episode: nn(i.episode),
      parsedTitle: nn(i.parsedTitle),
      parsedSeason: nn(i.parsedSeason),
      parsedKind: nn(i.parsedKind),
    })),
    groups: groups.map((g) => ({
      groupKey: g.groupKey,
      label: g.label,
      sortMode: g.sortMode,
      hasAmbiguity: g.hasAmbiguity,
      items: g.items.map((i) => i.fileId),
    })),
    clusters: clusters.map((c) => {
      const verdict = matchSingleCluster(c, { priorSeasons: [], libraryId: 'corpus', ulidSeed: 42 })
      return {
        clusterKey: c.clusterKey,
        tokens: c.normalizedTokens,
        representative: c.representative ? c.representative.fileId : null,
        items: c.items.map((i) => i.fileId),
        verdictKind: verdict.kind,
        confidence: nn(verdict.confidence),
      }
    }),
  }
  lines.push(JSON.stringify({ kind: 'pipeline', name: pc.name, in: { files: pc.files }, out }))
}

writeFileSync(outPath, lines.join('\n') + '\n')
console.log(`已生成 ${lines.length} 条语料 → ${outPath}`)
console.log(`  meta=${names.length} tokens=${titles.length} video=${videoProbes.length} pipeline=${pipelines.length}`)
