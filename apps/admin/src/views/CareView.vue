<script setup lang="ts">
import { useRoute } from 'vue-router'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, ElCheckbox, ElRadio } from 'element-plus'
import { api, errorMessage, type Appointment, type CarePhoto, type CarePlanContent, type CarePlanVersion, type CarePlanWorkspace, type CarePlanTemplate } from '../api'
import { slotTime } from '../dates'
import { auth } from '../auth'
import PrivatePhoto from '../components/PrivatePhoto.vue'

const route = useRoute()
const appointments = ref<Appointment[]>([])
const search = ref('')
const appliedSearch = ref('')
const status = ref<'confirmed' | 'completed'>('confirmed')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const templates = ref<CarePlanTemplate[]>([])
const templateId = ref('')
const templateName = ref('')
const templateBusy = ref(false)
const templateError = ref('')
const compareLeft = ref('')
const compareRight = ref('')
const onlyChanges = ref(false)
let listRequestVersion = 0
const comparisonOptions = computed(() => [
  ...workspace.value.versions.map(v => ({ id: v.id, label: `V${v.version}`, content: v.content })),
  ...(selected.value ? [{ id: 'editor', label: '当前编辑内容', content: content() }] : []),
])
const comparisonRows = computed(() => {
  const left = comparisonOptions.value.find(v => v.id === compareLeft.value)?.content
  const right = comparisonOptions.value.find(v => v.id === compareRight.value)?.content
  if (!left || !right) return []
  const rows = [{ label: '方案摘要', left: left.summary, right: right.summary }]
  for (let i = 0; i < Math.max(left.stages.length, right.stages.length); i++) {
    rows.push({ label: `阶段 ${i + 1} 名称`, left: left.stages[i]?.title ?? '', right: right.stages[i]?.title ?? '' })
    rows.push({ label: `阶段 ${i + 1} 安排`, left: left.stages[i]?.body ?? '', right: right.stages[i]?.body ?? '' })
  }
  rows.push({ label: '医嘱与注意事项', left: left.instructions, right: right.instructions })
  return rows.map(row => ({ ...row, changed: row.left !== row.right }))
})
const visibleComparisonRows = computed(() => comparisonRows.value.filter(row => !onlyChanges.value || row.changed))
const changedCount = computed(() => comparisonRows.value.filter(row => row.changed).length)
const editorDirty = computed(() => {
  const base = workspace.value.draft || latestFrom(workspace.value.versions)?.content || { summary: '', stages: [{ title: '', body: '' }], instructions: '' }
  // 服务端 draft 由 Go map 序列化，键序与本地 content() 不同；直接 JSON.stringify 比较永真，
  // 会导致自动保存死循环且发布按钮一直禁用。先按同一字段顺序规范化再比较。
  const normalized: CarePlanContent = { summary: base.summary || '', stages: (base.stages?.length ? base.stages : [{ title: '', body: '' }]).map(s => ({ title: s.title || '', body: s.body || '' })), instructions: base.instructions || '' }
  return JSON.stringify(content()) !== JSON.stringify(normalized)
})

const conflictRows = computed(() => {
  if (!conflict.value) return []
  const { server, mine } = conflict.value
  const rows: { key: string, label: string, server: string, mine: string, changed: boolean }[] = [{ key: 'summary', label: '方案摘要', server: server.summary, mine: mine.summary, changed: server.summary !== mine.summary }]
  for (let i = 0; i < Math.max(server.stages.length, mine.stages.length); i++) {
    rows.push({ key: `stage-${i}-title`, label: `阶段 ${i + 1} 名称`, server: server.stages[i]?.title ?? '', mine: mine.stages[i]?.title ?? '', changed: (server.stages[i]?.title ?? '') !== (mine.stages[i]?.title ?? '') })
    rows.push({ key: `stage-${i}-body`, label: `阶段 ${i + 1} 安排`, server: server.stages[i]?.body ?? '', mine: mine.stages[i]?.body ?? '', changed: (server.stages[i]?.body ?? '') !== (mine.stages[i]?.body ?? '') })
  }
  rows.push({ key: 'instructions', label: '医嘱与注意事项', server: server.instructions, mine: mine.instructions, changed: server.instructions !== mine.instructions })
  return rows
})
const conflictChangedCount = computed(() => conflictRows.value.filter(row => row.changed).length)

