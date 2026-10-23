import type { components } from '../../../../packages/contracts/src/schema';
import { API_BASE_URL } from '../config/api';
import { request, ApiError } from './api';
import { authenticated, cachedSession, requirePatientIdentity, downloadAuthenticated, patientApi } from './patient';

export type AssessmentView = components['schemas']['AssessmentView'];
export type AssessmentTask = components['schemas']['AssessmentTask'];
export const assessmentLabels: Record<AssessmentTask['status'], string> = { draft: '准备照片', queued: '等待分析', analyzing: '正在分析', retry_wait: '等待重试', failed: '分析失败', awaiting_review: '待医生复核', completed: '复核完成', rejected: '需补拍或面诊', cancelled: '已取消' };
export const assessmentQualityLabels: Record<string,string> = {pending:'等待检查',passed:'质检通过',rejected:'请按提示补拍',unavailable:'检查暂不可用'};
export const assessmentFailureLabels: Record<string,string> = {MODEL_TIMEOUT:'分析超时',MODEL_RATE_LIMITED:'服务繁忙，等待重试',MODEL_UNAVAILABLE:'分析服务暂不可用',WORKER_LEASE_EXPIRED:'任务执行中断，等待恢复',MEDIA_UNAVAILABLE:'照片读取失败，分析暂未完成',MODEL_OUTPUT_INVALID:'未取得可用的分析结果，请联系工作人员或预约面诊',MODEL_CONFIGURATION_ERROR:'辅助观察服务尚未就绪',AI_CONSENT_REVOKED:'AI用途授权已撤回',CONSENT_UNAVAILABLE:'授权需要重新确认，任务已停止',PATIENT_CANCELLED:'您已取消本任务'};
export const assessmentAngles = [{ value: 'front', label: '正面' }, { value: 'left', label: '本人左侧45°' }, { value: 'right', label: '本人右侧45°' }] as const;
export const assessmentKey = () => `ai-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
export const assessmentTime = (stamp: string) => new Date(Date.parse(stamp) + 8 * 3600000).toISOString().slice(0, 16).replace('T', ' ');

export const assessmentApi = {
  capabilities: () => request<components['schemas']['AssessmentCapabilities']>('/assessment-capabilities'),
  list: async (patientId: string, page = 1, status = '') => authenticated<AssessmentTask[]>(`/me/assessments?page=${page}&pageSize=20${status ? `&status=${encodeURIComponent(status)}` : ''}`, {}, patientId),
  get: async (id: string, patientId: string) => (await authenticated<AssessmentView>(`/me/assessments/${encodeURIComponent(id)}`, {}, patientId)).data!,
  create: async (body: components['schemas']['AssessmentCreate'], patientId: string) => (await authenticated<AssessmentView>('/me/assessments', { method: 'POST', data: body }, patientId)).data!,
  change: async (id: string, action: 'consent' | 'quality-check' | 'submit' | 'retry' | 'cancel', body: components['schemas']['AssessmentVersion'] | components['schemas']['AssessmentConsentChange'], key: string, patientId: string) => (await authenticated<AssessmentView>(`/me/assessments/${encodeURIComponent(id)}/${action}`, { method: 'POST', headers: { 'Idempotency-Key': key }, data: body }, patientId)).data!,
  photo: (id: string, photoId: string, patientId: string) => downloadAuthenticated(`/me/assessments/${encodeURIComponent(id)}/photos/${encodeURIComponent(photoId)}/content`, patientId),
  async upload(id: string, angle: string, filePath: string, version: number, key: string, patientId: string): Promise<AssessmentView> {
    const consent = await patientApi.privacyConsent(patientId);
    if (!consent.accepted) {
      const policy = await patientApi.privacyPolicy();
      const agreed = await new Promise<boolean>(resolve => wx.showModal({ title: policy.title, content: `${policy.content}\n版本：${policy.version}`, confirmText: '同意', success: result => resolve(result.confirm), fail: () => resolve(false) }));
      if (!agreed) throw new ApiError('请先同意隐私说明。', 403, 'PRIVACY_CONSENT_REQUIRED');
      await patientApi.setPrivacyConsent(policy.version, 'accepted', patientId);
    }
    const session = requirePatientIdentity(patientId);
    return new Promise((resolve, reject) => wx.uploadFile({ url: `${API_BASE_URL}/me/assessments/${encodeURIComponent(id)}/photos/${encodeURIComponent(angle)}`, filePath, name: 'file', formData: { version: String(version) }, header: { Authorization: `Bearer ${session.token}`, 'Idempotency-Key': key }, timeout: 30000,
      success(response) {
        if (cachedSession()?.token !== session.token || cachedSession()?.patient.id !== patientId) { reject(new ApiError('身份已变化，请重新加载页面。', 409, 'PATIENT_IDENTITY_CHANGED')); return; }
        let body: { data?: AssessmentView; error?: { code: string; message: string } } = {};
        try { body = JSON.parse(response.data); } catch { /* reject below */ }
        if (response.statusCode >= 200 && response.statusCode < 300 && body.data) resolve(body.data);
        else reject(new ApiError(body.error?.message || '照片上传失败，请重试原操作。', response.statusCode, body.error?.code || 'UPLOAD_FAILED'));
      }, fail() { reject(new ApiError('上传结果待确认，请重试原照片。', 0, 'NETWORK_ERROR')); },
    }));
  },
};
