import { acquireToken } from './token'

/** 鉴权用的自定义请求头。后端对所有非 GET 请求强制要求它（CSRF 防线之一） */
export const TOKEN_HEADER = 'X-Nagare-Token'

/**
 * 后端统一响应信封。所有 /api/* 响应都长这样：
 * `{ success: boolean; data: T | null; error?: string }`
 */
export interface ApiEnvelope<T> {
  success: boolean
  data: T | null
  error?: string
}

/**
 * 鉴权失败（HTTP 401）。
 * 单独立一类，让调用方能识别并提示用户走启动链接重新进入。
 */
export class ApiAuthError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ApiAuthError'
  }
}

/** 其余 API 失败：网络不通、响应不是信封、信封 success=false 等 */
export class ApiError extends Error {
  /** HTTP 状态码；请求根本没到达后端（网络层失败）时为 null */
  readonly status: number | null

  constructor(message: string, status: number | null, options?: ErrorOptions) {
    super(message, options)
    this.name = 'ApiError'
    this.status = status
  }
}

/**
 * 统一的 API 请求入口。
 *
 * - 自动附带 `X-Nagare-Token` 头（GET 也带，后端对非 GET 强制校验）；
 * - 解析统一信封，成功时直接返回 data；
 * - 401 → 抛 ApiAuthError；其余失败 → 抛 ApiError（保留原始错误作 cause）。
 */
export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const token = acquireToken()
  const headers = new Headers(init?.headers)
  if (token !== null) {
    headers.set(TOKEN_HEADER, token)
  }

  let response: Response
  try {
    response = await fetch(path, { ...init, headers })
  } catch (cause) {
    throw new ApiError('无法连接到 nagare 后端，请确认本地服务已启动', null, { cause })
  }

  // 401 的判定独立于响应体解析：就算 401 带着空/坏 body（比如未来某个
  // 错误路径漏走了统一信封），也必须进 ApiAuthError 分支给出可行动的提示，
  // 而不是退化成一句"响应不是合法 JSON"。
  if (response.status === 401) {
    const envelope = await parseEnvelope<T>(response)
    throw new ApiAuthError(envelope?.error ?? '缺少有效 token')
  }

  const envelope = await parseEnvelope<T>(response)
  if (envelope === null) {
    throw new ApiError(`后端响应不是合法的信封 JSON（HTTP ${response.status}）`, response.status)
  }
  if (!response.ok || !envelope.success) {
    throw new ApiError(envelope.error ?? `请求失败（HTTP ${response.status}）`, response.status)
  }
  // success=true 却没有 data 同样是违约。在唯一入口拦下来，
  // 别让 null 顶着 T 的类型漂进业务代码后在别处炸成空指针。
  if (envelope.data === null) {
    throw new ApiError('后端响应缺少 data 载荷', response.status)
  }
  return envelope.data
}

/**
 * 把响应体解析成统一信封；不是合法 JSON 或形状不对时返回 null。
 * 网络数据一律先按 unknown 收窄再定型，不做裸 cast。
 * data 的内层形状无法在这里逐字段校验（T 由调用方决定）——这是刻意保留的
 * 单一信任点，将来端点多了再按端点上 schema 校验。
 */
async function parseEnvelope<T>(response: Response): Promise<ApiEnvelope<T> | null> {
  let parsed: unknown
  try {
    parsed = await response.json()
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null
  const candidate = parsed as Partial<ApiEnvelope<unknown>>
  if (typeof candidate.success !== 'boolean') return null
  return {
    success: candidate.success,
    data: (candidate.data ?? null) as T | null,
    error: typeof candidate.error === 'string' ? candidate.error : undefined,
  }
}

/** GET /api/health 的 data 载荷 */
export interface HealthData {
  status: string
  version: string
}

/** 健康检查：验证「前端 → 鉴权中间件 → 后端」整条链路是否打通 */
export function fetchHealth(): Promise<HealthData> {
  return apiFetch<HealthData>('/api/health')
}