const selected = ref<Appointment | null>(null)
const loading = ref(false)
const detailLoading = ref(false)
const saving = ref(false)
const error = ref('')
const photos = ref<CarePhoto[]>([])
const workspace = ref<CarePlanWorkspace>({ draftVersion: 0, draft: null, versions: [] })
const summary = ref('')
const stages = ref<{ title: string; body: string }[]>([])
const instructions = ref('')
const changeReason = ref('')
const notice = ref('')
const autosaveStatus = ref('')
const conflict = ref<{ server: CarePlanContent, serverVersion: number, mine: CarePlanContent } | null>(null)
const conflictDialogVisible = ref(false)
const conflictChoices = ref<Record<string, 'server' | 'mine'>>({})
let autosaveTimer: ReturnType<typeof setTimeout> | undefined
let detailRequestVersion = 0

const canEdit = computed(() => Boolean(selected.value && selected.value.status === 'confirmed' && (auth.employee?.role === 'manager' || (['clinician', 'doctor'].includes(auth.employee?.role || '') && selected.value.doctorId === auth.employee?.staffId))))
const planHasDraft = computed(() => workspace.value.draft !== null)

function releasePhotoUrls() {
  photos.value = []
}

function useContent(content: CarePlanContent | undefined) {
  summary.value = content?.summary || ''
  stages.value = content?.stages?.length ? content.stages.map((stage) => ({ ...stage })) : [{ title: '', body: '' }]
  instructions.value = content?.instructions || ''
}

async function loadList() {
  const request = ++listRequestVersion
  loading.value = true
  error.value = ''
  try {
    const result = await api.appointments({ status: status.value, q: appliedSearch.value, page: page.value, pageSize })
    if (request !== listRequestVersion) return
    const lastPage = Math.max(1, Math.ceil(result.meta.total / pageSize))
    if (page.value > lastPage) { page.value = lastPage; await loadList(); return }
    appointments.value = result.data
    total.value = result.meta.total
  } catch (cause) {
    if (request === listRequestVersion) { appointments.value = []; total.value = 0; error.value = errorMessage(cause) }
  } finally { if (request === listRequestVersion) loading.value = false }
}

function applySearch() { appliedSearch.value = search.value.trim(); page.value = 1; void loadList() }
function changePage(value: number) { page.value = value; void loadList() }

async function loadTemplates() {
  templateError.value = ''
  try { templates.value = await api.careTemplates() }
  catch (cause) { templateError.value = errorMessage(cause) }
}

async function saveTemplate() {
  if (!canEdit.value || templateBusy.value) return
  if (!templateName.value.trim()) { templateError.value = '请填写模板名称。'; return }
  templateBusy.value = true
  templateError.value = ''
  try {
    const row = await api.createCareTemplate(templateName.value.trim(), content())
    templates.value.unshift(row); templateId.value = row.id; templateName.value = ''
    ElMessage.success('个人模板已保存')
  } catch (cause) { templateError.value = errorMessage(cause) }
  finally { templateBusy.value = false }
}

async function applyTemplate() {
  const row = templates.value.find(t => t.id === templateId.value)
  if (!row || !canEdit.value || saving.value) return
  try {
    await ElMessageBox.confirm('将用模板替换当前编辑内容，是否继续？', '应用方案模板', { confirmButtonText: '应用模板', cancelButtonText: '取消' })
    useContent(row.content)
    notice.value = '模板已应用，请核对并保存草稿后发布。'
  } catch { /* cancelled */ }
}

const libraryTemplates = ref<CarePlanTemplate[]>([])
const libraryId = ref('')
const libraryBusy = ref(false)
const libraryError = ref('')
const isManager = computed(() => auth.employee?.role === 'manager')

async function loadLibrary() {
  libraryError.value = ''
  try { libraryTemplates.value = (await api.careTemplateLibrary()).filter(t => (t as any).isShared) }
  catch (cause) { libraryError.value = errorMessage(cause) }
}

async function applyLibraryTemplate() {
  const row = libraryTemplates.value.find(t => t.id === libraryId.value)
  if (!row || !canEdit.value || !selected.value || saving.value) return
  try {
    await ElMessageBox.confirm(`将用模板库「${row.name}」新建方案草稿（覆盖当前草稿），是否继续？`, '从模板库新建', { confirmButtonText: '新建草稿', cancelButtonText: '取消' })
    libraryBusy.value = true
    await api.applyCareTemplate(row.id, selected.value.id, workspace.value.draftVersion)
    await reloadSelected()
    notice.value = '已从模板库新建草稿，请核对修改后发布。'
    ElMessage.success('已从模板库新建草稿')
  } catch (cause) { if ((cause as any) !== 'cancel') libraryError.value = errorMessage(cause) }
  finally { libraryBusy.value = false }
}

