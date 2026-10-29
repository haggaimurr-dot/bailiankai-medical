import { showConfirmDialog } from 'vant'
import type { components } from '../../../packages/contracts/src/schema'
import { API_BASE_URL, ApiError, request, requestEnvelope, type Envelope, type RequestOptions } from './api'
import { useSessionStore, type PatientIdentity, type PatientSession } from './stores/session'

export type { PatientIdentity, PatientSession }
export type Appointment = components['schemas']['Appointment']
export type AppointmentCreate = components['schemas']['AppointmentCreate']
export type AppointmentStatus = components['schemas']['AppointmentStatus']
export type WaitlistEntry = components['schemas']['WaitlistEntry']
export type WaitlistOffer = components['schemas']['WaitlistOffer']
export type CarePhoto = components['schemas']['CarePhoto']
export type CarePlanVersion = components['schemas']['CarePlanVersion']
export type FollowupTask = components['schemas']['FollowupTask']
export type FollowupWorkspace = components['schemas']['FollowupWorkspace']
export type PrivacyPolicy = components['schemas']['PrivacyPolicy']
export type PrivacyConsent = components['schemas']['PrivacyConsent']
export type TimelineEvent = components['schemas']['TimelineEvent']
export type AssessmentTask = components['schemas']['AssessmentTask']
export type AssessmentView = components['schemas']['AssessmentView']

const expiredMessage = '演示身份已过期。创建新身份后，旧预约仍属于旧身份，不会自动转入新身份。'
const identityChangedMessage = '患者身份已变化，请重新加载页面。原申请仍属于原账号，不会使用新身份重新提交。'

export function isSessionExpired(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 401 || error.code === 'SESSION_EXPIRED')
}

async function confirmPrivacy(policy: PrivacyPolicy): Promise<boolean> {
  try {
    await showConfirmDialog({
      title: policy.title,
      message: `${policy.content}\n版本：${policy.version}`,
      confirmButtonText: '同意',
      cancelButtonText: '暂不',
    })
    return true
  } catch {
    return false
  }
}

export async function authenticated<T>(path: string, options: RequestOptions = {}, expectedPatientId?: string, privacyRetried = false): Promise<Envelope<T>> {
  const store = useSessionStore()
  const session = await store.ensureSession()
  if (expectedPatientId !== undefined && store.requireIdentity(expectedPatientId).token !== session.token) {
    throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED')
  }
  try {
    const response = await requestEnvelope<T>(path, { ...options, headers: { ...options.headers, Authorization: `Bearer ${session.token}` } })
    if (store.session?.token !== session.token) throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED')
    return response
  } catch (error) {
    if (error instanceof ApiError && error.code === 'PRIVACY_CONSENT_REQUIRED' && !privacyRetried) {
      const policy = await request<PrivacyPolicy>('/privacy-policy')
      if (!(await confirmPrivacy(policy))) throw error
      store.requireIdentity(session.patient.id)
      await authenticated('/me/privacy-consent', { method: 'POST', data: { policyVersion: policy.version, action: 'accepted' } }, session.patient.id, true)
      return authenticated<T>(path, options, session.patient.id, true)
    }
    if (error instanceof ApiError && error.status === 401) {
      if (store.session?.token === session.token) store.expire()
      throw new ApiError(expiredMessage, 401, 'SESSION_EXPIRED')
    }
    throw error
  }
}

async function ensureConsentForUpload(patientId: string) {
  const consent = (await authenticated<PrivacyConsent>('/me/privacy-consent', {}, patientId)).data!
  if (!consent.accepted) {
    const policy = await request<PrivacyPolicy>('/privacy-policy')
    if (!(await confirmPrivacy(policy))) throw new ApiError('请先同意隐私说明。', 403, 'PRIVACY_CONSENT_REQUIRED')
    await authenticated('/me/privacy-consent', { method: 'POST', data: { policyVersion: policy.version, action: 'accepted' } }, patientId)
  }
}

