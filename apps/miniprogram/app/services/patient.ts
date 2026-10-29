import type { components } from '../../../../packages/contracts/src/schema';
import { SESSION_EXPIRED_KEY, SESSION_STORAGE_KEY, SUBMISSION_STORAGE_KEY } from '../config/storage';
import { API_BASE_URL, PATIENT_LOGIN_MODE } from '../config/api';
import { ApiError, request, requestEnvelope, type Envelope, type RequestOptions } from './api';

export type PatientSession = components['schemas']['PatientSession'];
export type Appointment = components['schemas']['Appointment'];
export type AppointmentCreate = components['schemas']['AppointmentCreate'];
export type AppointmentStatus = components['schemas']['AppointmentStatus'];
export type AppointmentPage = components['schemas']['AppointmentPage'];
export type CarePhoto = components['schemas']['CarePhoto'];
export type CarePlanVersion = components['schemas']['CarePlanVersion'];
export type CarePlanContent = components['schemas']['CarePlanContent'];

let creatingSession: Promise<PatientSession> | null = null;
let renewingSession: Promise<boolean> | null = null;
let sessionEpoch = 0;
const expiredMessage = PATIENT_LOGIN_MODE === 'wechat' ? '登录已失效，请重新登录。同一微信账号可恢复原患者资料和记录。' : '演示身份已过期。创建新身份后，旧预约仍属于旧身份，不会自动转入新身份。';
const identityChangedMessage = '患者身份已变化，请重新加载页面。原申请仍属于原账号，不会使用新身份重新提交。';

export function isSessionExpired(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 401 || error.code === 'SESSION_EXPIRED');
}

function expireSession() {
  const previous = wx.getStorageSync(SESSION_STORAGE_KEY) as PatientSession | undefined;
  wx.setStorageSync(SESSION_EXPIRED_KEY, { patientId: previous?.patient?.id || '' });
  wx.removeStorageSync(SESSION_STORAGE_KEY);
}

export function cachedSession(): PatientSession | null {
  const stored = wx.getStorageSync(SESSION_STORAGE_KEY) as PatientSession | undefined;
  if (!stored) return null;
  if (!stored.token || !stored.patient?.id || !Number.isFinite(Date.parse(stored.expiresAt)) || Date.parse(stored.expiresAt) <= Date.now()) {
    expireSession();
    return null;
  }
  return stored;
}

export function requirePatientIdentity(patientId: string): PatientSession {
  const session = cachedSession();
  if (!patientId || !session || session.patient.id !== patientId) {
    throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED');
  }
  return session;
}

async function createSession(): Promise<PatientSession> {
  const epoch = sessionEpoch;
  let code = '';
  if (PATIENT_LOGIN_MODE === 'wechat') code = await new Promise<string>((resolve, reject) => wx.login({
    success: (result) => result.code ? resolve(result.code) : reject(new ApiError('微信登录失败，请重试。', 0, 'WECHAT_LOGIN_FAILED')),
    fail: () => reject(new ApiError('微信登录失败，请重试。', 0, 'WECHAT_LOGIN_FAILED')),
  }));
  const session = await request<PatientSession>(PATIENT_LOGIN_MODE === 'wechat' ? '/patient/session' : '/dev/patient-sessions', { method: 'POST', data: code ? { code } : {} });
  if (epoch !== sessionEpoch) throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED');
  wx.setStorageSync(SESSION_STORAGE_KEY, session);
  wx.removeStorageSync(SESSION_EXPIRED_KEY);
  return session;
}

function sharedSessionCreation(): Promise<PatientSession> {
  if (!creatingSession) creatingSession = createSession().finally(() => { creatingSession = null; });
  return creatingSession;
}

export async function ensureSession(): Promise<PatientSession> {
  const session = cachedSession();
  if (session) return session;
  if (wx.getStorageSync(SESSION_EXPIRED_KEY)) throw new ApiError(expiredMessage, 401, 'SESSION_EXPIRED');
  return sharedSessionCreation();
}

export function confirmDialog(title: string, content: string, confirmText = '确定'): Promise<boolean> {
  return new Promise((resolve) => {
    wx.showModal({ title, content, confirmText, confirmColor: '#1763c6', success: (result) => resolve(result.confirm), fail: () => resolve(false) });
  });
}

/** Choice dialog: 'confirm' when the confirm button is tapped, 'cancel' when
 * the cancel button is tapped, null on any other dismissal. */
