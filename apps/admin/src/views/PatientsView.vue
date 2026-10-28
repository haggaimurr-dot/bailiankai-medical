<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElCheckbox, ElMessage, ElMessageBox, ElTabPane, ElTabs } from 'element-plus'
import PatientChartDialog from '../components/PatientChartDialog.vue'
import PatientBindingDialog from '../components/PatientBindingDialog.vue'
import PrivatePhoto from '../components/PrivatePhoto.vue'
import RecordExports from '../components/RecordExports.vue'
import { auth } from '../auth'
import { api, errorMessage, type PatientRecord, type PatientRecordSummary, type Clinic, type ConsultationTemplate } from '../api'
import { shanghaiDate, slotTime } from '../dates'

const route = useRoute()
const router = useRouter()
const patients = ref<PatientRecordSummary[]>([])
const record = ref<PatientRecord | null>(null)
const canWrite = computed(() => ['manager', 'doctor', 'clinician'].includes(auth.employee?.role || ''))
const chartDialog = ref<InstanceType<typeof PatientChartDialog>>()
const bindingDialog = ref<InstanceType<typeof PatientBindingDialog>>()
const clinics = ref<Clinic[]>([])
const templates = ref<ConsultationTemplate[]>([])
const selectedTemplateId = ref('')
const templateSaving = ref(false)
const tagFilter = ref('')
const visiblePatients = computed(() => patients.value)
const availableTags = computed(() => [...new Set(patients.value.flatMap(patient => patient.profile?.tags || []))])
const search = ref('')
const phoneLast4 = ref('')
// 脱敏手机号：11 位显示前 3 位 + **** + 后 4 位；7 位及以下只显示 ****
const maskPhone = (value: string) => value && value.length > 7 ? value.slice(0, 3) + '****' + value.slice(-4) : value ? '****' : ''
const page = ref(1)
const total = ref(0)
const selectedPatientIds = ref<string[]>([])
const recordExports = ref<InstanceType<typeof RecordExports>>()
const canExport = computed(() => ['manager', 'doctor', 'clinician', 'nurse'].includes(auth.employee?.role || ''))
const selectedPatients = computed(() => patients.value.filter(patient => selectedPatientIds.value.includes(patient.id)))
const allVisibleSelected = computed(() => visiblePatients.value.length > 0 && visiblePatients.value.every(patient => selectedPatientIds.value.includes(patient.id)))
function selectVisible(value: unknown) { selectedPatientIds.value = value ? visiblePatients.value.slice(0, 20).map(patient => patient.id) : [] }
function togglePatientSelection(id: string, selected: unknown) {
  if (!selected) selectedPatientIds.value = selectedPatientIds.value.filter(value => value !== id)
  else if (!selectedPatientIds.value.includes(id) && selectedPatientIds.value.length < 20) selectedPatientIds.value.push(id)
}
const loading = ref(false)
const detailLoading = ref(false)
const saving = ref(false)
const error = ref('')
const tab = ref('consultations')
const consultationClinicId = ref('')
const emptyConsultation = () => ({ medicalHistory: '', allergyHistory: '', treatmentHistory: '', skinType: '', goals: '', chiefComplaint: '', history: '', assessment: '', advice: '', visitType: 'initial' as 'initial' | 'revisit', parentConsultationId: '', appointmentId: '', visitedAt: '', nextVisitAt: '', stageIndex: 0, stageStatus: 'not_started' as 'not_started' | 'in_progress' | 'completed' })
const form = ref(emptyConsultation())
const consultationDates = ref<string[]>([])
const parentRecords = ref<PatientRecord['consultations']>([])
const stageLabels = { not_started: '未开始', in_progress: '进行中', completed: '已完成' }
const parentOptions = computed(() => parentRecords.value.filter(item => item.clinicId === consultationClinicId.value))
const toTimestamp = (value: string) => new Date(value + '+08:00').toISOString()
async function filterConsultations() {
  if (!record.value || saving.value) return
  const id = record.value.patient.id, version = ++requestVersion
  detailLoading.value = true
  try {
    const dates = consultationDates.value?.length === 2 ? { dateFrom: consultationDates.value[0], dateTo: consultationDates.value[1] } : undefined
    const result = await api.patientRecord(id, dates)
    if (version === requestVersion && record.value?.patient.id === id) record.value.consultations = result.consultations
  } catch (cause) { if (version === requestVersion) error.value = errorMessage(cause) }
  finally { if (version === requestVersion) detailLoading.value = false }
}
const previews = ref<Record<string, boolean>>({})
let requestVersion = 0
let listVersion = 0
const encounters = computed(() => record.value?.encounters || [])
const consultationClinics = computed(() => [...new Map([...(record.value?.patient.profile ? [{ id: record.value.patient.profile.clinicId, name: clinics.value.find(clinic => clinic.id === record.value?.patient.profile?.clinicId)?.name || record.value.patient.profile.clinicId }] : []), ...encounters.value.filter(encounter => auth.employee?.role === 'manager' || encounter.appointment.doctorId === auth.employee?.staffId).map(encounter => ({ id: encounter.appointment.clinicId, name: encounter.appointment.clinicName }))].map(clinic => [clinic.id, clinic])).values()])
const plans = computed(() => encounters.value.flatMap(encounter => encounter.carePlan.versions.map(plan => ({ ...plan, appointment: encounter.appointment }))))
const draftPlans = computed(() => encounters.value.filter(encounter => encounter.carePlan.draft))
const treatments = computed(() => encounters.value.flatMap(encounter => encounter.followup.treatment ? [{ ...encounter.followup.treatment, appointment: encounter.appointment }] : []))
const followups = computed(() => encounters.value.flatMap(encounter => encounter.followup.tasks))
const photos = computed(() => encounters.value.flatMap(encounter => [
  ...encounter.photos.map(photo => ({ ...photo, source: 'care' as const, label: '面诊照片', appointment: encounter.appointment })),
  ...encounter.followup.tasks.flatMap(task => task.photos.map(photo => ({ ...photo, source: 'followup' as const, label: task.title, appointment: encounter.appointment }))),
]))
const statusLabels: Record<string, string> = { pending: '待处理', confirmed: '已确认', completed: '已完成', cancelled: '已取消', rejected: '已拒绝', expired: '已过期', submitted: '待审核', reviewed: '已审核' }
const angleLabels: Record<string, string> = { front: '正面', left: '左侧', right: '右侧' }
const formatTime = (value: string) => shanghaiDate(new Date(value)) + ' ' + slotTime(value)

