<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { api, errorMessage } from '../api'
import { auth } from '../auth'
import { readPendingSupport, writePendingSupport, type PendingSupport } from '../supportPending'
import type { components } from '../../../../packages/contracts/src/schema'
type Ticket = components['schemas']['SupportTicket']
type Action = components['schemas']['SupportAction']
const labels: Record<string, string> = { open: '待分派', in_progress: '处理中', waiting_patient: '待客户回复', resolved: '已解决', closed: '已关闭' }
const categoryLabels: Record<string,string> = { general: '一般咨询', booking: '预约咨询', aftercare: '术后服务', complaint: '意见反馈' }
const rows = ref<Ticket[]>([]), selected = ref<Ticket | null>(null), events = ref<components['schemas']['SupportEvent'][]>([]), agents = ref<components['schemas']['SupportAgent'][]>([])
const uploads = ref<components['schemas']['SupportUpload'][]>([]), uploading = ref(false), syncState = ref('正在连接'), newMessages = ref(0)
let polling = false
const status = ref(''), assignment = ref(''), overdue = ref(false), page = ref(1), total = ref(0), eventPage = ref(1), eventTotal = ref(0)
const loading = ref(false), saving = ref(false), error = ref(''), content = ref(''), action = ref<Action['action']>('message'), assignee = ref(''), nextStatus = ref<Ticket['status']>('in_progress'), dueAt = ref('')
let generation = 0, detailGeneration = 0, timer: ReturnType<typeof setInterval> | undefined
let pending: PendingSupport | undefined
const restoring = ref(true)
const pendingStorage = `support-pending:${auth.employee?.id}`
const hasPending = ref(false)
async function remember(value: typeof pending) { await writePendingSupport(pendingStorage,value); pending = value; hasPending.value = !!value }
const editable = computed(() => selected.value && selected.value.status !== 'closed' && (auth.employee?.role === 'manager' || selected.value.assigneeId === auth.employee?.id))
const eligibleAgents = computed(() => agents.value.filter(a => selected.value && a.clinicIds.includes(selected.value.clinicId)))
const transitionOptions = computed(() => ({ open: ['in_progress','closed'], in_progress: ['waiting_patient','resolved','closed'], waiting_patient: ['in_progress','resolved','closed'], resolved: ['in_progress','closed'], closed: [] }[selected.value?.status || 'closed']))
function date(v: string | null) { return v ? new Date(v).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }) : '未设置' }
function isOverdue(t: Ticket) { return !!t.dueAt && Date.parse(t.dueAt) < Date.now() && !['resolved','closed'].includes(t.status) }
function key() { return `support_${Date.now()}_${Math.random().toString(36).slice(2)}` }
function selectRow(row: Ticket) { if (!saving.value && !uploading.value && !restoring.value) void open(row) }
const stats = ref<components['schemas']['SupportStats'] | null>(null)
async function load(silent = false) {
  const g = ++generation; if (!silent) loading.value = true
  if (!silent) api.supportStats().then(value => { if (g === generation) stats.value = value }).catch(() => {})
  try { const result = await api.supportTickets(status.value, assignment.value, overdue.value, page.value); if (g === generation) { rows.value = result.data; total.value = result.meta.total }; return true }
  catch (e) { if (g === generation) error.value = errorMessage(e); return false }
  finally { if (g === generation) loading.value = false }
}
async function open(t: Ticket, reset = true) {
  const g = ++detailGeneration; if (reset) { eventPage.value = 1; content.value = ''; action.value = 'message'; uploads.value = []; newMessages.value = 0; error.value = ''; selected.value = null; events.value = [] }
  try { const d = await api.supportDetail(t.id, eventPage.value); if (g === detailGeneration) { selected.value = d.data.ticket; events.value = d.data.events; eventTotal.value = d.meta.total } }
  catch (e) { if (g === detailGeneration) error.value = errorMessage(e) }
}
async function save(claim = false, automatic = false) {
  if (!selected.value || saving.value || uploading.value || restoring.value) return
  if (pending) { await retryPending(); return }
  const ticket = selected.value
  let body: Action
  if (automatic) body = { action: 'auto_assign', content: '按院区及未完成工单量自动分派', version: ticket.version, requestKey: key() }
  else if (claim) body = { action: 'assign', assigneeId: auth.employee!.id, dueAt: new Date(Date.now()+86400000).toISOString(), content: '主动接手，计划24小时内跟进', version: ticket.version, requestKey: key() }
  else {
    if (!content.value.trim() && !(['message','note'].includes(action.value) && uploads.value.length)) { error.value = '请填写回复、跟进记录或操作原因'; return }
    if (['assign','followup'].includes(action.value) && !dueAt.value) { error.value = '请设置跟进期限（北京时间）'; return }
    body = { action: action.value, content: content.value.trim(), version: ticket.version, requestKey: key(), ...(['message','note'].includes(action.value) && uploads.value.length ? { attachments: uploads.value } : {}), ...(action.value === 'assign' ? { assigneeId: assignee.value } : {}), ...(action.value === 'status' ? { status: nextStatus.value } : {}), ...(['assign','followup'].includes(action.value) ? { dueAt: new Date(`${dueAt.value}:00+08:00`).toISOString() } : {}) }
  }
  saving.value = true; detailGeneration++; error.value = ''
  try { await remember({ id: ticket.id, body }); await api.supportAction(ticket.id, body); await remember(undefined); content.value = ''; uploads.value = []; newMessages.value = 0; eventPage.value = 1; await open(ticket, false); await load() }
  catch (e) { error.value = errorMessage(e); /* Retry the exact request until the result is known. */ }
  finally { saving.value = false }
}
async function retryPending() { if (!pending || saving.value) return; saving.value = true; detailGeneration++; error.value = ''; try { const row = await api.supportAction(pending.id,pending.body); await remember(undefined); content.value = ''; uploads.value = []; newMessages.value = 0; eventPage.value = 1; await open(row,false); await load() } catch(e) { error.value = errorMessage(e) } finally { saving.value = false } }
async function discardPending() { try { await remember(undefined); if (selected.value) await open(selected.value,false) } catch(e) { error.value = errorMessage(e) } }
async function chooseFiles(e: Event) {
  const input = e.target as HTMLInputElement, files = Array.from(input.files || [])
  input.value = ''
  if (uploads.value.length + files.length > 3) { error.value = '每条消息最多3个附件'; return }
  if (files.some(file => file.size > 2 * 1024 * 1024 || file.size === 0 || !/\.(jpe?g|png|pdf)$/i.test(file.name))) { error.value = '附件支持 JPG、PNG、PDF，每个不超过2MB'; return }
  uploading.value = true
  try {
    const added = await Promise.all(files.map(file => new Promise<components['schemas']['SupportUpload']>((resolve,reject) => {
      const reader = new FileReader(); reader.onload = () => resolve({ name: file.name, data: String(reader.result).split(',')[1]! }); reader.onerror = () => reject(new Error('附件读取失败')); reader.readAsDataURL(file)
    })))
    uploads.value.push(...added)
  } catch(e) { error.value = errorMessage(e) } finally { uploading.value = false }
}
async function download(file: components['schemas']['SupportAttachment']) {
  if (!selected.value) return
  try { const blob = await api.supportAttachment(selected.value.id,file.id), url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = file.name; link.click(); setTimeout(() => URL.revokeObjectURL(url),1000) } catch(e) { error.value = errorMessage(e) }
}
async function sync() {
  if (polling || saving.value || uploading.value || document.hidden || restoring.value) return
  polling = true
  const ticket = selected.value, g = detailGeneration
  try {
    if (!await load(true)) { syncState.value = '连接中断，正在重试'; return }
    if (ticket) {
      const d = await api.supportDetail(ticket.id,1)
      if (g === detailGeneration && selected.value?.id === ticket.id && !saving.value) {
        if (d.data.ticket.version !== ticket.version) {
          newMessages.value += Math.max(0,d.meta.total - eventTotal.value)
          selected.value = d.data.ticket
          if (eventPage.value === 1) { events.value = d.data.events; eventTotal.value = d.meta.total }
        }
      }
    }
    syncState.value = '消息自动刷新中（每3秒）'
  } catch(e) { syncState.value = '连接中断，正在重试'; error.value = errorMessage(e) } finally { polling = false }
}
function latest() { if (selected.value) { eventPage.value = 1; newMessages.value = 0; void open(selected.value,false) } }
onMounted(async () => { try { pending = await readPendingSupport(pendingStorage); hasPending.value = !!pending } catch(e) { error.value = errorMessage(e) } finally { restoring.value = false }; await load(); try { agents.value = await api.supportAgents() } catch(e) { error.value = errorMessage(e) }; syncState.value = '消息自动刷新中（每3秒）'; timer = setInterval(() => { void sync() },3000) })
onUnmounted(() => { generation++; detailGeneration++; if (timer) clearInterval(timer) })
</script>

