<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { api, errorMessage, type KnowledgeArticle, type KnowledgeCategory, type KnowledgeEvent } from '../api'
import { auth } from '../auth'

const manager = computed(() => auth.employee?.role === 'manager')
const categories = ref<KnowledgeCategory[]>([]), clinics = ref<{id: string; name: string}[]>([]), rows = ref<KnowledgeArticle[]>([])
const current = ref<KnowledgeArticle>(), history = ref<KnowledgeEvent[]>([]), categoryHistory = ref<KnowledgeEvent[]>([])
const busy = ref(false), error = ref(''), notice = ref(''), editing = ref(false), categoryEditing = ref(false)
const selectedCategory = ref<KnowledgeCategory>(), page = ref(1), total = ref(0), note = ref(''), order = ref(0)
const filters = reactive({ clinicId: '', categoryId: '', status: '', reviewStatus: '', q: '' })
const form = reactive({ categoryId: '', title: '', summary: '', body: '', sourceText: '', sortOrder: 0 })
const categoryForm = reactive({ clinicId: '', name: '', sortOrder: 0, active: true })
const filteredCategories = computed(() => categories.value.filter(c => !filters.clinicId || c.clinicId === filters.clinicId))
const labels: Record<string, string> = { pending: '待审核', approved: '审核通过', rejected: '退回修改' }
const actions: Record<string, string> = { create: '创建', update: '编辑', review: '审核', publish: '发布', unpublish: '下架', sort: '排序' }
const validArticle = computed(() => !!form.title.trim() && !!form.body.trim() && (!!current.value || !!form.categoryId))
function date(value: string | null) { return value ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }) : '—' }
async function run(fn: () => Promise<void>) {
 if (busy.value) return
 busy.value = true; error.value = ''; notice.value = ''
 try { await fn() } catch (e) { error.value = errorMessage(e) } finally { busy.value = false }
}
async function list() { const result = await api.knowledgeArticles({ ...filters, q: filters.q.trim(), page: page.value }); rows.value = result.data; total.value = result.meta.total }
async function refresh() {
 const result = await Promise.all([api.knowledgeCategories(), manager.value ? api.adminClinics().then(rows => rows.map(c => ({ id: c.id, name: c.name }))) : api.clinics()])
 categories.value = result[0]; clinics.value = result[1].filter(c => auth.employee?.clinicIds.includes(c.id))
 await list()
 if (current.value && !editing.value) await detail(current.value.id)
}
function search() { page.value = 1; void run(list) }
async function detail(id: string) {
 const [article, events] = await Promise.all([api.knowledgeArticle(id), api.knowledgeHistory('articles', id)])
 current.value = article; history.value = events; note.value = ''; order.value = article.sortOrder
}
function select(row: KnowledgeArticle) { void run(async () => { await detail(row.id); editing.value = false }) }
function start() { current.value = undefined; history.value = []; Object.assign(form, { categoryId: filters.categoryId, title: '', summary: '', body: '', sourceText: '', sortOrder: 0 }); editing.value = true }
function edit() { if (!current.value) return; Object.assign(form, current.value, { sourceText: current.value.sourceText || '' }); editing.value = true }
async function save() { await run(async () => {
 const body = { title: form.title.trim(), summary: form.summary.trim(), body: form.body.trim(), sourceText: form.sourceText.trim() }
 const row = await api.saveKnowledgeArticle(current.value ? { ...body, version: current.value.version } : { ...body, categoryId: form.categoryId, sortOrder: form.sortOrder }, current.value?.id)
 current.value = row; editing.value = false; await list(); await detail(row.id); notice.value = '草稿已保存，等待医生审核。'
}) }
async function action(action: 'review' | 'publish' | 'unpublish' | 'sort', decision?: 'approved' | 'rejected') {
 if (!current.value || busy.value) return
 const row = current.value
 if (action === 'publish' || action === 'unpublish') {
  try { await ElMessageBox.confirm(action === 'publish' ? '将当前医生已审核的草稿发布到小程序？' : '下架后，文章将从列表、搜索、收藏和推荐中停止展示。', action === 'publish' ? '确认发布' : '确认下架', { type: 'warning' }) } catch { return }
  if (current.value?.id !== row.id || current.value.version !== row.version) return
 }
 await run(async () => {
  current.value = await api.knowledgeAction(row.id, action, { version: row.version, ...(action === 'review' ? { decision, note: note.value.trim() } : action === 'sort' ? { sortOrder: order.value } : {}) })
  await list(); await detail(row.id); notice.value = '操作已完成。'
 })
}
function startCategory(row?: KnowledgeCategory) { selectedCategory.value = row; categoryHistory.value = []; Object.assign(categoryForm, row || { clinicId: filters.clinicId || clinics.value[0]?.id || '', name: '', sortOrder: 0, active: true }); categoryEditing.value = true }
async function saveCategory() { await run(async () => {
 const body = { name: categoryForm.name.trim(), sortOrder: categoryForm.sortOrder, active: categoryForm.active }
 await api.saveKnowledgeCategory(selectedCategory.value ? { ...body, version: selectedCategory.value.version } : { ...body, clinicId: categoryForm.clinicId }, selectedCategory.value?.id)
 categoryEditing.value = false; await refresh(); notice.value = '分类已保存。'
}) }
function showCategoryHistory(row: KnowledgeCategory) { void run(async () => { categoryHistory.value = await api.knowledgeHistory('categories', row.id) }) }
onMounted(() => void run(refresh))
</script>

