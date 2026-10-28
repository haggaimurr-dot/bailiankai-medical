<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, errorMessage, type Appointment, type FollowupTask, type FollowupWorkspace, type FollowupStaff, type FollowupAssignmentEvent, type FollowupStaffEvent, type HandoffReceipt, type HandoffSetting, type HandoffFollowupEvent } from '../api'
import { auth, type Employee } from '../auth'
import FollowupDays from '../components/FollowupDays.vue'
import FollowupTools from '../components/FollowupTools.vue'
import type { FollowupTemplate } from '../api'
import PrivatePhoto from '../components/PrivatePhoto.vue'

const props = defineProps<{ handoffOnly?: boolean }>()
const route = useRoute()
const handoffSection = ref('receipts')
const receiptIdFilter = ref(typeof route.query.receiptId === 'string' ? route.query.receiptId : '')
const taskClinicId = ref('')
const dates = ref<[string, string] | null>(null)
const templates = ref<FollowupTemplate[]>([]), chosenTemplate = ref('')
const selectedStandard = computed(() => availableTemplates.value.find(item => item.id === chosenTemplate.value))
const availableTemplates = computed(() => templates.value.filter(item => item.active && item.standardStatus === 'formal' && item.clinicId === (selectedAppointment.value?.clinicId || taskClinicId.value)))
function applyTemplate(kind: 'days' | 'response') {
  const item = availableTemplates.value.find(item => item.id === chosenTemplate.value)
  if (!item) return
  if (kind === 'days') offsets.value = [...item.offsets]
  else { response.value = item.response; disposition.value = item.disposition }
}
// 常用治疗摘要模板（P2 治疗-后台）：内置常用文案，选填入摘要后可再编辑；
// 模板内容如需由院方自行维护，另立管理入口，本条只做录入表单选用。
const treatmentSummaryTemplates = [
  { name: '痘坑痘印修复', text: '今日行痘坑痘印修复治疗，治疗区域为面部痘坑痘印处。治疗过程顺利，患者耐受良好，无明显不适。治疗后局部轻微泛红，属正常反应。已交代术后护理要点，嘱按期复诊评估恢复情况。' },
  { name: '痘印管理', text: '完成面部痘印管理治疗 1 次，能量参数按皮肤耐受调整。治疗过程顺利，患者耐受良好。治疗后皮肤轻微泛红，已做镇静处理。已交代防晒与保湿护理，嘱按期复诊。' },
  { name: '光子嫩肤', text: '今日行光子嫩肤治疗，治疗区域为全面部。治疗过程顺利，患者耐受良好。治疗后皮肤轻微发热泛红，已冷敷处理。已交代防晒与保湿护理，嘱两周后复诊。' },
  { name: '水光补水', text: '今日行水光补水治疗，治疗区域为全面部。治疗过程顺利，患者耐受良好。治疗后针眼处轻微泛红，已做镇静处理。已交代 24 小时内避免沾水与化妆，嘱一周后复诊。' },
]
const chosenSummaryTemplate = ref('')
function applySummaryTemplate() {
  const item = treatmentSummaryTemplates.find(item => item.name === chosenSummaryTemplate.value)
  if (item) summary.value = item.text
}
async function loadTemplates() { try { templates.value = await api.followupTemplates() } catch (cause) { error.value = errorMessage(cause) } }
function dateFilter() { page.value = 1; void load() }

