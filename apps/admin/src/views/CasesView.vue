<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import type { components } from '../../../../packages/contracts/src/schema'
import { api, errorMessage, type Appointment, type CarePhoto, type FollowupWorkspace } from '../api'
import { auth } from '../auth'
import PrivatePhoto from '../components/PrivatePhoto.vue'
import CaseComparison from '../components/CaseComparison.vue'

type Case = components['schemas']['CaseView']
const manager = computed(() => auth.employee?.role === 'manager')
const rows = ref<Case[]>([]), current = ref<Case>(), history = ref<components['schemas']['CaseEvent'][]>([])
const appointments = ref<Appointment[]>([]), before = ref<CarePhoto[]>([]), workspace = ref<FollowupWorkspace>()
const page = ref(1), total = ref(0), status = ref(''), error = ref(''), busy = ref(false), editing = ref(false)
const form = reactive({ appointmentId: '', title: '', summary: '', body: '', category: '', tags: [] as string[], pairs: [{ beforeSourceId: '', afterSourceId: '' }] })
const note = ref('')
const category = ref(''), tag = ref(''), candidateSearch = ref(''), candidateQuery = ref(''), candidatePage = ref(1), candidateTotal = ref(0)
async function candidates() { const result = await api.appointments({ status: 'completed', q: candidateSearch.value, page: candidatePage.value, pageSize: 20 }); appointments.value = result.data; candidateTotal.value = result.meta.total }
function searchCandidates() { candidateSearch.value = candidateQuery.value.trim(); candidatePage.value = 1; void run(candidates) }
function clearSources() { form.pairs = [{ beforeSourceId: '', afterSourceId: '' }]; before.value = []; workspace.value = undefined }
const after = computed(() => (workspace.value?.tasks || []).filter(task => !!task.submittedAt).flatMap(task => task.photos || []))
const completePairs = computed(() => form.pairs.length > 0 && form.pairs.every(pair => pair.beforeSourceId && pair.afterSourceId))
function resetSources() { clearSources(); void sources() }
const labels: Record<string, string> = { pending: '待审核', approved: '审核通过', rejected: '审核拒绝' }
async function run(fn: () => Promise<void>) { if (busy.value) return; busy.value = true; error.value = ''; try { await fn() } catch (e) { error.value = errorMessage(e) } finally { busy.value = false } }
async function load() { const result = await api.cases(page.value, status.value, category.value.trim(), tag.value.trim()); rows.value = result.data; total.value = result.meta.total }
async function select(row: Case) { await run(async () => { current.value = await api.caseDetail(row.id); history.value = await api.caseHistory(row.id); editing.value = false; note.value = '' }) }
async function sources() { if (!form.appointmentId) return; await run(async () => { const result = await Promise.all([api.carePhotos(form.appointmentId), api.followupWorkspace(form.appointmentId)]); before.value = result[0]; workspace.value = result[1] }) }
async function start(row?: Case) {
 await run(async () => {
  current.value = row; editing.value = true; before.value = []; workspace.value = undefined
  Object.assign(form, row ? { appointmentId: row.appointmentId, title: row.content.title, summary: row.content.summary, body: row.content.body, category: row.content.category || '', tags: [...(row.content.tags || [])], pairs: row.content.pairs.map(pair => ({ beforeSourceId: pair.beforeSourceId, afterSourceId: pair.afterSourceId })) } : { appointmentId: '', title: '', summary: '', body: '', category: '', tags: [] as string[], pairs: [{ beforeSourceId: '', afterSourceId: '' }] })
  if (!row) { candidateQuery.value = ''; candidateSearch.value = ''; candidatePage.value = 1; await candidates() }
  else { const result = await Promise.all([api.carePhotos(form.appointmentId), api.followupWorkspace(form.appointmentId)]); before.value = result[0]; workspace.value = result[1] }
 })
}
async function save() {
 await run(async () => {
  const body = { title: form.title, summary: form.summary, body: form.body, category: form.category.trim(), tags: form.tags, pairs: form.pairs }
  const row = current.value ? await api.updateCase(current.value.id, { ...body, version: current.value.version }) : await api.createCase({ ...body, appointmentId: form.appointmentId })
  current.value = row; editing.value = false; await load(); history.value = await api.caseHistory(row.id)
 })
}
async function action(action: 'review' | 'publish' | 'unpublish', decision?: 'approved' | 'rejected') {
 if (!current.value) return
 if (action === 'unpublish') { try { await ElMessageBox.confirm('撤回后，小程序停止展示本案例的文字和图片。', '撤回案例', { type: 'warning' }) } catch { return } }
 await run(async () => {
  const row = current.value!
  current.value = await api.caseAction(row.id, action, action === 'review' ? { version: row.version, decision: decision!, note: note.value } : { version: row.version })
  await load(); history.value = await api.caseHistory(row.id)
 })
}
onMounted(() => void run(load))
</script>

