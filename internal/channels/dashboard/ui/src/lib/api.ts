/**
 * API client with bearer token authentication for Openclawssy Dashboard
 */

const STORAGE_KEY = 'openclawssy.dashboard.bearer'

/**
 * Structured API error with status code and error details
 */
export class ApiError extends Error {
  status: number
  code: string
  details: unknown
  url: string

  constructor({
    message,
    status,
    code,
    details,
    url,
  }: {
    message: string
    status: number
    code: string
    details: unknown
    url: string
  }) {
    super(message || 'API request failed')
    this.name = 'ApiError'
    this.status = status || 0
    this.code = code || 'api.error'
    this.details = details
    this.url = url || ''
  }
}

/**
 * Resolve bearer token from URL query params, localStorage, or prompt user
 */
export function resolveBearerToken(options?: {
  query?: string
  queryKeys?: string[]
  storage?: Storage
  storageKey?: string
  promptFn?: (message: string) => string | null
}): string {
  const {
    query = window.location.search,
    queryKeys = ['token', 'bearer', 'bearer_token'],
    storage = window.localStorage,
    storageKey = STORAGE_KEY,
    promptFn = window.prompt.bind(window),
  } = options || {}

  // Check URL query params
  const params = new URLSearchParams(query)
  for (const key of queryKeys) {
    const value = (params.get(key) || '').trim()
    if (value) {
      storage.setItem(storageKey, value)
      return value
    }
  }

  // Check localStorage
  const stored = (storage.getItem(storageKey) || '').trim()
  if (stored) {
    return stored
  }

  // Prompt user
  const prompted = (promptFn('Enter dashboard bearer token') || '').trim()
  if (prompted) {
    storage.setItem(storageKey, prompted)
  }
  return prompted
}

/**
 * Clear the stored bearer token
 */
export function clearBearerToken(storage: Storage = window.localStorage): void {
  storage.removeItem(STORAGE_KEY)
}

/**
 * Set bearer token in localStorage
 */
export function setBearerToken(token: string, storage: Storage = window.localStorage): void {
  storage.setItem(STORAGE_KEY, token)
}

/**
 * Get bearer token from localStorage without prompting
 */
export function getBearerToken(storage: Storage = window.localStorage): string | null {
  return storage.getItem(STORAGE_KEY)
}

function parseBody(text: string, contentType: string | null): unknown {
  if (!text) {
    return null
  }
  if ((contentType || '').includes('application/json')) {
    try {
      return JSON.parse(text)
    } catch {
      return { message: text }
    }
  }
  return text
}

/**
 * Request options for API calls
 */
export interface RequestOptions {
  skipAuth?: boolean
  headers?: Record<string, string>
  body?: unknown
}

/**
 * API client instance type
 */
export interface ApiClient {
  request: <T = unknown>(path: string, requestOptions?: RequestOptions & { method?: string }) => Promise<T>
  get: <T = unknown>(path: string, options?: RequestOptions) => Promise<T>
  post: <T = unknown>(path: string, body?: unknown, options?: RequestOptions) => Promise<T>
  put: <T = unknown>(path: string, body?: unknown, options?: RequestOptions) => Promise<T>
  patch: <T = unknown>(path: string, body?: unknown, options?: RequestOptions) => Promise<T>
  delete: <T = unknown>(path: string, options?: RequestOptions) => Promise<T>
  resolveBearerToken: () => string
}

/**
 * Create an API client with the given options
 */
export function createApiClient(options?: {
  baseUrl?: string
  fetchImpl?: typeof fetch
  tokenResolver?: typeof resolveBearerToken
}): ApiClient {
  const {
    baseUrl = '',
    fetchImpl = window.fetch.bind(window),
    tokenResolver = resolveBearerToken,
  } = options || {}

  async function request<T = unknown>(
    path: string,
    requestOptions: RequestOptions & { method?: string } = {}
  ): Promise<T> {
    const { skipAuth = false, headers = {}, body, method = 'GET' } = requestOptions
    const allHeaders = new Headers(headers)

    // Add auth header
    const token = skipAuth ? '' : tokenResolver()
    if (token) {
      allHeaders.set('Authorization', `Bearer ${token}`)
    }

    // Set content type for JSON body
    if (body !== undefined && !allHeaders.has('Content-Type')) {
      allHeaders.set('Content-Type', 'application/json')
    }

    const response = await fetchImpl(baseUrl + path, {
      method,
      headers: allHeaders,
      body: body === undefined || typeof body === 'string' ? body : JSON.stringify(body),
    })

    const text = await response.text()
    const data = parseBody(text, response.headers.get('Content-Type'))

    if (!response.ok) {
      const details = typeof data === 'object' && data ? data : { message: text }
      const errorDetails = details as { error?: { code?: string; message?: string }; message?: string }
      throw new ApiError({
        status: response.status,
        code: errorDetails?.error?.code || `http.${response.status}`,
        message: errorDetails?.error?.message || errorDetails?.message || response.statusText,
        details,
        url: path,
      })
    }

    return data as T
  }

  return {
    request,
    get: <T = unknown>(path: string, options?: RequestOptions) =>
      request<T>(path, { ...options, method: 'GET' }),
    post: <T = unknown>(path: string, body?: unknown, options?: RequestOptions) =>
      request<T>(path, { ...options, method: 'POST', body }),
    put: <T = unknown>(path: string, body?: unknown, options?: RequestOptions) =>
      request<T>(path, { ...options, method: 'PUT', body }),
    patch: <T = unknown>(path: string, body?: unknown, options?: RequestOptions) =>
      request<T>(path, { ...options, method: 'PATCH', body }),
    delete: <T = unknown>(path: string, options?: RequestOptions) =>
      request<T>(path, { ...options, method: 'DELETE' }),
    resolveBearerToken: tokenResolver,
  }
}

// Default API client instance
export const api = createApiClient()
