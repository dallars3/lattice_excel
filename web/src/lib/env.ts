// 环境变量（票 01）：三个 VITE_ 前缀变量，全部带默认值，不建 .env 文件。
// 事实源：《前端-技术方案-v0.1.md》第 16 章（VITE_API_BASE 默认同源空、VITE_WS_URL
// 默认同源 ws:// 推算、VITE_NOTICE_TEXT 试运行声明文案）。
export interface AppEnv {
  readonly API_BASE: string
  readonly WS_URL: string
  readonly NOTICE_TEXT: string
}

/** import.meta.env 中实际暴露的三个业务变量。 */
export interface EnvRawVars {
  readonly VITE_API_BASE?: string
  readonly VITE_WS_URL?: string
  readonly VITE_NOTICE_TEXT?: string
}

/** 推算 WS 同源地址所需的最小 location 形状（便于纯函数单测）。 */
export interface LocationLike {
  readonly protocol: string
  readonly host: string
}

/** 试运行免责声明默认文案：引自《前端需求文档-v0.1.md》3.1 首页固定展示文案。 */
export const DEFAULT_NOTICE_TEXT = '个人试运行产品，不保证服务持续与数据留存，重要数据请自行导出备份'

function sameOriginWsUrl(location: LocationLike): string {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${location.host}`
}

/** 由原始变量与页面 location 解析出运行时环境；纯函数，供表驱动单测。 */
export function resolveEnv(raw: EnvRawVars, location: LocationLike | undefined): AppEnv {
  return {
    API_BASE: raw.VITE_API_BASE ?? '',
    WS_URL: raw.VITE_WS_URL ?? (location === undefined ? '' : sameOriginWsUrl(location)),
    NOTICE_TEXT: raw.VITE_NOTICE_TEXT ?? DEFAULT_NOTICE_TEXT,
  }
}

const appEnv = resolveEnv(
  import.meta.env,
  typeof window === 'undefined' ? undefined : window.location,
)

/** API 基地址：默认空串（同源相对路径）。 */
export const API_BASE: string = appEnv.API_BASE

/** WS 地址：默认按页面协议推算同源 ws:// 或 wss://。 */
export const WS_URL: string = appEnv.WS_URL

/** 试运行声明文案：登录/注册/首页三处挂载（票 02 接入 HealthNotice）。 */
export const NOTICE_TEXT: string = appEnv.NOTICE_TEXT
