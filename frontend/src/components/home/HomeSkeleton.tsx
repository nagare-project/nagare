import styles from './HomeSkeleton.module.css'

/**
 * 发现页读取中的骨架（DESIGN.md「状态表 · 发现」）：横幅一块、下面三段，几何与真页面一样
 * （同样的横幅高度、同样的留白、中性的页面底色），换成真页面时不跳。与真横幅一样从页面最顶上开始，
 * 压在透明顶栏下面。纯 CSS 的慢闪；减弱动态效果时定住。
 */
export function HomeSkeleton() {
  return (
    <main className={styles.skeleton} aria-busy="true">
      <h1 className="visually-hidden">发现</h1>
      <span className="visually-hidden" role="status">正在读取发现页</span>
      <div className={styles.hero} aria-hidden="true">
        <div className={`${styles.pulse} ${styles.eyebrow}`} />
        <div className={styles.stage}>
          <div className={`${styles.pulse} ${styles.card} ${styles.cover}`} />
          <div className={styles.lines}>
            <div className={`${styles.pulse} ${styles.titleLine}`} />
            <div className={`${styles.pulse} ${styles.metaLine}`} />
            <div className={`${styles.pulse} ${styles.textLine}`} />
            <div className={`${styles.pulse} ${styles.textLine} ${styles.short}`} />
            <div className={`${styles.pulse} ${styles.button}`} />
          </div>
        </div>
      </div>
      {[0, 1, 2].map((section) => (
        <div key={section} className={styles.section} aria-hidden="true">
          <div className={`${styles.pulse} ${styles.heading}`} />
          <div className={styles.grid}>
            {[0, 1, 2, 3, 4, 5].map((card) => (
              <div key={card} className={`${styles.pulse} ${styles.card} ${styles.poster}`} />
            ))}
          </div>
        </div>
      ))}
    </main>
  )
}
