<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElRadio, ElCheckbox } from 'element-plus'
import type { components } from '../../../../packages/contracts/src/schema'
import { api, ApiError, errorMessage, type AssessmentView, type AssessmentContent, type AssessmentTask } from '../api'
import { auth } from '../auth'
import PrivatePhoto from '../components/PrivatePhoto.vue'

const route = useRoute(), router = useRouter()
const labels: Record<AssessmentTask['status'], string> = { draft:'准备照片', queued:'等待分析', analyzing:'正在分析', retry_wait:'等待重试', failed:'分析失败', awaiting_review:'待医生复核', completed:'复核完成', rejected:'需补拍或面诊', cancelled:'已取消' }
const observationCodes = {surface_irregularity:'表面不平整',color_variation:'颜色变化',other_visible_change:'其他可见变化'}
const regionNames = {forehead:'额头',left_cheek:'本人左侧面颊',right_cheek:'本人右侧面颊',nose:'鼻部',chin:'下巴',multiple:'多部位',unknown:'无法确定'}
const certaintyNames = {low:'低',medium:'中',high:'高',unknown:'无法确定'}
const angleLabels: Record<string,string> = { front:'正面', left:'本人左侧45°', right:'本人右侧45°' }
const mode = computed(() => String(route.meta.assessmentMode || 'list'))
const id = computed(() => String(route.params.id || ''))
const rows = ref<AssessmentTask[]>([]), selected = ref<AssessmentView | null>(null), total = ref(0), page = ref(1)
const loading = ref(false), saving = ref(false), error = ref(''), success = ref(''), status = ref(''), clinicId = ref(''), query = ref('')
const clinics = ref<components['schemas']['Clinic'][]>([]), reviewers = ref<components['schemas']['AssessmentReviewer'][]>([]), reviewer = ref('')
const events = ref<components['schemas']['AssessmentEvent'][]>([]), capabilities = ref<components['schemas']['AssessmentCapabilities'] | null>(null)
const decision = ref<'approved'|'amended'|'rejected'>('approved'), note = ref(''), limitations = ref(''), preparation = ref('')
const policy = ref<components['schemas']['AssessmentConsentPolicyList'] | null>(null)
const policyEditing = ref(false), policyContent = ref(''), policyVersion = ref(''), policySaving = ref(false)
async function loadPolicy() {
  if (auth.employee?.role !== 'manager') return
  try { policy.value = await api.assessmentConsentPolicy() } catch { /* 文案读失败不影响任务队列 */ }
}
function startPolicyEdit() {
  if (!policy.value) return
  policyContent.value = policy.value.policy.content; policyVersion.value = ''; policyEditing.value = true
}
const policyBy = (createdBy: string) => createdBy === auth.employee?.id ? '本人' : (createdBy || '系统')
async function savePolicy() {
  if (policySaving.value || !policyContent.value.trim()) return
  policySaving.value = true; error.value = ''; success.value = ''
  try {
    policy.value = await api.createAssessmentConsentPolicy(policyContent.value.trim(), policyVersion.value.trim() || undefined)
    policyEditing.value = false
    success.value = `已发布授权文案新版 ${policy.value.policy.version}，旧版授权已失效，患者需重新确认。`
  } catch (cause) { error.value = errorMessage(cause) } finally { policySaving.value = false }
}
const edited = ref<AssessmentContent>({ summary:'', observations:[], limitations:[], consultationAdvice:{recommended:true,reason:'',preparation:[]} })
const canReview = computed(() => auth.employee?.role === 'doctor' && selected.value?.task.reviewerEmployeeId === auth.employee.id && selected.value.consent.granted && ['awaiting_review','completed','rejected'].includes(selected.value.task.status))
type WriteBody = components['schemas']['AssessmentAssignment'] | components['schemas']['AssessmentReviewCreate']
type Pending = { action:'assignment'|'reviews'; body:WriteBody; key:string }
const pending = ref<Pending | null>(null)
const storageKey = () => `assessment-write-v1:${auth.employee?.id}:${id.value}`
let generation = 0, alive = true, timer: ReturnType<typeof setInterval> | undefined, reviewBaseVersion = 0, editIdentity = ''
const time = (stamp:string) => new Intl.DateTimeFormat('zh-CN',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}).format(new Date(stamp))
function initialiseReview(view: AssessmentView) {
  const identity = `${view.task.id}:${view.result?.id || ''}`
  if (identity === editIdentity) return
  editIdentity = identity; reviewBaseVersion = view.task.version; note.value = ''; decision.value = 'approved'
  const content = view.publishedReview?.finalContent || view.result?.analysis
  if (content) {
    edited.value = { summary:content.summary, observations:structuredClone(content.observations), limitations:[...content.limitations], consultationAdvice:structuredClone(content.consultationAdvice) }
    limitations.value = content.limitations.join('\n'); preparation.value = content.consultationAdvice.preparation.join('\n')
  }
}
async function load(poll = false) {
  if (loading.value || saving.value || !alive || document.visibilityState !== 'visible') return
  const request = ++generation
  loading.value = true
  if (!poll) error.value = ''
  try {
    if (!id.value) {
      const result = await api.assessments(status.value,clinicId.value,page.value,query.value.trim())
      if (request !== generation || !alive) return
      rows.value = result.data; total.value = result.meta.total
    } else {
      const view = await api.assessment(id.value)
      if (request !== generation || !alive) return
      selected.value = view
      if (!view.consent.granted) { editIdentity = ''; edited.value = {summary:'',observations:[],limitations:[],consultationAdvice:{recommended:true,reason:'',preparation:[]}}; note.value = ''; limitations.value = ''; preparation.value = ''; pending.value = null; sessionStorage.removeItem(storageKey()) }
      else initialiseReview(view)
      if (auth.employee?.role === 'manager' && view.consent.granted) {
        const candidates = await api.assessmentReviewers(view.task.clinicId)
        if (request === generation && alive) reviewers.value = candidates
      }
      if (!poll) {
        const audit = await api.assessmentEvents(id.value)
        if (request === generation && alive) events.value = audit
        const cached = sessionStorage.getItem(storageKey())
        if (cached && !pending.value && view.consent.granted) { try { pending.value = JSON.parse(cached) } catch { sessionStorage.removeItem(storageKey()) } }
      }
    }
  } catch (cause) {
    if (request !== generation || !alive) return
    error.value = errorMessage(cause)
    if (cause instanceof ApiError && [401,403,404].includes(cause.status)) { selected.value = null; rows.value = []; pending.value = null; sessionStorage.removeItem(storageKey()); events.value = []; reviewers.value = [] }
    else if (!id.value) rows.value = []
  } finally { if (request === generation) loading.value = false }
}
async function write(action?: 'assignment'|'reviews') {
  if (!selected.value || saving.value) return
  if (!pending.value) {
    if (!action) return
    let body:WriteBody
    if (action === 'assignment') body = { version:selected.value.task.version, reviewerEmployeeId:reviewer.value }
    else {
      if (!canReview.value || !selected.value.result) return
      const finalContent:AssessmentContent = { ...edited.value, limitations:limitations.value.split('\n').map(v=>v.trim()).filter(Boolean), consultationAdvice:{...edited.value.consultationAdvice,preparation:preparation.value.split('\n').map(v=>v.trim()).filter(Boolean)} }
      body = { version:reviewBaseVersion, resultId:selected.value.result.id, decision:decision.value, note:note.value.trim(), ...(decision.value === 'amended' ? {finalContent} : {}) }
    }
    pending.value = {action,body,key:crypto.randomUUID()}; sessionStorage.setItem(storageKey(),JSON.stringify(pending.value))
  }
  const operation = pending.value, taskId = id.value, employeeId = auth.employee?.id, request = ++generation
  saving.value = true; loading.value = false; error.value = ''; success.value = ''
  try {
    const view = await api.assessmentWrite(taskId,operation.action,operation.body,operation.key)
    if (!alive || request !== generation || auth.employee?.id !== employeeId) return
    selected.value = view; reviewBaseVersion = view.task.version
    pending.value = null; sessionStorage.removeItem(storageKey()); success.value = '操作已保存，患者读取最新复核结果。'
    const audit = await api.assessmentEvents(taskId)
    if (alive && request === generation) events.value = audit
  } catch (cause) {
    if (!alive || request !== generation) return
    error.value = errorMessage(cause)
    if (cause instanceof ApiError && cause.status >= 400 && cause.status < 500) { pending.value = null; sessionStorage.removeItem(storageKey()) }
    else error.value += ' 原请求已保留，请重试原操作以确认结果。'
  } finally { if (alive && request === generation) { saving.value = false; void load(true) } }
}
const decisionLabels: Record<string, string> = { approved: '通过', amended: '修订后发布', rejected: '补拍 / 面诊' }
const eventLabels: Record<string, string> = { created: '创建', read: '查看', consent: '授权变更', cancel: '患者取消', 'quality-check': '质检申请', submit: '提交分析', assignment: '分配复核医生', review: '医生复核', 'photo-upload': '上传照片', 'photo-read': '查看照片', cancelled: '系统取消', 'quality-checked': '质检完成', 'analysis-failed': '分析失败', 'analysis-completed': '分析完成' }
type ReviewDiff = { summary: { old: string, next: string } | null, added: AssessmentContent['observations'], removed: AssessmentContent['observations'], changed: { old: AssessmentContent['observations'][number], next: AssessmentContent['observations'][number] }[], limAdded: string[], limRemoved: string[], adviceChanged: boolean, adviceOld: AssessmentContent['consultationAdvice'], adviceNext: AssessmentContent['consultationAdvice'] }
function observationSame(a: AssessmentContent['observations'][number], b: AssessmentContent['observations'][number]): boolean {
  return a.code === b.code && a.description === b.description && a.region === b.region && a.certainty === b.certainty && a.evidence.map(e => e.photoId).sort().join() === b.evidence.map(e => e.photoId).sort().join()
}
function reviewDiff(review: NonNullable<AssessmentView['reviews']>[number]): ReviewDiff | null {
  const original = selected.value?.result?.analysis
  const final = review.finalContent
  if (!original || !final) return null
  const byId = new Map(original.observations.map(o => [o.id, o]))
  const added: AssessmentContent['observations'] = [], removed: AssessmentContent['observations'] = [], changed: ReviewDiff['changed'] = []
  const seen = new Set<string>()
  for (const o of final.observations) {
    seen.add(o.id)
    const old = byId.get(o.id)
    if (!old) added.push(o)
    else if (!observationSame(old, o)) changed.push({ old, next: o })
  }
  for (const o of original.observations) if (!seen.has(o.id)) removed.push(o)
  return {
    summary: original.summary !== final.summary ? { old: original.summary, next: final.summary } : null,
    added, removed, changed,
    limAdded: final.limitations.filter(l => !original.limitations.includes(l)),
    limRemoved: original.limitations.filter(l => !final.limitations.includes(l)),
    adviceChanged: original.consultationAdvice.reason !== final.consultationAdvice.reason || original.consultationAdvice.recommended !== final.consultationAdvice.recommended || original.consultationAdvice.preparation.join() !== final.consultationAdvice.preparation.join(),
    adviceOld: original.consultationAdvice, adviceNext: final.consultationAdvice,
  }
}
const reviewHistory = computed(() => (selected.value?.reviews || []).map(review => ({ review, diff: reviewDiff(review) })))
function addObservation() { if (!canReview.value || edited.value.observations.length >= 30) return; const photo = selected.value?.photos[0]; edited.value.observations.push({id:`review-${crypto.randomUUID()}`,code:'other_visible_change',description:'',region:'unknown',certainty:'unknown',evidence:photo?[{photoId:photo.id,angle:photo.angle}]:[]}) }
function setEvidence(observation: AssessmentContent['observations'][number], ids:string[]) { observation.evidence = (selected.value?.photos || []).filter(photo=>ids.includes(photo.id)).map(photo=>({photoId:photo.id,angle:photo.angle})) }
async function refresh() { if (saving.value) return; loading.value = false; await load(); if (selected.value) reviewBaseVersion = selected.value.task.version }
function visibility() { if (document.visibilityState !== 'visible') { generation++; loading.value = false; saving.value = false; selected.value = null; rows.value = []; events.value = []; reviewers.value = []; editIdentity = ''; edited.value = {summary:'',observations:[],limitations:[],consultationAdvice:{recommended:true,reason:'',preparation:[]}}; note.value = ''; limitations.value = ''; preparation.value = '' } else void load() }
watch(() => route.fullPath, () => { generation++; loading.value = false; saving.value = false; selected.value = null; rows.value = []; pending.value = null; editIdentity = ''; reviewer.value = ''; events.value = []; error.value = ''; success.value = ''; void load() }, {immediate:true})
onMounted(async () => {
  document.addEventListener('visibilitychange',visibility)
  timer = setInterval(() => void load(true),5000)
  const results = await Promise.allSettled([api.assessmentCapabilities(),api.clinics()])
  if (!alive) return
  if (results[0].status === 'fulfilled') capabilities.value = results[0].value
  if (results[1].status === 'fulfilled') clinics.value = results[1].value.filter(c=>auth.employee?.clinicIds.includes(c.id))
  void loadPolicy()
})
onBeforeUnmount(() => { alive = false; generation++; if(timer) clearInterval(timer); document.removeEventListener('visibilitychange',visibility) })
</script>

