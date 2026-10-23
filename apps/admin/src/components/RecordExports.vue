<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElCheckbox, ElCheckboxGroup, ElMessage, ElRadio } from 'element-plus'
import { auth } from '../auth'
import { api, errorMessage, type PatientRecordSummary, type RecordExportEvent, type RecordExportInput, type RecordExportJob } from '../api'
import { shanghaiDate, slotTime } from '../dates'

const props = defineProps<{ selectedPatients: PatientRecordSummary[]; currentPatient?: { id: string; displayName?: string | null } }>()
const canExport = computed(() => ['manager', 'doctor', 'clinician', 'nurse'].includes(auth.employee?.role || ''))
const isManager = computed(() => auth.employee?.role === 'manager')
const sectionOptions: { value: RecordExportInput['sections'][number]; label: string }[] = [
  { value: 'appointments', label: '预约' }, { value: 'consultations', label: '问诊记录' }, { value: 'care_plans', label: '方案' }, { value: 'treatments', label: '治疗' }, { value: 'followups', label: '随访' },
]
const formOpen = ref(false)
const scope = ref<'selected' | 'current'>('selected')
const sections = ref<RecordExportInput['sections']>(sectionOptions.map(item => item.value))
const dateFrom = ref('')
const dateTo = ref('')
const masked = ref(true)
const reason = ref('')
const creating = ref(false)
const formError = ref('')
const scopePatients = computed(() => scope.value === 'current' ? (props.currentPatient ? [props.currentPatient] : []) : props.selectedPatients)
const jobsOpen = ref(false)
const jobs = ref<RecordExportJob[]>([])
const page = ref(1)
const total = ref(0)
const loading = ref(false)
const jobsError = ref('')
const now = ref(Date.now())
const downloading = ref('')
const auditOpen = ref(false)
const auditJob = ref<RecordExportJob | null>(null)
const events = ref<RecordExportEvent[]>([])
const auditLoading = ref(false)
const auditError = ref('')
const statusLabels: Record<string, string> = { queued: '排队中', processing: '生成中', ready: '可下载', failed: '生成失败', expired: '已过期' }
const eventLabels: Record<string, string> = { requested: '申请导出', generated: '生成完成', failed: '生成失败', downloaded: '下载档案', download_denied: '下载被拒绝', expired: '文件过期' }
let formController: AbortController | undefined
let listController: AbortController | undefined
let auditController: AbortController | undefined
let downloadController: AbortController | undefined
let pollTimer: ReturnType<typeof setTimeout> | undefined
let expiryTimer: ReturnType<typeof setInterval> | undefined
let listVersion = 0
let auditVersion = 0
let downloadVersion = 0
let disposed = false
const formatTime = (value: string | null | undefined) => value ? shanghaiDate(new Date(value)) + ' ' + slotTime(value) : '—'
const jobStatus = (job: RecordExportJob) => job.status === 'ready' && (!job.expiresAt || Date.parse(job.expiresAt) <= now.value) ? 'expired' : job.status
const canDownload = (job: RecordExportJob) => jobStatus(job) === 'ready' && !!job.expiresAt && Date.parse(job.expiresAt) > now.value
const sectionLabel = (values: RecordExportJob['sections']) => values.map(value => sectionOptions.find(item => item.value === value)?.label || value).join('、')

function openCreate(value: 'selected' | 'current') {
  if (!canExport.value) return
  scope.value = value
  sections.value = sectionOptions.map(item => item.value)
  dateFrom.value = ''; dateTo.value = ''; masked.value = true; reason.value = ''; formError.value = ''
  formOpen.value = true
}

