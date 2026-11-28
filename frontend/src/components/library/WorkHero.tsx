import { Fragment, useState } from 'react'
import type { ReactNode } from 'react'
import './work-hero.css'

type Props = {
  /** 7:10 海报；缺省时画占位（showPoster 为 false 时整块不画：没认出的分组没有海报） */
  cover?: string
  showPoster?: boolean
  /** hero 的背景照片：作品横幅，没有就用海报（模糊铺开） */
  backdrop?: string
  backdropIsCover?: boolean
  /** 有海报色时给海报加一圈海报色光晕（颜色到了才加） */
  toned?: boolean
  title: string
  titleId?: string
  /** 罗马音标题（大写拉丁，--font-latin） */
  romaji?: string
  /** 眉行里紧跟罗马音的补充（如放送中倒计时胶囊） */
  eyebrow?: ReactNode
  /** 日文原名（--font-jp） */
  native?: string
  /** 元信息行开头的补充（如评分芯片） */
  metaPrefix?: ReactNode
  /** 元信息：「TV · 2026 秋 · 12 集」一类，逐项用圆点隔开 */
  meta: (string | null | undefined | false)[]
  /** 标题上方的返回链接 */
  back?: ReactNode
  /** 元信息下面、动作行上面的补充（离线/过期说明等） */
  children?: ReactNode
  /** 动作行：一个海报色实心主按钮 + 玻璃按钮（DESIGN.md「作品页 · 动作行三档权重」） */
  actions: ReactNode
}

/**
 * 作品页 hero（DESIGN.md「作品页」）：认出的作品（/entry）与本地分组（/anime/$clusterKey）共用。
 * 背景由封面氛围光、右偏双遮罩横幅与护读暗纱叠成；文字带宽而软的 text-shadow，按钮坐在玻璃面上。
 * 手机（≤600px）不堆叠：海报和文字并排，动作行横跨两列。
 */
export function WorkHero({
  cover,
  showPoster = true,
  backdrop,
  backdropIsCover = false,
  toned = false,
  title,
  titleId,
  romaji,
  eyebrow,
  native,
  metaPrefix,
  meta,
  back,
  children,
  actions,
}: Props) {
  // 记下的是哪个地址加载失败：换了作品之后封面换了地址，旧的失败不该把新图也藏起来
  const [failed, setFailed] = useState<string[]>([])
  const ok = (src?: string): src is string => src !== undefined && src !== '' && !failed.includes(src)
  const fail = (src: string) => setFailed((list) => (list.includes(src) ? list : [...list, src]))
  const metaItems = meta.filter((item): item is string => typeof item === 'string' && item !== '')
  const glowSrc = ok(cover) ? cover : ok(backdrop) ? backdrop : undefined
  const hasBackdrop = ok(backdrop) || glowSrc !== undefined

  return (
    <header className={showPoster ? 'work-hero' : 'work-hero work-hero--no-poster'}>
      {hasBackdrop && (
        <div className="work-hero-bg" aria-hidden="true">
          {glowSrc && (
            <div className="work-hero-glow">
              <img src={glowSrc} alt="" onError={() => fail(glowSrc)} />
            </div>
          )}
          {ok(backdrop) && (
            <div className={backdropIsCover ? 'work-hero-banner work-hero-banner--cover' : 'work-hero-banner'}>
              <img src={backdrop} alt="" onError={() => fail(backdrop)} />
            </div>
          )}
          <div className="work-hero-shade-top" />
          <div className="work-hero-shade-left" />
        </div>
      )}
      <div className="work-hero-inner">
        {showPoster && (
          <div className={toned ? 'work-hero-poster work-hero-poster--toned' : 'work-hero-poster'}>
            {ok(cover) ? <img src={cover} alt="" onError={() => fail(cover)} /> : <span className="work-hero-blank" aria-hidden="true">流</span>}
          </div>
        )}
        <div className="work-hero-text">
          {back}
          {(romaji || eyebrow) && (
            <div className="work-hero-eyebrow">
              {romaji && <p className="work-hero-romaji" lang="ja-Latn">{romaji}</p>}
              {eyebrow}
            </div>
          )}
          <h1 id={titleId} className="work-hero-title">{title}</h1>
          {native && native !== title && <p className="work-hero-native" lang="ja">{native}</p>}
          {(metaPrefix || metaItems.length > 0) && (
            <div className="work-hero-meta">
              {metaPrefix}
              {metaItems.map((item, i) => (
                <Fragment key={`${i}-${item}`}>
                  {(i > 0 || Boolean(metaPrefix)) && <span className="work-hero-meta-sep" aria-hidden="true" />}
                  <span>{item}</span>
                </Fragment>
              ))}
            </div>
          )}
          {children}
        </div>
        <div className="work-hero-actions">{actions}</div>
      </div>
    </header>
  )
}
