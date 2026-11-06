/** 通知域 query key（技术方案第 7 章）：顶栏未读计数为当前唯一消费点。 */
export const notificationKeys = {
  all: ['notifications'] as const,
  /** H-74 未读计数。 */
  unreadCount: () => ['notifications', 'unread-count'] as const,
}

export type NotificationUnreadCountQueryKey = ReturnType<
  typeof notificationKeys.unreadCount
>