async function shareTemplate() {
  const row = templates.value.find(t => t.id === templateId.value)
  if (!row || !isManager.value || !selected.value) return
  try {
    await ElMessageBox.confirm(`将个人模板「${row.name}」发布为本院区共享模板？`, '发布到模板库', { confirmButtonText: '发布共享', cancelButtonText: '取消' })
    libraryBusy.value = true
    await api.shareCareTemplate(row.id, (selected.value as any).clinicId || '')
    await loadLibrary()
    ElMessage.success('已发布到模板库')
  } catch (cause) { if ((cause as any) !== 'cancel') libraryError.value = errorMessage(cause) }
  finally { libraryBusy.value = false }
}

async function deleteTemplate() {
  const row = templates.value.find(t => t.id === templateId.value)
  if (!row || templateBusy.value) return
  try { await ElMessageBox.confirm(`删除个人模板“${row.name}”？`, '删除模板', { type: 'warning' }) }
  catch { return }
  templateBusy.value = true
  templateError.value = ''
  try { await api.deleteCareTemplate(row.id); templates.value = templates.value.filter(t => t.id !== row.id); templateId.value = '' }
  catch (cause) { templateError.value = errorMessage(cause) }
  finally { templateBusy.value = false }
}

function resetComparison() {
  compareLeft.value = workspace.value.versions[1]?.id || workspace.value.versions[0]?.id || ''
  compareRight.value = workspace.value.versions.length > 1 ? workspace.value.versions[0]!.id : 'editor'
}

async function selectAppointment(appointment: Appointment) {
  if (saving.value) return
  if (selected.value && selected.value.id !== appointment.id && editorDirty.value) {
    try { await ElMessageBox.confirm('当前编辑内容尚未保存，切换预约会丢弃这些修改。', '切换预约', { confirmButtonText: '丢弃并切换', cancelButtonText: '继续编辑' }) }
    catch { return }
  }
  const version = ++detailRequestVersion
  selected.value = appointment
  detailLoading.value = true
  error.value = ''
  notice.value = ''
  if (autosaveTimer) clearTimeout(autosaveTimer)
  autosaveTimer = undefined
  conflict.value = null
  conflictDialogVisible.value = false
  autosaveStatus.value = ''
  releasePhotoUrls()
  photos.value = []
  workspace.value = { draftVersion: 0, draft: null, versions: [] }
  useContent(undefined)
  changeReason.value = ''
  try {
    const [plan, rows] = await Promise.all([api.carePlan(appointment.id), api.carePhotos(appointment.id)])
    if (version !== detailRequestVersion || selected.value?.id !== appointment.id) return
    workspace.value = plan
    resetComparison()
    useContent(plan.draft || latestFrom(plan.versions)?.content)
    changeReason.value = plan.versions.length ? '' : '初次发布'
    photos.value = rows
  } catch (cause) {
    if (version === detailRequestVersion) error.value = errorMessage(cause)
  } finally { if (version === detailRequestVersion) detailLoading.value = false }
}

function latestFrom(versions: CarePlanVersion[]) { return [...versions].sort((a, b) => b.version - a.version)[0] }

function addStage() {
  if (stages.value.length < 8) stages.value.push({ title: '', body: '' })
}

function removeStage(index: number) {
  if (stages.value.length > 1) stages.value.splice(index, 1)
}

function content(): CarePlanContent {
  return { summary: summary.value.trim(), stages: stages.value.map((stage) => ({ title: stage.title.trim(), body: stage.body.trim() })), instructions: instructions.value.trim() }
}

function scheduleAutosave() {
  if (autosaveTimer) clearTimeout(autosaveTimer)
  autosaveTimer = setTimeout(() => { void autoSaveDraft() }, 1500)
}
watch([summary, stages, instructions], () => {
  if (!selected.value || !canEdit.value || conflict.value) return
  if (!editorDirty.value) return
  autosaveStatus.value = '有未保存的修改，正在等待自动保存…'
  scheduleAutosave()
}, { deep: true })
async function autoSaveDraft() {
  if (!selected.value || !canEdit.value || saving.value || conflict.value || !editorDirty.value) return
  autosaveStatus.value = '正在自动保存…'
  await saveDraft(true)
}
onBeforeUnmount(() => { if (autosaveTimer) clearTimeout(autosaveTimer) })