async function createExport() {
  if (creating.value || !canExport.value) return
  formError.value = ''
  if (!scopePatients.value.length || scopePatients.value.length > 20) { formError.value = '请选择 1 至 20 位患者。'; return }
  if (!sections.value.length) { formError.value = '请至少选择一项档案内容。'; return }
  if (!reason.value.trim()) { formError.value = '请填写导出用途。'; return }
  if (!!dateFrom.value !== !!dateTo.value) { formError.value = '请同时填写起始日期和结束日期。'; return }
  if (dateFrom.value && dateTo.value) {
    const from = Date.parse(dateFrom.value + 'T00:00:00Z'), to = Date.parse(dateTo.value + 'T00:00:00Z')
    if (!Number.isFinite(from) || !Number.isFinite(to) || to < from || (to - from) / 86400000 + 1 > 366) { formError.value = '日期范围须按先后顺序填写，且最多 366 天。'; return }
  }
  const body: RecordExportInput = { patientIds: scopePatients.value.map(item => item.id), sections: [...sections.value], masked: isManager.value ? masked.value : true, reason: reason.value.trim(), ...(dateFrom.value ? { dateFrom: dateFrom.value, dateTo: dateTo.value } : {}) }
  formController = new AbortController()
  creating.value = true
  try {
    await api.createRecordExport(body, formController.signal)
    if (disposed) return
    formOpen.value = false
    ElMessage.success('导出任务已提交，生成后可在导出任务中下载。')
    page.value = 1
    openJobs()
  } catch (cause) { if (!disposed) formError.value = errorMessage(cause) }
  finally { creating.value = false }
}

function clearPoll() { if (pollTimer) clearTimeout(pollTimer); pollTimer = undefined }
function schedulePoll() {
  clearPoll()
  if (jobsOpen.value && !document.hidden && jobs.value.some(item => item.status === 'queued' || item.status === 'processing')) pollTimer = setTimeout(() => { if (!document.hidden) void loadJobs(false) }, 2000)
}
async function loadJobs(showLoading = true) {
  if (!jobsOpen.value || disposed) return
  clearPoll()
  const version = ++listVersion
  listController?.abort(); listController = new AbortController()
  if (showLoading) loading.value = true
  jobsError.value = ''
  try {
    const result = await api.recordExports(page.value, listController.signal)
    if (version !== listVersion || !jobsOpen.value || disposed) return
    jobs.value = result.data; total.value = result.total; now.value = Date.now()
  } catch (cause) { if (version === listVersion && jobsOpen.value && !disposed) jobsError.value = errorMessage(cause) }
  finally { if (version === listVersion) { loading.value = false; schedulePoll() } }
}
function openJobs() {
  if (!canExport.value) return
  if (jobsOpen.value) { void loadJobs(); return }
  jobsOpen.value = true
}
function stopJobs() {
  clearPoll(); if (expiryTimer) clearInterval(expiryTimer); expiryTimer = undefined
  listVersion++; listController?.abort(); downloadVersion++; downloadController?.abort(); loading.value = false; downloading.value = ''
  auditOpen.value = false
}
watch(jobsOpen, value => {
  if (!value) { stopJobs(); return }
  now.value = Date.now(); expiryTimer = setInterval(() => { now.value = Date.now() }, 1000)
  void loadJobs()
})
watch(isManager, value => { if (!value) masked.value = true })
watch(canExport, value => { if (!value) { jobsOpen.value = false; formOpen.value = false } })
function visibilityChanged() {
  if (document.hidden) clearPoll()
  else if (jobsOpen.value) { now.value = Date.now(); void loadJobs(false) }
}
document.addEventListener('visibilitychange', visibilityChanged)

async function download(job: RecordExportJob) {
  now.value = Date.now()
  if (!canDownload(job) || downloading.value || !jobsOpen.value) return
  const version = ++downloadVersion
  downloading.value = job.id
  downloadController = new AbortController()
  try {
    const blob = await api.recordExportDownload(job.id, downloadController.signal)
    if (version !== downloadVersion || !jobsOpen.value || disposed) return
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    try { anchor.href = url; anchor.download = `patient-records-${job.id}.json`; document.body.appendChild(anchor); anchor.click() }
    finally { anchor.remove(); URL.revokeObjectURL(url) }
    ElMessage.success('档案已下载，请按导出用途妥善保管。')
    await loadJobs(false)
    if (auditOpen.value && auditJob.value?.id === job.id) await loadAudit(job)
  } catch (cause) {
    if (version === downloadVersion && jobsOpen.value && !disposed) { ElMessage.error(errorMessage(cause)); await loadJobs(false) }
  } finally { if (version === downloadVersion) downloading.value = '' }
}

