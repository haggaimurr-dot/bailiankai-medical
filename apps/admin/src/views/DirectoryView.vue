<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, ElCheckbox, ElRadio, ElCollapse, ElCollapseItem } from 'element-plus'
import { api, ApiError, errorMessage, type ClinicAdmin, type DoctorAdmin, type ClinicInput, type DoctorInput, type DirectoryRevision } from '../api'
import { auth, canAccess, setAuth, type Employee } from '../auth'
import { useScheduleStore } from '../stores/schedule'
import CredentialEditor from '../components/CredentialEditor.vue'
import DirectoryPreview from '../components/DirectoryPreview.vue'

type Kind = 'clinics' | 'doctors'
type Row = ClinicAdmin | DoctorAdmin
const route = useRoute(), router = useRouter(), schedule = useScheduleStore()
const kind = computed<Kind>(() => route.path === '/clinics' ? 'clinics' : 'doctors')
const label = computed(() => kind.value === 'clinics' ? '院区' : '医生')
const clinics = ref<ClinicAdmin[]>([]), doctors = ref<DoctorAdmin[]>([])
const loading = ref(false), busy = ref(false), error = ref(''), ready = ref(false)
const search = ref(''), clinicFilter = ref(''), stateFilter = ref('')
const rows = computed(() => (kind.value === 'clinics' ? clinics.value : doctors.value).filter(row =>
  (!search.value.trim() || row.name.includes(search.value.trim())) &&
  (kind.value === 'clinics' || !clinicFilter.value || ('clinicId' in row && row.clinicId === clinicFilter.value)) &&
  (!stateFilter.value || (stateFilter.value === 'inactive' ? !row.active : stateFilter.value === 'draft' ? !row.publishedAt : row.active))))