<template>
<section class="assessment-workspace">
  <div class="page-heading"><div><h1>AI 辅助评估</h1><p>患者照片辅助观察 · 医生分配与复核 · 时间均为北京时间</p></div><el-button :loading="loading" :disabled="saving" @click="refresh">刷新任务</el-button></div>
  <el-alert v-if="capabilities && (!capabilities.qualityAvailable || !capabilities.modelAvailable)" title="检查器或模型尚未配置。已保存任务可以查看和复核，新的分析需配置完成后提交。" type="warning" :closable="false" />
  <el-alert v-if="error" :title="error" type="error" :closable="false" class="feedback" />
  <el-alert v-if="success" :title="success" type="success" :closable="false" class="feedback" />
  <el-button v-if="pending" :loading="saving" @click="write()">重试原操作</el-button>
  <div v-if="!id && auth.employee?.role==='manager'" class="surface panel">
    <h2>AI 用途授权文案</h2>
    <p v-if="policy">当前版本 {{policy.policy.version}} · {{time(policy.policy.createdAt)}} · 发布人 {{policyBy(policy.policy.createdBy)}}</p>
    <p v-if="policy">{{policy.policy.content}}</p>
    <p v-else>授权文案加载中或暂不可用。</p>
    <el-button :disabled="!policy || policySaving" @click="startPolicyEdit">编辑文案并发布新版</el-button>
    <template v-if="policyEditing">
      <el-form label-position="top">
        <el-form-item label="新版文案"><el-input v-model="policyContent" type="textarea" :rows="6" maxlength="5000" show-word-limit /></el-form-item>
        <el-form-item label="版本号（留空自动升版，如 photo-ai-v2）"><el-input v-model="policyVersion" maxlength="64" placeholder="留空自动升版" /></el-form-item>
      </el-form>
      <p>发布新版后，之前按旧版授权的患者需重新确认，旧授权立即失效。</p>
      <el-button type="primary" :loading="policySaving" :disabled="!policyContent.trim()" @click="savePolicy">发布新版</el-button>
      <el-button :disabled="policySaving" @click="policyEditing=false">取消</el-button>
    </template>
    <details v-if="policy && policy.history.length>1"><summary>历史版本（{{policy.history.length}}）</summary><p v-for="item in policy.history" :key="item.version">{{item.version}} · {{time(item.createdAt)}} · {{policyBy(item.createdBy)}}</p></details>
  </div>
  <div v-if="!id" class="surface panel">
    <p v-if="mode==='entry'">评估由患者从小程序“AI 辅助评估”入口独立创建并明确授权。工作人员可在下方队列分配复核医生。</p>
    <div class="toolbar"><el-select v-model="status" placeholder="全部状态" clearable aria-label="评估状态" @change="page=1;load()"><el-option v-for="(label,value) in labels" :key="value" :label="label" :value="value" /></el-select><el-select v-model="clinicId" clearable placeholder="全部授权院区" @change="page=1;load()"><el-option v-for="clinic in clinics" :key="clinic.id" :value="clinic.id" :label="clinic.name" /></el-select><el-input v-model="query" placeholder="完整任务编号" @keyup.enter="page=1;load()" /><el-button @click="page=1;load()">查询任务</el-button><span>共 {{total}} 项</span></div>
    <el-table :data="rows"><el-table-column label="任务" min-width="250"><template #default="{row}"><strong>{{row.id}}</strong><p>{{row.clinicId}} · 患者 {{row.patientId}}</p></template></el-table-column><el-table-column label="状态"><template #default="{row}">{{labels[row.status as AssessmentTask['status']]}}</template></el-table-column><el-table-column label="更新时间" min-width="160"><template #default="{row}">{{time(row.updatedAt)}}</template></el-table-column><el-table-column label="操作" min-width="240"><template #default="{row}"><el-button link type="primary" @click="router.push(`/assessments/${row.id}/task`)">查看任务</el-button><el-button v-if="['awaiting_review','completed','rejected'].includes(row.status)" link type="primary" @click="router.push(`/assessments/${row.id}/review`)">{{auth.employee?.role==='doctor' ? '医生复核' : '查看复核资料'}}</el-button></template></el-table-column></el-table>
    <el-pagination v-if="total>20" v-model:current-page="page" :total="total" :page-size="20" layout="prev,pager,next" @current-change="load()" />
  </div>
  <template v-else-if="selected">
    <div class="surface panel"><h2>{{selected.task.id}}</h2><p>{{labels[selected.task.status]}} · 任务版本 {{selected.task.version}} · 分析尝试 {{selected.task.attemptCount}} / {{selected.task.maxAttempts}}</p><p v-if="selected.task.failureCode">处理结果：{{selected.task.failureCode}}</p><el-alert v-if="!selected.consent.granted" title="AI授权已撤回或尚未同意，医护无法读取照片及结果。" type="warning" :closable="false" /><div v-if="auth.employee?.role==='manager' && selected.consent.granted && !pending" class="toolbar"><el-select v-model="reviewer" placeholder="选择同院区在岗医生" aria-label="复核医生"><el-option v-for="candidate in reviewers" :key="candidate.id" :value="candidate.id" :label="candidate.displayName" /></el-select><el-button :loading="saving" :disabled="!reviewer" @click="write('assignment')">分配 / 改派医生</el-button></div><el-button @click="router.push('/assessments')">返回队列</el-button><el-button @click="router.push(`/assessments/${id}/result`)">患者结果预览</el-button></div>
    <div v-if="selected.consent.granted && mode!=='review'" class="photo-grid"><div v-for="photo in selected.photos" :key="photo.id" class="surface panel"><h3>{{angleLabels[photo.angle]}}</h3><PrivatePhoto :id="photo.id" source="assessment" :assessment-id="id" :alt="angleLabels[photo.angle] || '评估照片'" /><p>质检：{{photo.qualityCheck.status}}</p><p v-for="advice in photo.qualityCheck.retakeAdvice" :key="advice">{{advice}}</p></div></div>
    <div v-if="mode==='result'" class="surface panel"><h2>患者可见结果</h2><template v-if="selected.publishedReview"><p>{{selected.publishedReview.doctorName}} · {{time(selected.publishedReview.reviewedAt)}} · 复核版本 {{selected.publishedReview.version}}</p><p>{{selected.publishedReview.note}}</p><template v-if="selected.publishedReview.finalContent"><h3>观察概要</h3><p>{{selected.publishedReview.finalContent.summary}}</p><p v-for="observation in selected.publishedReview.finalContent.observations" :key="observation.id">{{observation.description}}</p><h3>观察局限</h3><p v-for="limit in selected.publishedReview.finalContent.limitations" :key="limit">{{limit}}</p><h3>面诊建议</h3><p>{{selected.publishedReview.finalContent.consultationAdvice.reason}}</p></template></template><el-empty v-else description="尚无经医生复核的患者结果" /><p>照片辅助观察，具体以面诊为准。</p></div>
    <div v-if="mode!=='result' && selected.result" :class="mode==='review' ? 'review-compare' : 'two-columns'">
      <div v-if="mode==='review'" class="surface panel review-photos"><h2>原图对照</h2><div v-for="photo in selected.photos" :key="photo.id" class="review-photo"><h3>{{angleLabels[photo.angle]}}</h3><PrivatePhoto :id="photo.id" source="assessment" :assessment-id="id" :alt="angleLabels[photo.angle] || '评估照片'" /><p>质检：{{photo.qualityCheck.status}}</p><p v-for="advice in photo.qualityCheck.retakeAdvice" :key="advice">{{advice}}</p></div></div>
      <div class="surface panel"><h2>原始辅助观察 · 仅医护查看</h2><p>{{selected.result.analysis.summary}}</p><p v-for="observation in selected.result.analysis.observations" :key="observation.id">{{observation.description}} · {{observation.region}} · {{observation.certainty}}</p><h3>观察局限</h3><p v-for="limit in selected.result.analysis.limitations" :key="limit">{{limit}}</p><h3>面诊建议</h3><p>{{selected.result.analysis.consultationAdvice.reason}}</p><h3>模型与处理版本</h3><p v-for="(value,key) in selected.result.analysis.model" :key="key">{{key}}：{{value}}</p></div>
      <div class="surface panel"><h2>医生复核与发布</h2><el-alert v-if="!canReview" title="只有当前分配、在岗的医生可以复核。主管可查看和分配任务。" type="info" :closable="false" /><el-form label-position="top"><el-form-item label="复核结论"><el-radio-group v-model="decision" :disabled="!canReview || saving || !!pending"><el-radio value="approved">通过</el-radio><el-radio value="amended">修订后发布</el-radio><el-radio value="rejected">补拍 / 面诊</el-radio></el-radio-group></el-form-item><template v-if="decision==='amended'"><el-form-item label="观察概要"><el-input v-model="edited.summary" type="textarea" maxlength="1000" :disabled="!canReview || saving || !!pending" /></el-form-item><div v-for="(observation,index) in edited.observations" :key="observation.id" class="observation-edit"><el-form-item label="可见现象"><el-input v-model="observation.description" type="textarea" maxlength="500" :disabled="!canReview || saving || !!pending" /></el-form-item><el-form-item label="观察分类"><el-select v-model="observation.code" :disabled="!canReview || saving || !!pending"><el-option v-for="(label,value) in observationCodes" :key="value" :label="label" :value="value" /></el-select></el-form-item><el-form-item label="部位（左右按本人方向）"><el-select v-model="observation.region" :disabled="!canReview || saving || !!pending"><el-option v-for="(label,value) in regionNames" :key="value" :label="label" :value="value" /></el-select></el-form-item><el-form-item label="观察确定程度"><el-select v-model="observation.certainty" :disabled="!canReview || saving || !!pending"><el-option v-for="(label,value) in certaintyNames" :key="value" :label="label" :value="value" /></el-select></el-form-item><el-form-item label="证据照片（至少一张）"><el-select multiple :model-value="observation.evidence.map(e=>e.photoId)" :disabled="!canReview || saving || !!pending" @change="setEvidence(observation,$event)"><el-option v-for="photo in selected.photos" :key="photo.id" :label="angleLabels[photo.angle]" :value="photo.id" /></el-select></el-form-item><el-button type="danger" link :disabled="!canReview || saving || !!pending" @click="edited.observations.splice(index,1)">移除此观察</el-button></div><el-button :disabled="!canReview || saving || !!pending || edited.observations.length>=30" @click="addObservation">补充可见观察</el-button><el-form-item label="观察局限（每行一项，至少一项）"><el-input v-model="limitations" type="textarea" :disabled="!canReview || saving || !!pending" /></el-form-item><el-form-item label="面诊建议"><el-input v-model="edited.consultationAdvice.reason" type="textarea" maxlength="1000" :disabled="!canReview || saving || !!pending" /></el-form-item><el-checkbox v-model="edited.consultationAdvice.recommended" :disabled="!canReview || saving || !!pending">建议面诊</el-checkbox><el-form-item label="面诊准备（每行一项）"><el-input v-model="preparation" type="textarea" :disabled="!canReview || saving || !!pending" /></el-form-item></template><el-form-item :label="decision==='rejected' ? '面向患者的补拍 / 面诊说明' : '复核依据 / 更正原因'"><el-input v-model="note" type="textarea" :rows="4" maxlength="2000" :disabled="!canReview || saving || !!pending" /></el-form-item></el-form><el-button type="primary" :loading="saving" :disabled="loading || !canReview || !note.trim() || !!pending" @click="write('reviews')">保存复核并发布</el-button><el-button :disabled="saving" @click="refresh">同步最新版本并保留草稿</el-button></div>
    </div>
    <div v-if="selected.reviews?.length" class="surface panel"><h2>复核历史 · 改动留痕</h2><div v-for="item in reviewHistory" :key="item.review.id" class="review-history">
      <p><strong>V{{item.review.version}} · {{item.review.doctorName}} · {{time(item.review.reviewedAt)}} · {{decisionLabels[item.review.decision] || item.review.decision}}</strong></p>
      <p>{{item.review.note}}</p>
      <template v-if="item.review.decision==='amended' && item.review.finalContent">
        <div v-if="item.diff" class="review-diff">
          <p v-if="item.diff.summary" class="diff-row"><span class="diff-old">改前：{{item.diff.summary.old}}</span><span class="diff-new">改后：{{item.diff.summary.next}}</span></p>
          <p v-for="obs in item.diff.added" :key="obs.id" class="diff-add">＋ 新增观察：{{obs.description}} · {{regionNames[obs.region as keyof typeof regionNames] || obs.region}} · {{certaintyNames[obs.certainty as keyof typeof certaintyNames] || obs.certainty}}</p>
          <p v-for="obs in item.diff.removed" :key="obs.id" class="diff-del">－ 删除观察：{{obs.description}} · {{regionNames[obs.region as keyof typeof regionNames] || obs.region}} · {{certaintyNames[obs.certainty as keyof typeof certaintyNames] || obs.certainty}}</p>
          <p v-for="pair in item.diff.changed" :key="pair.old.id" class="diff-row"><span class="diff-old">改前：{{pair.old.description}} · {{regionNames[pair.old.region as keyof typeof regionNames] || pair.old.region}} · {{certaintyNames[pair.old.certainty as keyof typeof certaintyNames] || pair.old.certainty}}</span><span class="diff-new">改后：{{pair.next.description}} · {{regionNames[pair.next.region as keyof typeof regionNames] || pair.next.region}} · {{certaintyNames[pair.next.certainty as keyof typeof certaintyNames] || pair.next.certainty}}</span></p>
          <p v-for="limit in item.diff.limAdded" :key="'a'+limit" class="diff-add">＋ 新增局限：{{limit}}</p>
          <p v-for="limit in item.diff.limRemoved" :key="'r'+limit" class="diff-del">－ 删除局限：{{limit}}</p>
          <p v-if="item.diff.adviceChanged" class="diff-row"><span class="diff-old">改前建议：{{item.diff.adviceOld.recommended ? '建议面诊' : '暂不面诊'}} · {{item.diff.adviceOld.reason}}</span><span class="diff-new">改后建议：{{item.diff.adviceNext.recommended ? '建议面诊' : '暂不面诊'}} · {{item.diff.adviceNext.reason}}</span></p>
          <p v-if="!item.diff.summary && !item.diff.added.length && !item.diff.removed.length && !item.diff.changed.length && !item.diff.limAdded.length && !item.diff.limRemoved.length && !item.diff.adviceChanged">与 AI 原始结论一致，未作修改。</p>
        </div>
        <p v-else>AI 原始结论不可用，无法对照改动。</p>
      </template>
      <p v-else-if="item.review.decision==='approved'">与 AI 原始结论一致，未作修改。</p>
    </div></div>
    <div class="surface panel"><h2>任务审计</h2><p v-for="event in events" :key="event.id">{{time(event.createdAt)}} · V{{event.version}} · {{eventLabels[event.action] || event.action}}<span v-if="event.detail"> · {{event.detail}}</span></p><p v-if="!events.length">暂无审计记录</p></div>
  </template>
  <el-empty v-else-if="!loading" description="没有可访问的评估任务" />