function releasePhotos() {
  previews.value = {}
}

async function loadList() {
  const version = ++listVersion
  loading.value = true
  error.value = ''
  try {
    const result = await api.patients(search.value.trim(), page.value, tagFilter.value, phoneLast4.value.replace(/\D/g, '').slice(0, 4))
    if (version !== listVersion) return
    patients.value = result.data
    selectedPatientIds.value = selectedPatientIds.value.filter(id => result.data.some(patient => patient.id === id))
    total.value = result.total
  } catch (cause) { if (version === listVersion) error.value = errorMessage(cause) }
  finally { if (version === listVersion) loading.value = false }
}

async function selectPatient(id: string, resetForm = true) {
  const version = ++requestVersion
  detailLoading.value = true
  record.value = null
  error.value = ''
  releasePhotos()
  if (resetForm) { form.value = emptyConsultation(); selectedTemplateId.value = '' }
  try {
    const result = await api.patientRecord(id)
    if (version !== requestVersion) return
    record.value = result
    parentRecords.value = result.consultations
    consultationDates.value = []
    consultationClinicId.value = consultationClinics.value[0]?.id || ''
    await router.replace({ query: { patientId: id } })
  } catch (cause) { if (version === requestVersion) error.value = errorMessage(cause) }
  finally { if (version === requestVersion) detailLoading.value = false }
}