function clinicName(id?: string) { return clinics.value.find(row => row.id === id)?.name || id || '' }
function timestamp(value: string | null) {
  if (!value || !Number.isFinite(Date.parse(value))) return '—'
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
}
async function load() {
  if (loading.value || busy.value) return
  loading.value = true; error.value = ''; ready.value = false
  try { const [c, d] = await Promise.all([api.adminClinics(), api.adminDoctors()]); clinics.value = c; doctors.value = d; ready.value = true }
  catch (cause) { error.value = errorMessage(cause) }
  finally { loading.value = false }
}
function replace(row: Row, target: Kind) {
  if (target === 'clinics') { const index = clinics.value.findIndex(item => item.id === row.id); if (index < 0) clinics.value.push(row as ClinicAdmin); else clinics.value[index] = row as ClinicAdmin }
  else { const index = doctors.value.findIndex(item => item.id === row.id); if (index < 0) doctors.value.push(row as DoctorAdmin); else doctors.value[index] = row as DoctorAdmin }
}
async function viewSchedule(row: DoctorAdmin) {
  schedule.clinicId = row.clinicId; schedule.doctorId = row.id
  await router.push('/schedule'); await schedule.refresh()
}
function rowKind(row: Row): Kind { return 'clinicId' in row ? 'doctors' : 'clinics' }
async function failure(cause: unknown, setError: (message: string) => void) {
  setError(errorMessage(cause))
  if (cause instanceof ApiError && cause.status === 409) {
    try { const [c, d] = await Promise.all([api.adminClinics(), api.adminDoctors()]); clinics.value = c; doctors.value = d }
    catch { ready.value = false; error.value = '最新资料加载失败，请刷新后继续。' }
    setError(errorMessage(cause) + '。列表已尝试刷新；请关闭并重新打开操作，以最新版本重试。')
  }
}
const editorOpen = ref(false), editorKind = ref<Kind>('doctors'), editing = ref<Row | null>(null), editorError = ref(''), staleEditor = ref(false)
const doctorForm = ref<DoctorInput>({ clinicId: '', name: '', title: '', summary: '', focus: [], initials: '', avatarUrl: '', serviceIntroduction: '', presentation: { isDemo: false, credentials: [] } })
const clinicForm = ref<ClinicInput>({ name: '', address: '', phone: '', presentation: { isDemo: false, brandName: '', tagline: '', introduction: '', logoUrl: '', hours: '', transport: '', arrivalGuide: '', credentials: [], environmentImages: [], featuredReviews: [] } })
const focusText = ref(''), latitude = ref<number | undefined>(), longitude = ref<number | undefined>()
function edit(row?: Row) {
  editorKind.value = row ? rowKind(row) : kind.value; editing.value = row ? JSON.parse(JSON.stringify(row)) : null
  editorError.value = ''; staleEditor.value = false
  if (editorKind.value === 'doctors') {
    const doctor = row as DoctorAdmin | undefined
    doctorForm.value = { clinicId: doctor?.clinicId || clinicFilter.value || clinics.value[0]?.id || '', name: doctor?.name || '', title: doctor?.title || '', summary: doctor?.summary || '', focus: [...(doctor?.focus || [])], initials: doctor?.initials || '', avatarUrl: doctor?.avatarUrl || '', serviceIntroduction: doctor?.serviceIntroduction || '', presentation: JSON.parse(JSON.stringify(doctor?.presentation || { isDemo: false, credentials: [] })) }
    focusText.value = doctorForm.value.focus.join('、')
  } else {
    const clinic = row as ClinicAdmin | undefined
    clinicForm.value = { name: clinic?.name || '', address: clinic?.address || '', phone: clinic?.phone || '', presentation: JSON.parse(JSON.stringify(clinic?.presentation || { isDemo: false, brandName: clinic?.name || '', tagline: '', introduction: '', logoUrl: '', hours: '', transport: '', arrivalGuide: '', credentials: [] })) }
    if (clinicForm.value.presentation) {
      clinicForm.value.presentation.environmentImages = clinicForm.value.presentation.environmentImages || []
      clinicForm.value.presentation.featuredReviews = clinicForm.value.presentation.featuredReviews || []
    }
    latitude.value = clinicForm.value.presentation?.location?.latitude; longitude.value = clinicForm.value.presentation?.location?.longitude
  }
  editorOpen.value = true
}
function https(value: string) {
  if (!value) return true
  try { const url = new URL(value); return value.length <= 2048 && url.protocol === 'https:' && !!url.hostname && !url.username && !url.password && !url.hash } catch { return false }
}
function input(): ClinicInput | DoctorInput {
  if (editorKind.value === 'doctors') return { ...doctorForm.value, focus: focusText.value.split(/[、,，\n]/).map(value => value.trim()).filter(Boolean) }
  const form = JSON.parse(JSON.stringify(clinicForm.value)) as ClinicInput
  if (form.presentation) {
    delete form.presentation.location
    if (latitude.value != null && longitude.value != null) form.presentation.location = { latitude: latitude.value, longitude: longitude.value }
  }
  return form
}
function validate(body: ClinicInput | DoctorInput) {
  if (!body.name.trim()) return '请填写名称。'
  if ('clinicId' in body) {
    if (!body.clinicId || !body.title.trim() || !body.summary.trim() || !body.initials.trim()) return '请填写所属院区、职称、简介与头像缩写。'
    if (!https(body.avatarUrl)) return '头像地址须为有效 HTTPS 地址。'
    if (body.focus.length > 20 || body.focus.some(value => [...value].length > 80)) return '擅长项目最多 20 项，每项最多 80 字。'
  } else {
    if (!body.address.trim() || !body.phone.trim() || !body.presentation?.brandName.trim()) return '请填写地址、电话与品牌名称。'
    if (!https(body.presentation.logoUrl)) return '标志图片须为有效 HTTPS 地址。'
    if ((latitude.value == null) !== (longitude.value == null)) return '经纬度须同时填写或同时留空。'
    if (latitude.value != null && (!Number.isFinite(latitude.value) || latitude.value < -90 || latitude.value > 90)) return '纬度须在 -90 至 90 之间。'
    if (longitude.value != null && (!Number.isFinite(longitude.value) || longitude.value < -180 || longitude.value > 180)) return '经度须在 -180 至 180 之间。'
    const environmentImages = body.presentation?.environmentImages || []
    if (environmentImages.length > 10 || environmentImages.some(row => !row.imageUrl || !https(row.imageUrl) || [...row.caption].length > 200)) return '环境图最多 10 张，每张须填写有效 HTTPS 图片地址，说明最多 200 字。'
    const featuredReviews = body.presentation?.featuredReviews || []
    if (featuredReviews.length > 10 || featuredReviews.some(row => !row.content.trim() || [...row.content].length > 1000 || [...row.displayName].length > 50 || (row.rating != null && (row.rating < 0 || row.rating > 5)))) return '精选评价最多 10 条，每条须填写内容（最多 1000 字），署名最多 50 字，评分 0–5 分。'
  }
  const credentials = body.presentation?.credentials || []
  if (credentials.length > 20 || credentials.some(row => !row.name.trim() || !https(row.imageUrl))) return '每项资质须填写名称，附件地址须为有效 HTTPS 地址。'
  return ''
}
async function save() {
  if (busy.value || staleEditor.value) return
  const body = input(); editorError.value = validate(body); if (editorError.value) return
  busy.value = true
  try {
    const saved = 'clinicId' in body ? await api.saveDoctor(body, editing.value?.id, editing.value?.version) : await api.saveClinic(body, editing.value?.id, editing.value?.version)
    replace(saved, editorKind.value); editorOpen.value = false; ElMessage.success('草稿已保存，发布后患者可见')
    if (!editing.value && editorKind.value === 'clinics') {
      try { setAuth(auth.token, await api.employeeSession()) } catch { error.value = '院区已创建，账号授权资料刷新失败，请重新登录后查看。' }
    }
  } catch (cause) { staleEditor.value = cause instanceof ApiError && cause.status === 409; await failure(cause, message => { editorError.value = message }) }
  finally { busy.value = false }
}
async function toggle(row: Row) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    await ElMessageBox.confirm(`确认${row.active ? '停用' : '启用'}「${row.name}」？${row.active ? '将关闭公开展示及新预约入口，已有预约保留。' : '启用不会自动发布草稿。'}`, '资料启停', { confirmButtonText: '确认', cancelButtonText: '取消' })
    replace(await api.directoryAction(rowKind(row), row.id, row.version, !row.active), rowKind(row))
    ElMessage.success('状态已更新')
  } catch (cause) { if (cause !== 'cancel' && cause !== 'close') await failure(cause, message => { error.value = message }) }
  finally { busy.value = false }
}
const previewOpen = ref(false), previewRow = ref<Row | null>(null), previewProfile = ref<ClinicInput | DoctorInput | null>(null), published = ref<ClinicInput | DoctorInput | null>(null)
const previewLoading = ref(false), previewError = ref(''), previewLocal = ref(false), previewStale = ref(false)
let previewRequest = 0
function snapshot(event: DirectoryRevision): ClinicInput | DoctorInput | null {
  try { const parsed = JSON.parse(event.contentJson); const row = parsed.doctor || parsed.profile || parsed; return row && typeof row.name === 'string' ? row : null } catch { return null }
}
async function preview(row?: Row) {
  const ticket = ++previewRequest
  previewError.value = ''; published.value = null; previewStale.value = false; previewLocal.value = !row
  if (!row) { const body = input(); editorError.value = validate(body); if (editorError.value) return; previewProfile.value = body; previewRow.value = null }
  else { previewRow.value = { ...row }; previewProfile.value = 'clinicId' in row ? { ...row, avatarUrl: row.avatarUrl || '', serviceIntroduction: row.serviceIntroduction || '' } : row }
  previewOpen.value = true; previewLoading.value = !!row
  if (row) {
    try { const history = await api.directoryHistory(rowKind(row), row.id); if (ticket !== previewRequest) return; const event = [...history].sort((a, b) => b.version - a.version).find(item => ['published', 'migrated', 'seeded'].includes(item.action)); published.value = event ? snapshot(event) : null }
    catch (cause) { if (ticket === previewRequest) previewError.value = errorMessage(cause) }
    finally { if (ticket === previewRequest) previewLoading.value = false }
  }
}
const publishReason = computed(() => {
  if (!previewRow.value) return '请先保存草稿，再从列表预览并发布。'
  if (!previewRow.value.active) return '请先启用资料。'
  if ('clinicId' in previewRow.value && previewRow.value.presentation && !previewRow.value.presentation.isDemo && previewRow.value.qualificationReview?.status !== 'approved') return '正式医生资料须通过资质审核后发布。'
  if ('clinicId' in previewRow.value) { const parent = clinics.value.find(row => row.id === (previewRow.value as DoctorAdmin).clinicId); if (!parent?.active || !parent.publishedAt) return '所属院区须先启用并发布。' }
  if (previewStale.value) return '版本已变化，请关闭预览后重新打开。'
  return ''
})
async function publish() {
  if (!previewRow.value || busy.value || publishReason.value || previewLoading.value || previewError.value) return
  busy.value = true
  const row = previewRow.value
  try { replace(await api.directoryAction(rowKind(row), row.id, row.version), rowKind(row)); previewOpen.value = false; ElMessage.success('资料已发布') }
  catch (cause) { previewStale.value = cause instanceof ApiError && cause.status === 409; await failure(cause, message => { previewError.value = message }) }
  finally { busy.value = false }
}
const historyOpen = ref(false), historyRows = ref<DirectoryRevision[]>([]), historyLoading = ref(false), historyError = ref(''), historyName = ref('')
const actions: Record<string, string> = { created: '新增草稿', updated: '修改草稿', status: '启停变更', published: '发布资料', 'qualification-approved': '资质审核通过', 'qualification-rejected': '资质审核退回', 'employee-linked': '关联账号', migrated: '迁移已有发布资料', seeded: '初始化发布资料' }
let historyRequest = 0
async function history(row: Row) {
  const ticket = ++historyRequest; historyRows.value = []; historyError.value = ''; historyName.value = row.name; historyOpen.value = true; historyLoading.value = true
  try { const result = await api.directoryHistory(rowKind(row), row.id); if (ticket === historyRequest) historyRows.value = [...result].sort((a, b) => b.version - a.version) }
  catch (cause) { if (ticket === historyRequest) historyError.value = errorMessage(cause) }
  finally { if (ticket === historyRequest) historyLoading.value = false }
}
const accountOpen = ref(false), accountDoctor = ref<DoctorAdmin | null>(null), accounts = ref<Employee[]>([]), accountId = ref(''), accountLoading = ref(false), accountError = ref(''), accountStale = ref(false)
const linkedAccount = computed(() => accounts.value.find(account => account.staffId === accountDoctor.value?.id))
const eligible = computed(() => accounts.value.filter(account => account.active && account.role === 'doctor' && !account.staffId && account.clinicIds.includes(accountDoctor.value?.clinicId || '')))
async function openAccount(row: DoctorAdmin) {
  accountDoctor.value = { ...row }; accounts.value = []; accountId.value = ''; accountError.value = ''; accountStale.value = false; accountOpen.value = true; accountLoading.value = true
  try { accounts.value = await api.employees() } catch (cause) { accountError.value = errorMessage(cause) }
  finally { accountLoading.value = false }
}
async function link() {
  const doctor = accountDoctor.value, employee = eligible.value.find(row => row.id === accountId.value)
  if (!doctor || !employee || busy.value || accountStale.value || !doctor.active || linkedAccount.value) return
  busy.value = true
  try {
    await api.linkDoctorEmployee(doctor.id, employee.id, doctor.version, employee.version)
    // Both versions change on linking; retrieve fresh directory state before another write.
    accountStale.value = true; accounts.value = []; accountOpen.value = false; ElMessage.success('账号已关联，该员工须重新登录')
    try { doctors.value = await api.adminDoctors() } catch { ready.value = false; error.value = '关联已完成，最新资料加载失败，请刷新后继续。' }
  } catch (cause) { accountStale.value = cause instanceof ApiError && cause.status === 409; await failure(cause, message => { accountError.value = message }) }
  finally { busy.value = false }
}
const reviewOpen = ref(false), reviewRow = ref<DoctorAdmin | null>(null), reviewNote = ref(''), reviewStatus = ref<'approved' | 'rejected'>('approved'), reviewConfirmed = ref(false), reviewError = ref(''), reviewStale = ref(false)
function reviewLabel(row: DoctorAdmin) {
  return row.qualificationReview?.status === 'approved' ? '审核通过' : row.qualificationReview?.status === 'rejected' ? '已退回' : '待审核'
}
function openReview(row: DoctorAdmin) {
  reviewRow.value = JSON.parse(JSON.stringify(row)); reviewNote.value = ''; reviewStatus.value = 'approved'; reviewConfirmed.value = false; reviewError.value = ''; reviewStale.value = false; reviewOpen.value = true
}
const reviewIncomplete = computed(() => !reviewRow.value?.presentation?.credentials.length || reviewRow.value.presentation.credentials.some(row => !row.name.trim() || !row.number.trim() || !row.issuer.trim() || !row.imageUrl || !https(row.imageUrl)))
async function submitReview() {
  const row = reviewRow.value
  if (!row || busy.value || reviewStale.value || !row.active) return
  if (!reviewNote.value.trim()) { reviewError.value = '请填写核验依据或退回原因。'; return }
  if (reviewStatus.value === 'approved' && (!reviewConfirmed.value || reviewIncomplete.value)) { reviewError.value = '请补齐证照名称、编号、颁发机构和图片，并确认已核验。'; return }
  busy.value = true; reviewError.value = ''
  try { replace(await api.reviewDoctor(row.id, row.version, reviewStatus.value, reviewNote.value.trim()), 'doctors'); reviewOpen.value = false; ElMessage.success(reviewStatus.value === 'approved' ? '资质审核通过，请预览后发布' : '资料已退回，修改后需重新审核') }
  catch (cause) { reviewStale.value = cause instanceof ApiError && cause.status === 409; await failure(cause, message => { reviewError.value = message }) }
  finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <div class="directory-management">
    <section class="page-heading"><div><div class="eyebrow">MEDICAL DIRECTORY</div><h1>医生与院区维护</h1><p>维护草稿、资质与员工关联；发布后更新患者端资料。</p></div><el-button :loading="loading" :disabled="busy" @click="load">刷新资料</el-button></section>
    <section class="surface directory-panel">
      <div class="directory-tabs"><el-button :type="kind === 'doctors' ? 'primary' : 'default'" :disabled="busy" @click="router.push('/doctors')">医生资料</el-button><el-button :type="kind === 'clinics' ? 'primary' : 'default'" :disabled="busy" @click="router.push('/clinics')">院区资料</el-button></div>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <div class="directory-toolbar"><el-input v-model="search" placeholder="搜索名称" clearable aria-label="搜索名称" /><el-select v-if="kind === 'doctors'" v-model="clinicFilter" clearable placeholder="全部院区" aria-label="筛选院区"><el-option v-for="clinic in clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select><el-select v-model="stateFilter" clearable placeholder="全部状态" aria-label="筛选状态"><el-option label="启用" value="active" /><el-option label="停用" value="inactive" /><el-option label="从未发布" value="draft" /></el-select><el-button type="primary" :disabled="busy || loading || !ready || (kind === 'doctors' && !clinics.length)" @click="edit()">新增{{ label }}</el-button></div>
      <el-table v-loading="loading" :data="rows" empty-text="暂无匹配资料" row-key="id">
        <el-table-column prop="name" label="名称" min-width="150" />
        <el-table-column :label="kind === 'doctors' ? '职称 / 所属院区' : '地址 / 电话'" min-width="200"><template #default="{ row }"><div>{{ row.title || row.address }}</div><small>{{ row.clinicId ? clinicName(row.clinicId) : row.phone }}</small></template></el-table-column>
        <el-table-column label="状态" width="105"><template #default="{ row }"><el-tag :type="row.active ? 'success' : 'info'">{{ row.active ? '启用' : '停用' }}</el-tag></template></el-table-column>
        <el-table-column v-if="kind === 'doctors'" label="资质审核" min-width="140"><template #default="{ row }"><el-tag :type="row.qualificationReview?.status === 'approved' ? 'success' : row.qualificationReview?.status === 'rejected' ? 'danger' : 'warning'">{{ reviewLabel(row) }}</el-tag><div><small>{{ row.presentation?.isDemo ? '演示资料' : row.presentation ? '正式资料' : '待补资料' }}</small></div></template></el-table-column>
        <el-table-column label="上次发布 / 当前版本" min-width="160"><template #default="{ row }"><div>{{ row.publishedAt ? timestamp(row.publishedAt) : '未发布' }}</div><small>当前草稿 v{{ row.version }}</small></template></el-table-column>
        <el-table-column label="操作" min-width="340"><template #default="{ row }"><div class="row-actions"><el-button size="small" :disabled="busy || !ready" @click="edit(row)">编辑</el-button><el-button size="small" :disabled="busy || !ready" @click="toggle(row)">{{ row.active ? '停用' : '启用' }}</el-button><el-button v-if="kind === 'doctors'" size="small" :disabled="busy || !ready || !row.active || !row.presentation" @click="openReview(row)">资质审核</el-button><el-button size="small" :disabled="busy || !ready" @click="preview(row)">预览与发布</el-button><el-button size="small" :disabled="busy" @click="history(row)">历史</el-button><el-button v-if="kind === 'doctors'" size="small" :disabled="busy || !ready" @click="openAccount(row)">账号关联</el-button><el-button v-if="kind === 'doctors' && canAccess('/schedule')" size="small" :disabled="busy || !row.active || !row.publishedAt" @click="viewSchedule(row)">查看排班</el-button></div></template></el-table-column>
      </el-table>
      <p class="hint">编辑保存为草稿，患者继续查看上次发布资料。修改医生资料后需重新审核；正式资料审核通过后方可发布。启停立即生效，已有预约及诊疗记录保留。</p>
    </section>
    <el-dialog v-model="editorOpen" :title="`${editing ? '编辑' : '新增'}${editorKind === 'doctors' ? '医生' : '院区'}资料`" width="720px" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy">
      <el-alert v-if="editorError" :title="editorError" type="error" :closable="false" />
      <el-form label-position="top" :disabled="busy || staleEditor">
        <template v-if="editorKind === 'doctors'">
          <el-form-item label="所属院区"><el-select v-model="doctorForm.clinicId" :disabled="!!editing" aria-label="所属院区"><el-option v-for="clinic in clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select><small v-if="editing">医生所属院区不可变更。</small></el-form-item>
          <el-form-item label="医生姓名"><el-input v-model="doctorForm.name" maxlength="80" /></el-form-item><el-form-item label="职称"><el-input v-model="doctorForm.title" maxlength="80" /></el-form-item><el-form-item label="医生简介"><el-input v-model="doctorForm.summary" type="textarea" :rows="3" maxlength="500" show-word-limit /></el-form-item><el-form-item label="擅长项目"><el-input v-model="focusText" placeholder="用顿号、逗号或换行分隔，最多 20 项" type="textarea" /></el-form-item><el-form-item label="头像缩写"><el-input v-model="doctorForm.initials" maxlength="16" /></el-form-item><el-form-item label="头像图片地址"><el-input v-model="doctorForm.avatarUrl" placeholder="https://，留空使用缩写" maxlength="2048" /></el-form-item><el-form-item label="服务介绍"><el-input v-model="doctorForm.serviceIntroduction" type="textarea" :rows="4" maxlength="5000" show-word-limit /></el-form-item>
          <template v-if="doctorForm.presentation"><el-form-item label="演示资料"><el-switch v-model="doctorForm.presentation.isDemo" /></el-form-item><CredentialEditor v-model="doctorForm.presentation.credentials" :disabled="busy || staleEditor" /></template>
        </template>
        <template v-else>
          <el-form-item label="院区名称"><el-input v-model="clinicForm.name" maxlength="120" /></el-form-item><el-form-item label="院区地址"><el-input v-model="clinicForm.address" maxlength="255" /></el-form-item><el-form-item label="联系电话"><el-input v-model="clinicForm.phone" maxlength="32" /></el-form-item>
          <template v-if="clinicForm.presentation">
            <el-form-item label="品牌名称"><el-input v-model="clinicForm.presentation.brandName" maxlength="120" /></el-form-item><el-form-item label="品牌标语"><el-input v-model="clinicForm.presentation.tagline" maxlength="120" /></el-form-item><el-form-item label="院区介绍"><el-input v-model="clinicForm.presentation.introduction" type="textarea" :rows="4" maxlength="5000" show-word-limit /></el-form-item><el-form-item label="标志图片地址"><el-input v-model="clinicForm.presentation.logoUrl" placeholder="https://" maxlength="2048" /></el-form-item><el-form-item label="营业时间"><el-input v-model="clinicForm.presentation.hours" maxlength="120" /></el-form-item><el-form-item label="交通说明"><el-input v-model="clinicForm.presentation.transport" type="textarea" maxlength="2000" /></el-form-item><el-form-item label="到院指引"><el-input v-model="clinicForm.presentation.arrivalGuide" type="textarea" maxlength="2000" /></el-form-item><div class="coordinates"><el-form-item label="纬度（GCJ-02，可留空）"><el-input-number v-model="latitude" :min="-90" :max="90" :controls="false" /></el-form-item><el-form-item label="经度（GCJ-02，可留空）"><el-input-number v-model="longitude" :min="-180" :max="180" :controls="false" /></el-form-item></div><el-form-item label="演示资料"><el-switch v-model="clinicForm.presentation.isDemo" /></el-form-item><CredentialEditor v-model="clinicForm.presentation.credentials" :disabled="busy || staleEditor" />
            <section class="credentials"><h3>门店实景轮播</h3><p>最多 10 张，填写公开的 HTTPS 环境图片地址与说明；发布后在患者端首页轮播展示。留空则首页不显示轮播区。图片须为院方实景素材，不得用演示图冒充。</p>
              <div v-for="(row, index) in clinicForm.presentation.environmentImages" :key="index" class="credential-row">
                <el-form-item :label="`图片地址 ${index + 1}`"><el-input v-model="row.imageUrl" :disabled="busy || staleEditor" maxlength="2048" placeholder="https://" /></el-form-item>
                <el-form-item :label="`图片说明 ${index + 1}`"><el-input v-model="row.caption" :disabled="busy || staleEditor" maxlength="200" /></el-form-item>
                <el-button :disabled="busy || staleEditor" type="danger" plain @click="clinicForm.presentation.environmentImages?.splice(index, 1)">移除图片 {{ index + 1 }}</el-button>
              </div>
              <el-button :disabled="busy || staleEditor || (clinicForm.presentation.environmentImages?.length || 0) >= 10" @click="clinicForm.presentation.environmentImages?.push({ imageUrl: '', caption: '' })">添加环境图</el-button>
            </section>
            <section class="credentials"><h3>患者评价精选</h3><p>最多 10 条，发布后在患者端首页展示；清空后首页不显示评价区。只选用经患者同意公开的评价原文。</p>
              <div v-for="(row, index) in clinicForm.presentation.featuredReviews" :key="index" class="credential-row">
                <el-form-item :label="`署名 ${index + 1}`"><el-input v-model="row.displayName" :disabled="busy || staleEditor" maxlength="50" /></el-form-item>
                <el-form-item :label="`评价内容 ${index + 1}`"><el-input v-model="row.content" :disabled="busy || staleEditor" type="textarea" :rows="3" maxlength="1000" show-word-limit /></el-form-item>
                <el-form-item :label="`评分 ${index + 1}`"><el-input-number v-model="row.rating" :disabled="busy || staleEditor" :min="0" :max="5" /></el-form-item>
                <el-button :disabled="busy || staleEditor" type="danger" plain @click="clinicForm.presentation.featuredReviews?.splice(index, 1)">移除评价 {{ index + 1 }}</el-button>
              </div>
              <el-button :disabled="busy || staleEditor || (clinicForm.presentation.featuredReviews?.length || 0) >= 10" @click="clinicForm.presentation.featuredReviews?.push({ displayName: '', content: '', rating: 5 })">添加精选评价</el-button>
            </section>
          </template>
        </template>
      </el-form>
      <template #footer><el-button :disabled="busy" @click="editorOpen = false">取消</el-button><el-button :disabled="busy || staleEditor" @click="preview()">预览草稿</el-button><el-button type="primary" :loading="busy" :disabled="staleEditor" @click="save">保存草稿</el-button></template>
    </el-dialog>
    <el-dialog v-model="reviewOpen" title="医生资质审核" width="760px" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy">
      <el-alert v-if="reviewError" :title="reviewError" type="error" :closable="false" />
      <DirectoryPreview v-if="reviewRow" :profile="{ ...reviewRow, avatarUrl: reviewRow.avatarUrl || '', serviceIntroduction: reviewRow.serviceIntroduction || '' }" :clinic-name="clinicName(reviewRow.clinicId)" />
      <p v-if="reviewRow?.presentation?.isDemo" class="hint">当前为 mock 资料，审核仅用于演示流程。</p>
      <p v-if="reviewRow?.qualificationReview" class="hint">上次审核：{{ reviewLabel(reviewRow) }} · {{ timestamp(reviewRow.qualificationReview.reviewedAt) }} · {{ reviewRow.qualificationReview.reviewerId }}<br />{{ reviewRow.qualificationReview.note }}</p>
      <el-form label-position="top" :disabled="busy || reviewStale">
        <el-form-item label="审核结果"><el-radio-group v-model="reviewStatus"><el-radio value="approved">通过</el-radio><el-radio value="rejected">退回</el-radio></el-radio-group></el-form-item>
        <el-form-item label="核验依据 / 退回原因"><el-input v-model="reviewNote" type="textarea" :rows="3" maxlength="2000" show-word-limit /></el-form-item>
        <el-checkbox v-if="reviewStatus === 'approved'" v-model="reviewConfirmed">已核对医生身份、职称及证照与所属院区的对应关系</el-checkbox>
      </el-form>
      <p v-if="reviewIncomplete && reviewStatus === 'approved'" class="hint">通过审核须至少提供一项完整证照：名称、编号、颁发机构及 HTTPS 图片地址。</p>
      <template #footer><el-button :disabled="busy" @click="reviewOpen = false">取消</el-button><el-button type="primary" :loading="busy" :disabled="reviewStale || !reviewRow?.active || (reviewStatus === 'approved' && (!reviewConfirmed || reviewIncomplete))" @click="submitReview">提交审核</el-button></template>
    </el-dialog>
    <el-dialog v-model="previewOpen" title="资料发布预览" width="960px" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy">
      <el-alert v-if="previewError" type="error" :title="previewError" :closable="false" /><p class="hint">{{ previewLocal ? '当前为未保存内容，请返回编辑页保存后发布。' : '确认当前草稿后发布，患者端将更新为此版本。' }}</p>
      <div v-loading="previewLoading" class="preview-columns"><section><h3>当前草稿{{ previewRow ? ` · v${previewRow.version}` : '' }}</h3><DirectoryPreview v-if="previewProfile" :profile="previewProfile" :clinic-name="'clinicId' in previewProfile ? clinicName(previewProfile.clinicId) : ''" /></section><section v-if="!previewLocal"><h3>上次发布资料</h3><DirectoryPreview v-if="published" :profile="published" :clinic-name="'clinicId' in published ? clinicName(published.clinicId) : ''" /><el-empty v-else-if="!previewLoading && !previewError" description="尚无发布快照" /></section></div>
      <p v-if="publishReason" class="hint">{{ publishReason }}</p><template #footer><el-button :disabled="busy" @click="previewOpen = false">关闭预览</el-button><el-button v-if="!previewLocal" type="primary" :loading="busy" :disabled="!!publishReason || previewLoading || !!previewError" @click="publish">确认发布</el-button></template>
    </el-dialog>
    <el-drawer v-model="historyOpen" :title="`${historyName} · 版本历史`" size="640px"><el-alert v-if="historyError" type="error" :title="historyError" :closable="false" /><div v-loading="historyLoading"><el-empty v-if="!historyLoading && !historyRows.length && !historyError" description="暂无历史记录" /><article v-for="event in historyRows" :key="event.id" class="history-event"><h3>v{{ event.version }} · {{ actions[event.action] || event.action }}</h3><p>{{ timestamp(event.createdAt) }} · 操作者 {{ event.actorId }}</p><el-collapse><el-collapse-item title="查看当时资料与附件" :name="event.id"><DirectoryPreview v-if="snapshot(event)" :profile="snapshot(event)!" :clinic-name="clinicName((snapshot(event) as DoctorInput)?.clinicId)" /><pre>{{ event.contentJson }}</pre></el-collapse-item></el-collapse></article></div></el-drawer>
    <el-dialog v-model="accountOpen" title="医生账号关联" width="560px" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy"><el-alert v-if="accountError" :title="accountError" type="error" :closable="false" /><div v-loading="accountLoading"><h3>{{ accountDoctor?.name }}</h3><p class="hint">每位医生最多关联一个启用的医生角色账号，账号须有该院区权限且未绑定其他人员。关联后该员工须重新登录。</p><p v-if="linkedAccount">已关联：{{ linkedAccount.displayName }}（{{ linkedAccount.username }}） · {{ linkedAccount.active ? '账号启用' : '账号停用' }}</p><template v-else><el-select v-model="accountId" placeholder="选择可关联账号" aria-label="选择可关联账号" :disabled="busy || accountLoading || accountStale || !accountDoctor?.active"><el-option v-for="employee in eligible" :key="employee.id" :label="`${employee.displayName}（${employee.username}）`" :value="employee.id" /></el-select><p v-if="!accountLoading && !eligible.length" class="hint">暂无符合条件的账号，可到员工账号页为该医生创建账号。</p></template><p v-if="!accountDoctor?.active" class="hint">请先启用医生资料。</p><RouterLink to="/employees" @click="accountOpen = false">前往员工账号维护</RouterLink></div><template #footer><el-button :disabled="busy" @click="accountOpen = false">关闭</el-button><el-button type="primary" :loading="busy" :disabled="accountLoading || accountStale || !!linkedAccount || !accountId || !accountDoctor?.active || !!accountError" @click="link">确认关联</el-button></template></el-dialog>
  </div>
</template>
<style scoped>
.directory-panel{padding:24px}.directory-tabs{display:flex;gap:8px;margin-bottom:20px}.directory-toolbar{display:flex;gap:12px;margin:18px 0}.directory-toolbar .el-input{max-width:240px}.directory-toolbar .el-select{width:180px}.row-actions{display:flex;flex-wrap:wrap;gap:6px}.row-actions .el-button{margin:0}.hint,small{color:#718096;font-size:13px;line-height:1.8}.hint{margin:16px 0}.el-form .el-select{width:100%}.coordinates{display:flex;gap:18px}.preview-columns{display:grid;grid-template-columns:1fr 1fr;gap:20px}.preview-columns h3{margin:16px 0}.history-event{padding:16px 0;border-bottom:1px solid #e3eaf2}.history-event p{font-size:13px;color:#718096;margin:10px 0}pre{white-space:pre-wrap;overflow-wrap:anywhere;font-size:12px;margin:15px 0}@media(max-width:900px){.directory-toolbar{flex-wrap:wrap}.preview-columns{grid-template-columns:1fr}}
</style>
