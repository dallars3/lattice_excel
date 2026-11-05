import { useQueryClient } from '@tanstack/react-query'

import { authKeys } from '@/query-keys'
import { authStore } from '@/stores'

import { LoginForm } from './LoginForm'
import styles from './ReloginModal.module.css'

/** 2001 统一重登弹窗：重登成功关闭弹窗并失效 me 查询，页面保持在原路径。 */
export function ReloginModal() {
  const open = authStore((state) => state.reloginOpen)
  const closeRelogin = authStore((state) => state.closeRelogin)
  const queryClient = useQueryClient()

  if (!open) return null

  return (
    <div className={styles.mask}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="relogin-title"
        className={styles.dialog}
      >
        <h2 id="relogin-title" className={styles.title}>
          登录状态已失效
        </h2>
        <p className={styles.hint}>请重新登录后继续操作，登录成功后将留在当前页面。</p>
        <LoginForm
          submitLabel="重新登录"
          onAuthenticated={() => {
            closeRelogin()
            void queryClient.invalidateQueries({ queryKey: authKeys.me() })
          }}
        />
      </div>
    </div>
  )
}
