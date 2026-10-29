import type { components } from '../../../packages/contracts/src/schema'

// Same-origin in production (served next to the API), proxied in dev.
export const API_BASE_URL = '/api/v1'

export type Clinic = components['schemas']['Clinic']
export type Doctor = components['schemas']['Doctor']
export type Slot = components['schemas']['Slot']
export type KnowledgeArticle = components['schemas']['KnowledgePublicArticle']
export type KnowledgeQuizQuestion = components['schemas']['KnowledgeQuizQuestion']
export type KnowledgeCategory = components['schemas']['KnowledgeCategory']
export type PageMeta = components['schemas']['PageMeta']

export interface Envelope<T> {
  data?: T
  error?: { code: string; message: string }
  meta?: PageMeta
}

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  data?: Record<string, unknown>
  headers?: Record<string, string>
}

export async function requestEnvelope<T>(path: string, options: RequestOptions = {}): Promise<Envelope<T>> {
  const method = options.method || 'GET'
  const url = new URL(`${API_BASE_URL}${path}`, window.location.origin)
  let body: string | undefined
  if (method === 'GET' && options.data) {
    for (const [key, value] of Object.entries(options.data)) {
      if (value !== undefined && value !== null && value !== '') url.searchParams.set(key, String(value))
    }
  } else if (options.data !== undefined) {
    body = JSON.stringify(options.data)
  }
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), 15000)
  let response: Response
  try {
    response = await fetch(url, {
      method,
      body,
      signal: controller.signal,
      headers: { 'Content-Type': 'application/json', ...options.headers },
    })
  } catch {
    throw new ApiError('连接服务失败，请检查网络后重试。', 0, 'NETWORK_ERROR')
  } finally {
    window.clearTimeout(timer)
  }
  const payload = (await response.json().catch(() => null)) as Envelope<T> | null
  if (response.status >= 200 && response.status < 300 && payload?.data !== undefined) return payload
  throw new ApiError(payload?.error?.message || '暂时无法读取数据，请稍后重试。', response.status, payload?.error?.code || 'REQUEST_FAILED')
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  return (await requestEnvelope<T>(path, options)).data!
}

function get<T>(path: string, query: Record<string, string> = {}): Promise<T> {
  return request<T>(path, { data: query })
}

export const api = {
  knowledgeCategories: (clinicId: string) => get<KnowledgeCategory[]>('/knowledge/categories', { clinicId }),
  knowledgeArticles: (clinicId: string, categoryId = '', page = 1, pageSize = 20, q = '') =>
    requestEnvelope<KnowledgeArticle[]>('/knowledge/articles', { data: { clinicId, ...(categoryId ? { categoryId } : {}), ...(q ? { q } : {}), page, pageSize } }),
  knowledgeRelated: (id: string) => get<KnowledgeArticle[]>(`/knowledge/articles/${encodeURIComponent(id)}/related`),
  knowledgeArticle: (id: string) => get<KnowledgeArticle>(`/knowledge/articles/${encodeURIComponent(id)}`),
  clinics: () => get<Clinic[]>('/clinics'),
  doctors: (clinicId = '') => get<Doctor[]>('/doctors', clinicId ? { clinicId } : {}),
  slots: (filters: { clinicId: string; date: string; doctorId?: string }) => {
    const query: Record<string, string> = { clinicId: filters.clinicId, date: filters.date }
    if (filters.doctorId) query.doctorId = filters.doctorId
    return get<Slot[]>('/slots', query)
  },
  slotRecommendations: (filters: { clinicId: string; doctorId: string; after?: string; limit?: number }) => {
    const query: Record<string, string> = { clinicId: filters.clinicId, doctorId: filters.doctorId }
    if (filters.after) query.after = filters.after
    if (filters.limit) query.limit = String(filters.limit)
    return get<Slot[]>('/slots/recommendations', query)
  },
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '暂时无法读取数据，请稍后重试。'
}