async function uploadFile<T>(path: string, file: File, patientId: string, extra?: Record<string, string>, headers?: Record<string, string>): Promise<T> {
  const store = useSessionStore()
  const session = store.requireIdentity(patientId)
  const form = new FormData()
  form.append('file', file)
  if (extra) for (const [k, v] of Object.entries(extra)) form.append(k, v)
  let response: Response
  try {
    response = await fetch(`${API_BASE_URL}${path}`, {
      method: 'POST',
      body: form,
      headers: { Authorization: `Bearer ${session.token}`, ...headers },
    })
  } catch {
    throw new ApiError('照片上传中断，请检查连接后重试。', 0, 'NETWORK_ERROR')
  }
  const body = (await response.json().catch(() => null)) as Envelope<T> | null
  if (store.session?.token !== session.token) throw new ApiError(identityChangedMessage, 409, 'PATIENT_IDENTITY_CHANGED')
  if (response.status >= 200 && response.status < 300 && body?.data !== undefined) return body.data
  if (response.status === 401) store.expire()
  throw new ApiError(body?.error?.message || '照片上传失败，请检查图片后重试。', response.status, body?.error?.code || 'UPLOAD_FAILED')
}

export async function uploadAuthenticated(path: string, file: File, patientId: string): Promise<CarePhoto> {
  await ensureConsentForUpload(patientId)
  return uploadFile<CarePhoto>(path, file, patientId)
}

/** Returns an object URL for an authenticated binary download (images). */
export async function downloadAuthenticated(path: string, expectedPatientId?: string): Promise<string> {
  const store = useSessionStore()
  const session = await store.ensureSession()
  if (expectedPatientId !== undefined) store.requireIdentity(expectedPatientId)
  let response: Response
  try {
    response = await fetch(`${API_BASE_URL}${path}`, { headers: { Authorization: `Bearer ${session.token}` } })
  } catch {
    throw new ApiError('照片读取失败，请检查连接后重试。', 0, 'NETWORK_ERROR')
  }
  if (response.status >= 200 && response.status < 300) {
    return URL.createObjectURL(await response.blob())
  }
  if (response.status === 401) store.expire()
  throw new ApiError('照片读取失败，请刷新后重试。', response.status, response.status === 401 ? 'SESSION_EXPIRED' : 'PHOTO_READ_FAILED')
}

export interface CareReminder {
  id: string; kind: 'medication' | 'care'; title: string; detail: string;
  scheduleJson: string; startDate: string; endDate: string | null; doneToday: boolean;
}
export interface ReminderCheckin { date: string; done: boolean }

