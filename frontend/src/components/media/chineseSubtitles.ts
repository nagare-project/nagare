import type { SearchItem, SourceCandidate } from '../../lib/endpoints'

/**
 * 资源带不带中文字幕：在线候选与磁力发布都按它把中文字幕的排在前面。
 *
 * - 2：确定有中文字幕（字幕语言是 zh-Hans / zh-Hant 一类，或标题、字幕组名里写着简繁、CHS/CHT 等）；
 * - 1：说不准（大多数在线站点不报字幕语言，标题里也没写）；
 * - 0：来源报了字幕语言，里面却没有中文。
 *
 * 只有来源明说了语言才会判成 0：标题里没写「简繁」不等于没有中文字幕。
 */
export type ChineseSubtitleScore = 0 | 1 | 2

const CHINESE_LANGUAGE = /^(?:zh|chi|zho|chs|cht)(?:[-_]|$)|[简繁簡中]/i
// 「简繁内封」「简日双语」「[繁中]」「[CHT]」「繁體內嵌」「北宇治字幕组」……单个「繁」字不算（日文标题里也有）
const CHINESE_MARK = /简繁|簡繁|[简繁簡][体體日中英字]|中文|中字|双语|雙語|字幕[组組社]|\b(?:CHS|CHT|BIG5|GB)\b/i

function chineseSubtitleScore(languages: readonly string[] | undefined, texts: readonly (string | null | undefined)[]): ChineseSubtitleScore {
  if (languages !== undefined && languages.length > 0) {
    return languages.some(language => CHINESE_LANGUAGE.test(language.trim())) ? 2 : 0
  }
  return texts.some(text => typeof text === 'string' && CHINESE_MARK.test(text)) ? 2 : 1
}

export function candidateChineseScore(candidate: SourceCandidate): ChineseSubtitleScore {
  return chineseSubtitleScore(candidate.metadata.subtitleLanguages, [candidate.metadata.title, candidate.metadata.fansub])
}

export function releaseChineseScore(item: SearchItem): ChineseSubtitleScore {
  return chineseSubtitleScore(undefined, [item.title, item.group, item.fansub])
}