</section>
</template>
<style scoped>
.page-heading,.toolbar{display:flex;align-items:center;gap:14px;flex-wrap:wrap;margin-bottom:20px}.page-heading{justify-content:space-between}.page-heading h1{margin:0}.panel{padding:24px;margin-top:20px}.panel p{white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.8}.toolbar .el-select{width:200px}.toolbar .el-input{width:270px}.photo-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:18px}.two-columns{display:grid;grid-template-columns:1fr 1fr;gap:20px}.review-compare{display:grid;grid-template-columns:280px minmax(0,1fr) minmax(0,1.15fr);gap:20px}.review-compare>.panel{margin-top:20px}.review-photos{position:sticky;top:20px;align-self:start}.review-photo+.review-photo{margin-top:18px}.review-history{border-top:1px solid #edf1f6;padding-top:14px;margin-top:14px}.review-history:first-of-type{border-top:0;padding-top:0;margin-top:0}.review-diff{margin-top:8px}.diff-row{display:flex;flex-direction:column;gap:4px}.diff-old{background:#fff5f5;border-left:3px solid #e8a13a;padding:6px 10px}.diff-new{background:#f0f7f4;border-left:3px solid #36897d;padding:6px 10px}.diff-add{background:#f0f7f4;border-left:3px solid #36897d;padding:6px 10px}.diff-del{background:#fff5f5;border-left:3px solid #c25e5e;padding:6px 10px}.feedback{margin:18px 0}.observation-edit{border:1px solid #e5ebf3;border-radius:10px;padding:18px;margin:18px 0}.observation-edit .el-select{width:100%}@media(max-width:1000px){.two-columns,.photo-grid,.review-compare{grid-template-columns:1fr}.review-photos{position:static}}
</style>