export const patientApi = {
  previewBinding: async (code: string, patientId: string) => (await authenticated<components['schemas']['PatientBindingPreview']>('/me/patient-binding/preview', { method: 'POST', data: { code } }, patientId)).data!,
  bindPatient: async (code: string, version: number, patientId: string) => {
    const session = (await authenticated<PatientSession>('/me/patient-binding', { method: 'POST', data: { code, version } }, patientId)).data!
    useSessionStore().persist(session)
    return session
  },
  supportAttachment: (ticketId: string, id: string, patientId: string) => downloadAuthenticated(`/me/support/${encodeURIComponent(ticketId)}/attachments/${encodeURIComponent(id)}`, patientId),
  supportTickets: (patientId: string, page = 1) => authenticated<components['schemas']['SupportTicket'][]>(`/me/support?page=${page}&pageSize=20`, {}, patientId),
  supportDetail: (id: string, patientId: string, page = 1) => authenticated<components['schemas']['SupportDetail']['data']>(`/me/support/${encodeURIComponent(id)}?page=${page}&pageSize=20`, {}, patientId),
  createSupport: async (body: components['schemas']['SupportCreate'], patientId: string) => (await authenticated<components['schemas']['SupportTicket']>('/me/support', { method: 'POST', data: body }, patientId)).data!,
  supportAction: async (id: string, body: components['schemas']['SupportAction'], patientId: string) => (await authenticated<components['schemas']['SupportTicket']>(`/me/support/${encodeURIComponent(id)}/actions`, { method: 'POST', data: body }, patientId)).data!,
  knowledgeFavorites: (patientId: string, filters: { clinicId?: string; categoryId?: string; q?: string; page: number }) => authenticated<components['schemas']['KnowledgePublicArticle'][]>('/me/knowledge/favorites', { data: { ...filters, pageSize: 20 } }, patientId),
  knowledgeFavorite: async (id: string, patientId: string) => (await authenticated<boolean>(`/me/knowledge/favorites/${encodeURIComponent(id)}`, {}, patientId)).data!,
  setKnowledgeFavorite: async (id: string, saved: boolean, patientId: string) => (await authenticated<boolean>(`/me/knowledge/favorites/${encodeURIComponent(id)}`, { method: saved ? 'PUT' : 'DELETE' }, patientId)).data!,
  timeline: (page: number, patientId: string) => authenticated<TimelineEvent[]>('/me/records', { data: { page, pageSize: 20 } }, patientId),
  advice: async (kind: string, id: string, patientId: string) => (await authenticated<TimelineEvent>(`/me/records/${encodeURIComponent(kind)}/${encodeURIComponent(id)}/read`, {}, patientId)).data!,
  confirmAdviceRead: async (kind: string, id: string, adviceVersion: string, patientId: string) => (await authenticated<components['schemas']['AdviceReceipt']>(`/me/records/${encodeURIComponent(kind)}/${encodeURIComponent(id)}/read`, { method: 'POST', data: { adviceVersion } }, patientId)).data!,
  cases: (page: number, patientId: string, filters: { category?: string; tag?: string; course?: string } = {}) => authenticated<components['schemas']['CaseView'][]>('/me/cases', { data: { page, pageSize: 20, ...filters } }, patientId),
  caseDetail: async (id: string, patientId: string) => (await authenticated<components['schemas']['CaseView']>(`/me/cases/${encodeURIComponent(id)}`, {}, patientId)).data!,
  caseConsent: async (id: string, body: components['schemas']['CaseConsent'], patientId: string) => (await authenticated<components['schemas']['CaseView']>(`/me/cases/${encodeURIComponent(id)}/consent`, { method: 'POST', data: body }, patientId)).data!,
  casePhotoFile: (id: string, imageId: string, patientId: string) => downloadAuthenticated(`/me/cases/${encodeURIComponent(id)}/images/${encodeURIComponent(imageId)}/content`, patientId),
  messageTemplates: async (patientId: string) => (await authenticated<components['schemas']['MessageTemplate'][]>('/me/messages/templates', {}, patientId)).data!,
  messageConsents: async (patientId: string) => (await authenticated<components['schemas']['MessageConsent'][]>('/me/messages/consents', {}, patientId)).data!,
  patientMessages: async (patientId: string, page = 1, pageSize = 20) => (await authenticated<components['schemas']['PatientMessageList']>('/me/messages', { data: { page, pageSize } }, patientId)).data!,
  recoveryStagePlans: async (patientId?: string) => (await authenticated<any[]>('/me/recovery-stage-plans', {}, patientId)).data!,
  faqs: async (patientId: string | undefined, keyword = '') => (await authenticated<any[]>('/me/faqs', { data: { ...(keyword ? { keyword } : {}) } }, patientId)).data!,
  recordShareLinks: async (patientId?: string) => (await authenticated<any[]>('/me/record-share-links', {}, patientId)).data!,
  createRecordShareLink: async (ttlHours: number, patientId?: string) => (await authenticated<{ id: string; token: string; expiresAt: string }>('/me/record-share-links', { method: 'POST', data: { ttlHours } }, patientId)).data!,
  revokeRecordShareLink: async (linkId: string, patientId?: string) => (await authenticated<any>(`/me/record-share-links/${encodeURIComponent(linkId)}/revoke`, { method: 'POST' }, patientId)).data!,
  lastVisitSummary: async (patientId?: string) => (await authenticated<any>('/me/last-visit-summary', {}, patientId)).data!,
  patientMessageUnreadCount: async (patientId: string) => (await authenticated<{ unreadCount: number }>('/me/messages/unread-count', {}, patientId)).data!,
  markPatientMessageRead: async (id: string, patientId: string) => (await authenticated<components['schemas']['MessageSaved']>(`/me/messages/${encodeURIComponent(id)}/read`, { method: 'POST', data: {} }, patientId)).data!,
  setMessageConsent: async (data: components['schemas']['MessageConsentChange'], patientId: string) => (await authenticated<components['schemas']['MessageSaved']>('/me/messages/consents', { method: 'POST', data }, patientId)).data!,
  privacyPolicy: () => request<PrivacyPolicy>('/privacy-policy'),
  privacyConsent: async (patientId: string) => (await authenticated<PrivacyConsent>('/me/privacy-consent', {}, patientId)).data!,
  setPrivacyConsent: async (policyVersion: string, action: 'accepted' | 'revoked', patientId: string) => (await authenticated<components['schemas']['PrivacyConsentEvent']>('/me/privacy-consent', { method: 'POST', data: { policyVersion, action } }, patientId)).data!,
  updateProfile: async (displayName: string, version: number, patientId: string) => {
    const patient = (await authenticated<PatientIdentity>('/me/profile', { method: 'PUT', data: { displayName, version } }, patientId)).data!
    useSessionStore().cachePatient(patient)
    return patient
  },
  // NOTE: POST /me/phone takes a WeChat-issued {code}; the server also
  // rejects identities without a WeChat OpenID, so demo/web identities
  // can never complete this — the profile page explains instead of
  // offering the action. Kept for contract completeness.
  authorizePhone: async (code: string, version: number, patientId: string) => {
    const patient = (await authenticated<PatientIdentity>('/me/phone', { method: 'POST', data: { code, version } }, patientId)).data!
    useSessionStore().cachePatient(patient)
    return patient
  },
  revokePhone: async (version: number, patientId: string) => {
    const patient = (await authenticated<PatientIdentity>('/me/phone/revoke', { method: 'POST', data: { version } }, patientId)).data!
    useSessionStore().cachePatient(patient)
    return patient
  },
  logout: async () => {
    const store = useSessionStore()
    if (store.session) {
      await authenticated<boolean>('/me/logout', { method: 'POST' }, store.session.patient.id).catch(() => undefined)
    }
    store.expire()
  },
  followupWorkspace: async (id: string, patientId?: string) => (await authenticated<FollowupWorkspace>(`/me/appointments/${encodeURIComponent(id)}/followups`, {}, patientId)).data!,
  followups: async (status = '', page = 1, patientId?: string, dates?: { dateFrom: string; dateTo: string }) => authenticated<FollowupTask[]>('/me/followups', { data: { ...(status ? { status } : {}), ...dates, page, pageSize: 20 } }, patientId),
  submitFollowup: async (id: string, body: components['schemas']['FollowupSubmit'], patientId: string) => (await authenticated<FollowupTask>(`/me/followups/${encodeURIComponent(id)}/submit`, { method: 'POST', data: body }, patientId)).data!,
  uploadFollowupPhoto: (id: string, angle: CarePhoto['angle'], file: File, patientId: string) => uploadAuthenticated(`/me/followups/${encodeURIComponent(id)}/photos/${angle}`, file, patientId),
  followupPhotoFile: (id: string, patientId?: string) => downloadAuthenticated(`/me/followup-photos/${encodeURIComponent(id)}/content`, patientId),
  me: async () => {
    const patient = (await authenticated<PatientIdentity>('/me')).data!
    useSessionStore().cachePatient(patient)
    return patient
  },
  create: async (body: AppointmentCreate, key: string, patientId: string) => (await authenticated<Appointment>('/me/appointments', { method: 'POST', data: body as unknown as Record<string, unknown>, headers: { 'Idempotency-Key': key } }, patientId)).data!,
  byRequest: async (key: string, patientId: string) => (await authenticated<Appointment>(`/me/appointments/by-request/${encodeURIComponent(key)}`, {}, patientId)).data!,
  checkInCode: async (id: string) => (await authenticated<components['schemas']['CheckInCodeResult']['data']>(`/me/appointments/${encodeURIComponent(id)}/check-in-code`, { method: 'POST', data: {} })).data!,
  appointment: async (id: string) => (await authenticated<Appointment>(`/me/appointments/${encodeURIComponent(id)}`)).data!,
  appointments: (status = '', page = 1) => authenticated<Appointment[]>('/me/appointments', { data: { ...(status ? { status } : {}), page, pageSize: 10 } }),
  appointmentTimeline: async (id: string) => (await authenticated<components['schemas']['AppointmentTimelineEvent'][]>(`/me/appointments/${encodeURIComponent(id)}/timeline`)).data!,
  reschedule: async (id: string, body: components['schemas']['AppointmentReschedule'], key: string, patientId: string) => (await authenticated<Appointment>(`/me/appointments/${encodeURIComponent(id)}/reschedule`, { method: 'POST', data: body as unknown as Record<string, unknown>, headers: { 'Idempotency-Key': key } }, patientId)).data!,
  cancel: async (id: string, version: number, cancelReason?: string, reasonDetail?: string) => (await authenticated<Appointment>(`/me/appointments/${encodeURIComponent(id)}/cancel`, { method: 'POST', data: { version, cancelReason, reasonDetail } })).data!,
  waitlist: () => authenticated<WaitlistEntry[]>('/me/waitlist', {}),
  confirmWaitlist: async (id: string) => (await authenticated<Appointment>(`/me/appointments/${encodeURIComponent(id)}/waitlist-confirm`, { method: 'POST', data: {} })).data!,
  doctorReviews: (doctorId: string) => request<components['schemas']['DoctorReviewPublic'][]>(`/doctors/${encodeURIComponent(doctorId)}/reviews`),
  reviewableAppointments: async (doctorId: string) => (await authenticated<components['schemas']['ReviewableAppointment'][]>(`/me/doctors/${encodeURIComponent(doctorId)}/reviewable`, {})).data!,
  myReviews: async (doctorId: string) => (await authenticated<components['schemas']['DoctorReview'][]>('/me/reviews', { data: { doctorId } })).data!,
  createReview: async (doctorId: string, body: components['schemas']['PatientReviewInput']) => (await authenticated<components['schemas']['DoctorReview']>(`/me/doctors/${encodeURIComponent(doctorId)}/reviews`, { method: 'POST', data: body })).data!,
  photoConsent: async (id: string, patientId?: string) => (await authenticated<components['schemas']['PhotoConsent']>(`/me/appointments/${encodeURIComponent(id)}/photo-consent`, {}, patientId)).data!,
  setPhotoConsent: async (id: string, granted: boolean, patientId?: string) => (await authenticated<components['schemas']['PhotoConsent']>(`/me/appointments/${encodeURIComponent(id)}/photo-consent`, { method: 'POST', data: { granted } }, patientId)).data!,
  photos: async (id: string, patientId?: string) => (await authenticated<CarePhoto[]>(`/me/appointments/${encodeURIComponent(id)}/photos`, {}, patientId)).data!,
  uploadPhoto: (id: string, angle: CarePhoto['angle'], file: File, patientId: string) => uploadAuthenticated(`/me/appointments/${encodeURIComponent(id)}/photos/${angle}`, file, patientId),
  photoFile: (id: string, patientId?: string) => downloadAuthenticated(`/me/photos/${encodeURIComponent(id)}/content`, patientId),
  planVersions: async (id: string, patientId?: string) => (await authenticated<CarePlanVersion[]>(`/me/appointments/${encodeURIComponent(id)}/care-plan`, {}, patientId)).data!,
  careReminders: (patientId: string) => authenticated<CareReminder[]>('/me/care-reminders', {}, patientId).then(r => r.data!),
  checkinReminder: (id: string, date: string, patientId: string) => authenticated<{ checked: boolean }>('/me/care-reminders/' + encodeURIComponent(id) + '/checkin', { method: 'POST', data: { date } }, patientId).then(r => r.data!),
  reminderCheckins: (id: string, patientId: string) => authenticated<ReminderCheckin[]>('/me/care-reminders/' + encodeURIComponent(id) + '/checkins', {}, patientId).then(r => r.data!),
}