async function loadAudit(job: RecordExportJob) {
  const version = ++auditVersion
  auditController?.abort(); auditController = new AbortController()
  auditJob.value = job; events.value = []; auditError.value = ''; auditLoading.value = true; auditOpen.value = true
  try {
    const result = await api.recordExportEvents(job.id, auditController.signal)
    if (version === auditVersion && auditOpen.value && !disposed) events.value = result
  } catch (cause) { if (version === auditVersion && auditOpen.value && !disposed) auditError.value = errorMessage(cause) }
  finally { if (version === auditVersion) auditLoading.value = false }
}
watch(auditOpen, value => { if (!value) { auditVersion++; auditController?.abort(); auditLoading.value = false } })
onBeforeUnmount(() => { disposed = true; stopJobs(); formController?.abort(); auditVersion++; auditController?.abort(); document.removeEventListener('visibilitychange', visibilityChanged) })
defineExpose({ openCreate, openJobs })
</script>

<template>
  <el-dialog v-model="formOpen" title="导出患者档案" width="620px" :close-on-click-modal="!creating" :close-on-press-escape="!creating" :show-close="!creating" destroy-on-close>
    <el-form label-position="top" @submit.prevent="createExport">
      <el-form-item label="导出范围">
        <el-radio-group v-model="scope" :disabled="creating"><el-radio value="selected" :disabled="!selectedPatients.length">目录已选患者（{{ selectedPatients.length }} 位）</el-radio><el-radio value="current" :disabled="!currentPatient">当前患者</el-radio></el-radio-group>
        <div class="scope-patients">{{ scopePatients.map(item => item.displayName || item.id).join('、') || '尚未选择患者' }}</div>
      </el-form-item>
      <el-form-item label="档案内容（至少一项）"><el-checkbox-group v-model="sections" :disabled="creating"><el-checkbox v-for="item in sectionOptions" :key="item.value" :value="item.value">{{ item.label }}</el-checkbox></el-checkbox-group></el-form-item>
      <el-form-item label="日期范围（可选，最多 366 天）"><div class="export-dates"><label>起始日期<input v-model="dateFrom" type="date" aria-label="导出起始日期" :disabled="creating" /></label><label>结束日期<input v-model="dateTo" type="date" aria-label="导出结束日期" :disabled="creating" /></label></div></el-form-item>
      <el-form-item label="脱敏方式"><el-radio-group v-model="masked" :disabled="creating"><el-radio :value="true">严格脱敏</el-radio><el-radio :value="false" :disabled="!isManager">完整档案（仅管理员）</el-radio></el-radio-group></el-form-item>
      <p class="export-note">严格脱敏会移除患者及员工身份标识、联系方式，以及主诉、病史、备注等自由文本，保留结构化记录。完整档案包含敏感资料。</p>
      <p class="export-note">导出为 JSON 文件，提交、生成和下载时均校验院区、患者及记录权限；不包含照片和照片链接。生成后 24 小时内可下载，过期需重新提交。</p>
      <el-form-item label="导出用途（必填）"><el-input v-model="reason" type="textarea" :rows="2" maxlength="200" show-word-limit :disabled="creating" placeholder="请说明使用目的，作为导出审计依据" /></el-form-item>
      <el-alert v-if="formError" :title="formError" type="error" :closable="false" show-icon />
      <div class="export-actions"><el-button :disabled="creating" @click="formOpen = false">取消</el-button><el-button type="primary" native-type="submit" :loading="creating">提交导出任务</el-button></div>
    </el-form>
  </el-dialog>
  <el-drawer v-model="jobsOpen" title="档案导出任务" size="min(920px, 100vw)" class="record-export-drawer" destroy-on-close>
    <div class="export-task-heading"><p>仅显示本人导出任务；生成后 24 小时内可下载，过期需重新提交。</p><el-button :loading="loading" @click="loadJobs()">刷新任务</el-button></div>
    <el-alert v-if="jobsError" :title="jobsError" type="error" :closable="false" show-icon />
    <div v-loading="loading" class="export-task-list">
      <article v-for="job in jobs" :key="job.id" class="export-task" :data-export-id="job.id">
        <header><strong>{{ job.patientCount }} 位患者 · {{ job.masked ? '严格脱敏' : '完整档案' }}</strong><el-tag :type="jobStatus(job) === 'ready' ? 'success' : jobStatus(job) === 'failed' ? 'danger' : 'info'">{{ statusLabels[jobStatus(job)] }}</el-tag></header>
        <p>{{ sectionLabel(job.sections) }}<span v-if="job.dateFrom || job.dateTo"> · {{ job.dateFrom || '不限起始' }} 至 {{ job.dateTo || '不限结束' }}</span></p>
        <p>用途：{{ job.reason }}</p><small>提交：{{ formatTime(job.createdAt) }} · 提交人：{{ job.employeeId }}</small>
        <p v-if="job.expiresAt" class="export-expiry">下载有效期至：{{ formatTime(job.expiresAt) }}</p><p v-else-if="job.status === 'queued' || job.status === 'processing'" class="export-note">正在后台生成，完成后显示下载有效期。</p>
        <p v-if="job.status === 'failed'" class="export-failure">生成失败（{{ job.errorCode || '未知原因' }}），请核对权限与范围后重新提交。</p>
        <footer><small>{{ job.sizeBytes ? (job.sizeBytes / 1024).toFixed(1) + ' KB · ' : '' }}已下载 {{ job.downloadCount }} 次</small><div><el-button @click="loadAudit(job)">查看审计</el-button><el-button type="primary" :disabled="!canDownload(job) || !!downloading" :loading="downloading === job.id" @click="download(job)">下载档案</el-button></div></footer>
      </article>
      <el-empty v-if="!loading && !jobs.length && !jobsError" description="暂无导出任务" />
    </div>
    <el-pagination v-if="total > 20" v-model:current-page="page" :total="total" :page-size="20" layout="total, prev, pager, next" @current-change="loadJobs()" />
  </el-drawer>
  <el-dialog v-model="auditOpen" title="导出审计" width="760px" destroy-on-close>
    <p class="export-note">任务：{{ auditJob?.id }} · 用途：{{ auditJob?.reason }}</p>
    <el-alert v-if="auditError" :title="auditError" type="error" :closable="false" show-icon />
    <el-table v-loading="auditLoading" :data="events" empty-text="暂无审计记录"><el-table-column label="时间" width="165"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column><el-table-column label="操作"><template #default="{ row }">{{ eventLabels[row.action] || row.action }}</template></el-table-column><el-table-column prop="actorId" label="操作人" /><el-table-column prop="detailCode" label="结果代码" /></el-table>
  </el-dialog>