async function saveConsultation() {
  if (!record.value || saving.value) return
  if (!consultationClinicId.value) { ElMessage.warning('请选择有权记录问诊的院区'); return }
  if (!form.value.chiefComplaint.trim()) { ElMessage.warning('请填写问诊主诉'); return }
  const id = record.value.patient.id
  if (form.value.visitType === 'revisit' && !form.value.parentConsultationId) { ElMessage.warning('请选择关联的既往问诊'); return }
  const { parentConsultationId, appointmentId, visitedAt, nextVisitAt, ...fields } = form.value
  const body = { ...fields, clinicId: consultationClinicId.value,
    ...(fields.visitType === 'revisit' ? { parentConsultationId } : {}), ...(appointmentId ? { appointmentId } : {}),
    ...(visitedAt ? { visitedAt: toTimestamp(visitedAt) } : {}), ...(nextVisitAt ? { nextVisitAt: toTimestamp(nextVisitAt) } : {}) }
  saving.value = true
  try {
    const consultation = await api.createConsultation(id, body)
    if (record.value?.patient.id === id) {
      parentRecords.value = [consultation, ...parentRecords.value]
      const day = shanghaiDate(new Date(consultation.visitedAt || consultation.recordedAt))
      if (!consultationDates.value?.length || (day >= consultationDates.value[0] && day <= consultationDates.value[1])) record.value.consultations.unshift(consultation)
      form.value = emptyConsultation()
    }
    ElMessage.success('问诊记录已保存')
  } catch (cause) { ElMessage.error(errorMessage(cause)) }
  finally { saving.value = false }
}

function preview(photo: typeof photos.value[number]) {
  const key = photo.source + photo.id
  previews.value[key] = true
}

const templateFields = ['chiefComplaint', 'history', 'assessment', 'advice', 'medicalHistory', 'allergyHistory', 'treatmentHistory', 'skinType', 'goals'] as const
const availableTemplates = computed(() => templates.value.filter(item => item.clinicId === consultationClinicId.value))
function applyTemplate() {
 const item = availableTemplates.value.find(item => item.id === selectedTemplateId.value)
 if (!item) return
 for (const key of templateFields) if (!form.value[key].trim()) form.value[key] = item[key] || ''
 form.value.visitType = item.visitType || 'initial'
 if (form.value.visitType === 'initial') form.value.parentConsultationId = ''
 ElMessage.success('模板已填入空白字段，请核对患者实际情况')
}
async function saveTemplate() {
 if (!consultationClinicId.value || templateSaving.value) return
 try {
  const { value } = await ElMessageBox.prompt('将当前问诊内容保存为可复用模板，请使用通用内容。', '保存问诊模板', { inputPlaceholder: '模板名称', inputValidator: value => !!value?.trim() && [...value.trim()].length <= 64 || '请输入1至64字模板名称' })
  templateSaving.value = true
  const fields = Object.fromEntries(templateFields.map(key => [key, form.value[key]])) as Pick<typeof form.value, typeof templateFields[number]>
  const item = await api.createConsultationTemplate({ ...fields, name: value.trim(), clinicId: consultationClinicId.value, visitType: form.value.visitType })
  templates.value.unshift(item); selectedTemplateId.value = item.id
  ElMessage.success('问诊模板已保存')
 } catch (cause) { if (cause !== 'cancel' && cause !== 'close') ElMessage.error(errorMessage(cause)) }
 finally { templateSaving.value = false }
}
async function deleteTemplate() {
 const id = selectedTemplateId.value
 if (!id || templateSaving.value) return
 templateSaving.value = true
 try { await api.deleteConsultationTemplate(id); templates.value = templates.value.filter(item => item.id !== id); selectedTemplateId.value = ''; ElMessage.success('模板已删除') }
 catch (cause) { ElMessage.error(errorMessage(cause)) }
 finally { templateSaving.value = false }
}
async function chartSaved(id: string) { await loadList(); await selectPatient(id); tab.value = 'consultations' }
function changeClinic() { form.value.parentConsultationId = ''; form.value.appointmentId = ''; selectedTemplateId.value = '' }
function applySearch() { page.value = 1; void loadList() }
onMounted(async () => {
  await Promise.all([loadList(), (async () => {
    try { clinics.value = (await api.clinics()).filter(clinic => auth.employee?.clinicIds?.includes(clinic.id)) }
    catch (cause) { ElMessage.error(errorMessage(cause)) }
  })(), (async () => {
    if (!canWrite.value) return
    try { templates.value = await api.consultationTemplates() }
    catch (cause) { ElMessage.error(errorMessage(cause)) }
  })()])
  if (typeof route.query.patientId === 'string') await selectPatient(route.query.patientId)
  if (route.query.exports === 'open') recordExports.value?.openJobs()
})
onBeforeUnmount(() => { requestVersion++; listVersion++; releasePhotos() })
</script>

