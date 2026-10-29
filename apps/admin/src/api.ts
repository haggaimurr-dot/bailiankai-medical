import { computed } from 'vue'
import type { components } from '../../../packages/contracts/src/schema'
import { auth, canAccess, clearAuth, type Employee } from './auth'

export type ClinicAdmin = components['schemas']['ClinicAdmin']
export type DoctorAdmin = components['schemas']['DoctorAdmin']
export type Credential = components['schemas']['Credential']
export type DirectoryRevision = components['schemas']['DirectoryRevision']
export type ClinicInput = components['schemas']['ClinicInput']
export type DoctorInput = components['schemas']['DoctorInput']
export type Clinic = components['schemas']['Clinic']
export type KnowledgeCategory = components['schemas']['KnowledgeCategory']
export type KnowledgeArticle = components['schemas']['KnowledgeArticle']
export type KnowledgeQuizQuestion = components['schemas']['KnowledgeQuizQuestion']
export type KnowledgeEvent = components['schemas']['KnowledgeEvent']
export type Doctor = components['schemas']['Doctor']
export type Slot = components['schemas']['Slot']
export type ScheduleSetting = components['schemas']['ScheduleSetting']
export type SchedulePatientTask = components['schemas']['SchedulePatientTask']
export type Appointment = components['schemas']['Appointment']
export type AppointmentPage = components['schemas']['AppointmentPage']
export type AppointmentReview = components['schemas']['AppointmentReview']
export type CarePhoto = components['schemas']['CarePhoto']
export type PatientChart = components['schemas']['PatientChart']
export type ConsultationTemplate = components['schemas']['ConsultationTemplate']
export type PatientRecord = components['schemas']['PatientRecord']
export type PatientRecordSummary = components['schemas']['PatientRecordSummary']
export type RecordExportInput = components['schemas']['RecordExportInput']
export type RecordExportJob = components['schemas']['RecordExportJob']
export type RecordExportPage = components['schemas']['RecordExportPage']
export type RecordExportEvent = components['schemas']['RecordExportEvent']
export type CarePlanContent = components['schemas']['CarePlanContent']
export type CarePlanWorkspace = components['schemas']['CarePlanWorkspace']
export type CarePlanVersion = components['schemas']['CarePlanVersion']
export type CarePlanTemplate = components['schemas']['CarePlanTemplate']

export type FollowupTask = components['schemas']['FollowupTask']
export type FollowupWorkspace = components['schemas']['FollowupWorkspace']
export type TreatmentCreate = components['schemas']['TreatmentCreate']
export type TreatmentCorrection = components['schemas']['TreatmentCorrection']
export type FollowupStaff = components['schemas']['FollowupStaff']
export type FollowupAssignmentEvent = components['schemas']['FollowupAssignmentEvent']
export type FollowupStaffEvent = components['schemas']['FollowupStaffEvent']
export type HandoffReceipt = components['schemas']['HandoffReceipt']
export type HandoffFollowupEvent = components['schemas']['HandoffFollowupEvent']
export type HandoffSetting = components['schemas']['HandoffSetting']
export type EmployeeEvent = components['schemas']['EmployeeEvent']
export type EmployeeLoginAttempt = components['schemas']['EmployeeLoginAttempt']

export type FollowupTemplate = components['schemas']['FollowupTemplate']
export type NursingFeedback = components['schemas']['NursingFeedback']

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

export const canEditSchedule = computed(() => canAccess('/schedule'))
export const canManageAppointments = computed(() => canAccess('appointment.review'))

async function requestEnvelope<T>(path: string, init?: RequestInit): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 15000)
  try {
    const signal = init?.signal ? AbortSignal.any([controller.signal, init.signal]) : controller.signal
    const response = await fetch(`/api/v1${path}`, { ...init, signal })
    const body = await response.json().catch(() => null)
    if (!response.ok) {
      if (response.status === 401 && path.startsWith('/admin/') && path !== '/admin/session') { clearAuth(); window.location.assign('/login') }
      throw new ApiError(response.status, body?.error?.code || 'REQUEST_FAILED', body?.error?.message || `请求失败（${response.status}），请稍后重试。`)
    }
    if (!body || !('data' in body)) throw new Error('服务返回的数据格式不正确，请稍后重试。')
    return body as T
  } catch (error) {
    if (error instanceof ApiError) throw error
    if (error instanceof DOMException && error.name === 'AbortError') throw new Error('服务响应超时，请检查连接后重试。')
    if (error instanceof TypeError) throw new Error('暂时无法连接服务，请检查服务是否已启动。')
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  return (await requestEnvelope<{ data: T }>(path, init)).data
}