export function confirmChoice(title: string, content: string, confirmText: string, cancelText: string): Promise<'confirm' | 'cancel' | null> {
  return new Promise((resolve) => {
    wx.showModal({ title, content, confirmText, cancelText, confirmColor: '#c0392b', success: (result) => resolve(result.confirm ? 'confirm' : 'cancel'), fail: () => resolve(null) });
  });
}

export function confirmNewSession(): Promise<boolean> {
  if (renewingSession) return renewingSession;
  // 确认对话框也属于同一轮重建，多个页面共享它，不能各自发起新身份。
  const oldDraft = wx.getStorageSync(SUBMISSION_STORAGE_KEY) as { key?: string; patientId?: string } | undefined;
  renewingSession = (async () => {
    if (PATIENT_LOGIN_MODE === 'wechat') return Boolean((await sharedSessionCreation()).token);
    const confirmed = await confirmDialog('创建新的演示身份', '旧预约仍属于旧身份，新身份不会继承这些记录。此操作也会清除本机旧身份的提交恢复信息。是否继续？', '创建新身份');
    if (!confirmed) return false;
    // 保留过期标记直到成功，网络失败时仍禁止自动换身份。
    const session = await sharedSessionCreation();
    const currentDraft = wx.getStorageSync(SUBMISSION_STORAGE_KEY) as { key?: string; patientId?: string } | undefined;
    if (oldDraft?.key && currentDraft?.key === oldDraft.key && currentDraft.patientId === oldDraft.patientId && currentDraft.patientId !== session.patient.id) {
      wx.removeStorageSync(SUBMISSION_STORAGE_KEY);
    }
    return Boolean(session.token);
  })().finally(() => { renewingSession = null; });
  return renewingSession;
}

export async function authenticated<T>(path: string, options: RequestOptions = {}, expectedPatientId?: string, privacyRetried = false) {
  const session = await ensureSession();
  if (expectedPatientId !== undefined && requirePatientIdentity(expectedPatientId).token !== session.token) {
    throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED');
  }
  try {
    const response = await requestEnvelope<T>(path, { ...options, headers: { ...options.headers, Authorization: `Bearer ${session.token}` } });
    if (cachedSession()?.token !== session.token) throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED');
    return response;
  } catch (error) {
    if (error instanceof ApiError && error.code === 'PRIVACY_CONSENT_REQUIRED' && !privacyRetried) {
      const policy = await request<PrivacyPolicy>('/privacy-policy');
      if (!await confirmDialog(policy.title, policy.content + '\n版本：' + policy.version, '同意')) throw error;
      requirePatientIdentity(session.patient.id);
      await authenticated('/me/privacy-consent', { method: 'POST', data: { policyVersion: policy.version, action: 'accepted' } }, session.patient.id, true);
      return authenticated<T>(path, options, session.patient.id, true);
    }
    if (error instanceof ApiError && error.status === 401) {
      // 延迟到达的旧请求不能清除用户刚创建的新身份。
      const current = wx.getStorageSync(SESSION_STORAGE_KEY) as PatientSession | undefined;
      if (current?.token === session.token) expireSession();
      throw new ApiError(expiredMessage, 401, 'SESSION_EXPIRED');
    }
    throw error;
  }
}

