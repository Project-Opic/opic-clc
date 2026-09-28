const TOKEN_KEY = 'cp_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

export interface ApiResult<T> {
  // OPIC 统一响应结构(O-ARC):returnInfo{returnCode, errorMsg} + data
  returnInfo: { returnCode: string; errorMsg: string }
  data: T
}

const SUCCESS_CODE = 'SUC0000'

export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  size: number
}

export async function request<T = any>(path: string, options: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  }
  const token = getToken()
  if (token) headers['Authorization'] = `Bearer ${token}`

  const res = await fetch(`/api${path}`, { ...options, headers })
  if (res.status === 401) {
    clearToken()
    window.location.href = '/login'
    throw new Error('登录已过期')
  }
  const body: ApiResult<T> = await res.json()
  if (!body.returnInfo || body.returnInfo.returnCode !== SUCCESS_CODE) {
    // OPIC 统一错误码(O-ARC):错误提示带出 7 位错误码,便于定位责任中心
    throw new Error(`[${body.returnInfo?.returnCode || 'UNKNOWN'}] ${body.returnInfo?.errorMsg || '请求失败'}`)
  }
  return body.data
}

export const http = {
  get<T = any>(path: string, params?: Record<string, any>): Promise<T> {
    let url = path
    if (params) {
      const qs = Object.entries(params)
        .filter(([, v]) => v !== undefined && v !== null && v !== '')
        .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
        .join('&')
      if (qs) url += `?${qs}`
    }
    return request<T>(url)
  },
  post<T = any>(path: string, data?: any): Promise<T> {
    return request<T>(path, { method: 'POST', body: JSON.stringify(data || {}) })
  },
  put<T = any>(path: string, data?: any): Promise<T> {
    return request<T>(path, { method: 'PUT', body: JSON.stringify(data || {}) })
  },
  del<T = any>(path: string): Promise<T> {
    return request<T>(path, { method: 'DELETE' })
  },
}
