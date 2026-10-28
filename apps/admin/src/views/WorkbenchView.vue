<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, canEditSchedule, errorMessage } from '../api'
import { auth } from '../auth'
import SchedulePatients from '../components/SchedulePatients.vue'
import { useScheduleStore } from '../stores/schedule'

const router = useRouter()
const scheduleStore = useScheduleStore()
const manager = computed(() => auth.employee?.role === 'manager')
type Notice = Awaited<ReturnType<typeof api.handoffNotifications>>['data'][number]
const notices = ref<Notice[]>([]), notificationPage = ref(1), notificationTotal = ref(0), unreadTotal = ref(0)
const notificationError = ref(''), busy = ref(false), reading = ref('')
const pending = ref<number | null>(null), overdue = ref<number | null>(null), supervisor = ref<number | null>(null)
const submitted = ref<number | null>(null), alerts = ref<number | null>(null)
const errors = ref<string[]>([])
let generation = 0, notificationGeneration = 0, timer: ReturnType<typeof setInterval> | undefined
const levelNames = { 1: '紧急', 2: '重要', 3: '一般' } as const
const cards = computed(() => [
  { title: '交接待确认', count: pending.value, detail: manager.value ? '查看接手人的确认进度' : '确认您已收到的任务', to: '/handoffs?status=pending', level: 3 as const },
  ...(manager.value ? [
    { title: '超时升级', count: overdue.value, detail: '超时未确认，联系接手人跟进', to: '/handoffs?status=overdue', level: 1 as const },
    { title: '主管待办', count: supervisor.value, detail: '超时未跟进、待回复和需处理事项', to: '/handoffs?status=todo', level: 2 as const },
  ] : []),
  { title: '随访待回评', count: submitted.value, detail: '查看患者反馈并提交医护回评', to: '/followups?status=submitted', level: 2 as const },
  { title: '异常待处理', count: alerts.value, detail: '核实异常并记录处理结果', to: '/followups?status=alert', level: 1 as const },
].sort((a, b) => a.level - b.level))
function timestamp(value: string) { return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
async function loadNotifications() {
  const request = ++notificationGeneration
  notificationError.value = ''
  try {
    const result = await api.handoffNotifications(notificationPage.value)
    if (request !== notificationGeneration) return
    notices.value = result.data; notificationTotal.value = result.meta.total; unreadTotal.value = result.meta.unreadTotal
  } catch (cause) { if (request === notificationGeneration) notificationError.value = errorMessage(cause) }
}
async function load() {
  if (busy.value) return
  const request = ++generation
  busy.value = true; errors.value = []
  const results = await Promise.allSettled([api.handoffReceipts('pending'), api.followups('submitted'), api.followups('alert'), loadNotifications()])
  if (request !== generation) return
  pending.value = null; overdue.value = null; supervisor.value = null; submitted.value = null; alerts.value = null
  const [handoffs, reviews, abnormal] = results
  if (handoffs.status === 'fulfilled') { pending.value = handoffs.value.meta.total; overdue.value = handoffs.value.meta.overdueTotal; supervisor.value = handoffs.value.meta.todoTotal }
  else errors.value.push(`交接待办：${errorMessage(handoffs.reason)}`)
  if (reviews.status === 'fulfilled') submitted.value = reviews.value.meta.total
  else errors.value.push(`随访回评：${errorMessage(reviews.reason)}`)
  if (abnormal.status === 'fulfilled') alerts.value = abnormal.value.meta.total
  else errors.value.push(`异常处理：${errorMessage(abnormal.reason)}`)
  busy.value = false
}
async function read(notice: Notice, open = false) {
  if (reading.value) return
  reading.value = notice.id; notificationError.value = ''
  try {
    if (!notice.readAt) { await api.readHandoffNotification(notice.id); window.dispatchEvent(new Event('handoff-notifications-changed')) }
    if (open) await router.push({ path: '/handoffs', query: { status: 'all', receiptId: notice.receiptId } })
    else await loadNotifications()
  } catch (cause) { notificationError.value = errorMessage(cause) }
  finally { reading.value = '' }
}
onMounted(() => {
  void load()
  // Populate the schedule store's clinic so the suspension contact list below loads
  // even when the user has not visited the schedule page in this session.
  void scheduleStore.refresh()
  timer = setInterval(() => { if (document.visibilityState === 'visible' && !reading.value) void load() }, 30000)
})
onBeforeUnmount(() => { ++generation; ++notificationGeneration; if (timer) clearInterval(timer) })
</script>

<template>
  <section class="workbench">
    <div class="page-heading"><div><div class="eyebrow">CARE WORKBENCH</div><h1>待办工作台</h1><p>按事项进入对应工作区，优先处理异常和超时交接。</p></div><el-button :loading="busy" @click="load">刷新待办</el-button></div>
    <el-alert v-for="message in errors" :key="message" :title="message" type="error" :closable="false" />
    <template v-if="canEditSchedule">
      <div class="suspension-banner" role="alert">
        <strong>⚠ 停诊待联系患者优先处理</strong>
        <span>停诊时段的预约患者需逐一联系改期；下方清单可逐条标记联系结果或安排改期。</span>
      </div>
      <SchedulePatients />
    </template>
    <div class="todo-grid" aria-label="分类待办">
      <RouterLink v-for="card in cards" :key="card.title" :to="card.to" class="surface todo-card" :class="[`level-${card.level}`, { active: (card.count || 0) > 0 }]"><span>{{ card.title }}<i class="level-tag">{{ levelNames[card.level] }}</i></span><strong>{{ busy ? '…' : card.count ?? '—' }}<small>项</small></strong><p>{{ card.detail }}</p><b>进入处理 →</b></RouterLink>
    </div>
    <p class="scope-note">仅展示当前账号授权范围内的事项；各分类可能包含同一任务。读取失败显示“—”。</p>
    <div class="surface shortcuts"><h2>常用工作区</h2><div><RouterLink to="/handoffs">人员交接与确认</RouterLink><RouterLink to="/followups">治疗与随访</RouterLink><RouterLink v-if="manager" to="/appointments">预约审批与到诊</RouterLink><RouterLink v-if="manager" to="/employees">员工账号管理</RouterLink></div></div>
    <section class="surface notification-panel" aria-labelledby="notification-title">
      <div class="panel-heading"><div><h2 id="notification-title">交接通知 <el-tag size="small" type="warning">{{ unreadTotal }} 条未读</el-tag></h2><p>任务交给接手人时发送站内通知；超过交接时限未确认时，自动升级提醒授权主管。</p></div><el-button @click="loadNotifications">刷新通知</el-button></div>
      <el-alert v-if="notificationError" :title="notificationError" type="error" :closable="false" />
      <p v-if="!notices.length && !notificationError" class="empty">暂无交接通知</p>
      <article v-for="notice in notices" :key="notice.id" class="notification-row" :class="{ unread: !notice.readAt }">
        <div><div class="notice-title"><el-tag :type="notice.kind === 'escalated' ? 'danger' : 'info'" size="small">{{ notice.kind === 'escalated' ? '超时升级提醒' : '任务交接通知' }}</el-tag><strong>{{ notice.taskTitle }}</strong><span v-if="!notice.readAt">未读</span></div><p>{{ notice.fromName }} → {{ notice.toName }} · {{ timestamp(notice.createdAt) }}</p><p v-if="notice.receiptStatus !== 'pending'">{{ notice.receiptStatus === 'confirmed' ? '交接已确认，可查看记录。' : '任务已转出，可查看历史记录。' }}</p></div>
        <div class="notice-actions"><el-button v-if="!notice.readAt" size="small" :disabled="!!reading" @click="read(notice)">标为已读</el-button><el-button type="primary" plain size="small" :disabled="!!reading" @click="read(notice, true)">查看交接</el-button></div>
      </article>
      <el-pagination v-if="notificationTotal > 20" layout="prev, pager, next" :total="notificationTotal" :page-size="20" v-model:current-page="notificationPage" @current-change="loadNotifications" />
      <p class="scope-note">通知已读仅表示您已查看，交接仍须由接手人确认。页面可见时每 30 秒刷新；每项交接对同一主管只发送一次超时升级提醒。</p>
    </section>
  </section>
</template>

<style scoped>
.todo-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 16px; }
.suspension-banner { display: flex; flex-direction: column; gap: 4px; border: 2px solid #e8a13a; border-left-width: 8px; background: #fff8f5; border-radius: 12px; padding: 14px 18px; margin-bottom: 4px; }
.suspension-banner strong { font-size: 15px; color: #b45309; }
.suspension-banner span { font-size: 13px; color: #6d8094; line-height: 1.6; }
.todo-card { display: flex; flex-direction: column; gap: 12px; padding: 22px; text-decoration: none; color: #334961; border: 1px solid #e3eaf1; transition: border-color .2s; }
.todo-card:hover, .todo-card:focus-visible { border-color: var(--el-color-primary); }
.todo-card strong { font-size: 32px; }.todo-card small { font-size: 12px; margin-left: 8px; font-weight: 400; }
.todo-card p, .scope-note, .notification-row p { font-size: 12px; color: #6d8094; line-height: 1.8; }
.todo-card b { font-size: 12px; color: var(--el-color-primary); }.todo-card .level-tag { font-style: normal; font-size: 11px; margin-left: 8px; padding: 2px 7px; border-radius: 20px; background: #eef1f6; color: #66788b; vertical-align: 1px; }.todo-card.level-1.active { border-color: #efc4b9; background: #fff8f5; }.todo-card.level-1.active strong { color: #b45b43; }.todo-card.level-1.active .level-tag { background: #fdeceb; color: #b45b43; }.todo-card.level-2.active { border-color: #ecd9a8; background: #fffaf0; }.todo-card.level-2.active strong { color: #96690f; }.todo-card.level-2.active .level-tag { background: #fdf3d7; color: #96690f; }.todo-card.level-3.active .level-tag { background: #e8f1fd; color: #1763c6; }
.scope-note { margin: 12px 0 22px; }.shortcuts { padding: 22px; margin-bottom: 22px; }.shortcuts h2 { font-size: 16px; margin-bottom: 16px; }.shortcuts > div { display: flex; flex-wrap: wrap; gap: 20px; }.shortcuts a { color: var(--el-color-primary); font-size: 13px; }
.notification-row { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 16px; padding: 18px 24px; border-top: 1px solid #edf0f4; }.notification-row.unread { background: #f5f9ff; }.notice-title { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; font-size: 13px; }.notice-title > span:last-child { font-size: 11px; color: #b07835; }.notification-row p { margin-top: 8px; }.notice-actions { display: flex; gap: 8px; }.notice-actions .el-button { margin: 0; }.notification-panel > .scope-note { padding: 0 24px; }.notification-panel > .el-pagination { padding: 16px 24px; }.empty { padding: 28px; text-align: center; color: #7d8a99; }
.workbench > .el-alert { margin-bottom: 12px; }
</style>