<template>
  <div class="support-workspace">
    <div class="page-heading"><div><h1>客服工作台</h1><p>在线咨询 · 自动分派 · 附件与服务评价</p><span class="support-meta" role="status">{{ syncState }}</span></div><el-button :loading="loading" @click="load()">刷新队列</el-button></div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <div v-if="hasPending" class="support-filters"><span>上次提交结果待确认，请先重试或刷新记录核对。</span><el-button @click="retryPending" :disabled="saving">重试上次提交</el-button><el-button @click="discardPending" :disabled="saving">核对后重新编辑</el-button></div>
    <p class="support-meta">新咨询自动分派给本院区未完成工单最少的在岗客服；暂无合适人员时进入待分派队列。</p>
    <div class="support-filters">
      <el-select v-model="status" placeholder="全部状态" clearable @change="page=1;load()"><el-option v-for="(label,value) in labels" :key="value" :label="label" :value="value" /></el-select>
      <el-select v-model="assignment" placeholder="全部可见工单" clearable @change="page=1;load()"><el-option label="我的工单" value="mine" /><el-option label="待分派" value="unassigned" /></el-select>
      <el-checkbox v-model="overdue" @change="page=1;load()">仅逾期未解决</el-checkbox><span>共 {{ total }} 条</span>
    </div>
    <div v-if="stats" class="support-stats"><span>工单 <strong>{{ stats.totalTickets }}</strong> 条</span><span>已首响 <strong>{{ stats.respondedTickets }}</strong> 条</span><span>平均首响 <strong>{{ stats.avgFirstResponseMinutes == null ? '—' : stats.avgFirstResponseMinutes.toFixed(0) }}</strong> 分钟</span><span>已评分 <strong>{{ stats.ratedTickets }}</strong> 条</span><span>平均评分 <strong>{{ stats.avgRating == null ? '—' : stats.avgRating.toFixed(1) }}</strong> / 5 星</span><small>首响只计患者可见的客服消息，内部记录不计入；评分只计已评分工单。全部授权院区。</small></div>
    <div class="support-columns">
      <section class="support-panel queue" aria-label="咨询工单队列">
        <el-table :data="rows" v-loading="loading" :row-class-name="({ row }: { row: Ticket }) => isOverdue(row) ? 'overdue-row' : ''" @row-click="selectRow" highlight-current-row empty-text="暂无符合条件的咨询工单">
          <el-table-column label="咨询工单" min-width="190"><template #default="{row}"><strong>{{ row.subject }}</strong><div class="support-meta">{{ categoryLabels[row.category] }} · {{ row.id }}</div><div class="support-meta">提交于 {{ date(row.createdAt) }}</div></template></el-table-column>
          <el-table-column label="状态" width="110"><template #default="{row}"><el-tag :type="isOverdue(row) ? 'danger' : 'info'">{{ labels[row.status] }}</el-tag><div v-if="isOverdue(row)" class="overdue">跟进已逾期</div></template></el-table-column>
        </el-table>
        <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="prev, pager, next" @current-change="load()" />
      </section>
      <section class="support-panel detail" aria-label="咨询详情与服务记录">
        <template v-if="selected">
          <h2>{{ selected.subject }}</h2><div class="support-meta">{{ labels[selected.status] }} · 院区 {{ selected.clinicId }} · 客户 {{ selected.patientId }}</div>
          <p>负责人：{{ agents.find(a=>a.id===selected?.assigneeId)?.displayName || selected.assigneeId || '待分派' }}<br />跟进期限：{{ date(selected.dueAt) }}</p>
          <el-button v-if="!selected.assigneeId && selected.status !== 'closed'" type="primary" :loading="saving" @click="save(true)">接手工单（24小时内跟进）</el-button>
          <el-button v-if="auth.employee?.role === 'manager' && !selected.assigneeId && selected.status === 'open'" :loading="saving" @click="save(false,true)">自动分派</el-button>
          <p v-if="selected.rating">客户评分：{{ selected.rating }} / 5 星<span v-if="selected.ratingComment"> · {{ selected.ratingComment }}</span></p><p v-else class="support-meta">{{ ['resolved','closed'].includes(selected.status) ? '等待客户评价' : '结单后客户可评分' }}</p>
          <h3>在线咨询与服务记录</h3><el-button v-if="newMessages" @click="latest">{{ newMessages }} 条新服务记录 · 查看最新</el-button>
          <div class="support-history"><article v-for="event in events" :key="event.id" :class="{ internal: event.internal }"><div class="support-meta">{{ event.actorType === 'patient' ? '客户' : event.actorType === 'system' ? '系统' : '客服' }} · {{ event.internal ? '内部服务记录' : '咨询消息' }} · {{ date(event.createdAt) }}</div><p>{{ event.content }}</p><el-button v-for="file in event.attachments || []" :key="file.id" link type="primary" @click="download(file)">下载附件：{{ file.name }}（{{ Math.ceil(file.size / 1024) }} KB）</el-button></article></div>
          <el-pagination v-model:current-page="eventPage" :page-size="20" :total="eventTotal" layout="prev, pager, next" @current-change="open(selected,false)" />
          <el-form v-if="editable" label-position="top" class="support-compose">
            <el-form-item label="服务操作"><el-select v-model="action" :disabled="saving"><el-option label="回复客户" value="message" /><el-option label="内部服务记录" value="note" /><el-option label="记录跟进并设置期限" value="followup" /><el-option label="更新工单状态" value="status" /><el-option v-if="auth.employee?.role === 'manager'" label="分派 / 改派负责人" value="assign" /></el-select></el-form-item>
            <el-form-item v-if="action==='assign'" label="接手负责人"><el-select v-model="assignee"><el-option v-for="agent in eligibleAgents" :key="agent.id" :label="agent.displayName" :value="agent.id" /></el-select></el-form-item>
            <el-form-item v-if="action==='status'" label="目标状态"><el-select v-model="nextStatus"><el-option v-for="s in transitionOptions" :key="s" :label="labels[s]" :value="s" /></el-select></el-form-item>
            <el-form-item v-if="action==='assign'||action==='followup'" label="跟进期限（北京时间）"><input v-model="dueAt" type="datetime-local" aria-label="跟进期限" /></el-form-item>
            <el-form-item :label="action==='message' ? '发送给客户' : '内部记录 / 操作原因（客户不可见）'"><el-input v-model="content" type="textarea" :rows="4" maxlength="2000" show-word-limit :disabled="saving" /></el-form-item>
            <el-form-item v-if="action==='message'||action==='note'" label="附件（JPG / PNG / PDF，最多3个，每个2MB）"><input type="file" aria-label="添加附件" accept=".jpg,.jpeg,.png,.pdf" multiple :disabled="saving || uploading || hasPending" @change="chooseFiles" /><div v-for="(file,index) in uploads" :key="index">{{ file.name }}<el-button link :disabled="saving || hasPending" @click="uploads.splice(index,1)">移除</el-button></div></el-form-item>
            <el-button type="primary" :disabled="uploading" :loading="saving" @click="save()">提交</el-button><el-button @click="retryPending" :disabled="saving">重试上次提交</el-button><el-button @click="discardPending" :disabled="saving">刷新核对后重新编辑</el-button>
          </el-form><p v-else class="support-meta">{{ selected.status==='closed' ? '工单已关闭，服务记录保留。' : '接手后可回复客户与记录跟进。' }}</p>
        </template><el-empty v-else description="选择工单查看咨询及服务记录" />
      </section>
    </div>
  </div>
