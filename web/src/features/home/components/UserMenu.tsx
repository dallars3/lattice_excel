import { useState } from 'react'

import { themeStore } from '@/stores'

import styles from './UserMenu.module.css'

export interface UserMenuProps {
  /** 展示名优先，缺失时由调用方回退用户名。 */
  displayName: string
  onLogout: () => void | Promise<void>
}

/** 顶栏用户信息菜单（票 07）：展示当前身份，外观切换与退出登录归入菜单内。 */
export function UserMenu({ displayName, onLogout }: UserMenuProps) {
  const [open, setOpen] = useState(false)
  const themeMode = themeStore((state) => state.mode)
  const name = displayName.trim() === '' ? '用户' : displayName.trim()

  return (
    <div className={styles.wrap}>
      <button
        type="button"
        className={styles.trigger}
        aria-label={`用户菜单：${name}`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <span className={styles.name}>{name}</span>
        <span className={styles.caret} aria-hidden="true">
          ▾
        </span>
      </button>

      {open ? (
        <div className={styles.menu} role="menu" aria-label="用户菜单">
          <button
            type="button"
            role="menuitem"
            className={styles.menuItem}
            onClick={() => themeStore.getState().toggle()}
          >
            {themeMode === 'dark' ? '切换浅色外观' : '切换深色外观'}
          </button>
          <button
            type="button"
            role="menuitem"
            className={styles.menuItem}
            onClick={() => {
              setOpen(false)
              void onLogout()
            }}
          >
            退出登录
          </button>
        </div>
      ) : null}
    </div>
  )
}