<template>
  <div class="page-heading"><div><div class="eyebrow">PATIENT RECORDS</div><h1>统一患者档案</h1><p>以患者为中心，串联问诊、预约、方案、治疗与随访</p></div><div class="record-page-actions"><el-button v-if="canWrite" type="primary" @click="chartDialog?.open()">新建患者档案</el-button><el-button v-if="canExport" @click="recordExports?.openJobs()">导出任务</el-button><el-button @click="loadList">刷新目录</el-button></div></div>
  <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="record-alert" />
  <div class="patient-layout">
    <section class="panel patient-directory" v-loading="loading">
      <h2>患者目录 <small>{{ total }} 位</small></h2>
      <form class="patient-search" @submit.prevent="applySearch"><el-input v-model="search" placeholder="姓名 / 患者编号" clearable maxlength="64" /><el-input v-model="phoneLast4" placeholder="手机号后四位" clearable maxlength="4" inputmode="numeric" class="phone-last4" @input="phoneLast4 = phoneLast4.replace(/\D/g, '').slice(0, 4)" /><el-button native-type="submit">搜索</el-button></form>
      <el-select aria-label="患者标签筛选" v-model="tagFilter" clearable filterable allow-create default-first-option placeholder="按患者标签筛选" class="tag-filter" @change="applySearch"><el-option v-for="tag in availableTags" :key="tag" :value="tag" :label="tag" /></el-select>
      <div v-if="canExport && patients.length" class="patient-selection"><el-checkbox :model-value="allVisibleSelected" :indeterminate="selectedPatients.length > 0 && !allVisibleSelected" @change="selectVisible">选择本页患者</el-checkbox><el-button size="small" :disabled="!selectedPatients.length" @click="recordExports?.openCreate('selected')">导出已选（{{ selectedPatients.length }}）</el-button><small>每次最多导出 20 位患者</small></div>
      <el-empty v-if="!loading && !visiblePatients.length" description="暂无匹配患者" />
      <div v-for="patient in visiblePatients" :key="patient.id" class="patient-directory-item"><el-checkbox v-if="canExport" :model-value="selectedPatientIds.includes(patient.id)" :aria-label="'选择患者 ' + (patient.displayName || patient.id)" :disabled="!selectedPatientIds.includes(patient.id) && selectedPatientIds.length >= 20" @change="togglePatientSelection(patient.id, $event)" /><button class="patient-row" :class="{ active: record?.patient.id === patient.id }" :disabled="saving" @click="selectPatient(patient.id)">
        <strong>{{ patient.displayName || '未设置姓名' }}</strong><span>{{ patient.appointmentCount }} 次预约</span><span v-if="patient.phoneMasked">手机号：{{ patient.phoneMasked }}</span><small>{{ patient.id }} · {{ patient.wechatBound ? '已绑定微信' : '未绑定微信' }}</small><div class="patient-tags"><el-tag v-for="tag in patient.profile?.tags" :key="tag" size="small">{{ tag }}</el-tag></div>
      </button></div>
      <el-pagination v-if="total > 20" v-model:current-page="page" :total="total" :page-size="20" layout="prev, pager, next" small @current-change="loadList" />
    </section>
    <section class="panel patient-detail" v-loading="detailLoading">
      <el-empty v-if="!record && !detailLoading" description="从患者目录选择患者，查看完整档案" />
      <template v-if="record">
        <header class="patient-header"><div><h2>{{ record.patient.displayName || '未设置姓名' }}</h2><p>患者编号：{{ record.patient.id }}</p><p v-if="record.patient.phoneMasked">手机号：{{ record.patient.phoneMasked }}</p><p>建档时间：{{ formatTime(record.patient.createdAt) }}</p></div><div class="record-header-actions"><el-tag :type="record.patient.wechatBound ? 'success' : 'info'">{{ record.patient.wechatBound ? '已绑定微信' : '未绑定微信' }}</el-tag><el-button v-if="canWrite && record.patient.profile" :disabled="saving" @click="bindingDialog?.open(record.patient)">微信身份绑定</el-button><el-button v-if="canWrite" :disabled="saving" @click="chartDialog?.open(record.patient, consultationClinicId)">编辑资料 / 标签</el-button><el-button v-if="canExport" @click="recordExports?.openCreate('current')">导出此档案</el-button></div></header>
        <div v-if="record.patient.profile" class="patient-profile"><span>性别：{{ { unknown: '未填写', female: '女', male: '男' }[record.patient.profile.gender] }}</span><span>出生日期：{{ record.patient.profile.birthDate || '未填写' }}</span><span>联系手机号：{{ maskPhone(record.patient.profile.contactPhone) || '未填写' }}</span></div>
        <div class="patient-tags"><el-tag v-for="tag in record.patient.profile?.tags" :key="tag">{{ tag }}</el-tag></div>
        <p class="access-note">仅显示当前账号有权查看的资料；医护问诊记录仅本人和管理员可见。照片预览仍需患者有效授权。</p>
        <el-tabs v-model="tab">
          <el-tab-pane :label="'问诊记录 (' + record.consultations.length + ')'" name="consultations">
            <el-date-picker v-model="consultationDates" type="daterange" value-format="YYYY-MM-DD" start-placeholder="问诊起始日期" end-placeholder="问诊结束日期" :disabled="saving" @change="filterConsultations" />
            <h3>新增问诊 / 复诊记录</h3>
            <el-form v-if="canWrite" label-position="top" @submit.prevent="saveConsultation">
              <el-form-item label="问诊所属院区"><el-select v-model="consultationClinicId" :disabled="saving" @change="changeClinic"><el-option v-for="clinic in consultationClinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></el-form-item><div class="template-toolbar"><el-select aria-label="问诊模板" v-model="selectedTemplateId" clearable placeholder="选择问诊模板" :disabled="saving || templateSaving"><el-option v-for="item in availableTemplates" :key="item.id" :value="item.id" :label="item.name + (item.visitType === 'revisit' ? ' · 复诊' : ' · 初诊')" /></el-select><el-button :disabled="!selectedTemplateId || saving || templateSaving" @click="applyTemplate">应用模板</el-button><el-button :disabled="!consultationClinicId || saving" :loading="templateSaving" @click="saveTemplate">存为模板</el-button><el-button :disabled="!selectedTemplateId || saving || templateSaving" @click="deleteTemplate">删除模板</el-button><small>应用时填充空白字段；患者、预约、复诊关联和日期需单独填写。</small></div>
              <el-form-item label="主诉（必填）"><el-input v-model="form.chiefComplaint" type="textarea" :rows="2" maxlength="1000" show-word-limit :disabled="saving" /></el-form-item>
              <el-form-item label="就诊类型"><el-select v-model="form.visitType" :disabled="saving"><el-option label="初诊" value="initial" /><el-option label="复诊" value="revisit" /></el-select></el-form-item>
              <el-form-item v-if="form.visitType === 'revisit'" label="关联既往问诊（必填）"><el-select v-model="form.parentConsultationId" :disabled="saving"><el-option v-for="item in parentOptions" :key="item.id" :value="item.id" :label="formatTime(item.visitedAt || item.recordedAt) + ' · ' + item.chiefComplaint" /></el-select></el-form-item>
              <el-form-item label="关联预约（可选）"><el-select v-model="form.appointmentId" clearable :disabled="saving"><el-option v-for="item in encounters.filter(item => item.appointment.clinicId === consultationClinicId)" :key="item.appointment.id" :value="item.appointment.id" :label="formatTime(item.appointment.startsAt) + ' · ' + item.appointment.doctorName" /></el-select></el-form-item>
              <el-form-item label="实际问诊时间（默认当前时间）"><el-date-picker v-model="form.visitedAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ss" :disabled="saving" /></el-form-item>
              <el-form-item label="建议下次复诊时间"><el-date-picker v-model="form.nextVisitAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ss" :disabled="saving" /></el-form-item>
              <el-form-item label="当前阶段（0表示未指定）"><el-input-number v-model="form.stageIndex" :min="0" :max="8" :disabled="saving" /></el-form-item>
              <el-form-item label="阶段进度"><el-select v-model="form.stageStatus" :disabled="saving"><el-option v-for="(label, value) in stageLabels" :key="value" :label="label" :value="value" /></el-select></el-form-item>
              <h3>结构化问诊信息</h3>
              <div class="consultation-grid"><el-form-item label="肤质"><el-select v-model="form.skinType" clearable :disabled="saving"><el-option v-for="value in ['干性', '油性', '混合性', '中性', '敏感性', '待评估']" :key="value" :label="value" :value="value" /></el-select></el-form-item><el-form-item label="患者诉求"><el-input v-model="form.goals" type="textarea" :rows="2" maxlength="5000" :disabled="saving" /></el-form-item></div>
              <el-form-item label="既往病史"><el-input v-model="form.medicalHistory" type="textarea" :rows="2" maxlength="5000" :disabled="saving" /></el-form-item>
              <el-form-item label="过敏史"><el-input v-model="form.allergyHistory" type="textarea" :rows="2" maxlength="5000" :disabled="saving" /></el-form-item>
              <el-form-item label="既往治疗史"><el-input v-model="form.treatmentHistory" type="textarea" :rows="2" maxlength="5000" :disabled="saving" /></el-form-item>
              <el-form-item label="病史 / 过敏史"><el-input v-model="form.history" type="textarea" :rows="2" maxlength="5000" :disabled="saving" /></el-form-item>
              <el-form-item label="评估 / 诊断"><el-input v-model="form.assessment" type="textarea" :rows="2" maxlength="5000" :disabled="saving" /></el-form-item>
              <el-form-item label="处理建议"><el-input v-model="form.advice" type="textarea" :rows="2" maxlength="5000" :disabled="saving" /></el-form-item>
              <el-button type="primary" native-type="submit" :loading="saving">保存问诊记录</el-button>
            </el-form>
            <article v-for="item in record.consultations" :key="item.id" class="record-card"><h3>{{ item.visitType === 'revisit' ? '复诊' : '初诊' }} · {{ item.chiefComplaint }}</h3><p v-if="item.parentConsultationId">关联问诊：{{ parentRecords.find(parent => parent.id === item.parentConsultationId)?.chiefComplaint || item.parentConsultationId }}</p><p v-if="item.stageIndex">阶段 {{ item.stageIndex }} · {{ stageLabels[item.stageStatus || 'not_started'] }}</p><p v-if="item.nextVisitAt">建议复诊：{{ formatTime(item.nextVisitAt) }}</p><small>{{ formatTime(item.visitedAt || item.recordedAt) }} · 记录人 {{ item.recordedBy }}</small><p v-if="item.skinType">肤质：{{ item.skinType }}</p><p v-if="item.goals">诉求：{{ item.goals }}</p><p v-if="item.medicalHistory">既往病史：{{ item.medicalHistory }}</p><p v-if="item.allergyHistory">过敏史：{{ item.allergyHistory }}</p><p v-if="item.treatmentHistory">既往治疗史：{{ item.treatmentHistory }}</p><p v-if="item.history">病史：{{ item.history }}</p><p v-if="item.assessment">评估：{{ item.assessment }}</p><p v-if="item.advice">建议：{{ item.advice }}</p></article>
            <el-empty v-if="!record.consultations.length" description="暂无问诊记录，问诊记录不依赖预约" />
          </el-tab-pane>
          <el-tab-pane :label="'预约 (' + encounters.length + ')'" name="appointments">
            <el-table :data="encounters"><el-table-column label="预约时间"><template #default="{ row }">{{ formatTime(row.appointment.startsAt) }}</template></el-table-column><el-table-column prop="appointment.clinicName" label="诊所" /><el-table-column prop="appointment.doctorName" label="医生" /><el-table-column label="状态"><template #default="{ row }">{{ statusLabels[row.appointment.status] || row.appointment.status }}</template></el-table-column><el-table-column prop="appointment.note" label="预约备注" /></el-table>
          </el-tab-pane>
          <el-tab-pane :label="'方案 (' + (plans.length + draftPlans.length) + ')'" name="plans">
            <article v-for="encounter in draftPlans" :key="encounter.appointment.id" class="record-card"><h3>{{ encounter.carePlan.draft?.summary }} <el-tag size="small" type="warning">未发布草稿</el-tag></h3><small>{{ encounter.appointment.doctorName }} · 预约 {{ formatTime(encounter.appointment.startsAt) }}</small><RouterLink :to="{ path: '/care', query: { appointmentId: encounter.appointment.id } }">查看方案草稿</RouterLink></article>
            <article v-for="plan in plans" :key="plan.id" class="record-card"><h3>{{ plan.content.summary }} <el-tag size="small">V{{ plan.version }}</el-tag></h3><small>{{ formatTime(plan.publishedAt) }} · {{ plan.appointment.doctorName }}</small><p v-for="(stage, index) in plan.content.stages" :key="index"><strong>{{ stage.title }}</strong>：{{ stage.body }}</p><p>{{ plan.content.instructions }}</p><p>发布说明：{{ plan.changeReason }}</p><RouterLink :to="{ path: '/care', query: { appointmentId: plan.appointment.id } }">查看方案与草稿</RouterLink></article>
            <el-empty v-if="!plans.length && !draftPlans.length" description="暂无方案" />
          </el-tab-pane>
          <el-tab-pane :label="'治疗 (' + treatments.length + ')'" name="treatments">
            <article v-for="item in treatments" :key="item.id" class="record-card"><h3>{{ item.summary }}</h3><small>{{ formatTime(item.treatedAt) }} · {{ item.appointment.doctorName }} · V{{ item.version }}</small><p v-if="item.stageIndex">方案阶段 {{ item.stageIndex }}</p><p>{{ item.procedure }} · {{ item.bodyArea }} · {{ item.device }}</p><p>{{ item.parameters }}</p><p>{{ item.notes }}</p><RouterLink :to="{ path: '/followups', query: { appointmentId: item.appointment.id } }">查看治疗与修订记录</RouterLink></article><el-empty v-if="!treatments.length" description="暂无治疗记录" />
          </el-tab-pane>
          <el-tab-pane :label="'随访 (' + followups.length + ')'" name="followups">
            <article v-for="item in followups" :key="item.id" class="record-card"><h3>{{ item.title }} <el-tag size="small">{{ statusLabels[item.status] || item.status }}</el-tag></h3><small>{{ formatTime(item.dueAt) }} · {{ item.assigneeName || '未分配' }}</small><p v-if="item.symptoms">症状反馈：{{ item.symptoms }}</p><p v-if="item.response">医护回复：{{ item.response }}</p><RouterLink :to="{ path: '/followups', query: { appointmentId: item.appointmentId, taskId: item.id } }">处理随访</RouterLink></article><el-empty v-if="!followups.length" description="暂无随访任务" />
          </el-tab-pane>
          <el-tab-pane :label="'照片 (' + photos.length + ')'" name="photos">
            <p class="access-note">按患者汇总面诊及随访照片；未授权或已撤销授权的照片不显示。</p>
            <div class="patient-photos"><article v-for="photo in photos" :key="photo.source + photo.id" class="record-card"><PrivatePhoto v-if="tab === 'photos' && previews[photo.source + photo.id]" :id="photo.id" :source="photo.source" :alt="photo.label" /><el-button v-else @click="preview(photo)">查看授权照片</el-button><h3>{{ photo.label }} · {{ angleLabels[photo.angle] || photo.angle }}</h3><small>{{ formatTime(photo.uploadedAt) }}</small><p>{{ photo.appointment.doctorName }} · 预约 {{ formatTime(photo.appointment.startsAt) }}</p></article></div><el-empty v-if="!photos.length" description="暂无已授权照片" />
          </el-tab-pane>
        </el-tabs>
      </template>
    </section>
  </div>
  <PatientBindingDialog ref="bindingDialog" @refreshed="chartSaved" />
  <PatientChartDialog ref="chartDialog" :clinics="clinics" @saved="chartSaved" />
  <RecordExports ref="recordExports" :selected-patients="selectedPatients" :current-patient="record?.patient" />