<template>
 <section class="knowledge-page">
  <div class="heading"><div><h1>知识库</h1><p>整理科普内容，经在岗医生审核后向患者发布。</p></div><el-button v-if="manager" type="primary" :disabled="busy" @click="start">新增文章</el-button></div>
  <el-alert title="修改草稿会清除本次审核。已发布文章继续展示原快照，重新审核并发布后才会更新；需停止展示请下架。" type="info" :closable="false" />
  <el-alert v-if="error" :title="error" type="error" :closable="false" role="alert" />
  <el-alert v-if="notice" :title="notice" type="success" :closable="false" />
  <el-tabs><el-tab-pane label="文章 · 编辑、审核与发布">
   <div class="filters"><el-select v-model="filters.clinicId" placeholder="全部授权院区" clearable :disabled="busy" @change="filters.categoryId = ''; search()"><el-option v-for="c in clinics" :key="c.id" :label="c.name" :value="c.id"/></el-select><el-select v-model="filters.categoryId" placeholder="全部分类" clearable :disabled="busy" @change="search"><el-option v-for="c in filteredCategories" :key="c.id" :label="c.name + (c.active ? '' : '（停用）')" :value="c.id"/></el-select><el-select v-model="filters.status" placeholder="全部发布状态" clearable :disabled="busy" @change="search"><el-option label="已发布" value="published"/><el-option label="未发布" value="unpublished"/></el-select><el-select v-model="filters.reviewStatus" placeholder="全部审核状态" clearable :disabled="busy" @change="search"><el-option label="待审核" value="pending"/><el-option label="审核通过" value="approved"/><el-option label="退回修改" value="rejected"/></el-select><el-input v-model="filters.q" placeholder="搜索标题、摘要或正文" maxlength="100" clearable :disabled="busy" @keyup.enter="search"/><el-button :disabled="busy" @click="search">搜索</el-button><el-button :disabled="busy" @click="run(refresh)">刷新</el-button></div>
   <el-table :data="rows" v-loading="busy" @row-click="select"><el-table-column prop="title" label="文章标题" min-width="220"/><el-table-column label="分类"><template #default="{row}">{{ categories.find(c => c.id === row.categoryId)?.name }}</template></el-table-column><el-table-column label="审核"><template #default="{row}"><el-tag>{{ labels[row.reviewStatus] }}</el-tag></template></el-table-column><el-table-column label="对外状态"><template #default="{row}">{{ row.publishedAt ? '已发布' : '未发布' }}</template></el-table-column><el-table-column prop="sortOrder" label="排序" width="90"/><el-table-column prop="version" label="版本" width="90"/></el-table>
   <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" :disabled="busy" @current-change="run(list)"/>
   <el-card v-if="editing" class="editor"><h2>{{ current ? '编辑草稿' : '新增文章' }}</h2><el-alert v-if="current?.reviewStatus==='rejected' && current.reviewNote" :title="`驳回意见（${current.reviewerName || '医生'} · ${date(current.reviewedAt)}）：${current.reviewNote}`" type="warning" :closable="false" /><el-form label-position="top" :disabled="busy">
    <el-form-item label="所属分类"><el-select v-model="form.categoryId" :disabled="!!current" placeholder="选择分类"><el-option v-for="c in categories" :key="c.id" :label="c.name" :value="c.id"/></el-select></el-form-item><el-form-item label="标题"><el-input v-model="form.title" maxlength="200" show-word-limit/></el-form-item><el-form-item label="摘要"><el-input v-model="form.summary" type="textarea" :rows="2" maxlength="1000" show-word-limit/></el-form-item><el-form-item label="正文（纯文本）"><el-input v-model="form.body" type="textarea" :rows="12" maxlength="50000" show-word-limit/></el-form-item><el-form-item label="参考来源（审核通过前必填）"><el-input v-model="form.sourceText" type="textarea" :rows="4" maxlength="5000" show-word-limit placeholder="每行一条，填写来源名称、发布机构、年份或版本及链接，供医生核对"/></el-form-item><el-form-item v-if="!current" label="排序（数值越小越靠前）"><el-input-number v-model="form.sortOrder" :min="0" :max="1000000" :precision="0"/></el-form-item><el-button type="primary" :disabled="!validArticle" @click="save">保存草稿并提交审核</el-button><el-button @click="editing = false">取消</el-button>
   </el-form></el-card>
   <el-card v-else-if="current" class="detail"><div class="heading"><h2>{{ current.title }}</h2><el-button v-if="manager" :disabled="busy" @click="edit">编辑草稿</el-button></div><p>{{ current.summary }}</p><p class="body">{{ current.body }}</p><h3>参考来源</h3><p class="body">{{ current.sourceText || '尚未录入，请退回补充后再审核通过。' }}</p><p>审核状态：{{ labels[current.reviewStatus] }} · 审核医生：{{ current.reviewerName || '—' }} · {{ date(current.reviewedAt) }}</p><el-alert v-if="current.reviewStatus==='rejected' && current.reviewNote" :title="`驳回意见（${current.reviewerName || '医生'} · ${date(current.reviewedAt)}）：${current.reviewNote}`" type="warning" :closable="false" /><el-alert v-else-if="current.reviewStatus==='pending' && current.reviewNote" :title="`上次审核意见（${current.reviewerName || '医生'} · ${date(current.reviewedAt)}）：${current.reviewNote}，已重提待审核`" type="info" :closable="false" /><p v-else>审核意见：{{ current.reviewNote || '—' }}</p><p>发布时间：{{ date(current.publishedAt) }} · 当前版本 {{ current.version }}</p>
    <div v-if="!manager" class="review"><p>请核对正文与来源的一致性、适用范围及风险说明。通过后将记录您的医生身份和审核时间。</p><el-input v-model="note" type="textarea" placeholder="审核意见（退回时必填）" maxlength="1000" :disabled="busy"/><el-button type="primary" :disabled="busy || !current.sourceText?.trim()" @click="action('review', 'approved')">审核通过</el-button><el-button type="danger" :disabled="busy || !note.trim()" @click="action('review', 'rejected')">退回修改</el-button></div>
    <div v-else class="publish"><el-button type="primary" :disabled="busy || !current.sourceText?.trim() || current.reviewStatus !== 'approved' || !categories.find(c => c.id === current?.categoryId)?.active" @click="action('publish')">审核后发布</el-button><el-button :disabled="busy || !current.publishedAt" @click="action('unpublish')">下架文章</el-button><el-input-number v-model="order" :min="0" :max="1000000" :precision="0" :disabled="busy"/><el-button :disabled="busy || order === current.sortOrder" @click="action('sort')">保存排序</el-button></div>
    <h3>操作历史（北京时间）</h3><el-table :data="history"><el-table-column prop="version" label="版本"/><el-table-column label="操作"><template #default="{row}">{{ actions[row.action] || row.action }}</template></el-table-column><el-table-column prop="actorId" label="操作人"/><el-table-column label="时间"><template #default="{row}">{{ date(row.createdAt) }}</template></el-table-column><el-table-column type="expand"><template #default="{row}"><pre class="snapshot">{{ row.contentJson }}</pre></template></el-table-column></el-table>
   </el-card>
  </el-tab-pane><el-tab-pane label="分类管理">
   <el-button v-if="manager" type="primary" :disabled="busy || !clinics.length" @click="startCategory()">新增分类</el-button><el-table :data="categories"><el-table-column prop="name" label="分类"/><el-table-column prop="clinicId" label="院区编号"/><el-table-column prop="sortOrder" label="排序"/><el-table-column label="状态"><template #default="{row}">{{ row.active ? '启用' : '停用' }}</template></el-table-column><el-table-column label="操作"><template #default="{row}"><el-button v-if="manager" :disabled="busy" @click="startCategory(row)">编辑分类</el-button><el-button :disabled="busy" @click="showCategoryHistory(row)">历史</el-button></template></el-table-column></el-table>
   <el-card v-if="categoryEditing"><el-form label-position="top" :disabled="busy"><el-form-item label="院区"><el-select v-model="categoryForm.clinicId" :disabled="!!selectedCategory"><el-option v-for="c in clinics" :key="c.id" :label="c.name" :value="c.id"/></el-select></el-form-item><el-form-item label="分类名称"><el-input v-model="categoryForm.name" maxlength="120"/></el-form-item><el-form-item label="排序"><el-input-number v-model="categoryForm.sortOrder" :min="0" :max="1000000" :precision="0"/></el-form-item><el-form-item label="启用分类（停用后所有所属文章暂停对外展示）"><el-switch v-model="categoryForm.active"/></el-form-item><el-button type="primary" :disabled="!categoryForm.name.trim() || !categoryForm.clinicId" @click="saveCategory">保存分类</el-button><el-button @click="categoryEditing = false">取消</el-button></el-form></el-card>
   <div v-if="categoryHistory.length"><h3>分类变更历史</h3><div v-for="event in categoryHistory" :key="event.id"><p>版本 {{ event.version }} · {{ actions[event.action] }} · {{ event.actorId }} · {{ date(event.createdAt) }}</p><pre class="snapshot">{{ event.contentJson }}</pre></div></div>
  </el-tab-pane></el-tabs>
 </section>
</template>

<style scoped>
.heading,.filters,.publish{display:flex;align-items:center;gap:12px;justify-content:space-between}.heading p{color:#708096}.filters{margin:20px 0;flex-wrap:wrap;justify-content:flex-start}.filters .el-select{width:170px}.filters .el-input{width:260px}.el-alert,.el-pagination,.editor,.detail{margin:16px 0}.body,.snapshot{white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.8}.body{padding:20px;background:#f5f8fc;border-radius:12px}.snapshot{padding:12px;max-height:360px;overflow:auto}.publish{justify-content:flex-start;flex-wrap:wrap}.review .el-input{margin-bottom:12px}
</style>