</template>

<style scoped>
.page-heading,.support-filters{display:flex;align-items:center;gap:16px;justify-content:space-between;margin-bottom:22px}.page-heading h1{margin:0}.page-heading p,.support-meta{color:#7c8796;font-size:13px}.support-filters{justify-content:flex-start;flex-wrap:wrap;margin-top:20px}.support-filters .el-select{width:180px}.support-columns{display:grid;grid-template-columns:minmax(340px,0.9fr) minmax(380px,1.1fr);gap:22px}
.support-stats{display:flex;flex-wrap:wrap;gap:18px;align-items:center;margin:20px 0;padding:12px 16px;border:1px solid #e8edf3;border-radius:12px;background:#f7fafc;font-size:13px;color:#4a5568}.support-stats strong{color:#1a202c}.support-stats small{color:#7c8796}.support-panel{background:white;border:1px solid #e8edf3;border-radius:16px;padding:24px;min-width:0}.support-history{max-height:460px;overflow:auto}.support-history article{background:#f2f7fc;border-radius:12px;padding:14px;margin:12px 0}.support-history article.internal{background:#fff8e8}.support-history p{white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.7;margin-bottom:0}.support-compose{margin-top:20px}.overdue{color:#c94d4d;margin-top:6px;font-size:12px}:deep(.overdue-row td){background:#fff5f5 !important}:deep(.overdue-row td:first-child){box-shadow:inset 3px 0 0 #e02424}input{padding:10px;border:1px solid #dce4ed;border-radius:6px}.el-pagination{margin-top:18px}@media(max-width:1050px){.support-columns{grid-template-columns:1fr}}
</style>
