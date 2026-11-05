// query key 工厂统一失效口径：认证/记录/首页/Base 域 key（技术方案第 7 章）。
export { authKeys, type AuthMeQueryKey } from './auth'
export {
  homeKeys,
  type HomeAggregateQueryKey,
  type HomeSearchQueryKey,
} from './home'
export { baseKeys, type BaseDetailQueryKey } from './base'
export { tableKeys, type TableListQueryKey } from './table'
export { viewKeys, type ViewListQueryKey } from './view'
export { recordsKeys, type RecordsQueryKey } from './records'
