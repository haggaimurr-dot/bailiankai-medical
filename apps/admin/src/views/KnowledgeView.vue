<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { api, errorMessage, type KnowledgeArticle, type KnowledgeCategory, type KnowledgeEvent, type KnowledgeQuizQuestion } from '../api'
import { auth } from '../auth'

const manager = computed(() => auth.employee?.role === 'manager')
const categories = ref<KnowledgeCategory[]>([]), clinics = ref<{id: string; name: string}[]>([]), rows = ref<KnowledgeArticle[]>([])
const current = ref<KnowledgeArticle>(), history = ref<KnowledgeEvent[]>([]), categoryHistory = ref<KnowledgeEvent[]>([])
const busy = ref(false), error = ref(''), notice = ref(''), editing = ref(false), categoryEditing = ref(false)
const selectedCategory = ref<KnowledgeCategory>(), page = ref(1), total = ref(0), note = ref(''), order = ref(0)
const filters = reactive({ clinicId: '', categoryId: '', status: '', reviewStatus: '', q: '' })
const form = reactive({ categoryId: '', title: '', summary: '', body: '', sourceText: '', videoUrl: '', sortOrder: 0 })
const quizForm = ref<KnowledgeQuizQuestion[]>([])
const stats = ref<{ total: number; uniquePatients: number; articles: { articleId: string; title: string; opens: number; uniquePatients: number }[] } | null>(null)
const statsDays = ref(30)
async function loadStats() { await run(async () => { stats.value = await api.knowledgeStats({ ...(filters.clinicId ? { clinicId: filters.clinicId } : {}), days: statsDays.value }) }) }
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
function start() { current.value = undefined; history.value = []; Object.assign(form, { categoryId: filters.categoryId, title: '', summary: '', body: '', sourceText: '', videoUrl: '', sortOrder: 0 }); quizForm.value = []; editing.value = true }
function edit() { if (!current.value) return; Object.assign(form, current.value, { sourceText: current.value.sourceText || '', videoUrl: current.value.videoUrl || '' }); quizForm.value = JSON.parse(JSON.stringify(current.value.quiz || [])); editing.value = true }
async function save() { await run(async () => {
 const body = { title: form.title.trim(), summary: form.summary.trim(), body: form.body.trim(), sourceText: form.sourceText.trim(), videoUrl: form.videoUrl.trim(), quiz: quizForm.value }
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
    <el-form-item label="所属分类"><el-select v-model="form.categoryId" :disabled="!!current" placeholder="选择分类"><el-option v-for="c in categories" :key="c.id" :label="c.name" :value="c.id"/></el-select></el-form-item><el-form-item label="标题"><el-input v-model="form.title" maxlength="200" show-word-limit/></el-form-item><el-form-item label="摘要"><el-input v-model="form.summary" type="textarea" :rows="2" maxlength="1000" show-word-limit/></el-form-item><el-form-item label="正文（纯文本）"><el-input v-model="form.body" type="textarea" :rows="12" maxlength="50000" show-word-limit/></el-form-item><el-form-item label="参考来源（审核通过前必填）"><el-input v-model="form.sourceText" type="textarea" :rows="4" maxlength="5000" show-word-limit placeholder="每行一条，填写来源名称、发布机构、年份或版本及链接，供医生核对"/></el-form-item><el-form-item label="短视频科普地址（可选，须为 HTTPS）"><el-input v-model="form.videoUrl" maxlength="2048" placeholder="https://…"/></el-form-item><el-form-item label="看完测一测（自测题，可选；只做科普互动，不作诊断结论）"><div v-for="(q, qi) in quizForm" :key="qi" class="quiz-editor"><el-input v-model="q.question" maxlength="200" show-word-limit :placeholder="`第 ${qi + 1} 题题干`"/><div v-for="(opt, oi) in q.options" :key="oi" class="quiz-opt-row"><el-radio :model-value="q.answerIndex" :label="oi" @change="q.answerIndex = oi">正确</el-radio><el-input v-model="q.options[oi]" maxlength="100" :placeholder="`选项 ${oi + 1}`"/><el-button :disabled="q.options.length <= 2" @click="q.options.splice(oi, 1)">删</el-button></div><div class="quiz-opt-row"><el-button :disabled="q.options.length >= 6" @click="q.options.push('')">加选项</el-button><el-button type="danger" @click="quizForm.splice(qi, 1)">删除本题</el-button></div><el-input v-model="q.explanation" type="textarea" :rows="2" maxlength="500" placeholder="解析（可选，患者交卷后可见）"/></div><el-button :disabled="quizForm.length >= 20" @click="quizForm.push({ question: '', options: ['', ''], answerIndex: 0, explanation: '' })">加一题</el-button></el-form-item><el-form-item v-if="!current" label="排序（数值越小越靠前）"><el-input-number v-model="form.sortOrder" :min="0" :max="1000000" :precision="0"/></el-form-item><el-button type="primary" :disabled="!validArticle" @click="save">保存草稿并提交审核</el-button><el-button @click="editing = false">取消</el-button>
   </el-form></el-card>
   <el-card v-else-if="current" class="detail"><div class="heading"><h2>{{ current.title }}</h2><el-button v-if="manager" :disabled="busy" @click="edit">编辑草稿</el-button></div><p>{{ current.summary }}</p><p class="body">{{ current.body }}</p><h3>参考来源</h3><p class="body">{{ current.sourceText || '尚未录入，请退回补充后再审核通过。' }}</p><h3>短视频科普</h3><p class="body">{{ current.videoUrl || '未设置' }}</p><h3>看完测一测（{{ (current.quiz || []).length }} 题）</h3><div v-if="(current.quiz || []).length"><div v-for="(q, qi) in (current.quiz || [])" :key="qi" class="quiz-view"><p><strong>{{ qi + 1 }}. {{ q.question }}</strong></p><p v-for="(opt, oi) in q.options" :key="oi" :class="{ ok: oi === q.answerIndex }">{{ oi === q.answerIndex ? '✔' : '·' }} {{ opt }}</p><p v-if="q.explanation" class="muted">解析：{{ q.explanation }}</p></div></div><p v-else class="muted">未设置自测题。</p><p>审核状态：{{ labels[current.reviewStatus] }} · 审核医生：{{ current.reviewerName || '—' }} · {{ date(current.reviewedAt) }}</p><el-alert v-if="current.reviewStatus==='rejected' && current.reviewNote" :title="`驳回意见（${current.reviewerName || '医生'} · ${date(current.reviewedAt)}）：${current.reviewNote}`" type="warning" :closable="false" /><el-alert v-else-if="current.reviewStatus==='pending' && current.reviewNote" :title="`上次审核意见（${current.reviewerName || '医生'} · ${date(current.reviewedAt)}）：${current.reviewNote}，已重提待审核`" type="info" :closable="false" /><p v-else>审核意见：{{ current.reviewNote || '—' }}</p><p>发布时间：{{ date(current.publishedAt) }} · 当前版本 {{ current.version }}</p>
    <div v-if="!manager" class="review"><p>请核对正文与来源的一致性、适用范围及风险说明。通过后将记录您的医生身份和审核时间。</p><el-input v-model="note" type="textarea" placeholder="审核意见（退回时必填）" maxlength="1000" :disabled="busy"/><el-button type="primary" :disabled="busy || !current.sourceText?.trim()" @click="action('review', 'approved')">审核通过</el-button><el-button type="danger" :disabled="busy || !note.trim()" @click="action('review', 'rejected')">退回修改</el-button></div>
    <div v-else class="publish"><el-button type="primary" :disabled="busy || !current.sourceText?.trim() || current.reviewStatus !== 'approved' || !categories.find(c => c.id === current?.categoryId)?.active" @click="action('publish')">审核后发布</el-button><el-button :disabled="busy || !current.publishedAt" @click="action('unpublish')">下架文章</el-button><el-input-number v-model="order" :min="0" :max="1000000" :precision="0" :disabled="busy"/><el-button :disabled="busy || order === current.sortOrder" @click="action('sort')">保存排序</el-button></div>
    <h3>操作历史（北京时间）</h3><el-table :data="history"><el-table-column prop="version" label="版本"/><el-table-column label="操作"><template #default="{row}">{{ actions[row.action] || row.action }}</template></el-table-column><el-table-column prop="actorId" label="操作人"/><el-table-column label="时间"><template #default="{row}">{{ date(row.createdAt) }}</template></el-table-column><el-table-column type="expand"><template #default="{row}"><pre class="snapshot">{{ row.contentJson }}</pre></template></el-table-column></el-table>
   </el-card>
  </el-tab-pane><el-tab-pane label="阅读统计">
   <div class="filters"><el-select v-model="filters.clinicId" placeholder="全部授权院区" clearable :disabled="busy"><el-option v-for="c in clinics" :key="c.id" :label="c.name" :value="c.id"/></el-select><el-select v-model="statsDays" :disabled="busy"><el-option :label="'近 7 天'" :value="7"/><el-option :label="'近 30 天'" :value="30"/><el-option :label="'近 90 天'" :value="90"/></el-select><el-button :disabled="busy" @click="loadStats">刷新统计</el-button></div>
   <el-alert title="只统计已发布快照的打开；草稿与未发布文章不计入。去重患者数仅计登录打开。" type="info" :closable="false" />
   <div v-if="stats">
    <p>总打开 {{ stats.total }} 次 · 去重患者 {{ stats.uniquePatients }} 人</p>
    <el-table :data="stats.articles"><el-table-column prop="title" label="文章" min-width="240"/><el-table-column prop="opens" label="打开次数" width="110"/><el-table-column prop="uniquePatients" label="去重患者" width="110"/><el-table-column label="操作" width="120"><template #default="{row}"><el-button :disabled="busy" @click="select({ id: row.articleId } as KnowledgeArticle)">查看文章</el-button></template></el-table-column></el-table>
   </div>
   <el-empty v-else description="点「刷新统计」查看阅读数据" />
  </el-tab-pane><el-tab-pane label="分类管理">
   <el-button v-if="manager" type="primary" :disabled="busy || !clinics.length" @click="startCategory()">新增分类</el-button><el-table :data="categories"><el-table-column prop="name" label="分类"/><el-table-column prop="clinicId" label="院区编号"/><el-table-column prop="sortOrder" label="排序"/><el-table-column label="状态"><template #default="{row}">{{ row.active ? '启用' : '停用' }}</template></el-table-column><el-table-column label="操作"><template #default="{row}"><el-button v-if="manager" :disabled="busy" @click="startCategory(row)">编辑分类</el-button><el-button :disabled="busy" @click="showCategoryHistory(row)">历史</el-button></template></el-table-column></el-table>
   <el-card v-if="categoryEditing"><el-form label-position="top" :disabled="busy"><el-form-item label="院区"><el-select v-model="categoryForm.clinicId" :disabled="!!selectedCategory"><el-option v-for="c in clinics" :key="c.id" :label="c.name" :value="c.id"/></el-select></el-form-item><el-form-item label="分类名称"><el-input v-model="categoryForm.name" maxlength="120"/></el-form-item><el-form-item label="排序"><el-input-number v-model="categoryForm.sortOrder" :min="0" :max="1000000" :precision="0"/></el-form-item><el-form-item label="启用分类（停用后所有所属文章暂停对外展示）"><el-switch v-model="categoryForm.active"/></el-form-item><el-button type="primary" :disabled="!categoryForm.name.trim() || !categoryForm.clinicId" @click="saveCategory">保存分类</el-button><el-button @click="categoryEditing = false">取消</el-button></el-form></el-card>
   <div v-if="categoryHistory.length"><h3>分类变更历史</h3><div v-for="event in categoryHistory" :key="event.id"><p>版本 {{ event.version }} · {{ actions[event.action] }} · {{ event.actorId }} · {{ date(event.createdAt) }}</p><pre class="snapshot">{{ event.contentJson }}</pre></div></div>
  </el-tab-pane></el-tabs>
 </section>
</template>

<style scoped>
.heading,.filters,.publish{display:flex;align-items:center;gap:12px;justify-content:space-between}.heading p{color:#708096}.filters{margin:20px 0;flex-wrap:wrap;justify-content:flex-start}.filters .el-select{width:170px}.filters .el-input{width:260px}.el-alert,.el-pagination,.editor,.detail{margin:16px 0}.body,.snapshot{white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.8}.body{padding:20px;background:#f5f8fc;border-radius:12px}.snapshot{padding:12px;max-height:360px;overflow:auto}.publish{justify-content:flex-start;flex-wrap:wrap}.review .el-input{margin-bottom:12px}.quiz-editor{border:1px solid #dcdfe6;border-radius:8px;padding:12px;margin-bottom:12px}.quiz-editor .el-input{margin-bottom:8px}.quiz-opt-row{display:flex;align-items:center;gap:8px;margin-bottom:8px}.quiz-view{border-top:1px solid #ebeef5;padding:8px 0}.quiz-view .ok{color:#1a9e54;font-weight:600}
</style>