</template>

<style scoped>
.scope-patients { width: 100%; color: #6f8299; font-size: 12px; overflow-wrap: anywhere; }
.export-dates { display: flex; flex-wrap: wrap; gap: 18px; }
.export-dates label { display: grid; gap: 6px; font-size: 12px; color: #6f8299; }
.export-dates input { border: 1px solid #d6e0eb; border-radius: 6px; padding: 8px 12px; color: #30445e; font: inherit; }
.export-note { font-size: 12px; color: #72869d; line-height: 1.8; margin: 0 0 14px; }
.export-actions { display: flex; justify-content: flex-end; margin-top: 18px; }
.export-task-heading { display: flex; gap: 18px; align-items: start; margin-bottom: 18px; }
.export-task-heading p { flex: 1; font-size: 12px; color: #72869d; line-height: 1.8; }
.export-task-list { min-height: 120px; }
.export-task { border: 1px solid #e1e9f2; padding: 18px; border-radius: 12px; margin: 14px 0; }
.export-task header, .export-task footer { display: flex; justify-content: space-between; gap: 12px; align-items: center; }
.export-task p { font-size: 13px; color: #526b87; margin: 10px 0; overflow-wrap: anywhere; }
.export-task small { color: #7b8ea5; font-size: 12px; }
.export-task .export-expiry { color: #94743c; font-size: 12px; }
.export-task .export-failure { color: #ba655d; }
.export-task footer { margin-top: 14px; flex-wrap: wrap; }
</style>
