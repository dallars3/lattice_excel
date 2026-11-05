import styles from '../BaseWorkspacePage.module.css'

/** 网格区骨架（票 03）：无字段/记录票，保留组件边界与空态；票 04~06 在此扩展渲染。 */
export function GridArea() {
  return (
    <section aria-label="网格" className={styles.gridArea}>
      <div className={styles.gridSkeleton}>
        <p className={styles.gridSkeletonTitle}>表格视图</p>
        <p className={styles.gridSkeletonHint}>字段与记录将在后续版本接入</p>
      </div>
    </section>
  )
}
