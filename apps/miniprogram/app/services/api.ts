import type { components } from '../../../../packages/contracts/src/schema';
import { API_BASE_URL } from '../config/api';

export type Clinic = components['schemas']['Clinic'];
export type Doctor = components['schemas']['Doctor'];
export type Slot = components['schemas']['Slot'];
export type KnowledgeArticle = components['schemas']['KnowledgePublicArticle'];
export type KnowledgeCategory = components['schemas']['KnowledgeCategory'];

export interface Envelope<T> {
  data?: T;
  error?: { code: string; message: string };
  meta?: components['schemas']['PageMeta'];
}

export class ApiError extends Error {
  constructor(message: string, public status: number, public code: string) {
    super(message);
    this.name = 'ApiError';
  }
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  data?: Record<string, unknown>;
  headers?: Record<string, string>;
}

export function requestEnvelope<T>(path: string, options: RequestOptions = {}): Promise<Envelope<T>> {
  return new Promise((resolve, reject) => {
    wx.request<Envelope<T>>({
      url: `${API_BASE_URL}${path}`,
      method: options.method || 'GET',
      data: options.data,
      header: { 'Content-Type': 'application/json', ...options.headers },
      timeout: 10000,
      success(response) {
        if (response.statusCode >= 200 && response.statusCode < 300 && response.data?.data !== undefined) {
          resolve(response.data);
          return;
        }
        reject(new ApiError(response.data?.error?.message || '暂时无法读取数据，请稍后重试。', response.statusCode, response.data?.error?.code || 'REQUEST_FAILED'));
      },
      fail() {
        reject(new ApiError('连接服务失败，请检查网络及本地服务是否已启动。', 0, 'NETWORK_ERROR'));
      },
    });
  });
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  return (await requestEnvelope<T>(path, options)).data!;
}

function get<T>(path: string, query: Record<string, string> = {}): Promise<T> {
  return request<T>(path, { data: query });
}

export const api = {
  knowledgeCategories: (clinicId: string) => get<KnowledgeCategory[]>('/knowledge/categories', { clinicId }),
  knowledgeArticles: (clinicId: string, categoryId = '', page = 1, pageSize = 20, q = '') => requestEnvelope<KnowledgeArticle[]>('/knowledge/articles', { data: { clinicId, ...(categoryId ? { categoryId } : {}), ...(q ? { q } : {}), page, pageSize } }),
  knowledgeRelated: (id: string) => get<KnowledgeArticle[]>(`/knowledge/articles/${encodeURIComponent(id)}/related`),
  knowledgeArticle: (id: string) => get<KnowledgeArticle>(`/knowledge/articles/${encodeURIComponent(id)}`),
  clinics: () => get<Clinic[]>('/clinics'),
  doctors: (clinicId = '') => get<Doctor[]>('/doctors', clinicId ? { clinicId } : {}),
  slots: (filters: { clinicId: string; date: string; doctorId?: string }) => {
    const query: Record<string, string> = { clinicId: filters.clinicId, date: filters.date };
    if (filters.doctorId) query.doctorId = filters.doctorId;
    return get<Slot[]>('/slots', query);
  },
  slotRecommendations: (filters: { clinicId: string; doctorId: string; after?: string; limit?: number }) => {
    const query: Record<string, string> = { clinicId: filters.clinicId, doctorId: filters.doctorId };
    if (filters.after) query.after = filters.after;
    if (filters.limit) query.limit = String(filters.limit);
    return get<Slot[]>('/slots/recommendations', query);
  },
};

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '暂时无法读取数据，请稍后重试。';
}