async function uploadAuthenticated(path: string, filePath: string, expectedPatientId: string): Promise<CarePhoto> {
  const consent = (await authenticated<PrivacyConsent>('/me/privacy-consent', {}, expectedPatientId)).data!;
  if (!consent.accepted) {
    const policy = await request<PrivacyPolicy>('/privacy-policy');
    if (!await confirmDialog(policy.title, policy.content + '\n版本：' + policy.version, '同意')) throw new ApiError('请先同意隐私说明。', 403, 'PRIVACY_CONSENT_REQUIRED');
    await authenticated('/me/privacy-consent', { method: 'POST', data: { policyVersion: policy.version, action: 'accepted' } }, expectedPatientId);
  }
  const session = requirePatientIdentity(expectedPatientId);
  return new Promise((resolve, reject) => {
    wx.uploadFile({
      url: `${API_BASE_URL}${path}`,
      filePath,
      name: 'file',
      header: { Authorization: `Bearer ${session.token}` },
      timeout: 30000,
      success(response) {
        const current = wx.getStorageSync(SESSION_STORAGE_KEY) as PatientSession | undefined;
        if (!current || current.token !== session.token || current.patient.id !== expectedPatientId) {
          reject(new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED'));
          return;
        }
        let body: Envelope<CarePhoto> | undefined;
        try { body = JSON.parse(response.data) as Envelope<CarePhoto>; } catch { /* handled below */ }
        if (response.statusCode >= 200 && response.statusCode < 300 && body?.data) {
          resolve(body.data);
          return;
        }
        if (response.statusCode === 401) expireSession();
        reject(new ApiError(body?.error?.message || '照片上传失败，请检查图片后重试。', response.statusCode, body?.error?.code || 'UPLOAD_FAILED'));
      },
      fail() { reject(new ApiError('照片上传中断，请检查连接后重试。', 0, 'NETWORK_ERROR')); },
    });
  });
}

export async function downloadAuthenticated(path: string, expectedPatientId?: string): Promise<string> {
  const session = await ensureSession();
  if (expectedPatientId !== undefined && requirePatientIdentity(expectedPatientId).token !== session.token) {
    throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED');
  }
  return new Promise((resolve, reject) => {
    wx.downloadFile({
      url: `${API_BASE_URL}${path}`,
      header: { Authorization: `Bearer ${session.token}` },
      timeout: 30000,
      success(response) {
        const current = wx.getStorageSync(SESSION_STORAGE_KEY) as PatientSession | undefined;
        if (!current || current.token !== session.token) {
          reject(new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED'));
          return;
        }
        if (response.statusCode >= 200 && response.statusCode < 300) { resolve(response.tempFilePath); return; }
        if (response.statusCode === 401) expireSession();
        reject(new ApiError('照片读取失败，请刷新后重试。', response.statusCode, response.statusCode === 401 ? 'SESSION_EXPIRED' : 'PHOTO_READ_FAILED'));
      },
      fail() { reject(new ApiError('照片读取失败，请检查连接后重试。', 0, 'NETWORK_ERROR')); },
    });
  });
}

export type FollowupTask = components['schemas']['FollowupTask'];
export type FollowupWorkspace = components['schemas']['FollowupWorkspace'];
export type TreatmentRevision = components['schemas']['TreatmentRevision'];
export type PatientIdentity = components['schemas']['PatientIdentity'];
function cachePatient(patient: PatientIdentity) {
  const current = cachedSession();
  if (current?.patient.id === patient.id && (patient.version || 1) >= (current.patient.version || 1)) wx.setStorageSync(SESSION_STORAGE_KEY, { ...current, patient });
  return patient;
}
export type PrivacyPolicy = components['schemas']['PrivacyPolicy'];
export type PrivacyConsent = components['schemas']['PrivacyConsent'];
export type TimelineEvent = components['schemas']['TimelineEvent'];
export const patientApi = {
  previewBinding: async (code: string, patientId: string) => (await authenticated<components['schemas']['PatientBindingPreview']>('/me/patient-binding/preview', { method: 'POST', data: { code } }, patientId)).data!,
  bindChart: async (code: string, version: number, patientId: string) => {
    const epoch = sessionEpoch;
    const session = (await authenticated<PatientSession>('/me/patient-binding', { method: 'POST', data: { code, version } }, patientId)).data!;
    if (epoch !== sessionEpoch) throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED');
    sessionEpoch += 1;
    wx.setStorageSync(SESSION_STORAGE_KEY, session);
    wx.removeStorageSync(SESSION_EXPIRED_KEY);
    return session.patient;
  },
  supportAttachment: (ticketId: string, id: string, patientId: string) => downloadAuthenticated(`/me/support/${encodeURIComponent(ticketId)}/attachments/${encodeURIComponent(id)}`,patientId),
  supportTickets: (patientId: string, page = 1) => authenticated<components['schemas']['SupportTicket'][]>(`/me/support?page=${page}&pageSize=20`, {}, patientId),
  supportDetail: (id: string, patientId: string, page = 1) => authenticated<components['schemas']['SupportDetail']['data']>(`/me/support/${encodeURIComponent(id)}?page=${page}&pageSize=20`, {}, patientId),
  createSupport: async (body: components['schemas']['SupportCreate'], patientId: string) => (await authenticated<components['schemas']['SupportTicket']>('/me/support', { method: 'POST', data: body }, patientId)).data!,
  supportAction: async (id: string, body: components['schemas']['SupportAction'], patientId: string) => (await authenticated<components['schemas']['SupportTicket']>(`/me/support/${encodeURIComponent(id)}/actions`, { method: 'POST', data: body }, patientId)).data!,
  knowledgeFavorites: (patientId: string, filters: { clinicId?: string; categoryId?: string; q?: string; page: number }) => authenticated<components['schemas']['KnowledgePublicArticle'][]>('/me/knowledge/favorites', { data: { ...filters, pageSize: 20 } }, patientId),
  knowledgeFavorite: async (id: string, patientId: string) => (await authenticated<boolean>(`/me/knowledge/favorites/${encodeURIComponent(id)}`, {}, patientId)).data!,
  setKnowledgeFavorite: async (id: string, saved: boolean, patientId: string) => (await authenticated<boolean>(`/me/knowledge/favorites/${encodeURIComponent(id)}`, { method: saved ? 'PUT' : 'DELETE' }, patientId)).data!,
  advice: async (kind: string, id: string, patientId: string) => (await authenticated<TimelineEvent>(`/me/records/${encodeURIComponent(kind)}/${encodeURIComponent(id)}/read`, {}, patientId)).data!,
  timeline: (page: number, patientId: string) => authenticated<TimelineEvent[]>('/me/records', { data: { page, pageSize: 20 } }, patientId),
  confirmAdviceRead: async (kind: string, id: string, adviceVersion: string, patientId: string) => (await authenticated<components['schemas']['AdviceReceipt']>(`/me/records/${encodeURIComponent(kind)}/${encodeURIComponent(id)}/read`, { method: 'POST', data: { adviceVersion } }, patientId)).data!,
  cases: (page: number, patientId: string, filters: { category?: string; tag?: string; course?: string } = {}) => authenticated<components['schemas']['CaseView'][]>('/me/cases', { data: { page, pageSize: 20, ...filters } }, patientId),
  caseDetail: async (id: string, patientId: string) => (await authenticated<components['schemas']['CaseView']>(`/me/cases/${encodeURIComponent(id)}`, {}, patientId)).data!,
  caseConsent: async (id: string, body: components['schemas']['CaseConsent'], patientId: string) => (await authenticated<components['schemas']['CaseView']>(`/me/cases/${encodeURIComponent(id)}/consent`, { method: 'POST', data: body }, patientId)).data!,
  casePhotoFile: (id: string, imageId: string, patientId: string) => downloadAuthenticated(`/me/cases/${encodeURIComponent(id)}/images/${encodeURIComponent(imageId)}/content`, patientId),
  messageTemplates: async (patientId: string) => (await authenticated<components['schemas']['MessageTemplate'][]>('/me/messages/templates', {}, patientId)).data!,
  messageConsents: async (patientId: string) => (await authenticated<components['schemas']['MessageConsent'][]>('/me/messages/consents', {}, patientId)).data!,
  patientMessages: async (patientId: string, page = 1, pageSize = 20) => (await authenticated<components['schemas']['PatientMessageList']>('/me/messages', { data: { page, pageSize } }, patientId)).data!,
  patientMessageUnreadCount: async (patientId: string) => (await authenticated<{ unreadCount: number }>('/me/messages/unread-count', {}, patientId)).data!,
  markPatientMessageRead: async (id: string, patientId: string) => (await authenticated<components['schemas']['MessageSaved']>(`/me/messages/${encodeURIComponent(id)}/read`, { method: 'POST', data: {} }, patientId)).data!,
  setMessageConsent: async (data: components['schemas']['MessageConsentChange'], patientId: string) => (await authenticated<components['schemas']['MessageSaved']>('/me/messages/consents', { method: 'POST', data }, patientId)).data!,
  privacyPolicy: () => request<PrivacyPolicy>('/privacy-policy'),
  privacyConsent: async (patientId: string) => (await authenticated<PrivacyConsent>('/me/privacy-consent', {}, patientId)).data!,
  setPrivacyConsent: async (policyVersion: string, action: 'accepted' | 'revoked', patientId: string) => (await authenticated<components['schemas']['PrivacyConsentEvent']>('/me/privacy-consent', { method: 'POST', data: { policyVersion, action } }, patientId)).data!,
  updateProfile: async (displayName: string, version: number, patientId: string) => cachePatient((await authenticated<PatientIdentity>('/me/profile', { method: 'PUT', data: { displayName, version } }, patientId)).data!),
  authorizePhone: async (code: string, version: number, patientId: string) => cachePatient((await authenticated<PatientIdentity>('/me/phone', { method: 'POST', data: { code, version } }, patientId)).data!),
  revokePhone: async (version: number, patientId: string) => cachePatient((await authenticated<PatientIdentity>('/me/phone/revoke', { method: 'POST', data: { version } }, patientId)).data!),
  logout: async () => {
    const session = cachedSession();
    if (session) await authenticated<boolean>('/me/logout', { method: 'POST' }, session.patient.id);
    sessionEpoch += 1;
    expireSession();
  },
  followupWorkspace: async (id: string, patientId?: string) => (await authenticated<FollowupWorkspace>(`/me/appointments/${encodeURIComponent(id)}/followups`, {}, patientId)).data!,
  followups: async (status = '', page = 1, patientId?: string, dates?: { dateFrom: string; dateTo: string }) => authenticated<FollowupTask[]>('/me/followups', { data: { ...(status ? { status } : {}), ...dates, page, pageSize: 20 } }, patientId),
  submitFollowup: async (id: string, body: components['schemas']['FollowupSubmit'], patientId: string) => (await authenticated<FollowupTask>(`/me/followups/${encodeURIComponent(id)}/submit`, { method: 'POST', data: body }, patientId)).data!,
  uploadFollowupPhoto: (id: string, angle: CarePhoto['angle'], filePath: string, patientId: string) => uploadAuthenticated(`/me/followups/${encodeURIComponent(id)}/photos/${angle}`, filePath, patientId),
  followupPhotoFile: (id: string, patientId?: string) => downloadAuthenticated(`/me/followup-photos/${encodeURIComponent(id)}/content`, patientId),
  me: async () => cachePatient((await authenticated<PatientIdentity>('/me')).data!),
  create: async (body: AppointmentCreate, key: string, patientId: string) => (await authenticated<Appointment>('/me/appointments', { method: 'POST', data: body, headers: { 'Idempotency-Key': key } }, patientId)).data!,
  byRequest: async (key: string, patientId: string) => (await authenticated<Appointment>(`/me/appointments/by-request/${encodeURIComponent(key)}`, {}, patientId)).data!,
  checkInCode: async (id: string) => (await authenticated<components['schemas']['CheckInCodeResult']['data']>(`/me/appointments/${encodeURIComponent(id)}/check-in-code`, { method: 'POST', data: {} })).data!,
  appointment: async (id: string) => (await authenticated<Appointment>(`/me/appointments/${encodeURIComponent(id)}`)).data!,
  list: async (status = '', page = 1): Promise<AppointmentPage> => {
    const response = await authenticated<Appointment[]>('/me/appointments', { data: { ...(status ? { status } : {}), page, pageSize: 10 } });
    return { data: response.data!, meta: response.meta! };
  },
  appointmentTimeline: async (id: string) => (await authenticated<components['schemas']['AppointmentTimelineEvent'][]>(`/me/appointments/${encodeURIComponent(id)}/timeline`)).data!,
  reschedule: async (id: string, body: components['schemas']['AppointmentReschedule'], key: string, patientId: string) => (await authenticated<Appointment>(`/me/appointments/${encodeURIComponent(id)}/reschedule`, { method: 'POST', data: body, headers: { 'Idempotency-Key': key } }, patientId)).data!,
  cancel: async (id: string, version: number) => (await authenticated<Appointment>(`/me/appointments/${encodeURIComponent(id)}/cancel`, { method: 'POST', data: { version } })).data!,
  photoConsent: async (id: string, patientId?: string) => (await authenticated<components['schemas']['PhotoConsent']>(`/me/appointments/${encodeURIComponent(id)}/photo-consent`, {}, patientId)).data!,
  setPhotoConsent: async (id: string, granted: boolean, patientId?: string) => (await authenticated<components['schemas']['PhotoConsent']>(`/me/appointments/${encodeURIComponent(id)}/photo-consent`, { method: 'POST', data: { granted } }, patientId)).data!,
  photos: async (id: string, patientId?: string) => (await authenticated<CarePhoto[]>(`/me/appointments/${encodeURIComponent(id)}/photos`, {}, patientId)).data!,
  uploadPhoto: (id: string, angle: CarePhoto['angle'], filePath: string, patientId: string) => uploadAuthenticated(`/me/appointments/${encodeURIComponent(id)}/photos/${angle}`, filePath, patientId),
  photoFile: (id: string, patientId?: string) => downloadAuthenticated(`/me/photos/${encodeURIComponent(id)}/content`, patientId),
  planVersions: async (id: string, patientId?: string) => (await authenticated<CarePlanVersion[]>(`/me/appointments/${encodeURIComponent(id)}/care-plan`, {}, patientId)).data!,
};
