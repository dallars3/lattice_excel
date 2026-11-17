import { Link, useSearchParams } from 'react-router-dom'

import { HealthNotice } from '@/features/health/HealthNotice'

import styles from './ErrorPage.module.css'

export const SHARE_LINK_UNAVAILABLE_KIND = 'share-link-unavailable'

const COPY = {
  'share-link-unavailable': {
    title: '分享链接不可用',
    description: '该分享链接已失效或被关闭，请联系分享者重新获取链接。',
  },
} as const

type ErrorKind = keyof typeof COPY

function isKnownKind(value: string | null): value is ErrorKind {
  return value !== null && value in COPY
}

/** /err 失效落点（H-95）：展示明确的失效说明，不回退首页。 */
export function ErrorPage() {
  const [search] = useSearchParams()
  const kind = search.get('kind')
  const copy = isKnownKind(kind)
    ? COPY[kind]
    : { title: '页面不可用', description: '请检查链接是否正确，或返回首页继续操作。' }

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>{copy.title}</h1>
      <HealthNotice />
      <p className={styles.description}>{copy.description}</p>
      <p className={styles.links}>
        <Link to="/home">返回首页</Link>
        <span aria-hidden="true"> · </span>
        <Link to="/login">去登录</Link>
      </p>
    </main>
  )
}