async function saveDraft(silent = false) {
  if (!selected.value || saving.value || !canEdit.value) return
  saving.value = true
  error.value = ''
  try {
    workspace.value = await api.saveCarePlanDraft(selected.value.id, workspace.value.draftVersion, content())
    useContent(workspace.value.draft || undefined)
    autosaveStatus.value = `已自动保存 ${new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', timeZone: 'Asia/Shanghai' })}`
    if (!silent) {
      notice.value = '草稿已保存，患者端仍只显示已发布版本。'
      ElMessage.success('方案草稿已保存')
    }
  } catch (cause) {
    if (cause instanceof Error && 'status' in cause && (cause as { status?: number }).status === 409) {
      await openConflict()
      return
    }
    error.value = errorMessage(cause)
    if (silent) autosaveStatus.value = '自动保存失败，请点“保存草稿”手动保存。'
  } finally { saving.value = false }
}

async function openConflict() {
  if (!selected.value) return
  if (autosaveTimer) clearTimeout(autosaveTimer)
  autosaveTimer = undefined
  try {
    const plan = await api.carePlan(selected.value.id)
    const server = plan.draft || latestFrom(plan.versions)?.content || { summary: '', stages: [{ title: '', body: '' }], instructions: '' }
    workspace.value = plan
    conflict.value = { server, serverVersion: plan.draftVersion, mine: content() }
    const choices: Record<string, 'server' | 'mine'> = {}
    for (const row of conflictRows.value) choices[row.key] = row.mine ? 'mine' : 'server'
    conflictChoices.value = choices
    conflictDialogVisible.value = true
    autosaveStatus.value = '检测到版本冲突，自动保存已暂停，请先合并。'
  } catch (cause) {
    error.value = errorMessage(cause)
    notice.value = '草稿版本已变化，但读取服务端最新内容失败，请刷新页面后重试。'
  }
}

function mergedContent(): CarePlanContent {
  const c = conflict.value
  if (!c) return content()
  const pick = (key: string, serverText: string, mineText: string) => conflictChoices.value[key] === 'server' ? serverText : mineText
  const stages: { title: string; body: string }[] = []
  for (let i = 0; i < Math.max(c.server.stages.length, c.mine.stages.length); i++) {
    stages.push({ title: pick(`stage-${i}-title`, c.server.stages[i]?.title ?? '', c.mine.stages[i]?.title ?? ''), body: pick(`stage-${i}-body`, c.server.stages[i]?.body ?? '', c.mine.stages[i]?.body ?? '') })
  }
  return { summary: pick('summary', c.server.summary, c.mine.summary), stages, instructions: pick('instructions', c.server.instructions, c.mine.instructions) }
}

async function resolveConflictMerge() {
  if (!conflict.value) return
  useContent(mergedContent())
  conflict.value = null
  conflictDialogVisible.value = false
  notice.value = '已按你的选择合并双方内容，正在保存…'
  await saveDraft(false)
}

function resolveConflictServer() {
  if (!conflict.value) return
  useContent(conflict.value.server)
  conflict.value = null
  conflictDialogVisible.value = false
  autosaveStatus.value = ''
  notice.value = '已加载服务端最新草稿，你的未保存修改已丢弃。'
}

function dismissConflict() {
  conflictDialogVisible.value = false
}

async function publish() {
  if (!selected.value || saving.value || !canEdit.value || !planHasDraft.value) return
  if (editorDirty.value) { error.value = '当前内容有未保存修改，请先保存草稿后发布。'; return }
  if (!changeReason.value.trim()) { error.value = '请填写本次发布或修订说明。'; return }
  saving.value = true
  error.value = ''
  try {
    await api.publishCarePlan(selected.value.id, workspace.value.draftVersion, changeReason.value.trim())
    changeReason.value = ''
    await reloadSelected()
    notice.value = '方案已发布，新版本已对患者可见。'
    ElMessage.success('方案已发布')
  } catch (cause) {
    error.value = errorMessage(cause)
    if (cause instanceof Error && 'status' in cause && (cause as { status?: number }).status === 409) {
      await reloadSelected()
      notice.value = '草稿版本已变化，已刷新最新内容。请重新核对后发布。'
    }
  } finally { saving.value = false }
}

async function reloadSelected() {
  if (!selected.value) return
  const plan = await api.carePlan(selected.value.id)
  workspace.value = plan
  resetComparison()
  useContent(plan.draft || latestFrom(plan.versions)?.content)
}

function timestamp(value: string) {
  const date = new Date(value)
  return `${date.toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai' })} ${slotTime(value)}`
}

async function loadLinkedAppointment(id: unknown) {
  if (typeof id !== 'string') return
  try {
    const linked = await api.appointment(id)
    if (route.query.appointmentId !== id) return
    if (linked.status === 'confirmed' || linked.status === 'completed') await selectAppointment(linked)
    else error.value = '该预约当前无法查看方案。'
  } catch (cause) { error.value = errorMessage(cause) }
}
onMounted(() => { void loadTemplates(); void loadLibrary(); void loadList(); void loadLinkedAppointment(route.query.appointmentId) })
watch(() => route.query.appointmentId, id => { void loadLinkedAppointment(id) })
onBeforeUnmount(() => { detailRequestVersion++; listRequestVersion++; releasePhotoUrls() })
</script>