const mode = ref('queue'), status = ref(typeof route.query.status === 'string' && ['submitted', 'alert', 'overdue'].includes(route.query.status) ? route.query.status : ''), treatmentStatus = ref<'confirmed' | 'completed'>('confirmed'), page = ref(1), total = ref(0)
const tasks = ref<FollowupTask[]>([]), appointments = ref<Appointment[]>([])
const appointmentPage = ref(1), appointmentTotal = ref(0)
const selectedAppointment = ref<Appointment | null>(null), selectedTask = ref<FollowupTask | null>(null)
const workspace = ref<FollowupWorkspace>({ treatment: null, treatmentRevisions: [], tasks: [] })
const plans = ref<{ id: string; version: number }[]>([])
const emptyTreatmentFields = () => ({ stageIndex: 0, procedure: '', bodyArea: '', device: '', parameters: '' })
const treatmentFields = ref(emptyTreatmentFields())
const planVersionId = ref(''), summary = ref(''), notes = ref(''), offsets = ref<number[]>([1, 3, 7])
const treatedAt = ref(localTime()), response = ref(''), disposition = ref('')
const correctionOpen = ref(false), correctionReason = ref('')
const loading = ref(false), detailLoading = ref(false), saving = ref(false), error = ref(''), photoNotice = ref('')
const photos = ref<{ id: string; angle: string }[]>([])
const staff = ref<FollowupStaff[]>([]), assigneeFilter = ref(''), treatmentAssigneeId = ref('')
const assignmentTargetId = ref(''), assignmentReason = ref(''), assignmentHistory = ref<FollowupAssignmentEvent[]>([])
const leaveStaffId = ref(''), handoffToId = ref(''), handoffReason = ref(''), staffHistory = ref<FollowupStaffEvent[]>([])
const employeeAccounts = ref<Employee[]>([]), receipts = ref<HandoffReceipt[]>([])
const receiptStatus = ref<'pending' | 'overdue' | 'todo' | 'confirmed' | 'superseded' | 'all'>(auth.employee?.role === 'manager' ? 'todo' : 'pending'), receiptToStaffId = ref(''), receiptPage = ref(1), receiptTotal = ref(0), receiptOverdueTotal = ref(0), receiptTodoTotal = ref(0), receiptOverdueHours = ref(24), receiptLoading = ref(false), confirmingReceiptId = ref('')
const handoffSetting = ref<HandoffSetting | null>(null), overdueHoursDraft = ref(24), settingSaving = ref(false)
const openReceiptId = ref(''), handoffFollowups = ref<HandoffFollowupEvent[]>([]), handoffFollowupsLoading = ref(false), followupSaving = ref(false)
const followupAction = ref(''), followupResult = ref(''), followupStatusAfter = ref<'awaiting_reply' | 'needs_action' | 'resolved'>('needs_action'), followupDraftKey = ref(''), followupDraftAction = ref(''), followupDraftResult = ref(''), followupDraftStatus = ref(''), followupDraftVersion = ref(0)
const followupStatusLabels: Record<string, string> = { untracked: '未跟进', awaiting_reply: '待回复', needs_action: '需处理', resolved: '已解决' }
const canTreat = computed(() => auth.employee?.role === 'manager' || staff.value.some(item => item.id === auth.employee?.staffId && item.role === 'doctor'))
const eligibleRecipients = computed(() => staff.value.filter(item => item.status === 'on_duty' && employeeAccounts.value.some(account => account.active && account.staffId === item.id)))
const labels: Record<string, string> = { pending: '待提交', submitted: '待回评', alert: '异常待处理', reviewed: '已回评', handled: '异常已处理', overdue: '逾期未提交' }
const severityLabels = ['没有不适', '轻微不适', '明显不适', '严重不适']
let generation = 0, listGeneration = 0, receiptGeneration = 0, timer: ReturnType<typeof setInterval> | undefined
if (['pending', 'overdue', 'todo', 'confirmed', 'superseded', 'all'].includes(String(route.query.status)) && props.handoffOnly) receiptStatus.value = route.query.status as typeof receiptStatus.value
function localTime(value: string | Date = new Date()) { return new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value)).replace(' ', 'T') }
function timestamp(value: string) { return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
// Abnormal (alert) tasks share the followup due date as their handling
// deadline: within 24h is "临近处理时限", past due is "处理已超时".
const alertDeadlineHours = 24
function alertDeadline(task: FollowupTask) {
  if (task.status !== 'alert') return ''
  const ms = Date.parse(task.dueAt) - Date.now()
  if (ms < 0) return 'overdue'
  if (ms <= alertDeadlineHours * 3600 * 1000) return 'soon'
  return ''
}
function alertDeadlineLabel(task: FollowupTask) {
  const state = alertDeadline(task)
  if (state === 'overdue') return '处理已超时'
  if (state === 'soon') {
    const hours = Math.max(1, Math.ceil((Date.parse(task.dueAt) - Date.now()) / 3600000))
    return `临近处理时限（${hours} 小时后到期）`
  }
  return ''
}
function releasePhotos() { photos.value = [] }
async function load() {
  const request = ++listGeneration
  loading.value = true; error.value = ''
  try {
    if (mode.value === 'templates' || mode.value === 'nursing') { loading.value = false; return }
    if (mode.value === 'queue') {
      const result = await api.followups(status.value, page.value, assigneeFilter.value, dates.value ? { dateFrom: dates.value[0], dateTo: dates.value[1] } : undefined)
      if (request !== listGeneration) return
      tasks.value = result.data; total.value = result.meta.total
    } else {
      const result = await api.appointments({ status: treatmentStatus.value, doctorId: auth.employee?.role === 'clinician' ? auth.employee.staffId || undefined : undefined, page: appointmentPage.value, pageSize: 20 })
      if (request !== listGeneration) return
      appointments.value = result.data; appointmentTotal.value = result.meta.total
    }
  } catch (cause) { if (request === listGeneration) error.value = errorMessage(cause) }
  finally { if (request === listGeneration) loading.value = false }
}
async function loadStaff() {
  try {
    if (auth.employee?.role === 'manager') {
      const [people, accounts] = await Promise.all([api.followupStaff(), api.employees()])
      staff.value = people; employeeAccounts.value = accounts
    } else staff.value = await api.followupStaff()
    if (!staff.value.some(item => item.id === leaveStaffId.value)) leaveStaffId.value = staff.value[0]?.id || ''
    if (!eligibleRecipients.value.some(item => item.id === handoffToId.value)) handoffToId.value = ''
    if (leaveStaffId.value && auth.employee?.role === 'manager') await loadStaffHistory()
  } catch (cause) { error.value = errorMessage(cause) }
}
async function loadReceipts() {
  const request = ++receiptGeneration
  receiptLoading.value = true
  try {
    const result = await api.handoffReceipts(receiptStatus.value, receiptPage.value, auth.employee?.role === 'manager' ? receiptToStaffId.value : '', receiptIdFilter.value)
    if (request !== receiptGeneration) return
    receipts.value = result.data; receiptTotal.value = result.meta.total; receiptOverdueTotal.value = result.meta.overdueTotal; receiptTodoTotal.value = result.meta.todoTotal; receiptOverdueHours.value = result.meta.overdueHours
  } catch (cause) { if (request === receiptGeneration) error.value = errorMessage(cause) }
  finally { if (request === receiptGeneration) receiptLoading.value = false }
}
async function loadHandoffSetting() {
  if (auth.employee?.role !== 'manager') return
  try { handoffSetting.value = await api.handoffSetting(); overdueHoursDraft.value = handoffSetting.value.overdueHours }
  catch (cause) { error.value = errorMessage(cause) }
}
async function saveHandoffSetting() {
  if (!handoffSetting.value || settingSaving.value) return
  if (!Number.isInteger(overdueHoursDraft.value) || overdueHoursDraft.value < 1 || overdueHoursDraft.value > 720) { error.value = '交接超时时限须为 1–720 小时。'; return }
  settingSaving.value = true; error.value = ''
  try {
    handoffSetting.value = await api.updateHandoffSetting(overdueHoursDraft.value, handoffSetting.value.version)
    receiptPage.value = 1; await loadReceipts()
    ElMessage.success('交接超时时限已更新')
  } catch (cause) { error.value = errorMessage(cause) + ' 请刷新设置后重试。'; await loadHandoffSetting() }
  finally { settingSaving.value = false }
}
function refreshCurrent() {
  if (!props.handoffOnly) return load()
  if (handoffSection.value === 'staff') return loadStaff()
  if (handoffSection.value === 'settings') return loadHandoffSetting()
  return loadReceipts()
}
function showOverdueReceipts() { receiptStatus.value = 'overdue'; receiptPage.value = 1; void loadReceipts() }
function showSupervisorTodo() { receiptStatus.value = 'todo'; receiptPage.value = 1; void loadReceipts() }
function canRecordFollowup(receipt: HandoffReceipt) {
  if (receipt.overdue || receipt.followupCount > 0) return true
  const endedAt = receipt.status === 'confirmed' ? receipt.confirmedAt : receipt.status === 'superseded' ? receipt.supersededAt : null
  return !!endedAt && new Date(endedAt).getTime() - new Date(receipt.createdAt).getTime() > receiptOverdueHours.value * 3600000
}
async function openReceiptFollowups(receipt: HandoffReceipt) {
  if (openReceiptId.value === receipt.id) { openReceiptId.value = ''; return }
  openReceiptId.value = receipt.id; handoffFollowups.value = []; followupAction.value = ''; followupResult.value = ''; followupStatusAfter.value = receipt.followupStatus === 'awaiting_reply' ? 'awaiting_reply' : 'needs_action'; followupDraftKey.value = ''; handoffFollowupsLoading.value = true
  try {
    const rows = await api.handoffFollowups(receipt.id)
    if (openReceiptId.value === receipt.id) handoffFollowups.value = rows
  } catch (cause) { error.value = errorMessage(cause) }
  finally { if (openReceiptId.value === receipt.id) handoffFollowupsLoading.value = false }
}
async function saveReceiptFollowup(receipt: HandoffReceipt) {
  if (followupSaving.value || openReceiptId.value !== receipt.id) return
  const actionText = followupAction.value.trim(), resultText = followupResult.value.trim()
  if (!actionText || !resultText || actionText.length > 500 || resultText.length > 1000) { error.value = '请填写跟进措施（最多 500 字）和处理结果（最多 1000 字）。'; return }
  if (!followupDraftKey.value || followupDraftAction.value !== actionText || followupDraftResult.value !== resultText || followupDraftStatus.value !== followupStatusAfter.value) {
    followupDraftKey.value = crypto.randomUUID(); followupDraftAction.value = actionText; followupDraftResult.value = resultText; followupDraftStatus.value = followupStatusAfter.value; followupDraftVersion.value = receipt.followupVersion
  }
  followupSaving.value = true; error.value = ''
  try {
    await api.recordHandoffFollowup(receipt.id, { requestKey: followupDraftKey.value, actionText, resultText, statusAfter: followupStatusAfter.value, followupVersion: followupDraftVersion.value })
    const rows = await api.handoffFollowups(receipt.id)
    if (openReceiptId.value === receipt.id) handoffFollowups.value = rows
    followupAction.value = ''; followupResult.value = ''; followupDraftKey.value = ''
    await loadReceipts()
    ElMessage.success('跟进记录已保存')
  } catch (cause) { error.value = errorMessage(cause) + ' 如为网络中断，可直接重试；若其他主管已更新，请刷新交接清单。' }
  finally { followupSaving.value = false }
}
async function confirmReceipt(receipt: HandoffReceipt) {
  if (confirmingReceiptId.value || receipt.status !== 'pending' || receipt.toStaffId !== auth.employee?.staffId) return
  confirmingReceiptId.value = receipt.id; error.value = ''
  try {
    await api.confirmHandoffReceipt(receipt.id, receipt.version)
    await loadReceipts()
    ElMessage.success('已确认收到随访任务')
  } catch (cause) { error.value = errorMessage(cause) + ' 请刷新交接清单后重试。' }
  finally { confirmingReceiptId.value = '' }
}
async function loadStaffHistory() {
  if (!leaveStaffId.value) { staffHistory.value = []; return }
  try { staffHistory.value = await api.followupStaffEvents(leaveStaffId.value) }
  catch (cause) { error.value = errorMessage(cause) }
}
function changeMode() { generation++; selectedTask.value = null; selectedAppointment.value = null; releasePhotos(); void load() }
function changeTreatmentStatus() { generation++; appointmentPage.value = 1; selectedAppointment.value = null; void load() }
async function selectAppointment(appointment: Appointment) {
  if (saving.value) return
  const request = ++generation
  chosenTemplate.value = ''; chosenSummaryTemplate.value = ''; offsets.value = [1, 3, 7]; selectedAppointment.value = appointment; selectedTask.value = null; detailLoading.value = true; error.value = ''; releasePhotos()
  workspace.value = { treatment: null, treatmentRevisions: [], tasks: [] }; plans.value = []; treatmentFields.value = emptyTreatmentFields(); planVersionId.value = ''; summary.value = ''; notes.value = ''; treatedAt.value = localTime(); correctionOpen.value = false; correctionReason.value = ''; chosenSummaryTemplate.value = ''
  treatmentAssigneeId.value = staff.value.find(item => item.id === appointment.doctorId && item.status === 'on_duty')?.id || staff.value.find(item => item.status === 'on_duty')?.id || ''
  try {
    const [data, plan] = await Promise.all([api.followupWorkspace(appointment.id), api.carePlan(appointment.id)])
    if (request !== generation) return
    workspace.value = data; plans.value = plan.versions; planVersionId.value = data.treatment?.planVersionId || plan.versions[0]?.id || ''
    if (data.treatment) { treatmentFields.value = { stageIndex: data.treatment.stageIndex || 0, procedure: data.treatment.procedure || '', bodyArea: data.treatment.bodyArea || '', device: data.treatment.device || '', parameters: data.treatment.parameters || '' }; summary.value = data.treatment.summary; notes.value = data.treatment.notes; treatedAt.value = localTime(data.treatment.treatedAt) }
  } catch (cause) { if (request === generation) error.value = errorMessage(cause) }
  finally { if (request === generation) detailLoading.value = false }
}
async function record() {
  const appointment = selectedAppointment.value
  if (!appointment || saving.value) return
  const dayOffsets = [...offsets.value].sort((a, b) => a - b)
  if (!summary.value.trim() || !planVersionId.value || !treatmentAssigneeId.value || !treatedAt.value || !dayOffsets.length || dayOffsets.length > 8 || dayOffsets.some(n => !Number.isInteger(n) || n < 1 || n > 90) || new Set(dayOffsets).size !== dayOffsets.length) { error.value = '请填写治疗摘要、在岗负责人、已发布方案和 1–90 的不重复随访天数。'; return }
  const date = new Date(treatedAt.value + '+08:00')
  if (!Number.isFinite(date.getTime()) || date.getTime() > Date.now()) { error.value = '实际治疗时间不能在未来。'; return }
  saving.value = true; error.value = ''
  try {
    await ElMessageBox.confirm('确认实际治疗已发生？保存后会生成随访任务。后续更正须填写原因并保留版本历史。', '记录实际治疗', { confirmButtonText: '确认并生成任务', cancelButtonText: '继续编辑' })
    workspace.value = await api.recordTreatment(appointment.id, { ...treatmentFields.value, version: appointment.version, planVersionId: planVersionId.value, assigneeId: treatmentAssigneeId.value, summary: summary.value.trim(), notes: notes.value.trim(), treatedAt: date.toISOString(), offsets: dayOffsets })
    chosenSummaryTemplate.value = ''; treatmentStatus.value = 'completed'; appointmentPage.value = 1; await load()
    ElMessage.success('已记录治疗并生成随访任务')
  } catch (cause) {
    if (cause !== 'cancel' && cause !== 'close') error.value = errorMessage(cause)
  } finally { saving.value = false }
}
async function correct() {
  const appointment = selectedAppointment.value, treatment = workspace.value.treatment
  if (!appointment || !treatment || saving.value) return
  const reason = correctionReason.value.trim(), text = summary.value.trim(), date = new Date(treatedAt.value + '+08:00')
  if (!reason || reason.length > 300 || !text || text.length > 1000 || notes.value.trim().length > 2000 || !planVersionId.value || !Number.isFinite(date.getTime()) || date.getTime() > Date.now()) { error.value = '请填写更正原因、治疗摘要、已发布方案和有效的实际治疗时间。'; return }
  saving.value = true; error.value = ''
  try {
    await ElMessageBox.confirm('确认更正治疗记录？原始内容和每次更正都会保留。若随访已有反馈或照片，实际治疗时间不能调整。', '更正治疗记录', { confirmButtonText: '确认更正', cancelButtonText: '继续编辑' })
    workspace.value = await api.correctTreatment(appointment.id, { ...treatmentFields.value, version: treatment.version, planVersionId: planVersionId.value, summary: text, notes: notes.value.trim(), treatedAt: date.toISOString(), correctionReason: reason })
    correctionOpen.value = false; correctionReason.value = ''
    ElMessage.success('治疗记录已更正，历史版本已保留')
  } catch (cause) {
    if (cause !== 'cancel' && cause !== 'close') error.value = errorMessage(cause) + ' 请刷新记录核对最新版本。'
  } finally { saving.value = false }
}
async function selectTask(task: FollowupTask, preserve = false) {
  if (saving.value) return
  const request = ++generation
  if (!preserve) { chosenTemplate.value = ''; taskClinicId.value = ''; void api.appointment(task.appointmentId).then(item => { if (selectedTask.value?.id === task.id) taskClinicId.value = item.clinicId }).catch(cause => { if (selectedTask.value?.id === task.id) error.value = errorMessage(cause) }) }
  selectedTask.value = task; selectedAppointment.value = null; detailLoading.value = true; error.value = ''; photoNotice.value = ''; releasePhotos()
  if (!preserve) { response.value = ''; disposition.value = '' }
  try {
    const [data, history] = await Promise.all([api.followupWorkspace(task.appointmentId), api.followupAssignmentHistory(task.id)])
    if (request !== generation) return
    workspace.value = data
    const latest = data.tasks.find(item => item.id === task.id)
    if (!latest) throw new Error('任务已更新，请刷新队列。')
    selectedTask.value = latest
    assignmentHistory.value = history
    if (!preserve) assignmentTargetId.value = latest.assigneeId
    photos.value = latest.photos.map(photo => ({ id: photo.id, angle: photo.angle }))
  } catch (cause) { if (request === generation) error.value = errorMessage(cause) }
  finally { if (request === generation) detailLoading.value = false }
}
async function assignTask() {
  const task = selectedTask.value
  if (!task || saving.value) return
  if (!assignmentTargetId.value || assignmentTargetId.value === task.assigneeId || !assignmentReason.value.trim()) { error.value = '请选择不同的在岗负责人并填写改派原因。'; return }
  saving.value = true; error.value = ''
  try {
    selectedTask.value = await api.assignFollowup(task.id, { version: task.version, assigneeId: assignmentTargetId.value, reason: assignmentReason.value.trim() })
    assignmentReason.value = ''
    assignmentHistory.value = await api.followupAssignmentHistory(task.id)
    await Promise.all([load(), loadReceipts()])
    ElMessage.success('负责人已改派，等待接手人确认')
  } catch (cause) { error.value = errorMessage(cause) + ' 请刷新任务核对最新负责人。' }
  finally { saving.value = false }
}
async function leaveStaff() {
  const source = staff.value.find(item => item.id === leaveStaffId.value)
  if (!source || source.status !== 'on_duty' || saving.value) return
  if (!handoffToId.value || handoffToId.value === source.id || !handoffReason.value.trim()) { error.value = '请选择其他在岗接手人并填写交接原因。'; return }
  saving.value = true; error.value = ''
  try {
    await ElMessageBox.confirm(`确认将 ${source.displayName} 的全部未完成任务交给接手人，并将其标记为离岗？`, '离岗交接', { confirmButtonText: '确认交接', cancelButtonText: '继续编辑' })
    const result = await api.leaveFollowupStaff(source.id, { version: source.version, toStaffId: handoffToId.value, reason: handoffReason.value.trim() })
    handoffReason.value = ''; await loadStaff(); await Promise.all([load(), loadReceipts()])
    saving.value = false
    if (selectedTask.value) await selectTask(selectedTask.value, true)
    ElMessage.success(`已转移 ${result.transferred} 个未完成任务，等待接手人逐条确认`)
  } catch (cause) { if (cause !== 'cancel' && cause !== 'close') error.value = errorMessage(cause) + ' 请刷新人员状态后重试。' }
  finally { saving.value = false }
}
async function returnStaff() {
  const source = staff.value.find(item => item.id === leaveStaffId.value)
  if (!source || source.status !== 'off_duty' || saving.value) return
  saving.value = true; error.value = ''
  try {
    await api.returnFollowupStaff(source.id, source.version)
    await loadStaff(); ElMessage.success('人员已恢复在岗，已交接任务仍由接手人负责')
  } catch (cause) { error.value = errorMessage(cause) + ' 请刷新人员状态后重试。' }
  finally { saving.value = false }
}
async function review() {
  const task = selectedTask.value
  if (!task || saving.value || !['submitted', 'alert'].includes(task.status)) return
  if (!response.value.trim() || (task.status === 'alert' && !disposition.value.trim())) { error.value = '请填写回评内容；异常任务需要处理记录。'; return }
  saving.value = true; error.value = ''
  try {
    selectedTask.value = await api.reviewFollowup(task.id, { version: task.version, action: task.status === 'alert' ? 'handle' : 'review', response: response.value.trim(), disposition: disposition.value.trim() })
    ElMessage.success(task.status === 'alert' ? '异常处理已记录' : '回评已发送'); response.value = ''; disposition.value = ''; await load()
  } catch (cause) { error.value = errorMessage(cause) + ' 请刷新任务核对最新状态。' }
  finally { saving.value = false }
}
async function loadLinkedAppointment() {
  if (typeof route.query.appointmentId !== 'string') return
  try {
    if (typeof route.query.taskId === 'string') {
      const data = await api.followupWorkspace(route.query.appointmentId)
      const task = data.tasks.find(item => item.id === route.query.taskId)
      if (!task) { error.value = '随访任务不存在或无权访问'; return }
      await selectTask(task)
    } else if (canTreat.value) {
      const appointment = await api.appointment(route.query.appointmentId)
      mode.value = 'treatment'
      treatmentStatus.value = appointment.status === 'completed' ? 'completed' : 'confirmed'
      await load()
      await selectAppointment(appointment)
    }
  } catch (cause) { error.value = errorMessage(cause) }
}
onMounted(() => {
  if (!props.handoffOnly) void loadTemplates()
  void loadStaff().then(() => { if (!props.handoffOnly) void loadLinkedAppointment() });
  if (props.handoffOnly) { void loadReceipts(); void loadHandoffSetting() } else void load()
  timer = setInterval(() => { if (!saving.value && selectedTask.value && !detailLoading.value) void selectTask(selectedTask.value, true); if (props.handoffOnly && !saving.value && !receiptLoading.value && !confirmingReceiptId.value && !followupSaving.value && !settingSaving.value && document.visibilityState === 'visible') void loadReceipts() }, 15000)
})
onBeforeUnmount(() => { generation++; listGeneration++; receiptGeneration++; if (timer) clearInterval(timer); releasePhotos() })
</script>

<template>
  <div class="followup-page">
    <div class="heading"><div><div class="eyebrow">CONTINUITY OF CARE</div><h1>{{ props.handoffOnly ? '人员交接' : '治疗与随访' }}</h1><p>{{ props.handoffOnly ? '确认任务接收，跟进超时交接，管理人员离岗与恢复。' : '记录实际治疗，跟进患者反馈，留下回评与异常处理记录。' }}</p></div><el-button :disabled="saving || detailLoading" @click="refreshCurrent">刷新</el-button></div>
    <el-radio-group v-if="!props.handoffOnly" v-model="mode" :disabled="saving" @change="changeMode"><el-radio-button value="queue">随访任务</el-radio-button><el-radio-button v-if="canTreat" value="treatment">治疗记录与更正</el-radio-button><el-radio-button value="templates">护理与随访规范</el-radio-button><el-radio-button value="nursing">护理反馈</el-radio-button></el-radio-group>
    <el-alert v-if="error" class="notice" type="error" :closable="false" :title="error" />
    <div v-if="!props.handoffOnly" class="panel-head"><RouterLink to="/workbench">查看待办工作台</RouterLink><RouterLink to="/handoffs">交接确认与人员管理 →</RouterLink></div>
    <el-radio-group v-if="props.handoffOnly" v-model="handoffSection"><el-radio-button value="receipts">交接确认与跟进</el-radio-button><el-radio-button v-if="auth.employee?.role === 'manager'" value="staff">人员离岗交接</el-radio-button><el-radio-button v-if="auth.employee?.role === 'manager'" value="settings">提醒设置</el-radio-button></el-radio-group>
    <section v-if="props.handoffOnly && handoffSection === 'receipts'" class="panel receipt-panel">
      <el-alert v-if="receiptIdFilter" title="当前显示通知关联的交接记录" type="info" :closable="false"><el-button size="small" @click="receiptIdFilter = ''; receiptPage = 1; loadReceipts()">查看完整交接清单</el-button></el-alert>
      <div class="panel-head"><div><h2>任务交接确认 <el-tag v-if="receiptStatus === 'pending'" type="warning" size="small">{{ receiptTotal }} 项待确认</el-tag></h2><p>{{ auth.employee?.role === 'manager' ? '查看交接进度；接手人登录后逐条确认收到任务。' : '请确认您已收到的交接任务。' }}</p></div><el-button :disabled="receiptLoading" @click="loadReceipts">刷新交接</el-button></div>
      <div v-if="auth.employee?.role === 'manager'" class="todo-summary" :class="{ active: receiptTodoTotal > 0 }"><div><strong>{{ receiptTodoTotal }} 项主管待办</strong><p>包含超时未跟进，以及待回复、需处理的交接；确认后仍需记录“已解决”才能结案。</p></div><el-button v-if="receiptTodoTotal > 0" type="primary" plain size="small" @click="showSupervisorTodo">查看待办</el-button></div>
      <div v-if="auth.employee?.role === 'manager'" class="overdue-summary" :class="{ active: receiptOverdueTotal > 0 }"><div><strong>{{ receiptOverdueTotal > 0 ? `${receiptOverdueTotal} 项交接超时未确认` : '当前没有超时交接' }}</strong><p>接手后超过 {{ receiptOverdueHours }} 小时仍未确认即标为超时。</p></div><el-button v-if="receiptOverdueTotal > 0" type="danger" plain size="small" @click="showOverdueReceipts">只看超时</el-button></div>

      <div class="receipt-filters"><el-select v-model="receiptStatus" @change="receiptPage = 1; loadReceipts()"><el-option v-if="auth.employee?.role === 'manager'" label="主管待办" value="todo" /><el-option label="待确认" value="pending" /><el-option label="超时未确认" value="overdue" /><el-option label="已确认" value="confirmed" /><el-option label="已转出" value="superseded" /><el-option label="全部" value="all" /></el-select><el-select v-if="auth.employee?.role === 'manager'" v-model="receiptToStaffId" @change="receiptPage = 1; loadReceipts()"><el-option label="全部接手人" value="" /><el-option v-for="person in staff" :key="person.id" :label="person.displayName" :value="person.id" /></el-select></div>
      <el-skeleton v-if="receiptLoading && !receipts.length" :rows="2" animated />
      <p v-else-if="!receipts.length" class="receipt-empty">{{ receiptStatus === 'todo' ? '当前没有主管待办。' : `当前没有${receiptStatus === 'pending' ? '待确认' : '符合条件的'}交接任务。` }}</p>
      <div v-for="receipt in receipts" :key="receipt.id" class="receipt-item">
        <div class="receipt-row" :class="{ overdue: receipt.overdue && auth.employee?.role === 'manager' }"><div><strong>{{ receipt.patientName }} · {{ receipt.taskTitle }}</strong><p>{{ receipt.fromName }} → {{ receipt.toName }} · {{ timestamp(receipt.createdAt) }} · 应随访 {{ timestamp(receipt.dueAt) }}</p><p>{{ receipt.reason }}</p><small v-if="receipt.confirmedAt">{{ receipt.confirmedByName }} 于 {{ timestamp(receipt.confirmedAt) }} 确认</small><small v-else-if="receipt.supersededAt">{{ timestamp(receipt.supersededAt) }} 已转给其他负责人</small></div><div class="receipt-action"><el-tag :type="receipt.overdue && auth.employee?.role === 'manager' ? 'danger' : receipt.status === 'pending' ? 'warning' : receipt.status === 'confirmed' ? 'success' : 'info'">{{ receipt.overdue && auth.employee?.role === 'manager' ? '超时未确认' : receipt.status === 'pending' ? '待确认' : receipt.status === 'confirmed' ? '已确认' : '已转出' }}</el-tag><el-tag v-if="auth.employee?.role === 'manager' && (receipt.overdue || receipt.followupCount)" :type="receipt.followupStatus === 'resolved' ? 'success' : receipt.followupStatus === 'needs_action' ? 'danger' : 'warning'">{{ followupStatusLabels[receipt.followupStatus] }}</el-tag><el-button v-if="auth.employee?.role === 'manager' && canRecordFollowup(receipt)" size="small" @click="openReceiptFollowups(receipt)">{{ openReceiptId === receipt.id ? '收起跟进' : receipt.followupCount ? `跟进记录（${receipt.followupCount}）` : '记录跟进' }}</el-button><el-button v-if="receipt.status === 'pending' && receipt.toStaffId === auth.employee?.staffId" type="primary" size="small" :loading="confirmingReceiptId === receipt.id" :disabled="!!confirmingReceiptId" @click="confirmReceipt(receipt)">确认收到</el-button></div></div>
        <div v-if="auth.employee?.role === 'manager' && openReceiptId === receipt.id" class="receipt-followups"><el-skeleton v-if="handoffFollowupsLoading" :rows="2" animated /><template v-else><h3>主管跟进历史</h3><p v-if="!handoffFollowups.length" class="followup-empty">尚无跟进记录。</p><div v-for="entry in handoffFollowups" :key="entry.id" class="followup-entry"><strong>{{ entry.actorName }} · {{ timestamp(entry.createdAt) }} · {{ followupStatusLabels[entry.statusAfter] }}</strong><p>跟进措施：{{ entry.actionText }}</p><p>处理结果：{{ entry.resultText }}</p></div><div v-if="canRecordFollowup(receipt)" class="followup-form"><el-input v-model="followupAction" type="textarea" :rows="2" maxlength="500" show-word-limit placeholder="本次跟进措施，例如电话联系接手人" /><el-input v-model="followupResult" type="textarea" :rows="2" maxlength="1000" show-word-limit placeholder="本次处理结果，例如已确认接收、尚待回复" /><div class="followup-status-select"><span>跟进状态</span><el-select v-model="followupStatusAfter"><el-option label="待回复" value="awaiting_reply" /><el-option label="需处理" value="needs_action" /><el-option label="已解决" value="resolved" /></el-select></div><el-button type="primary" :loading="followupSaving" @click="saveReceiptFollowup(receipt)">保存跟进记录</el-button></div></template></div>
      </div>
      <el-pagination v-if="receiptTotal > 20" small layout="prev, pager, next" :total="receiptTotal" :page-size="20" v-model:current-page="receiptPage" @current-change="loadReceipts" />
    </section>
    <section v-if="props.handoffOnly && handoffSection === 'staff' && auth.employee?.role === 'manager'" class="panel staff-panel">
      <div class="panel-head"><div><h2>随访负责人及离岗交接</h2><p>离岗会一次性转移该人员全部未完成任务；已完成任务和历史记录保留。</p></div><el-button :disabled="saving" @click="loadStaff">刷新人员</el-button></div>
      <div class="staff-list"><div v-for="person in staff" :key="person.id" class="staff-chip"><strong>{{ person.displayName }}</strong><span>{{ person.role === 'doctor' ? '医生' : '护理' }}</span><el-tag size="small" :type="person.status === 'on_duty' ? 'success' : 'info'">{{ person.status === 'on_duty' ? '在岗' : '离岗' }}</el-tag></div></div>
      <div class="handoff-form"><el-select v-model="leaveStaffId" :disabled="saving" placeholder="选择人员" @change="loadStaffHistory"><el-option v-for="person in staff" :key="person.id" :label="person.displayName" :value="person.id" /></el-select><template v-if="auth.employee?.role === 'manager' && staff.find(person => person.id === leaveStaffId)?.status === 'on_duty'"><el-select v-model="handoffToId" :disabled="saving" placeholder="选择在岗接手人"><el-option v-for="person in eligibleRecipients.filter(item => item.id !== leaveStaffId)" :key="person.id" :label="person.displayName" :value="person.id" /></el-select><el-input v-model="handoffReason" :disabled="saving" maxlength="300" placeholder="交接原因" /><el-button type="warning" :loading="saving" :disabled="!handoffToId" @click="leaveStaff">离岗并交接</el-button></template><el-button v-else-if="auth.employee?.role === 'manager' && leaveStaffId" type="primary" plain :loading="saving" @click="returnStaff">恢复在岗</el-button></div>
      <p v-if="!eligibleRecipients.length" class="account-hint">接手人需要启用且关联医护人员的员工账号。请先到 <RouterLink to="/employees">员工账号</RouterLink> 设置。</p>
      <details v-if="staffHistory.length" class="history"><summary>查看人员在岗与交接记录（{{ staffHistory.length }}）</summary><div v-for="event in staffHistory" :key="event.id" class="history-row">{{ timestamp(event.createdAt) }} · 操作人 {{ event.actorName }} · {{ event.eventType === 'leave' ? '离岗交接' : '恢复在岗' }}{{ event.toName ? ` · 交给 ${event.toName}（${event.transferredCount} 项）` : '' }}{{ event.reason ? ` · ${event.reason}` : '' }}</div></details>
    </section>
    <section v-if="props.handoffOnly && handoffSection === 'settings' && auth.employee?.role === 'manager'" class="panel">
      <div class="panel-head"><div><h2>交接通知与超时升级</h2><p>交接时通知接手人；超时未确认且未结案时，自动向授权院区的启用主管账号发送一次站内升级提醒。系统每分钟检查一次。</p></div></div>
      <div v-if="auth.employee?.role === 'manager' && handoffSetting" class="overdue-setting"><span>超时时限</span><el-input-number v-model="overdueHoursDraft" :min="1" :max="720" :step="1" :precision="0" size="small" aria-label="交接超时时限（小时）" /><span>小时</span><el-button type="primary" plain size="small" :loading="settingSaving" :disabled="overdueHoursDraft === handoffSetting.overdueHours" @click="saveHandoffSetting">保存时限</el-button></div>
      <p class="account-hint">通知已读不会代替交接确认。修改时限后，待确认交接按新时限判断；已发送的通知保留。时限适用于全部院区，保存需具备全部院区授权。</p>
    </section>
    <FollowupTools v-if="!props.handoffOnly && (mode === 'templates' || mode === 'nursing')" :mode="mode" @templates-changed="loadTemplates" />
    <div v-if="!props.handoffOnly && mode === 'queue'" class="date-filter"><span>随访到期日期（北京时间）</span><el-date-picker v-model="dates" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始日期" end-placeholder="结束日期" :disabled="saving" @change="dateFilter" /><el-button :disabled="saving || !dates" @click="dates = null; dateFilter()">清空日期</el-button></div>
    <div v-if="!props.handoffOnly && (mode === 'queue' || mode === 'treatment')" class="layout">
      <section class="panel list">
        <template v-if="mode === 'queue'">
          <div class="panel-head"><h2>任务队列</h2><el-select v-model="status" :disabled="saving" @change="page = 1; load()"><el-option label="全部状态" value="" /><el-option v-for="(label, key) in labels" :key="key" :label="label" :value="key" /></el-select></div>
          <div v-if="auth.employee?.role === 'manager'" class="filter-row"><el-select v-model="assigneeFilter" :disabled="saving" placeholder="全部负责人" @change="page = 1; load()"><el-option label="全部负责人" value="" /><el-option v-for="person in staff" :key="person.id" :label="person.displayName" :value="person.id" /></el-select></div>
          <el-skeleton v-if="loading" :rows="4" animated />
          <div v-else-if="!tasks.length" class="empty">当前没有随访任务</div>
          <button v-for="task in tasks" :key="task.id" class="list-item" :class="{ selected: selectedTask?.id === task.id, overdue: task.overdue, 'alert-overdue': alertDeadline(task) === 'overdue' }" :disabled="saving" @click="selectTask(task)"><strong>{{ task.patientName }} · {{ task.title }}</strong><span>{{ timestamp(task.dueAt) }} · 负责人 {{ task.assigneeName }}</span><em :class="task.status">{{ labels[task.status] }}{{ task.overdue ? ' · 已逾期' : '' }}</em><span v-if="alertDeadline(task)" class="alert-deadline" :class="alertDeadline(task)">{{ alertDeadlineLabel(task) }}</span></button>
          <el-pagination small layout="prev, pager, next" :total="total" :page-size="20" v-model:current-page="page" :disabled="saving" @current-change="load" />
        </template>
        <template v-else>
          <div class="panel-head"><h2>预约</h2><el-select v-model="treatmentStatus" :disabled="saving" @change="changeTreatmentStatus"><el-option label="待记录治疗" value="confirmed" /><el-option label="已治疗 / 可更正" value="completed" /></el-select></div>
          <el-skeleton v-if="loading" :rows="4" animated />
          <div v-else-if="!appointments.length" class="empty">当前状态暂无预约</div>
          <button v-for="item in appointments" :key="item.id" class="list-item" :class="{ selected: selectedAppointment?.id === item.id }" :disabled="saving" @click="selectAppointment(item)"><strong>{{ item.contactName }} · {{ item.doctorName }}</strong><span>{{ timestamp(item.startsAt) }}</span></button>
          <el-pagination small layout="prev, pager, next" :total="appointmentTotal" :page-size="20" v-model:current-page="appointmentPage" :disabled="saving" @current-change="load" />
        </template>
      </section>
      <section class="panel detail">
        <el-skeleton v-if="detailLoading" :rows="8" animated />
        <template v-else-if="selectedAppointment">
          <div class="panel-head"><div><h2>{{ selectedAppointment.contactName }} · 实际治疗记录</h2><p>{{ selectedAppointment.doctorName }} · {{ timestamp(selectedAppointment.startsAt) }}</p></div><RouterLink :to="{ path: '/care', query: { appointmentId: selectedAppointment.id } }">查看方案与照片</RouterLink></div>
          <template v-if="workspace.treatment">
            <h3>当前治疗记录 · V{{ workspace.treatment.version }}</h3><p v-if="workspace.treatment.stageIndex">方案阶段 {{ workspace.treatment.stageIndex }}</p><p>{{ workspace.treatment.procedure }} · {{ workspace.treatment.bodyArea }} · {{ workspace.treatment.device }}</p><p class="body">{{ workspace.treatment.parameters }}</p><p class="body">{{ workspace.treatment.summary }}</p><p>{{ timestamp(workspace.treatment.treatedAt) }} · 方案版本编号 {{ workspace.treatment.planVersionId }}</p><p class="body">{{ workspace.treatment.notes }}</p>
            <p v-if="workspace.treatment.correctedAt">最近更正：{{ timestamp(workspace.treatment.correctedAt) }}</p>
            <el-button v-if="!correctionOpen" type="primary" plain :disabled="saving" @click="correctionOpen = true">更正治疗记录</el-button>
            <el-form v-else label-position="top" class="correction-form">
              <el-alert type="info" :closable="false" title="更正会生成新版本并保留原文。已有随访反馈或照片时，实际治疗时间不可调整。" />
              <el-form-item label="本次依据的已发布方案"><el-select v-model="planVersionId" :disabled="saving"><el-option v-for="plan in plans" :key="plan.id" :label="`方案 V${plan.version}`" :value="plan.id" /></el-select></el-form-item>
              <el-form-item label="实际治疗时间（北京时间）"><input v-model="treatedAt" type="datetime-local" step="1" :disabled="saving" :max="localTime()" /></el-form-item>
              <el-form-item label="常用摘要模板"><el-select v-model="chosenSummaryTemplate" clearable placeholder="选择模板填入摘要，可再编辑" :disabled="saving" @change="applySummaryTemplate"><el-option v-for="item in treatmentSummaryTemplates" :key="item.name" :label="item.name" :value="item.name" /></el-select></el-form-item><el-form-item label="实际治疗摘要"><el-input v-model="summary" :disabled="saving" type="textarea" :rows="3" maxlength="1000" /></el-form-item>
              <el-form-item label="方案阶段（0表示未指定）"><el-input-number v-model="treatmentFields.stageIndex" :min="0" :max="8" :disabled="saving" /></el-form-item>
              <el-form-item label="治疗项目"><el-input v-model="treatmentFields.procedure" maxlength="100" :disabled="saving" /></el-form-item>
              <el-form-item label="治疗部位"><el-input v-model="treatmentFields.bodyArea" maxlength="100" :disabled="saving" /></el-form-item>
              <el-form-item label="治疗设备"><el-input v-model="treatmentFields.device" maxlength="100" :disabled="saving" /></el-form-item>
              <el-form-item label="操作参数 / 使用剂量"><el-input v-model="treatmentFields.parameters" type="textarea" :rows="2" maxlength="2000" :disabled="saving" /></el-form-item>
              <el-form-item label="治疗补充记录"><el-input v-model="notes" :disabled="saving" type="textarea" :rows="3" maxlength="2000" /></el-form-item>
              <el-form-item label="更正原因"><el-input v-model="correctionReason" :disabled="saving" type="textarea" :rows="2" maxlength="300" placeholder="说明本次更正的原因" /></el-form-item>
              <el-button :disabled="saving" @click="correctionOpen = false">取消编辑</el-button><el-button type="primary" :loading="saving" @click="correct">保存更正</el-button>
            </el-form>
            <h3>治疗记录版本历史</h3><div v-for="revision in workspace.treatmentRevisions" :key="revision.version" class="revision"><strong>V{{ revision.version }} · {{ timestamp(revision.changedAt) }}</strong><p>{{ revision.correctionReason || '原始记录' }}</p><p v-if="revision.stageIndex">方案阶段 {{ revision.stageIndex }}</p><p>{{ revision.procedure }} · {{ revision.bodyArea }} · {{ revision.device }}</p><p class="body">{{ revision.parameters }}</p><p class="body">{{ revision.summary }}</p><small>治疗时间：{{ timestamp(revision.treatedAt) }} · 方案版本编号 {{ revision.planVersionId }}</small><p v-if="revision.notes" class="body">{{ revision.notes }}</p></div>
            <h3>已生成的随访任务</h3><div v-for="task in workspace.tasks" :key="task.id" class="task-line">{{ task.title }} · {{ timestamp(task.dueAt) }} <el-tag>{{ labels[task.status] }}</el-tag></div>
          </template>
          <template v-else>
            <el-alert v-if="!plans.length" type="info" :closable="false" title="请先在方案与照片工作台发布方案，再记录治疗。" />
            <el-form label-position="top">
              <el-form-item label="本次依据的已发布方案"><el-select v-model="planVersionId" :disabled="saving"><el-option v-for="plan in plans" :key="plan.id" :label="`方案 V${plan.version}`" :value="plan.id" /></el-select></el-form-item>
              <el-form-item label="实际治疗时间（北京时间）"><input v-model="treatedAt" type="datetime-local" step="1" :disabled="saving" :max="localTime()" /></el-form-item>
              <el-form-item label="常用摘要模板"><el-select v-model="chosenSummaryTemplate" clearable placeholder="选择模板填入摘要，可再编辑" :disabled="saving" @change="applySummaryTemplate"><el-option v-for="item in treatmentSummaryTemplates" :key="item.name" :label="item.name" :value="item.name" /></el-select></el-form-item><el-form-item label="实际治疗摘要"><el-input v-model="summary" :disabled="saving" type="textarea" :rows="3" maxlength="1000" placeholder="记录实际实施的治疗内容" /></el-form-item>
              <el-form-item label="方案阶段（0表示未指定）"><el-input-number v-model="treatmentFields.stageIndex" :min="0" :max="8" :disabled="saving" /></el-form-item>
              <el-form-item label="治疗项目"><el-input v-model="treatmentFields.procedure" maxlength="100" :disabled="saving" /></el-form-item>
              <el-form-item label="治疗部位"><el-input v-model="treatmentFields.bodyArea" maxlength="100" :disabled="saving" /></el-form-item>
              <el-form-item label="治疗设备"><el-input v-model="treatmentFields.device" maxlength="100" :disabled="saving" /></el-form-item>
              <el-form-item label="操作参数 / 使用剂量"><el-input v-model="treatmentFields.parameters" type="textarea" :rows="2" maxlength="2000" :disabled="saving" /></el-form-item>
              <el-form-item label="治疗补充记录"><el-input v-model="notes" :disabled="saving" type="textarea" :rows="3" maxlength="2000" /></el-form-item>
              <el-form-item label="随访模板"><el-select v-model="chosenTemplate" clearable placeholder="选择本院区启用模板" :disabled="saving"><el-option v-for="item in availableTemplates" :key="item.id" :label="item.name" :value="item.id" /></el-select><el-button :disabled="saving || !chosenTemplate" @click="applyTemplate('days')">应用随访天数</el-button></el-form-item><el-alert v-if="selectedStandard" :closable="false" type="info" :title="`适用治疗：${selectedStandard.applicableTreatment} · V${selectedStandard.version}`"><p class="body">护理要求：{{ selectedStandard.nursingInstructions }}</p><p class="body">随访检查要点：{{ selectedStandard.followupChecklist }}</p></el-alert><el-form-item label="治疗后随访天数"><FollowupDays v-model="offsets" :disabled="saving" :treated-at="treatedAt" /></el-form-item>
              <el-form-item label="随访负责人"><el-select v-model="treatmentAssigneeId" :disabled="saving" placeholder="选择在岗负责人"><el-option v-for="person in staff.filter(item => item.status === 'on_duty')" :key="person.id" :label="person.displayName" :value="person.id" /></el-select></el-form-item>
              <el-button type="primary" :loading="saving" :disabled="!plans.length" @click="record">记录治疗并生成随访</el-button>
            </el-form>
          </template>
        </template>
        <template v-else-if="selectedTask">
          <div class="panel-head"><div><h2>{{ selectedTask.patientName }} · {{ selectedTask.title }}</h2><p>{{ timestamp(selectedTask.dueAt) }} · 北京时间</p></div><el-tag :type="selectedTask.status === 'alert' ? 'danger' : 'info'">{{ labels[selectedTask.status] }}</el-tag></div>
          <el-alert v-if="alertDeadline(selectedTask)" :title="`${alertDeadlineLabel(selectedTask)}：应随访 ${timestamp(selectedTask.dueAt)}（北京时间），请尽快核实异常并记录处理结果。`" :type="alertDeadline(selectedTask) === 'overdue' ? 'error' : 'warning'" show-icon :closable="false" class="alert-deadline-banner" />
          <el-button :disabled="saving" @click="selectTask(selectedTask, true)">刷新任务与授权</el-button>
          <div class="assignment"><h3>随访负责人</h3><p>当前负责人：<strong>{{ selectedTask.assigneeName }}</strong> · 自 {{ timestamp(selectedTask.assignedAt) }}</p><div v-if="auth.employee?.role === 'manager'" class="assignment-form"><el-select v-model="assignmentTargetId" :disabled="saving" placeholder="选择新负责人"><el-option v-for="person in eligibleRecipients" :key="person.id" :label="person.displayName" :value="person.id" /></el-select><el-input v-model="assignmentReason" :disabled="saving" maxlength="300" placeholder="改派原因" /><el-button type="primary" plain :loading="saving" @click="assignTask">改派任务</el-button></div><details class="history"><summary>查看负责人变动记录（{{ assignmentHistory.length }}）</summary><div v-for="event in assignmentHistory" :key="event.id" class="history-row">{{ timestamp(event.createdAt) }} · 操作人 {{ event.actorName }} · {{ event.fromName || '初始分配' }} → {{ event.toName }} · {{ event.reason }}</div></details></div>
          <div v-if="workspace.treatment" class="treatment-summary"><h3>实际治疗</h3><p class="body">{{ workspace.treatment.summary }}</p><small>{{ timestamp(workspace.treatment.treatedAt) }}</small><RouterLink :to="{ path: '/care', query: { appointmentId: selectedTask.appointmentId } }">查看关联方案</RouterLink></div>
          <h3>患者反馈</h3>
          <p v-if="selectedTask.status === 'pending'">患者尚未提交{{ selectedTask.overdue ? '，已超过计划随访时间。' : '。' }}</p>
          <template v-else><el-alert v-if="selectedTask.status === 'alert'" type="warning" :closable="false" title="患者反馈明显/严重不适或主动请求关注，请医护核实并记录处理。" /><p>{{ severityLabels[selectedTask.severity] }}{{ selectedTask.concern ? ' · 请求医护关注' : '' }}</p><p class="body">{{ selectedTask.symptoms }}</p></template>
          <h3>本次随访照片</h3><p v-if="photoNotice" class="photo-error">{{ photoNotice }}</p>
          <div v-if="photos.length" class="photo-grid"><figure v-for="photo in photos" :key="photo.id"><PrivatePhoto :id="photo.id" source="followup" :alt="photo.angle" /><figcaption>{{ photo.angle === 'front' ? '正面' : photo.angle === 'left' ? '左侧' : '右侧' }}</figcaption></figure></div>
          <p v-else>{{ selectedTask.photosGranted ? '本次尚未上传照片。' : '患者尚未授权或已撤回授权，照片不可查看。' }}</p>
          <el-form v-if="selectedTask.status === 'submitted' || selectedTask.status === 'alert'" label-position="top">
            <el-form-item label="回评模板"><el-select v-model="chosenTemplate" clearable placeholder="选择本院区启用模板" :disabled="saving"><el-option v-for="item in availableTemplates" :key="item.id" :label="item.name" :value="item.id" /></el-select><el-button :disabled="saving || !chosenTemplate" @click="applyTemplate('response')">应用回评内容</el-button><small>应用后请核对实际情况；异常处理须记录已执行措施。</small></el-form-item><el-alert v-if="selectedStandard" :closable="false" type="info" :title="`适用治疗：${selectedStandard.applicableTreatment} · V${selectedStandard.version}`"><p class="body">护理要求：{{ selectedStandard.nursingInstructions }}</p><p class="body">随访检查要点：{{ selectedStandard.followupChecklist }}</p></el-alert><el-form-item label="给患者的回评"><el-input v-model="response" :disabled="saving" type="textarea" :rows="4" maxlength="2000" /></el-form-item>
            <el-form-item v-if="selectedTask.status === 'alert'" label="异常处理记录（患者可见）"><el-input v-model="disposition" :disabled="saving" type="textarea" :rows="3" maxlength="1000" placeholder="记录核实、联系或复诊安排等实际处理情况" /></el-form-item>
            <el-button type="primary" :loading="saving" @click="review">{{ selectedTask.status === 'alert' ? '提交回评并完成异常处理' : '提交医护回评' }}</el-button>
          </el-form>
          <div v-else-if="selectedTask.response" class="review-result"><h3>已发送回评</h3><p class="body">{{ selectedTask.response }}</p><h3 v-if="selectedTask.disposition">异常处理记录</h3><p class="body">{{ selectedTask.disposition }}</p><small v-if="selectedTask.reviewedAt">{{ timestamp(selectedTask.reviewedAt) }}</small></div>
        </template>
        <div v-else class="empty">{{ mode === 'queue' ? '选择任务查看反馈、回评或处理异常。' : '选择预约，记录或更正实际治疗。' }}</div>
      </section>
    </div>
  </div>
</template>
<style scoped>
.date-filter{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-top:20px;font-size:13px}.date-filter .el-date-editor{max-width:100%}

.heading,.panel-head{display:flex;align-items:center;justify-content:space-between;gap:18px}.heading{margin-bottom:22px}.heading h1{font-size:25px;color:#304761}.heading p,.panel-head p{color:#8391a5;margin-top:8px}.layout{display:grid;grid-template-columns:320px minmax(0,1fr);gap:20px;margin-top:20px;align-items:start}.panel{border:1px solid #e3eaf2;border-radius:12px;background:white;overflow:hidden}.panel-head{padding:18px 0;border-bottom:1px solid #edf1f6;margin-bottom:18px}.panel-head h2{font-size:16px}.list .panel-head{padding:18px;margin:0}.list .el-select{width:135px}.detail{padding:0 24px 24px}.list-item{display:block;width:100%;text-align:left;padding:18px;border:0;border-bottom:1px solid #edf1f6;background:white;cursor:pointer}.list-item strong,.list-item span,.list-item em{display:block;font-size:12px}.list-item span{margin-top:7px;color:#8391a5}.list-item em{font-style:normal;color:#527295;margin-top:8px}.list-item em.alert,.photo-error{color:#bf4c4c}.list-item.overdue{background:#fff5f5;box-shadow:inset 3px 0 #e02424}.list-item.overdue em{color:#bf4c4c}.list-item.alert-overdue{background:#fff5f5;box-shadow:inset 3px 0 #e02424}.alert-deadline{display:block;font-size:12px;margin-top:6px}.alert-deadline.soon{color:#b7791f}.alert-deadline.overdue{color:#bf4c4c;font-weight:600}.alert-deadline-banner{margin-bottom:16px}.list-item.selected{background:#eff6ff;box-shadow:inset 3px 0 #1763c6}.empty{padding:65px 20px;text-align:center;color:#8391a5}.notice{margin-top:18px}.detail h3{font-size:14px;margin:24px 0 12px}.detail p{font-size:13px;line-height:1.8}.body{white-space:pre-wrap}.detail small{color:#8391a5}.detail a{font-size:12px;color:#1763c6;margin-left:12px}.detail .el-form{margin-top:22px}.detail .el-select{width:100%}input{border:1px solid #dce3ee;padding:10px;border-radius:5px;color:#304761;width:100%;font:inherit}.task-line{padding:12px 0;border-bottom:1px solid #edf1f6;font-size:12px}.photo-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}.photo-grid figure{margin:0}.photo-grid img{width:100%;height:180px;object-fit:contain;background:#f5f7fa}.photo-grid figcaption{font-size:11px;text-align:center;padding:7px;color:#8391a5}.el-pagination{padding:12px}.review-result,.treatment-summary{border-bottom:1px solid #edf1f6;padding-bottom:18px}@media(max-width:1000px){.layout{grid-template-columns:1fr}.photo-grid{grid-template-columns:1fr}}
.correction-form { margin-top: 20px; padding: 18px; background: #f7fafc; border-radius: 8px; }
.revision { padding: 14px 0; border-top: 1px solid #edf1f6; }
.revision strong { font-size: 12px; color: #304761; }
.revision p { margin: 7px 0; }
.revision small { display: block; }
.staff-panel{padding:0 18px 18px;margin-top:20px}.staff-list{display:flex;flex-wrap:wrap;gap:10px;margin-bottom:14px}.staff-chip{display:flex;align-items:center;gap:8px;border:1px solid #e3eaf2;border-radius:8px;padding:8px 10px;font-size:12px}.staff-chip span{color:#8391a5}.handoff-form,.assignment-form{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.handoff-form .el-select,.assignment-form .el-select{width:170px}.handoff-form .el-input,.assignment-form .el-input{width:min(320px,100%)}.filter-row{padding:10px 18px;border-bottom:1px solid #edf1f6}.filter-row .el-select{width:100%}.assignment{background:#f7fafc;border-radius:8px;padding:3px 16px 16px;margin-top:16px}.history{margin-top:14px;font-size:12px;color:#527295}.history summary{cursor:pointer}.history-row{padding:9px 0;border-top:1px solid #e3eaf2;color:#304761}.history-row:first-of-type{margin-top:9px}
.receipt-panel{padding:0 18px 12px;margin-top:20px}.receipt-panel .panel-head{margin-bottom:12px}.receipt-panel .panel-head h2{display:flex;align-items:center;gap:10px}.receipt-filters{display:flex;gap:10px;margin-bottom:12px}.receipt-filters .el-select{width:165px}.receipt-row{display:flex;justify-content:space-between;gap:16px;padding:12px 0;border-top:1px solid #edf1f6;font-size:12px}.receipt-row strong{font-size:13px}.receipt-row p{margin:6px 0;color:#66788f}.receipt-row small{color:#8391a5}.receipt-action{display:flex;align-items:center;gap:8px;flex-shrink:0}.receipt-empty{padding:20px;color:#8391a5}.account-hint{font-size:12px;color:#986719}.account-hint a{color:#1763c6}@media(max-width:700px){.receipt-row{display:block}.receipt-action{margin-top:10px}}
.overdue-summary{display:flex;align-items:center;justify-content:space-between;gap:12px;background:#f4f8f6;border:1px solid #dcebe3;border-radius:8px;padding:12px;margin-bottom:12px}.overdue-summary.active{background:#fff2f0;border-color:#f2b7af;color:#b7332b}.overdue-summary strong{font-size:13px}.overdue-summary p{font-size:12px;margin:5px 0 0;color:#66788f}.overdue-setting{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:12px;font-size:12px;color:#52657b}.overdue-setting .el-input-number{width:118px}.receipt-row.overdue{background:#fff5f2;border-left:3px solid #d8493e;padding-left:10px}.receipt-row.overdue strong{color:#ae352c}
.receipt-followups{padding:14px;border:1px solid #e7edf4;border-radius:8px;background:#f8fafc;margin:0 0 12px}.receipt-followups h3{font-size:13px;margin:0 0 10px}.receipt-followups p{font-size:12px;line-height:1.6;white-space:pre-wrap}.followup-empty{color:#8391a5}.followup-entry{padding:10px 0;border-top:1px solid #e3eaf2}.followup-entry strong{font-size:12px;color:#304761}.followup-entry p{margin:5px 0;color:#52657b}.followup-form{display:grid;gap:10px;margin-top:12px}.followup-form .el-button{justify-self:start}
.todo-summary{display:flex;align-items:center;justify-content:space-between;gap:12px;background:#f3f7ff;border:1px solid #d5e3fa;border-radius:8px;padding:12px;margin-bottom:10px}.todo-summary.active{background:#ecf4ff;border-color:#9dc1f5}.todo-summary strong{font-size:13px;color:#285d9d}.todo-summary p{font-size:12px;margin:5px 0 0;color:#66788f}.followup-status-select{display:flex;align-items:center;gap:10px;font-size:12px;color:#52657b}.followup-status-select .el-select{width:160px}.receipt-action{flex-wrap:wrap;justify-content:flex-end}
</style>