export const assessmentApi = {
  capabilities: () => request<components['schemas']['AssessmentCapabilities']>('/assessment-capabilities'),
  list: async (patientId: string, page = 1, status = '') => authenticated<AssessmentTask[]>(`/me/assessments?page=${page}&pageSize=20${status ? `&status=${encodeURIComponent(status)}` : ''}`, {}, patientId),
  get: async (id: string, patientId: string) => (await authenticated<AssessmentView>(`/me/assessments/${encodeURIComponent(id)}`, {}, patientId)).data!,
  create: async (body: components['schemas']['AssessmentCreate'], patientId: string) => (await authenticated<AssessmentView>('/me/assessments', { method: 'POST', data: body as unknown as Record<string, unknown> }, patientId)).data!,
  change: async (id: string, action: 'consent' | 'quality-check' | 'submit' | 'retry' | 'cancel', body: components['schemas']['AssessmentVersion'] | components['schemas']['AssessmentConsentChange'], key: string, patientId: string) => (await authenticated<AssessmentView>(`/me/assessments/${encodeURIComponent(id)}/${action}`, { method: 'POST', headers: { 'Idempotency-Key': key }, data: body as unknown as Record<string, unknown> }, patientId)).data!,
  photo: (id: string, photoId: string, patientId: string) => downloadAuthenticated(`/me/assessments/${encodeURIComponent(id)}/photos/${encodeURIComponent(photoId)}/content`, patientId),
  upload: async (id: string, angle: string, file: File, version: number, key: string, patientId: string): Promise<AssessmentView> => {
    await ensureConsentForUpload(patientId)
    return uploadFile<AssessmentView>(`/me/assessments/${encodeURIComponent(id)}/photos/${encodeURIComponent(angle)}`, file, patientId, { version: String(version) }, { 'Idempotency-Key': key })
  },
}