</template>

<style scoped>
.patient-profile, .patient-tags, .template-toolbar { display: flex; flex-wrap: wrap; gap: 8px; }
.patient-profile { font-size: 13px; margin-bottom: 12px; }
.patient-tags { grid-column: 1 / -1; }
.tag-filter { margin-bottom: 12px; width: 100%; }
.template-toolbar { margin-bottom: 18px; align-items: center; }
.template-toolbar small { width: 100%; color: #7a8a9e; }
.consultation-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.patient-layout { display: grid; grid-template-columns: 290px minmax(0, 1fr); gap: 18px; align-items: start; }
.patient-directory, .patient-detail { padding: 22px; min-width: 0; }
.patient-directory h2, .patient-header h2 { margin: 0 0 12px; font-size: 20px; }
.patient-directory h2 small { font-size: 12px; font-weight: normal; color: #7a8a9e; }
.patient-search { display: flex; gap: 8px; margin-bottom: 16px; }
.patient-search .phone-last4 { max-width: 140px; }
.record-page-actions, .record-header-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
.record-page-actions .el-button + .el-button { margin-left: 0; }
.patient-selection { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; margin: 0 0 14px; }
.patient-selection small { width: 100%; color: #7a8a9e; font-size: 11px; }
.patient-directory-item { display: flex; align-items: center; gap: 9px; }
.patient-directory-item > .el-checkbox { flex-shrink: 0; }
.patient-directory-item .patient-row { flex: 1; min-width: 0; }
.patient-row { display: grid; width: 100%; grid-template-columns: 1fr auto; gap: 8px; padding: 14px 12px; border: 1px solid #e5edf5; border-radius: 10px; background: white; text-align: left; margin-bottom: 10px; cursor: pointer; color: #30445e; }
.patient-row.active { background: #eef5ff; border-color: #a4c9f6; }
.patient-row small { grid-column: 1 / -1; overflow-wrap: anywhere; color: #7a8a9e; }
.patient-row span { font-size: 12px; }
.patient-header { display: flex; justify-content: space-between; gap: 14px; }
.patient-header p, .access-note { font-size: 12px; color: #7a8a9e; line-height: 1.8; overflow-wrap: anywhere; }
.record-card { padding: 18px; margin: 16px 0; border: 1px solid #e5edf5; border-radius: 12px; }
.record-card h3 { font-size: 15px; margin: 0 0 8px; }
.record-card small { color: #7a8a9e; }
.record-card p { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.8; font-size: 13px; }
.record-card a { color: #387bd4; font-size: 13px; }
.patient-photos { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 14px; }
.patient-photos .el-image { width: 100%; height: 200px; }
.record-alert { margin-bottom: 16px; }
@media (max-width: 1000px) { .patient-layout { grid-template-columns: 1fr; } }
</style>
