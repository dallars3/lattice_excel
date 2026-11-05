/** 认证域 query key（技术方案第 7 章）：me 为会话事实源，登出/重登后整体失效。 */
export const authKeys = {
  all: ['auth'] as const,
  me: () => ['auth', 'me'] as const,
}

export type AuthMeQueryKey = ReturnType<typeof authKeys.me>
