const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api'

export interface ApiError {
  code: string
  message: string
  retryable: boolean
  requestId?: string
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.headers || {}),
    },
  })

  if (!response.ok) throw await parseApiError(response)
  return response.json() as Promise<T>
}

async function parseApiError(response: Response): Promise<ApiError> {
  try {
    const body = (await response.json()) as Partial<ApiError> & { error?: string }
    return {
      code: body.code || 'http_error',
      message: body.message || body.error || '服务暂时不可用，请稍后重试',
      retryable: Boolean(body.retryable),
      requestId: body.requestId,
    }
  } catch {
    return { code: 'http_error', message: '服务暂时不可用，请稍后重试', retryable: true }
  }
}

export function createRequestId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) return crypto.randomUUID()
  return `lesson-${Date.now()}-${Math.random().toString(16).slice(2)}`
}