// ---- Appointment submission drafts (idempotency), ported from the
// miniprogram's services/submission.ts: the request key is generated and
// persisted BEFORE the network write so an unknown outcome can be
// resolved via by-request instead of double-booking.
const SUBMISSION_STORAGE_KEY = 'blk-patient-submission'
const CONTACT_TTL = 24 * 60 * 60 * 1000

export interface Submission {
  key: string
  patientId: string
  createdAt: number
  body: AppointmentCreate | null
  selection: { slotId: string; clinicId: string; doctorId: string; date: string }
}

export function loadSubmission(): Submission | null {
  try {
    const draft = JSON.parse(window.localStorage.getItem(SUBMISSION_STORAGE_KEY) || 'null') as Submission | null
    if (!draft?.key || !draft.patientId || !draft.selection?.slotId) return null
    if (Date.now() - draft.createdAt > CONTACT_TTL && draft.body) {
      draft.body = null
      window.localStorage.setItem(SUBMISSION_STORAGE_KEY, JSON.stringify(draft))
    }
    return draft
  } catch {
    return null
  }
}

export function clearSubmission(key?: string) {
  if (!key) return
  const draft = loadSubmission()
  if (draft?.key === key) window.localStorage.removeItem(SUBMISSION_STORAGE_KEY)
}

export function randomKey(): string {
  const bytes = new Uint8Array(16)
  window.crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

export function saveSubmission(patientId: string, body: AppointmentCreate, selection: Submission['selection']): Submission {
  useSessionStore().requireIdentity(patientId)
  if (loadSubmission()) throw new Error('已有一笔申请等待确认，请先查询原申请结果。')
  const draft: Submission = { key: randomKey(), patientId, createdAt: Date.now(), body: { ...body }, selection: { ...selection } }
  window.localStorage.setItem(SUBMISSION_STORAGE_KEY, JSON.stringify(draft))
  return draft
}