<template>
 <section class="cases-page">
  <div class="heading"><div><h1>案例库</h1><p>整理治疗前后对比，经患者独立授权和医生审核后发布。</p></div><el-button v-if="manager" type="primary" :disabled="busy" @click="start()">新增案例</el-button></div>
  <el-alert title="诊疗照片授权不能替代案例展示授权。修改文案、分类、标签或图片会撤回当前展示，并要求重新授权和审核。" type="info" :closable="false" />
  <el-alert v-if="error" :title="error" type="error" :closable="false" role="alert" />
  <div class="filters"><el-select v-model="status" placeholder="全部案例" clearable style="width:180px" :disabled="busy" @change="page = 1; run(load)"><el-option label="已发布" value="published"/><el-option label="未发布" value="unpublished"/></el-select><el-input v-model="category" placeholder="按案例分类筛选" maxlength="40" clearable :disabled="busy" style="width:180px" @keyup.enter="page = 1; run(load)"/><el-input v-model="tag" placeholder="按案例标签筛选" maxlength="30" clearable :disabled="busy" style="width:180px" @keyup.enter="page = 1; run(load)"/><el-button :disabled="busy" @click="page = 1; run(load)">筛选</el-button><el-button :disabled="busy" @click="run(load)">刷新</el-button></div>
  <el-table :data="rows" v-loading="busy" @row-click="select"><el-table-column label="案例"><template #default="{row}">{{ row.content.title }}</template></el-table-column><el-table-column label="分类"><template #default="{row}">{{ row.content.category || '未分类' }}</template></el-table-column><el-table-column label="标签"><template #default="{row}"><el-tag v-for="label in row.content.tags" :key="label">{{ label }}</el-tag></template></el-table-column><el-table-column label="授权"><template #default="{row}">{{ row.consentRevision === row.revision ? '本版本已授权' : '待患者授权' }}</template></el-table-column><el-table-column label="审核"><template #default="{row}">{{ labels[row.reviewStatus] }}</template></el-table-column><el-table-column label="发布"><template #default="{row}">{{ row.publishedAt ? '已发布' : '未发布' }}</template></el-table-column></el-table>
  <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="prev, pager, next" :disabled="busy" @current-change="run(load)" />
  <el-card v-if="editing" class="editor"><h2>{{ current ? '编辑案例' : '新增案例' }}</h2><el-form label-position="top">
   <div v-if="!current" class="candidate-search"><el-input v-model="candidateQuery" placeholder="搜索已治疗预约：姓名、手机号或编号" maxlength="100" :disabled="busy" clearable @keyup.enter="searchCandidates"/><el-button :disabled="busy" @click="searchCandidates">搜索预约</el-button><el-pagination v-model:current-page="candidatePage" :page-size="20" :total="candidateTotal" layout="total, prev, pager, next" :disabled="busy" @current-change="run(candidates)"/><p v-if="!appointments.length">未找到符合条件的已治疗预约。</p></div><el-form-item label="治疗记录"><el-select v-if="!current" v-model="form.appointmentId" :disabled="busy" placeholder="选择已治疗预约" @change="resetSources"><el-option v-for="a in appointments" :key="a.id" :label="`${a.contactName} · ${a.startsAt} · ${a.id}`" :value="a.id"/></el-select><span v-else>{{ form.appointmentId }}</span><el-input v-if="!current" v-model="form.appointmentId" placeholder="也可输入其他已治疗预约编号" :disabled="busy" @input="clearSources"/><el-button :disabled="busy || !form.appointmentId" @click="sources">加载照片</el-button></el-form-item>
   <el-form-item label="案例分类"><el-input v-model="form.category" maxlength="40" placeholder="例如：瘢痕修复"/></el-form-item><el-form-item label="案例标签（最多10个，每个30字）"><el-select v-model="form.tags" multiple filterable allow-create default-first-option :multiple-limit="10" placeholder="输入标签后回车添加"/></el-form-item><el-form-item label="标题"><el-input v-model="form.title" maxlength="200" /></el-form-item><el-form-item label="摘要"><el-input v-model="form.summary" maxlength="1000" /></el-form-item><el-form-item label="展示正文（请移除姓名、联系方式及其他身份信息）"><el-input v-model="form.body" type="textarea" :rows="5" maxlength="20000" /></el-form-item>
   <div v-for="(pair,index) in form.pairs" :key="index"><div class="pair"><div><el-form-item label="治疗前照片"><el-select v-model="pair.beforeSourceId" placeholder="选择面诊照片" @change="pair.afterSourceId = ''"><el-option v-for="p in before" :key="p.id" :label="`${p.angle} · ${p.uploadedAt}`" :value="p.id"/></el-select></el-form-item><PrivatePhoto v-if="pair.beforeSourceId" :id="pair.beforeSourceId" source="care" alt="治疗前照片"/></div><div><el-form-item label="治疗后照片（同角度、已提交随访）"><el-select v-model="pair.afterSourceId" placeholder="选择随访照片"><el-option v-for="p in after.filter(photo => photo.angle === before.find(b => b.id === pair.beforeSourceId)?.angle)" :key="p.id" :label="`${p.angle} · ${p.uploadedAt}`" :value="p.id"/></el-select></el-form-item><PrivatePhoto v-if="pair.afterSourceId" :id="pair.afterSourceId" source="followup" alt="治疗后照片"/></div></div><el-button v-if="form.pairs.length > 1" :disabled="busy" @click="form.pairs.splice(index,1)">移除此组</el-button></div>
   <el-button v-if="form.pairs.length < 3" :disabled="busy" @click="form.pairs.push({ beforeSourceId: '', afterSourceId: '' })">添加对比角度</el-button>
   <el-button type="primary" :disabled="busy || !form.title.trim() || !form.body.trim() || !completePairs" @click="save">保存并等待患者授权</el-button><el-button :disabled="busy" @click="editing = false">取消</el-button>
  </el-form></el-card>
  <el-card v-else-if="current" class="detail"><div class="heading"><h2>{{ current.content.title }}</h2><el-button v-if="manager" :disabled="busy" @click="start(current)">编辑</el-button></div><p>分类：{{ current.content.category || '未分类' }} <el-tag v-for="label in current.content.tags" :key="label">{{ label }}</el-tag></p><p>{{ current.content.summary }}</p><p class="body">{{ current.content.body }}</p>
   <div v-for="pair in current.content.pairs" :key="pair.beforeImageId" class="pair"><div><p>治疗前 · {{ pair.beforeAt }}</p><PrivatePhoto :id="pair.beforeImageId" :case-id="current.id" source="case" alt="治疗前案例图片"/></div><div><p>治疗后 · {{ pair.afterAt }}</p><PrivatePhoto :id="pair.afterImageId" :case-id="current.id" source="case" alt="治疗后案例图片"/></div></div>
   <CaseComparison v-for="pair in current.content.pairs" :key="`${current.revision}-${pair.beforeImageId}`" :case-id="current.id" :before-id="pair.beforeImageId" :after-id="pair.afterImageId" :angle="pair.angle"/>
   <p>展示授权：{{ current.consentRevision === current.revision ? '本版本已授权' : '尚未授权或已撤回' }} · {{ labels[current.reviewStatus] }} · {{ current.publishedAt ? '已发布' : '未发布' }}</p><p v-if="current.reviewNote">审核意见：{{ current.reviewNote }}</p>
   <template v-if="!manager"><el-input v-model="note" placeholder="审核意见（拒绝时必填）" maxlength="1000"/><el-button :disabled="busy" @click="action('review', 'approved')">审核通过</el-button><el-button :disabled="busy || !note.trim()" @click="action('review', 'rejected')">拒绝</el-button></template>
   <template v-else><el-button type="primary" :disabled="busy || current.consentRevision !== current.revision || current.reviewStatus !== 'approved' || !!current.publishedAt" @click="action('publish')">审核后发布</el-button><el-button type="danger" :disabled="busy || !current.publishedAt" @click="action('unpublish')">撤回展示</el-button></template>
   <h3>操作历史</h3><p v-for="event in history" :key="event.id">{{ event.createdAt }} · {{ event.action }} · 版本 {{ event.version }} · {{ event.actorId }}</p>
  </el-card>
 </section>
</template>
<style scoped>
.candidate-search { margin-bottom:16px }.candidate-search .el-input { max-width:420px; margin-right:12px }.el-tag { margin-right:6px }.filters { flex-wrap:wrap }.heading,.filters { display:flex; justify-content:space-between; align-items:center; gap:16px; margin-bottom:20px }.heading h1 { margin:0 }.heading p,p { color:#64748b }.filters { justify-content:flex-start; margin-top:20px }.editor,.detail { margin-top:24px }.pair { display:grid; grid-template-columns:1fr 1fr; gap:24px; margin-bottom:24px }.body { white-space:pre-wrap; color:#334155 }.el-alert { margin-bottom:12px }.el-pagination { margin-top:16px }
</style>