<template>
  <div class="page-heading">
    <div><div class="eyebrow">CARE PLAN & PHOTOS</div><h1>方案与照片</h1><p>查看患者已授权的面诊照片，编辑医生方案并保留发布版本。</p></div>
    <el-button class="refresh-button" :loading="loading" @click="loadList">刷新预约</el-button>
  </div>
  <div class="care-demo-note">开发演示环境 · 只使用虚构患者和照片 · 后台操作关联当前员工账号</div>
  <el-alert v-if="error" class="care-error" type="error" :closable="false" :title="error" />
  <div class="care-layout">
    <section class="care-list-panel">
      <div class="care-panel-heading"><div><h2>预约选择</h2><p>选择一条预约查看面诊资料</p></div><el-tag size="small">{{ total }}</el-tag></div>
      <div class="care-filters">
        <el-input v-model="search" aria-label="搜索预约" clearable maxlength="100" placeholder="姓名、手机号或预约编号" @keyup.enter="applySearch" @clear="applySearch" />
        <el-select v-model="status" aria-label="预约状态" @change="applySearch"><el-option label="已确认" value="confirmed" /><el-option label="已治疗" value="completed" /></el-select>
        <el-button :loading="loading" @click="applySearch">搜索</el-button>
      </div>
      <div v-if="loading && !appointments.length" class="care-state">正在读取预约…</div>
      <div v-else-if="!appointments.length" class="care-state">暂无匹配预约</div>
      <button v-for="item in appointments" :key="item.id" class="care-appointment" :class="{ selected: selected?.id === item.id }" :disabled="saving" @click="selectAppointment(item)">
        <span class="care-patient">{{ item.contactName }} <small>演示患者</small></span>
        <span class="care-visit">{{ item.doctorName }} · {{ timestamp(item.startsAt) }}</span>
        <span class="care-id">预约 {{ item.id.slice(-8).toUpperCase() }}</span>
      </button>
      <el-pagination class="care-pagination" small layout="prev, pager, next" :pager-count="5" :current-page="page" :page-size="pageSize" :total="total" @current-change="changePage" />
    </section>
    <section class="care-detail-panel">
      <div v-if="!selected" class="care-state care-empty">选择左侧预约，查看已授权照片或编辑方案。</div>
      <template v-else>
        <div class="care-detail-heading"><div><div class="eyebrow">{{ selected.doctorName }} · {{ selected.contactName }}</div><h2>面诊资料与方案</h2><p>{{ timestamp(selected.startsAt) }} · {{ selected.clinicName }}</p></div><el-tag type="success">{{ selected.status === 'completed' ? '已治疗' : '已确认' }}</el-tag></div>
        <el-skeleton v-if="detailLoading" :rows="5" animated />
        <template v-else>
          <el-alert v-if="notice" class="care-notice" type="success" :closable="false" :title="notice" />
          <section class="care-section">
            <div class="care-section-heading"><div><h3>面诊照片</h3><p>患者授权有效时才会返回照片；撤回授权后照片不会显示。</p></div><el-tag v-if="photos.length" size="small">{{ photos.length }} 张</el-tag></div>
            <div v-if="photos.length" class="care-photo-grid">
              <figure v-for="photo in photos" :key="photo.id" class="care-photo">
                <PrivatePhoto :id="photo.id" source="care" :alt="`${photo.angle} 面诊照片`" />
                <figcaption>{{ photo.angle === 'front' ? '正面' : photo.angle === 'left' ? '左侧' : '右侧' }} · {{ timestamp(photo.uploadedAt) }}</figcaption>
              </figure>
            </div>
            <div v-else class="care-empty-small">尚无可查看照片。患者可能尚未上传或已撤回查看授权。</div>
          </section>
          <section class="care-section plan-editor">
            <div class="care-section-heading"><div><h3>治疗方案</h3><p>草稿只在后台显示；发布后患者才能看到。已发布版本保留。</p></div><el-tag v-if="planHasDraft" type="warning" size="small">草稿 V{{ workspace.draftVersion }}</el-tag></div>
            <div class="template-tools">
              <h4>个人方案模板</h4><p>仅当前员工可见；请勿将患者姓名、联系方式写入模板。</p>
              <el-alert v-if="templateError" type="error" :closable="false" :title="templateError" />
              <div class="template-actions"><el-select v-model="templateId" aria-label="方案模板" placeholder="选择个人模板" clearable><el-option v-for="item in templates" :key="item.id" :label="item.name" :value="item.id" /></el-select><el-button :disabled="!canEdit || saving || !templateId" @click="applyTemplate">应用模板</el-button><el-button :disabled="templateBusy || !templateId" @click="deleteTemplate">删除模板</el-button><el-button :disabled="templateBusy" @click="loadTemplates">刷新模板</el-button></div>
              <div v-if="canEdit" class="template-actions"><el-input v-model="templateName" aria-label="模板名称" maxlength="80" placeholder="模板名称" /><el-button :loading="templateBusy" :disabled="saving" @click="saveTemplate">存为模板</el-button><el-button v-if="isManager" :disabled="templateBusy || !templateId" @click="shareTemplate">发布到模板库</el-button></div>
              <h4>方案模板库</h4><p>本院区共享模板；选择后可一键新建方案草稿。</p>
              <el-alert v-if="libraryError" type="error" :closable="false" :title="libraryError" />
              <div class="template-actions"><el-select v-model="libraryId" aria-label="模板库" placeholder="选择共享模板" clearable><el-option v-for="item in libraryTemplates" :key="item.id" :label="item.name + '（' + ((item as any).ownerName || '') + '）'" :value="item.id" /></el-select><el-button :disabled="!canEdit || saving || libraryBusy || !libraryId" @click="applyLibraryTemplate">从模板库新建草稿</el-button><el-button :disabled="libraryBusy" @click="loadLibrary">刷新模板库</el-button></div>
            </div>
            <el-alert v-if="!canEdit" type="info" :closable="false" title="当前预约方案只读。" />
            <el-form label-position="top" class="care-form" :disabled="!canEdit || saving">
              <el-form-item label="方案摘要"><el-input v-model="summary" type="textarea" :rows="3" maxlength="1000" show-word-limit placeholder="由负责医生填写本次面诊安排摘要" /></el-form-item>
              <div class="care-section-heading stage-heading"><div><h4>阶段安排</h4><p>请填写患者应了解的阶段和安排。</p></div><el-button size="small" :disabled="stages.length >= 8" @click="addStage">添加阶段</el-button></div>
              <div v-for="(stage, index) in stages" :key="index" class="stage-editor">
                <div class="stage-editor-heading"><strong>阶段 {{ index + 1 }}</strong><el-button v-if="stages.length > 1" link type="danger" @click="removeStage(index)">删除</el-button></div>
                <el-input v-model="stage.title" maxlength="40" placeholder="阶段名称" />
                <el-input v-model="stage.body" type="textarea" :rows="3" maxlength="1000" show-word-limit placeholder="阶段安排说明" />
              </div>
              <el-form-item label="医嘱与注意事项"><el-input v-model="instructions" type="textarea" :rows="3" maxlength="2000" show-word-limit placeholder="由负责医生填写；没有补充事项可留空" /></el-form-item>
              <el-form-item v-if="planHasDraft" label="发布说明"><el-input v-model="changeReason" maxlength="300" placeholder="初次发布或本次更正的说明" /></el-form-item>
              <div class="care-actions">
                <el-button :loading="saving" :disabled="!canEdit" @click="saveDraft(false)">保存草稿</el-button><span v-if="autosaveStatus" class="autosave-status">{{ autosaveStatus }}</span>
                <el-button type="primary" :loading="saving" :disabled="!canEdit || !planHasDraft || editorDirty" @click="publish">{{ workspace.versions.length ? '发布新版本' : '发布方案' }}</el-button>
              </div>
            </el-form>
            <el-alert v-if="conflict" type="warning" :closable="false" class="conflict-banner" title="检测到版本冲突：另一位同事已先保存了这份草稿。自动保存已暂停，你的未保存修改仍在编辑器里，没有被覆盖。"><template #default><el-button size="small" @click="conflictDialogVisible = true">查看冲突并合并</el-button></template></el-alert>
          </section>
          <section v-if="workspace.versions.length" class="care-section">
            <div class="care-section-heading"><div><h3>已发布版本</h3><p>发布内容不可原地修改。</p></div></div>
            <div class="comparison-controls">
              <h4>历史全文对照与版本差异</h4>
              <el-select v-model="compareLeft" aria-label="对照基准"><el-option v-for="item in comparisonOptions" :key="item.id" :label="item.label" :value="item.id" /></el-select>
              <span>对照</span><el-select v-model="compareRight" aria-label="对照版本"><el-option v-for="item in comparisonOptions" :key="item.id" :label="item.label" :value="item.id" /></el-select>
              <el-checkbox v-model="onlyChanges" aria-label="只看差异">只看差异</el-checkbox>
              <p>{{ changedCount ? `${changedCount} 项内容发生变化` : '内容相同，无差异' }} · 阶段按顺序对照</p>
            </div>
            <div class="comparison-table-wrap"><table class="comparison-table"><thead><tr><th>内容</th><th>{{ comparisonOptions.find(v => v.id === compareLeft)?.label }}</th><th>{{ comparisonOptions.find(v => v.id === compareRight)?.label }}</th></tr></thead><tbody><tr v-for="row in visibleComparisonRows" :key="row.label" :class="{ changed: row.changed }"><th>{{ row.label }}<small v-if="row.changed">{{ !row.left ? '新增' : !row.right ? '删除' : '修改' }}</small></th><td>{{ row.left || '（空）' }}</td><td>{{ row.right || '（空）' }}</td></tr></tbody></table></div>
            <div v-for="version in workspace.versions" :key="version.id" class="care-version-card">
              <div><strong>V{{ version.version }}</strong><span>{{ timestamp(version.publishedAt) }}</span></div>
              <h4>方案摘要</h4><p>{{ version.content.summary }}</p>
              <div v-for="(stage, index) in version.content.stages" :key="index" class="history-stage"><h4>阶段 {{ index + 1 }} · {{ stage.title }}</h4><p>{{ stage.body }}</p></div>
              <h4>医嘱与注意事项</h4><p>{{ version.content.instructions || '无补充事项' }}</p>
              <small>{{ version.publishedBy }} · {{ version.changeReason }}</small>
            </div>
          </section>
        </template>
      </template>
    </section>
  </div>
  <el-dialog v-model="conflictDialogVisible" title="草稿版本冲突 · 请合并后再保存" width="900px" :close-on-click-modal="false">
    <p>另一位同事已先保存了这份草稿（服务端草稿 V{{ conflict?.serverVersion }}）。你的未保存修改仍在编辑器里，没有被覆盖。请逐项选择保留哪一方的文字，合并后保存；也可以直接采用服务端版本。</p>
    <p v-if="conflict">{{ conflictChangedCount }} 项内容不一致 · 阶段按顺序对照</p>
    <div class="comparison-table-wrap"><table class="comparison-table"><thead><tr><th>内容</th><th>服务端最新</th><th>我的未保存修改</th><th>合并时保留</th></tr></thead><tbody><tr v-for="row in conflictRows" :key="row.key" :class="{ changed: row.changed }"><th>{{ row.label }}<small v-if="row.changed">不一致</small></th><td>{{ row.server || '（空）' }}</td><td>{{ row.mine || '（空）' }}</td><td><el-radio-group v-model="conflictChoices[row.key]"><el-radio value="server">服务端</el-radio><el-radio value="mine">我的</el-radio></el-radio-group></td></tr></tbody></table></div>
    <template #footer>
      <el-button @click="dismissConflict">稍后处理</el-button>
      <el-button @click="resolveConflictServer">采用服务端版本</el-button>
      <el-button type="primary" :loading="saving" @click="resolveConflictMerge">合并并保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.care-filters { display: grid; grid-template-columns: 1fr auto; gap: 10px; padding: 14px; }
