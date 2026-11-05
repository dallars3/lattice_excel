// httpClient 层门面（票 04）：统一信封 {code,message,data}、ApiError、error-catalog 13 码、
// client_request_id 幂等、游标分页 helper。业务侧统一从此入口消费。
export {
  ApiError,
  buildUrl,
  http,
  isApiError,
  setApiErrorObserver,
  setAuthTokenGetter,
  type ApiErrorObserver,
  type Envelope,
  type HttpMethod,
  type QueryParams,
  type RequestOptions,
} from './client'
export {
  ALL_ERROR_CODES,
  ERROR_CATALOG,
  lookupErrorCatalog,
  type CatalogEntry,
  type ErrorCode,
  type ErrorLayer,
  type RecoveryStrategy,
} from './error-catalog'
export {
  IdempotencyCoordinator,
  createRequestId,
  defaultIdempotency,
  resetIdempotency,
} from './idempotency'
export {
  DEFAULT_CURSOR_LIMIT,
  MAX_CURSOR_LIMIT,
  clampCursorLimit,
  extractCursorPage,
  getNextPageParam,
  type CursorEnvelopeBase,
  type CursorPage,
} from './cursor'