function adminRequest(init: RequestInit = {}): RequestInit {
  return { ...init, headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${auth.token}`, ...init.headers } }
}

function query(values: Record<string, string | number | undefined>) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) if (value !== '' && value !== undefined) params.set(key, String(value))
  return params.size ? `?${params}` : ''
}

export type PermissionRow = { key: string; label: string; roles: Record<Employee['role'], boolean> }
export type AssessmentTask = components['schemas']['AssessmentTask']
export type AssessmentView = components['schemas']['AssessmentView']
export type AssessmentContent = components['schemas']['AssessmentContent']
export const api = {
  assessmentCapabilities: () => request<components['schemas']['AssessmentCapabilities']>('/assessment-capabilities'),
  assessments: (status = '', clinicId = '', page = 1, q = '') => requestEnvelope<components['schemas']['AssessmentPage']>(`/admin/assessments${query({ status, clinicId, page, pageSize: 20, q })}`, adminRequest()),
  assessmentReviewStats: () => request<components['schemas']['AssessmentReviewStats']>('/admin/assessments/review-stats', adminRequest()),
  assessment: (id: string) => request<AssessmentView>(`/admin/assessments/${encodeURIComponent(id)}`, adminRequest()),
  assessmentReviewers: (clinicId: string) => request<components['schemas']['AssessmentReviewer'][]>(`/admin/assessments/reviewers${query({ clinicId })}`, adminRequest()),
  assessmentEvents: (id: string) => request<components['schemas']['AssessmentEvent'][]>(`/admin/assessments/${encodeURIComponent(id)}/events`, adminRequest()),
  assessmentAccessAudit: (page = 1, pageSize = 20) => requestEnvelope<components['schemas']['AssessmentAccessAuditPage']>(`/admin/assessments/access-audit${query({ page, pageSize })}`, adminRequest()),
  assessmentConsentPolicy: () => request<components['schemas']['AssessmentConsentPolicyList']>('/admin/assessment-consent-policy', adminRequest()),
  createAssessmentConsentPolicy: (content: string, version?: string) => request<components['schemas']['AssessmentConsentPolicyList']>('/admin/assessment-consent-policy', adminRequest({ method: 'POST', body: JSON.stringify(version ? { content, version } : { content }) })),
  assessmentWrite: (id: string, action: 'assignment' | 'reviews', body: components['schemas']['AssessmentAssignment'] | components['schemas']['AssessmentReviewCreate'], key: string) => request<AssessmentView>(`/admin/assessments/${encodeURIComponent(id)}/${action}`, adminRequest({ method: 'POST', headers: { 'Idempotency-Key': key }, body: JSON.stringify(body) })),
  assessmentPhotoFile: async (assessmentId: string, id: string, signal?: AbortSignal) => {
    const response = await fetch(`/api/v1/admin/assessments/${encodeURIComponent(assessmentId)}/photos/${encodeURIComponent(id)}/content`, { ...adminRequest(), signal });
    if (!response.ok) throw new ApiError(response.status, 'PHOTO_DENIED', '评估照片当前无法访问，请刷新核对授权。');
    return URL.createObjectURL(await response.blob());
  },
  supportAttachment: async (ticketId: string, id: string) => {
    const response = await fetch(`/api/v1/admin/support/${encodeURIComponent(ticketId)}/attachments/${encodeURIComponent(id)}`, { headers: { Authorization: `Bearer ${auth.token}` }, cache: 'no-store', signal: AbortSignal.timeout(15000) })
    if (!response.ok) { const body = await response.json().catch(() => null); throw new ApiError(response.status, body?.error?.code || 'REQUEST_FAILED', body?.error?.message || '附件下载失败') }
    return response.blob()
  },
  supportTickets: (status = '', assignment = '', overdue = false, page = 1) => requestEnvelope<components['schemas']['SupportPage']>(`/admin/support?${new URLSearchParams({ ...(status ? { status } : {}), ...(assignment ? { assignment } : {}), overdue: String(overdue), page: String(page), pageSize: '20' })}`, adminRequest()),
  supportDetail: (id: string, page = 1) => requestEnvelope<components['schemas']['SupportDetail']>(`/admin/support/${encodeURIComponent(id)}?page=${page}&pageSize=20`, adminRequest()),
  supportAgents: () => request<components['schemas']['SupportAgent'][]>('/admin/support/agents', adminRequest()),
  supportStats: () => request<components['schemas']['SupportStats']>('/admin/support/stats', adminRequest()),
  supportAction: (id: string, body: components['schemas']['SupportAction']) => request<components['schemas']['SupportTicket']>(`/admin/support/${encodeURIComponent(id)}/actions`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  knowledgeCategories: () => request<KnowledgeCategory[]>('/admin/knowledge/categories', adminRequest({ cache: 'no-store' })),
  knowledgeArticles: (filters: { clinicId?: string; categoryId?: string; status?: string; reviewStatus?: string; q?: string; page: number }) => requestEnvelope<{ data: KnowledgeArticle[]; meta: { total: number } }>('/admin/knowledge/articles' + query({ ...filters, pageSize: 20 }), adminRequest({ cache: 'no-store' })),
  knowledgeArticle: (id: string) => request<KnowledgeArticle>('/admin/knowledge/articles/' + encodeURIComponent(id), adminRequest({ cache: 'no-store' })),
  faqs: (filters: { keyword?: string; category?: string; clinicId?: string } = {}) => request<any[]>('/admin/faqs' + query(filters), adminRequest({ cache: 'no-store' })),
  createFaq: (body: { category: string; question: string; answer: string; sortOrder: number; enabled: boolean }) => request<any>('/admin/faqs', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  updateFaq: (id: string, body: { category: string; question: string; answer: string; sortOrder: number; enabled: boolean }) => request<any>('/admin/faqs/' + encodeURIComponent(id), adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  deleteFaq: (id: string) => request<any>('/admin/faqs/' + encodeURIComponent(id), adminRequest({ method: 'DELETE' })),
  patientTier: (patientId: string) => request<any>('/admin/patients/' + encodeURIComponent(patientId) + '/tier', adminRequest({ cache: 'no-store' })),
  setPatientTier: (patientId: string, tier: string) => request<any>('/admin/patients/' + encodeURIComponent(patientId) + '/tier', adminRequest({ method: 'PUT', body: JSON.stringify({ tier }) })),
  tfaStatus: () => request<any>('/admin/2fa/status', adminRequest({ cache: 'no-store' })),
  tfaSetup: () => request<any>('/admin/2fa/setup', adminRequest({ method: 'POST' })),
  tfaEnable: (code: string) => request<any>('/admin/2fa/enable', adminRequest({ method: 'POST', body: JSON.stringify({ code }) })),
  tfaDisable: (code: string) => request<any>('/admin/2fa/disable', adminRequest({ method: 'POST', body: JSON.stringify({ code }) })),
  knowledgeHistory: (kind: 'articles' | 'categories', id: string) => request<KnowledgeEvent[]>(`/admin/knowledge/${kind}/${encodeURIComponent(id)}/history`, adminRequest({ cache: 'no-store' })),
  saveKnowledgeCategory: (body: { name: string; sortOrder: number; active: boolean; clinicId?: string; version?: number }, id?: string) => request<KnowledgeCategory>('/admin/knowledge/categories' + (id ? '/' + encodeURIComponent(id) : ''), adminRequest({ method: id ? 'PUT' : 'POST', body: JSON.stringify(body) })),
  saveKnowledgeArticle: (body: { title: string; summary: string; body: string; sourceText: string; videoUrl?: string; quiz?: KnowledgeQuizQuestion[]; categoryId?: string; sortOrder?: number; version?: number }, id?: string) => request<KnowledgeArticle>('/admin/knowledge/articles' + (id ? '/' + encodeURIComponent(id) : ''), adminRequest({ method: id ? 'PUT' : 'POST', body: JSON.stringify(body) })),
  knowledgeStats: (params: { clinicId?: string; days?: number }) => request<{ total: number; uniquePatients: number; articles: { articleId: string; title: string; opens: number; uniquePatients: number }[] }>('/admin/knowledge/stats' + query(params), adminRequest({ cache: 'no-store' })),
  knowledgeAction: (id: string, action: 'review' | 'publish' | 'unpublish' | 'sort', body: { version: number; decision?: 'approved' | 'rejected'; note?: string; sortOrder?: number }) => request<KnowledgeArticle>(`/admin/knowledge/articles/${encodeURIComponent(id)}/${action}`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  employeePermissions: () => request<PermissionRow[]>('/admin/permissions', adminRequest()),
  changePassword: (currentPassword: string, password: string) => request<Employee>('/admin/session/password', adminRequest({ method: 'POST', body: JSON.stringify({ currentPassword, password }) })),
  setEmployeeAccess: (id: string, version: number, role: Employee['role'], staffId: string) => request<Employee>(`/admin/employees/${encodeURIComponent(id)}/access`, adminRequest({ method: 'POST', body: JSON.stringify({ version, role, staffId }) })),
  scheduleSetting: (clinicId: string) => request<ScheduleSetting>(`/admin/schedule/settings/${encodeURIComponent(clinicId)}`, adminRequest()),
  saveScheduleSetting: (clinicId: string, body: components['schemas']['ScheduleSettingChange']) => request<ScheduleSetting>(`/admin/schedule/settings/${encodeURIComponent(clinicId)}`, adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  schedulePatients: (clinicId: string) => request<SchedulePatientTask[]>(`/admin/schedule/patients${query({ clinicId })}`, adminRequest()),
  updateScheduleContact: (id: string, body: components['schemas']['ScheduleContactInput']) => request(`/admin/schedule/patients/${encodeURIComponent(id)}/contact`, adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  rescheduleAffectedPatient: (id: string, slotId: string, version: number, key: string) => request<Appointment>(`/admin/schedule/patients/${encodeURIComponent(id)}/reschedule`, adminRequest({ method: 'POST', headers: { 'Idempotency-Key': key }, body: JSON.stringify({ slotId, version }) })),
  careTemplates: () => request<CarePlanTemplate[]>('/admin/care-plan-templates', adminRequest()),
  createCareTemplate: (name: string, content: CarePlanContent) => request<CarePlanTemplate>('/admin/care-plan-templates', adminRequest({ method: 'POST', body: JSON.stringify({ name, content }) })),
  deleteCareTemplate: (id: string) => request<{ deleted: boolean }>('/admin/care-plan-templates/' + encodeURIComponent(id), adminRequest({ method: 'DELETE' })),
  careTemplateLibrary: () => request<CarePlanTemplate[]>('/admin/care-plan-templates/library', adminRequest()),
  shareCareTemplate: (id: string, clinicId: string) => request('/admin/care-plan-templates/' + encodeURIComponent(id) + '/share', adminRequest({ method: 'POST', body: JSON.stringify({ clinicId }) })),
  applyCareTemplate: (id: string, appointmentId: string, expectedVersion: number) => request('/admin/care-plan-templates/' + encodeURIComponent(id) + '/apply', adminRequest({ method: 'POST', body: JSON.stringify({ appointmentId, expectedVersion }) })),
  adminCareReminders: (patientId: string) => request('/admin/patients/' + encodeURIComponent(patientId) + '/care-reminders', adminRequest()),
  createCareReminder: (patientId: string, body: unknown) => request('/admin/patients/' + encodeURIComponent(patientId) + '/care-reminders', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  handoverBatches: (page = 1) => request('/admin/handover-batches' + query({ page, pageSize: 20 }), adminRequest()),
  createHandoverBatch: (body: { taskIds: string[]; toStaffId: string; reason: string }) => request('/admin/handover-batches', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  nudgeHandoverBatch: (batchId: string) => request('/admin/handover-batches/' + encodeURIComponent(batchId) + '/nudge', adminRequest({ method: 'POST' })),
  recoveryStagePlans: (patientId: string) => request('/admin/patients/' + encodeURIComponent(patientId) + '/recovery-stage-plans', adminRequest()),
  createRecoveryStagePlan: (patientId: string, body: { appointmentId: string; startDate: string }) => request('/admin/patients/' + encodeURIComponent(patientId) + '/recovery-stage-plans', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  checkRecoveryStagePlans: () => request('/admin/recovery-stage-plans/check', adminRequest({ method: 'POST' })),
  adminClinics: () => request<ClinicAdmin[]>('/admin/clinics', adminRequest()),
  adminDoctors: () => request<DoctorAdmin[]>('/admin/doctors', adminRequest()),
  reviewDoctor: (id: string, version: number, status: 'approved' | 'rejected', note: string) => request<DoctorAdmin>(`/admin/doctors/${encodeURIComponent(id)}/review`, adminRequest({ method: 'POST', body: JSON.stringify({ version, status, note }) })),
  saveClinic: (body: ClinicInput, id?: string, version?: number) => request<ClinicAdmin>(`/admin/clinics${id ? '/' + encodeURIComponent(id) : ''}`, adminRequest({ method: id ? 'PUT' : 'POST', body: JSON.stringify({ ...body, ...(id ? { version } : {}) }) })),
  saveDoctor: (body: DoctorInput, id?: string, version?: number) => request<DoctorAdmin>(`/admin/doctors${id ? '/' + encodeURIComponent(id) : ''}`, adminRequest({ method: id ? 'PUT' : 'POST', body: JSON.stringify({ ...body, ...(id ? { version } : {}) }) })),
  directoryAction: (kind: 'clinics' | 'doctors', id: string, version: number, active?: boolean) => request<ClinicAdmin | DoctorAdmin>(`/admin/${kind}/${encodeURIComponent(id)}/${active === undefined ? 'publish' : 'status'}`, adminRequest({ method: 'POST', body: JSON.stringify({ version, ...(active === undefined ? {} : { active }) }) })),
  directoryHistory: (kind: 'clinics' | 'doctors', id: string) => request<DirectoryRevision[]>(`/admin/${kind}/${encodeURIComponent(id)}/history`, adminRequest()),
  linkDoctorEmployee: (id: string, employeeId: string, version: number, employeeVersion: number) => request<Employee>(`/admin/doctors/${encodeURIComponent(id)}/employee`, adminRequest({ method: 'POST', body: JSON.stringify({ employeeId, version, employeeVersion }) })),
  cases: (page = 1, status = '', category = '', tag = '') => requestEnvelope<{ data: components['schemas']['CaseView'][]; meta: components['schemas']['PageMeta'] }>(`/admin/cases${query({ page, pageSize: 20, status, category, tag })}`, adminRequest()),
  caseDetail: (id: string) => request<components['schemas']['CaseView']>(`/admin/cases/${encodeURIComponent(id)}`, adminRequest()),
  createCase: (body: components['schemas']['CaseCreate']) => request<components['schemas']['CaseView']>('/admin/cases', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  updateCase: (id: string, body: components['schemas']['CaseUpdate']) => request<components['schemas']['CaseView']>(`/admin/cases/${encodeURIComponent(id)}`, adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  caseAction: (id: string, action: 'review' | 'publish' | 'unpublish', body: components['schemas']['CaseReview'] | components['schemas']['CaseVersion']) => request<components['schemas']['CaseView']>(`/admin/cases/${encodeURIComponent(id)}/${action}`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  caseHistory: (id: string) => request<components['schemas']['CaseEvent'][]>(`/admin/cases/${encodeURIComponent(id)}/history`, adminRequest()),
  casePhotoFile: async (caseId: string, imageId: string, signal?: AbortSignal) => {
    const response = await fetch(`/api/v1/admin/cases/${encodeURIComponent(caseId)}/images/${encodeURIComponent(imageId)}/content`, { ...adminRequest(), signal })
    if (!response.ok) throw new ApiError(response.status, 'PHOTO_DENIED', '案例图片暂不可读取，请刷新案例。')
    return URL.createObjectURL(await response.blob())
  },
  messageTemplates: () => request<components['schemas']['MessageTemplate'][]>('/admin/messages/templates', adminRequest()),
  saveMessageTemplate: (data: components['schemas']['MessageTemplateChange']) => request<components['schemas']['MessageTemplate']>('/admin/messages/templates', adminRequest({ method: 'PUT', body: JSON.stringify(data) })),
  notificationQueue: (status = '', page = 1, failureCategory = '') => requestEnvelope<{ data: components['schemas']['Notification'][]; meta: { total: number } }>('/admin/messages/queue' + query({ status: status || undefined, failureCategory: failureCategory || undefined, page, pageSize: 20 }), adminRequest()),
  notificationMergeSetting: () => request<components['schemas']['NotificationMergeSetting']>('/admin/messages/merge-settings', adminRequest()),
  notificationBacklogSetting: () => request<components['schemas']['NotificationBacklogSetting']>('/admin/messages/backlog-settings', adminRequest()),
  saveNotificationBacklogSetting: (body: components['schemas']['NotificationBacklogSettingUpdate']) => request<components['schemas']['NotificationBacklogSetting']>('/admin/messages/backlog-settings', adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  notificationBacklog: () => request<components['schemas']['NotificationBacklog']>('/admin/messages/backlog', adminRequest()),
  saveNotificationMergeSetting: (body: components['schemas']['NotificationMergeSettingUpdate']) => request<components['schemas']['NotificationMergeSetting']>('/admin/messages/merge-settings', adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  notificationAttempts: (id: string) => request<components['schemas']['NotificationAttempt'][]>('/admin/messages/queue/' + encodeURIComponent(id) + '/attempts', adminRequest()),
  retryNotification: (id: string) => request('/admin/messages/queue/' + encodeURIComponent(id) + '/retry', adminRequest({ method: 'POST' })),
  createSlots: (slots: components['schemas']['SlotCreate'][]) => request<Slot[]>('/admin/slots', adminRequest({ method: 'POST', body: JSON.stringify({ slots }) })),
  previewSlotBatch: (body: components['schemas']['SlotBatchGenerate']) => request<components['schemas']['SlotBatchPreview']>('/admin/slots/batch-preview', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  createSlotBatch: (body: components['schemas']['SlotBatchGenerate']) => request<components['schemas']['SlotBatchResult']>('/admin/slots/batch-create', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  suspendSlots: (slots: components['schemas']['SlotReference'][], reason: string) => request<Slot[]>('/admin/slots/suspend', adminRequest({ method: 'POST', body: JSON.stringify({ slots, reason }) })),
  // 排班模板与节假日：模板一键套用生成排班，节假日当天自动停诊。
  scheduleTemplates: (clinicId: string) => request<components['schemas']['ScheduleTemplateRecord'][]>('/admin/schedule-templates' + query({ clinicId }), adminRequest()),
  createScheduleTemplate: (body: { clinicId: string } & components['schemas']['ScheduleTemplateInput']) => request<components['schemas']['ScheduleTemplateRecord']>('/admin/schedule-templates', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  updateScheduleTemplate: (id: string, body: components['schemas']['ScheduleTemplateInput']) => request<components['schemas']['ScheduleTemplateRecord']>('/admin/schedule-templates/' + encodeURIComponent(id), adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  deleteScheduleTemplate: (id: string) => request('/admin/schedule-templates/' + encodeURIComponent(id), adminRequest({ method: 'DELETE' })),
  previewTemplateApply: (id: string, body: components['schemas']['TemplateApplyInput']) => request<components['schemas']['TemplateApplyPreview']>('/admin/schedule-templates/' + encodeURIComponent(id) + '/apply-preview', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  applyScheduleTemplate: (id: string, body: components['schemas']['TemplateApplyInput']) => request<components['schemas']['TemplateApplyResult']>('/admin/schedule-templates/' + encodeURIComponent(id) + '/apply', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  holidays: (from: string, to: string) => request<components['schemas']['Holiday'][]>('/admin/holidays' + query({ from, to }), adminRequest()),
  createHoliday: (body: components['schemas']['HolidayInput']) => request<components['schemas']['Holiday']>('/admin/holidays', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  deleteHoliday: (date: string) => request('/admin/holidays/' + encodeURIComponent(date), adminRequest({ method: 'DELETE' })),
  // 到诊指引：主管按院区配置面诊流程指引文案，到诊后自动推送给患者。
  arrivalGuidance: (clinicId: string) => request<components['schemas']['ArrivalGuidanceSetting']>('/admin/arrival-guidance' + query({ clinicId }), adminRequest()),
  saveArrivalGuidance: (body: components['schemas']['ArrivalGuidanceInput']) => request<components['schemas']['ArrivalGuidanceSetting']>('/admin/arrival-guidance', adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  // 模型效果抽检与人工评分回流。
  sampleQualityAssessments: (body: { clinicId?: string; count: number }) => request<components['schemas']['QualitySampleItem'][]>('/admin/assessments/quality-sample', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  saveQualityScore: (assessmentId: string, body: components['schemas']['QualityScoreInput']) => request<components['schemas']['QualityScore']>(`/admin/assessments/${encodeURIComponent(assessmentId)}/quality-score`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  qualityReport: (modelVersion = '') => request<components['schemas']['QualityReport']>('/admin/assessments/quality-report' + query({ modelVersion: modelVersion || undefined }), adminRequest()),
  patients: (search = '', page = 1, tag = '', phoneLast4 = '') => requestEnvelope<components['schemas']['PatientDirectoryPage']>('/admin/patients' + query({ search, page, tag, phoneLast4, pageSize: 20 }), adminRequest()),
  createPatientChart: (body: components['schemas']['PatientChartCreate']) => request<PatientRecordSummary>('/admin/patients', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  createPatientBindingCode: (id: string) => request<components['schemas']['PatientBindingCode']>('/admin/patients/' + encodeURIComponent(id) + '/wechat-binding', adminRequest({ method: 'POST' })),
  revokePatientBindingCode: async (id: string) => {
    const response = await fetch('/api/v1/admin/patients/' + encodeURIComponent(id) + '/wechat-binding', adminRequest({ method: 'DELETE', signal: AbortSignal.timeout(15000) }))
    if (!response.ok) { const body = await response.json().catch(() => null); throw new ApiError(response.status, body?.error?.code || 'REQUEST_FAILED', body?.error?.message || '撤销绑定码失败') }
  },
  savePatientChart: (id: string, body: components['schemas']['PatientChartUpdate']) => request<PatientChart>('/admin/patients/' + encodeURIComponent(id) + '/profile', adminRequest({ method: 'PUT', body: JSON.stringify(body) })),
  consultationTemplates: () => request<ConsultationTemplate[]>('/admin/consultation-templates', adminRequest()),
  createConsultationTemplate: (body: components['schemas']['ConsultationTemplateCreate']) => request<ConsultationTemplate>('/admin/consultation-templates', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  deleteConsultationTemplate: async (id: string) => {
    const response = await fetch('/api/v1/admin/consultation-templates/' + encodeURIComponent(id), adminRequest({ method: 'DELETE' }))
    if (!response.ok) { const body = await response.json().catch(() => null); throw new ApiError(response.status, body?.error?.code || 'REQUEST_FAILED', body?.error?.message || '删除模板失败') }
  },
  patientRecord: (id: string, dates?: { dateFrom: string; dateTo: string }) => request<PatientRecord>('/admin/patients/' + encodeURIComponent(id) + query(dates || {}), adminRequest()),
  createRecordExport: (body: RecordExportInput, signal?: AbortSignal) => request<RecordExportJob>('/admin/record-exports', adminRequest({ method: 'POST', body: JSON.stringify(body), signal })),
  recordExports: (page = 1, signal?: AbortSignal) => requestEnvelope<RecordExportPage>('/admin/record-exports' + query({ page, pageSize: 20 }), adminRequest({ cache: 'no-store', signal })),
  recordExport: (id: string, signal?: AbortSignal) => request<RecordExportJob>('/admin/record-exports/' + encodeURIComponent(id), adminRequest({ cache: 'no-store', signal })),
  recordExportEvents: (id: string, signal?: AbortSignal) => request<RecordExportEvent[]>('/admin/record-exports/' + encodeURIComponent(id) + '/events', adminRequest({ cache: 'no-store', signal })),
  recordExportDownload: async (id: string, signal?: AbortSignal) => {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), 15000)
    try {
      const response = await fetch('/api/v1/admin/record-exports/' + encodeURIComponent(id) + '/download', adminRequest({ cache: 'no-store', signal: signal ? AbortSignal.any([controller.signal, signal]) : controller.signal }))
      if (!response.ok) {
        const body = await response.json().catch(() => null)
        if (response.status === 401) { clearAuth(); window.location.assign('/login') }
        throw new ApiError(response.status, body?.error?.code || 'EXPORT_DOWNLOAD_FAILED', body?.error?.message || '档案下载失败，请刷新任务后重试。')
      }
      return await response.blob()
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === 'AbortError') throw new Error('档案下载已取消或超时，请刷新任务后重试。')
      if (cause instanceof TypeError) throw new Error('暂时无法连接服务，请检查连接后重试。')
      throw cause
    } finally { clearTimeout(timeout) }
  },
  createConsultation: (id: string, body: components['schemas']['ConsultationCreate']) => request<components['schemas']['Consultation']>('/admin/patients/' + encodeURIComponent(id) + '/consultations', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  loginEmployee: (username: string, password: string) => request<{ employee?: Employee; token?: string; requires2FA?: boolean; employeeId?: string }>('/admin/session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password }) }),
  verify2FALogin: (employeeId: string, code: string) => request<{ employee: Employee; token: string }>('/admin/session/2fa', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ employeeId, code }) }),
  employeeSession: () => request<Employee>('/admin/session', adminRequest()),
  logoutEmployee: () => request<{ loggedOut: boolean }>('/admin/session', adminRequest({ method: 'DELETE' })),
  employees: () => request<Employee[]>('/admin/employees', adminRequest()),
  overview: () => request<components['schemas']['OperationsOverview']>('/admin/overview', adminRequest()),
  analytics: (filters: { dateFrom: string; dateTo: string; clinicId?: string; doctorId?: string; groupBy: 'doctor' | 'clinic' | 'day' }) => request<components['schemas']['OperationsAnalyticsReport']>('/admin/analytics' + query(filters), adminRequest()),
  analyticsFunnel: (filters: { dateFrom: string; dateTo: string; clinicId?: string }) => request<components['schemas']['ChannelFunnelReport']>('/admin/analytics/funnel' + query(filters), adminRequest()),
  resolveArrivalScan: (token: string) => request<Appointment>('/admin/arrival-scans/resolve', adminRequest({ method: 'POST', body: JSON.stringify({token}) })),
  confirmArrivalScan: (token: string) => request<Appointment>('/admin/arrival-scans/confirm', adminRequest({ method: 'POST', body: JSON.stringify({token}) })),
  suspensionReschedule: (id: string, body: components['schemas']['SuspensionReschedule']) => request<Appointment>(`/admin/appointments/${encodeURIComponent(id)}/suspension-reschedule`, adminRequest({method:'POST',body:JSON.stringify(body)})),
  remindAppointments: (appointmentIds: string[]) => request<components['schemas']['AppointmentRemindResult'][]>('/admin/appointments/remind', adminRequest({ method: 'POST', body: JSON.stringify({ appointmentIds }) })),
  cancellationStats: (dateFrom: string, dateTo: string, clinicId = '', doctorId = '') => request<components['schemas']['CancellationStats']>(`/admin/appointments/cancellation-stats${query({ dateFrom, dateTo, clinicId, doctorId })}`, adminRequest()),
  registerArrival: (id: string, body: components['schemas']['AppointmentArrival']) => request<Appointment>(`/admin/appointments/${encodeURIComponent(id)}/arrival`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  setEmployeeClinics: (id: string, clinicIds: string[], version: number) => request<Employee>(`/admin/employees/${encodeURIComponent(id)}/clinics`, adminRequest({ method: 'POST', body: JSON.stringify({ clinicIds, version }) })),
  employeeEvents: (id: string) => request<EmployeeEvent[]>(`/admin/employees/${encodeURIComponent(id)}/events`, adminRequest()),
  loginAttempts: (username = '', page = 1) => requestEnvelope<components['schemas']['EmployeeLoginAttemptPage']>(`/admin/employees/login-attempts${query({ username, page, pageSize: 20 })}`, adminRequest()),
  loginAnomalyCount: () => request<{ count: number }>('/admin/employees/login-anomaly-count', adminRequest()),
  createEmployee: (body: { username: string; displayName: string; password: string; role: Employee['role']; clinicIds: string[]; staffId?: string }) => request<Employee>('/admin/employees', adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  setEmployeeActive: (id: string, version: number, active: boolean) => request<Employee>(`/admin/employees/${encodeURIComponent(id)}/status`, adminRequest({ method: 'POST', body: JSON.stringify({ version, active }) })),
  resetEmployeePassword: (id: string, version: number, password: string) => request<Employee>(`/admin/employees/${encodeURIComponent(id)}/password`, adminRequest({ method: 'POST', body: JSON.stringify({ version, password }) })),
  followupTemplates: () => request<FollowupTemplate[]>('/admin/followup-templates', adminRequest()),
  saveFollowupTemplate: (id: string, body: components['schemas']['FollowupTemplateChange']) => request<FollowupTemplate>('/admin/followup-templates' + (id ? '/' + encodeURIComponent(id) : ''), adminRequest({ method: id ? 'PUT' : 'POST', body: JSON.stringify(body) })),
  nursingFeedback: (id: string, dates?: { dateFrom: string; dateTo: string }) => request<NursingFeedback[]>(`/admin/appointments/${encodeURIComponent(id)}/nursing-feedback${query(dates || {})}`, adminRequest()),
  recordNursingFeedback: (id: string, body: components['schemas']['NursingFeedbackCreate']) => request<NursingFeedback>(`/admin/appointments/${encodeURIComponent(id)}/nursing-feedback`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  followupWorkspace: (id: string) => request<FollowupWorkspace>(`/admin/appointments/${encodeURIComponent(id)}/followups`, adminRequest()),
  recordTreatment: (id: string, body: TreatmentCreate) => request<FollowupWorkspace>(`/admin/appointments/${encodeURIComponent(id)}/treatment`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  correctTreatment: (id: string, body: TreatmentCorrection) => request<FollowupWorkspace>(`/admin/appointments/${encodeURIComponent(id)}/treatment/corrections`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  followups: (status = '', page = 1, assigneeId = '', dates?: { dateFrom: string; dateTo: string }) => requestEnvelope<components['schemas']['FollowupTaskList']>(`/admin/followups${query({ status, assigneeId, ...dates, page, pageSize: 20 })}`, adminRequest()),
  todoStats: () => request<components['schemas']['TodoStats']>('/admin/todos/stats', adminRequest()),
  followupStaff: () => request<FollowupStaff[]>('/admin/followup-staff', adminRequest()),
  followupStaffEvents: (id: string) => request<FollowupStaffEvent[]>(`/admin/followup-staff/${encodeURIComponent(id)}/events`, adminRequest()),
  handoffNotifications: (page = 1, pageSize = 20) => requestEnvelope<components['schemas']['HandoffNotificationPage']>(`/admin/handoff-notifications${query({ page, pageSize })}`, adminRequest()),
  readHandoffNotification: (id: string) => request<{ read: boolean }>(`/admin/handoff-notifications/${encodeURIComponent(id)}/read`, adminRequest({ method: 'POST' })),
  appointmentNotifications: (page = 1, pageSize = 20) => requestEnvelope<components['schemas']['AppointmentNotificationPage']>(`/admin/appointment-notifications${query({ page, pageSize })}`, adminRequest()),
  readAppointmentNotification: (id: string) => request<{ read: boolean }>(`/admin/appointment-notifications/${encodeURIComponent(id)}/read`, adminRequest({ method: 'POST' })),
  handoffReceipts: (status: 'pending' | 'overdue' | 'todo' | 'confirmed' | 'superseded' | 'all' = 'pending', page = 1, toStaffId = '', receiptId = '') => requestEnvelope<components['schemas']['HandoffReceiptPage']>(`/admin/handoff-receipts${query({ status, page, pageSize: 20, toStaffId, receiptId })}`, adminRequest()),
  confirmHandoffReceipt: (id: string, version: number) => request<HandoffReceipt>(`/admin/handoff-receipts/${encodeURIComponent(id)}/confirm`, adminRequest({ method: 'POST', body: JSON.stringify({ version }) })),
  handoffFollowups: (id: string) => request<HandoffFollowupEvent[]>(`/admin/handoff-receipts/${encodeURIComponent(id)}/followups`, adminRequest()),
  recordHandoffFollowup: (id: string, body: components['schemas']['HandoffFollowupCreate']) => request<HandoffFollowupEvent>(`/admin/handoff-receipts/${encodeURIComponent(id)}/followups`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  handoffSetting: () => request<HandoffSetting>('/admin/handoff-settings', adminRequest()),
  updateHandoffSetting: (overdueHours: number, version: number) => request<HandoffSetting>('/admin/handoff-settings', adminRequest({ method: 'PUT', body: JSON.stringify({ overdueHours, version }) })),
  leaveFollowupStaff: (id: string, body: components['schemas']['FollowupLeave']) => request<components['schemas']['FollowupLeaveResult']['data']>(`/admin/followup-staff/${encodeURIComponent(id)}/leave`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  returnFollowupStaff: (id: string, version: number) => request<FollowupStaff>(`/admin/followup-staff/${encodeURIComponent(id)}/return`, adminRequest({ method: 'POST', body: JSON.stringify({ version }) })),
  assignFollowup: (id: string, body: components['schemas']['FollowupAssignment']) => request<FollowupTask>(`/admin/followups/${encodeURIComponent(id)}/assignment`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  followupAssignmentHistory: (id: string) => request<FollowupAssignmentEvent[]>(`/admin/followups/${encodeURIComponent(id)}/assignments`, adminRequest()),
  reviewFollowup: (id: string, body: components['schemas']['FollowupReview']) => request<FollowupTask>(`/admin/followups/${encodeURIComponent(id)}/review`, adminRequest({ method: 'POST', body: JSON.stringify(body) })),
  followupPhotoFile: async (id: string, signal?: AbortSignal) => {
    const response = await fetch(`/api/v1/admin/followup-photos/${encodeURIComponent(id)}/content`, adminRequest({ cache: 'no-store', signal }))
    if (!response.ok) throw new Error('随访照片无法读取，请刷新核对患者授权。')
    return URL.createObjectURL(await response.blob())
  },
  clinics: () => request<Clinic[]>('/clinics'),
  doctors: (clinicId?: string) => request<Doctor[]>(`/doctors${query({ clinicId })}`),
  slots: (filters: { clinicId?: string; doctorId?: string; date?: string }) => request<Slot[]>(`/slots${query(filters)}`),
  updateSlot: (slotId: string, values: Pick<Slot, 'capacity' | 'status' | 'version'>) => request<Slot>(`/admin/slots/${encodeURIComponent(slotId)}`, adminRequest({
    method: 'PATCH',
    body: JSON.stringify(values),
  })),
  appointments: (filters: { clinicId?: string; arrival?: 'expected' | 'arrived' | ''; needsReschedule?: 'true'; q?: string; status?: Appointment['status'] | ''; date?: string; doctorId?: string; page?: number; pageSize?: number }) => requestEnvelope<AppointmentPage>(`/admin/appointments${query(filters)}`, adminRequest()),
  appointment: (id: string) => request<Appointment>(`/admin/appointments/${encodeURIComponent(id)}`, adminRequest()),
  waitlistQueue: (slotId: string) => request<components['schemas']['WaitlistEntry'][]>(`/admin/waitlist${query({ slotId })}`, adminRequest()),
  doctorReviews: (status = '') => request<components['schemas']['DoctorReview'][]>(`/admin/reviews${query({ status })}`, adminRequest()),
  decideDoctorReview: (id: string, decision: 'approved' | 'rejected', note?: string) => request<components['schemas']['DoctorReview']>(`/admin/reviews/${encodeURIComponent(id)}/review`, adminRequest({ method: 'POST', body: JSON.stringify({ decision, ...(note ? { note } : {}) }) })),
  sweepWaitlist: () => request<components['schemas']['WaitlistSweepResult']['data']>('/admin/waitlist', adminRequest({ method: 'POST' })),
  reviewAppointment: (id: string, values: AppointmentReview) => request<Appointment>(`/admin/appointments/${encodeURIComponent(id)}/review`, adminRequest({
    method: 'POST',
    body: JSON.stringify(values),
  })),
  carePhotos: (id: string) => request<CarePhoto[]>(`/admin/appointments/${encodeURIComponent(id)}/photos`, adminRequest()),
  carePhotoFile: async (id: string, signal?: AbortSignal) => {
    const response = await fetch(`/api/v1/admin/photos/${encodeURIComponent(id)}/content`, adminRequest({ cache: 'no-store', signal }))
    if (!response.ok) {
      const body = await response.json().catch(() => null)
      throw new ApiError(response.status, body?.error?.code || 'PHOTO_READ_FAILED', body?.error?.message || '照片读取失败，请重新加载。')
    }
    return URL.createObjectURL(await response.blob())
  },
  carePlan: async (id: string) => (await request<components['schemas']['CarePlanWorkspace']>(`/admin/appointments/${encodeURIComponent(id)}/care-plan`, adminRequest())),
  saveCarePlanDraft: (id: string, version: number, content: CarePlanContent) => request<CarePlanWorkspace>(`/admin/appointments/${encodeURIComponent(id)}/care-plan/draft`, adminRequest({ method: 'PUT', body: JSON.stringify({ version, content }) })),
  publishCarePlan: (id: string, version: number, changeReason: string) => request<CarePlanVersion>(`/admin/appointments/${encodeURIComponent(id)}/care-plan/publish`, adminRequest({ method: 'POST', body: JSON.stringify({ version, changeReason }) })),
}

export const errorMessage = (error: unknown) => error instanceof Error ? error.message : '发生了未知错误，请重试。'