.care-filters > :first-child { grid-column: 1 / -1; }
.care-pagination { padding: 14px 8px; justify-content: center; }
.template-tools { margin-bottom: 18px; padding: 14px; border: 1px solid #e3eaf2; border-radius: 8px; }
.template-tools p { color: #71839a; font-size: 12px; margin: 8px 0; }
.template-actions, .comparison-controls { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 10px; align-items: center; }
.template-actions .el-input, .template-actions .el-select { width: 240px; max-width: 100%; }
.comparison-controls h4, .comparison-controls p { flex-basis: 100%; }
.comparison-controls .el-select { width: 180px; }
.comparison-table-wrap { overflow-x: auto; margin: 15px 0; }
.comparison-table { width: 100%; border-collapse: collapse; table-layout: fixed; font-size: 12px; }
.comparison-table th, .comparison-table td { border: 1px solid #e3eaf2; padding: 10px; text-align: left; vertical-align: top; white-space: pre-wrap; overflow-wrap: anywhere; }
.comparison-table th:first-child { width: 110px; }
.comparison-table thead { background: #f7f9fc; }
.comparison-table .changed td { background: #fff7e6; }
.comparison-table small { display: block; color: #a66b13; margin-top: 5px; }
.care-version-card h4 { margin-top: 12px; font-size: 12px; color: #43668d; }

.care-demo-note { margin: -7px 0 18px; padding: 11px 14px; border: 1px solid #e4ebf3; border-radius: 8px; background: #f7f9fc; color: #71839a; font-size: 11px; }
.care-error, .care-notice { margin-bottom: 16px; }
.care-layout { display: grid; grid-template-columns: 300px minmax(0, 1fr); gap: 17px; align-items: start; }
.care-list-panel, .care-detail-panel { min-width: 0; border: 1px solid #e3eaf2; border-radius: 11px; background: #fff; overflow: hidden; }
.care-panel-heading, .care-detail-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 19px 20px; border-bottom: 1px solid #edf1f6; }
.care-panel-heading h2, .care-detail-heading h2 { color: #304761; font-size: 15px; font-weight: 600; }
.care-panel-heading p, .care-detail-heading p { margin-top: 4px; color: #8191a5; font-size: 10px; }
.care-detail-heading { padding: 22px 25px; }
.care-detail-heading .eyebrow { margin: 0; }
.care-appointment { display: block; width: 100%; padding: 14px 19px; border: 0; border-bottom: 1px solid #eff2f7; background: #fff; text-align: left; cursor: pointer; }
.care-appointment:hover { background: #f8fbff; }
.care-appointment.selected { background: #eff6ff; box-shadow: inset 3px 0 #1763c6; }
.care-patient, .care-visit, .care-id { display: block; }
.care-patient { color: #314b68; font-size: 12px; font-weight: 600; }
.care-patient small { margin-left: 5px; color: #91a0b2; font-size: 9px; font-weight: 400; }
.care-visit { margin-top: 5px; color: #687f99; font-size: 10px; }
.care-id { margin-top: 4px; color: #a1adbc; font-size: 9px; }
.care-state { padding: 28px 18px; color: #8797aa; font-size: 11px; text-align: center; }
.care-empty { padding: 80px 25px; }
.care-section { padding: 21px 25px; border-bottom: 1px solid #edf1f6; }
.care-section-heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 15px; }
.care-section-heading h3 { color: #3d5877; font-size: 13px; font-weight: 600; }
.care-section-heading h4 { color: #3d5877; font-size: 12px; font-weight: 600; }
.care-section-heading p { margin-top: 4px; color: #8797aa; font-size: 10px; line-height: 1.7; }
.care-photo-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.care-photo { min-width: 0; margin: 0; border: 1px solid #e6edf5; border-radius: 8px; overflow: hidden; }
.care-photo img { display: block; width: 100%; height: 205px; object-fit: contain; background: #f4f6f9; }
.care-photo figcaption { padding: 8px; color: #7d8ea3; font-size: 9px; }
.care-empty-small { padding: 21px; border: 1px dashed #dfe7f0; border-radius: 8px; color: #8998aa; font-size: 10px; text-align: center; }
.care-form { max-width: 820px; }
.care-form :deep(.el-form-item) { margin-bottom: 16px; }
.care-form :deep(.el-form-item__label) { color: #526a86; font-size: 11px; padding-bottom: 5px; }
.care-form :deep(.el-input__wrapper), .care-form :deep(.el-textarea__inner) { font-size: 11px; }
.stage-heading { margin: 2px 0 11px; }
.stage-editor { display: grid; gap: 9px; padding: 12px; margin-bottom: 13px; border: 1px solid #e8eef5; border-radius: 8px; background: #fafcff; }
.stage-editor-heading { display: flex; justify-content: space-between; align-items: center; color: #5e7692; font-size: 10px; }
.care-actions { display: flex; gap: 9px; align-items: center; flex-wrap: wrap; }
.autosave-status { font-size: 12px; color: #71839a; }
.conflict-banner { margin-top: 14px; }
.care-version-card { padding: 13px 15px; margin-top: 9px; border: 1px solid #e8eef5; border-radius: 8px; background: #fafcff; }
.care-version-card > div { display: flex; align-items: center; gap: 10px; color: #43668d; font-size: 11px; }
.care-version-card > div span, .care-version-card small { color: #8b9bad; font-size: 9px; font-weight: 400; }
.care-version-card p { margin-top: 8px; color: #556b84; font-size: 10px; white-space: pre-wrap; }
.care-version-card small { display: block; margin-top: 8px; }
@media (max-width: 900px) { .care-layout { grid-template-columns: 1fr; } .care-list-panel { max-height: 320px; overflow-y: auto; } }
@media (max-width: 600px) { .care-section { padding: 17px; } .care-photo-grid { grid-template-columns: 1fr; } .care-photo img { height: 260px; } }
</style>
