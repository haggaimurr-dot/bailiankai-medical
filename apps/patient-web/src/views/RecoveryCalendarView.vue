<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { errorMessage } from '../api'
import { isSessionExpired, patientApi, type FollowupTask } from '../patient'
import { useSessionStore } from '../stores/session'
import {
  beijingDate,
  calendarDays,
  calendarEvents,
  recoverySeries,
  shiftMonth,
  type CalendarEvent,
} from '../utils/recovery-calendar'

const router = useRouter()
const session = useSessionStore()

const loading = ref(true)
const expired = ref(false)
const error = ref('')
const month = ref(beijingDate().slice(0, 7))
const selectedDate = ref(beijingDate())
const rangeEvents = ref<CalendarEvent[]>([])
const allTasks = ref<FollowupTask[]>([])
const stagePlans = ref<any[]>([])
const pendingScope = ref<'range' | 'all'>('range')
const seriesId = ref('')
const baselineId = ref('')
const compareId = ref('')

const days = computed(() => calendarDays(month.value, selectedDate.value, rangeEvents.value))
const gridStart = computed(() => days.value[0]?.date || '')
const gridEnd = computed(() => days.value[41]?.date || '')
const monthLabel = computed(() => {
  const [y, m] = month.value.split('-')
  return `${y}年${Number(m)}月`
})
const inRangeEvents = computed(() => rangeEvents.value.filter((e) => e.date >= gridStart.value && e.date <= gridEnd.value))
const summary = computed(() => ({
  tasks: inRangeEvents.value.filter((e) => e.kind === 'task').length,
  pending: inRangeEvents.value.filter((e) => e.pending).length,
  reviews: inRangeEvents.value.filter((e) => e.kind === 'review').length,
}))
const dayEvents = computed(() => rangeEvents.value.filter((e) => e.date === selectedDate.value))
const allEvents = computed(() => calendarEvents(allTasks.value))
const pendingInRange = computed(() => inRangeEvents.value.filter((e) => e.pending))
const pendingAll = computed(() => allEvents.value.filter((e) => e.pending))
const pendingList = computed(() => (pendingScope.value === 'range' ? pendingInRange.value : pendingAll.value))
const series = computed(() => recoverySeries(allTasks.value))
const activeSeries = computed(() => series.value.find((s) => s.appointmentId === seriesId.value) || null)
const baseline = computed(() => activeSeries.value?.points.find((p) => p.id === baselineId.value) || null)
const compare = computed(() => activeSeries.value?.points.find((p) => p.id === compareId.value) || null)
const trendText = computed(() => {
  const s = activeSeries.value
  if (!s) return ''
  if (s.points.length < 2) return '至少需要两次已提交反馈才能对照。'
  if (!baseline.value || !compare.value) return '请选择基准反馈与对照反馈。'
  if (baseline.value.id === compare.value.id || baseline.value.date >= compare.value.date) return '请选择早于对照反馈的基准反馈。'
  if (baseline.value.severityLabel === '未记录' || compare.value.severityLabel === '未记录') return '不适程度未完整记录。'
  if (compare.value.severity < baseline.value.severity) return '自述不适程度降低，恢复趋势向好。'
  if (compare.value.severity > baseline.value.severity) return '自述不适程度升高，请留意恢复情况，必要时联系医护。'
  return '自述不适程度持平。'
})

async function fetchAll(dates?: { dateFrom: string; dateTo: string }): Promise<FollowupTask[]> {
  const collected: FollowupTask[] = []
  let page = 1
  for (;;) {
    const envelope = await patientApi.followups('', page, session.patientId, dates)
    if (!envelope.meta) throw new Error('随访分页信息缺失，请重新加载。')
    const items = envelope.data || []
    if (!items.length && collected.length < envelope.meta.total) throw new Error('随访数据读取不完整，请重新加载。')
    collected.push(...items)
    if (collected.length >= envelope.meta.total) return collected
    page += 1
  }
}

async function loadRange() {
  const grid = calendarDays(month.value, selectedDate.value, [])
  const dates = { dateFrom: grid[0].date, dateTo: grid[41].date }
  rangeEvents.value = calendarEvents(await fetchAll(dates))
}

async function load() {
  loading.value = true
  expired.value = false
  error.value = ''
  try {
    await patientApi.me()
    const [, all] = await Promise.all([loadRange(), fetchAll()])
    allTasks.value = all
    if (!seriesId.value && series.value.length) {
      seriesId.value = series.value[0].appointmentId
      baselineId.value = series.value[0].points[0]?.id || ''
      compareId.value = series.value[0].points[series.value[0].points.length - 1]?.id || ''
    }
  } catch (cause) {
    if (isSessionExpired(cause)) expired.value = true
    else error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

async function relogin() {
  session.expire()
  try {
    await session.ensureSession()
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  }
}

async function moveMonth(offset: number) {
  month.value = shiftMonth(month.value, offset)
  try {
    await loadRange()
  } catch (cause) {
    error.value = errorMessage(cause)
  }
}

function backToToday() {
  const today = beijingDate()
  selectedDate.value = today
  if (month.value !== today.slice(0, 7)) {
    month.value = today.slice(0, 7)
    void moveMonth(0)
  }
}

function pickDay(date: string) {
  selectedDate.value = date
  if (date.slice(0, 7) !== month.value) {
    month.value = date.slice(0, 7)
    void moveMonth(0)
  }
}

function pickSeries(id: string) {
  seriesId.value = id
  const s = series.value.find((item) => item.appointmentId === id)
  baselineId.value = s?.points[0]?.id || ''
  compareId.value = s?.points[s.points.length - 1]?.id || ''
}

function openEvent(event: CalendarEvent) {
  router.push({ path: '/followups', query: { appointmentId: event.appointmentId, taskId: event.taskId } })
}

async function loadStagePlans() {
  try { stagePlans.value = await patientApi.recoveryStagePlans(session.patient?.id) || [] }
  catch { /* 无计划时静默 */ }
}
onMounted(() => { load(); loadStagePlans() })
</script>

<template>
  <main class="page">
    <p class="chip">开发演示 · 仅显示当前患者账号的随访记录</p>
    <p class="eyebrow">MY RECOVERY</p>
    <h1>恢复日历</h1>
    <p class="lede">随访任务按计划日期展示，反馈与回评按实际发生日期展示，时间均为北京时间。</p>

    <div v-if="loading" class="card"><van-loading size="24px">正在整理恢复日历…</van-loading></div>

    <div v-else-if="expired" class="card">
      <h2>登录已失效</h2>
      <p class="muted">演示身份已过期。创建新身份后，旧记录仍属于旧身份，不会自动转入新身份。</p>
      <button class="primary" @click="relogin">重新登录</button>
    </div>

    <div v-else-if="error" class="card">
      <h2>日历暂时无法加载</h2>
      <div class="error-box">{{ error }}</div>
      <button class="ghost" style="margin-top: 10px" @click="load">重新加载</button>
    </div>

    <template v-else>
      <p class="muted">当前日历范围：{{ gridStart }} 至 {{ gridEnd }}</p>
      <button class="link-btn" @click="router.push('/recovery/trend')">查看恢复趋势图 →</button>
      <section v-if="stagePlans.length" class="card stage-plans">
        <h3>我的恢复阶段计划</h3>
        <div v-for="plan in stagePlans" :key="plan.id" class="stage-plan">
          <p class="stage-title">开始 {{ plan.startDate }} · 已推送 {{ plan.milestones.filter((m: any) => m.notified).length }}/6</p>
          <div v-for="m in plan.milestones" :key="m.id" class="stage-row">
            <span :class="['stage-dot', m.notified ? 'done' : 'todo']"></span>
            <span>{{ m.stageLabel }}</span>
            <span class="stage-status">{{ m.notified ? '小结已推送' : '待到达' }}</span>
          </div>
        </div>
      </section>
      <div class="summary-strip">
        <span><strong>{{ summary.tasks }}</strong> 范围内随访任务</span>
        <span><strong>{{ summary.pending }}</strong> 范围内待填写</span>
        <span><strong>{{ summary.reviews }}</strong> 范围内医护回评</span>
      </div>

      <section class="card">
        <div class="row month-nav">
          <button class="ghost nav-btn" aria-label="上个月" @click="moveMonth(-1)">‹</button>
          <strong class="grow center">{{ monthLabel }}</strong>
          <button class="ghost nav-btn" aria-label="下个月" @click="moveMonth(1)">›</button>
        </div>
        <div class="row month-tools">
          <span class="muted grow">北京时间</span>
          <button class="link-btn" @click="backToToday">回到今天</button>
        </div>
        <div class="weekday-row"><span v-for="w in ['一', '二', '三', '四', '五', '六', '日']" :key="w">{{ w }}</span></div>
        <div class="grid">
          <button
            v-for="day in days"
            :key="day.date"
            class="day-cell"
            :class="{ dim: !day.inMonth, today: day.today, selected: day.selected }"
            @click="pickDay(day.date)"
          >
            <span>{{ day.day }}</span>
            <span class="dots">
              <i v-if="day.hasTask" class="dot task" />
              <i v-if="day.hasPending" class="dot pending" />
              <i v-if="day.hasDone" class="dot done" />
              <i v-if="day.hasReview" class="dot review" />
            </span>
          </button>
        </div>
        <p class="muted legend">
          <i class="dot task" /> 随访任务 <i class="dot pending" /> 待填写 <i class="dot done" /> 已完成 <i class="dot review" /> 医护回评
        </p>
      </section>

      <section class="card">
        <h2>{{ selectedDate }} · {{ dayEvents.length }} 项记录</h2>
        <van-empty v-if="!dayEvents.length" description="当天暂无随访记录" />
        <p v-if="!dayEvents.length" class="muted">有随访任务、反馈或回评的日期会在日历上标记圆点。</p>
        <article v-for="event in dayEvents" :key="event.id" class="event-row">
          <div class="row">
            <span class="chip" :class="event.kind === 'review' ? 'success' : event.pending ? 'warning' : ''">{{ event.kindLabel }}</span>
            <span class="muted">{{ event.time }}</span>
            <span class="muted grow right">{{ event.statusLabel }}</span>
          </div>
          <p class="event-title">{{ event.title }}</p>
          <p class="muted">随访负责人：{{ event.assigneeName || '待分配' }}</p>
          <p v-if="event.content" class="body-text">{{ event.content }}</p>
          <p v-if="event.disposition" class="muted">异常处理：{{ event.disposition }}</p>
          <p v-if="event.overdue" class="overdue">已超过计划时间，仍可提交反馈。</p>
          <button class="ghost" @click="openEvent(event)">{{ event.pending ? '填写反馈' : '查看随访详情' }}</button>
        </article>
      </section>

      <section class="card">
        <h2>待填写反馈</h2>
        <div class="filter-row">
          <button class="chip filter-chip" :class="{ active: pendingScope === 'range' }" @click="pendingScope = 'range'">当前日历范围</button>
          <button class="chip filter-chip" :class="{ active: pendingScope === 'all' }" @click="pendingScope = 'all'">全部日期（{{ pendingAll.length }}）</button>
        </div>
        <van-empty v-if="!pendingList.length" :description="pendingScope === 'range' ? '当前日历范围内没有待填写反馈' : '暂无待填写反馈'" />
        <article v-for="event in pendingList" :key="event.id" class="event-row">
          <div class="row">
            <strong class="grow">{{ event.title }}</strong>
            <span v-if="event.date.slice(0, 7) !== month" class="chip warning">跨月待办</span>
          </div>
          <p class="muted">{{ event.date }} {{ event.time }} · 随访负责人：{{ event.assigneeName || '待分配' }}</p>
          <p v-if="event.overdue" class="overdue">已超过计划时间，仍可提交反馈。</p>
          <button class="ghost" @click="openEvent(event)">填写反馈</button>
        </article>
      </section>

      <section class="card">
        <h2>恢复趋势对照</h2>
        <template v-if="series.length && activeSeries">
          <label class="field-label" for="series-picker">反馈序列</label>
          <select id="series-picker" class="field" :value="seriesId" @change="pickSeries(($event.target as HTMLSelectElement).value)">
            <option v-for="s in series" :key="s.appointmentId" :value="s.appointmentId">{{ s.label }}</option>
          </select>
          <div class="compare-grid">
            <div>
              <label class="field-label" for="baseline-picker">基准反馈</label>
              <select id="baseline-picker" v-model="baselineId" class="field">
                <option v-for="p in activeSeries.points" :key="p.id" :value="p.id">{{ p.date.slice(0, 10) }} · {{ p.title }}</option>
              </select>
              <template v-if="baseline">
                <p class="body-text">不适程度：{{ baseline.severityLabel }}</p>
                <p v-if="baseline.symptoms" class="muted">{{ baseline.symptoms }}</p>
                <p v-if="baseline.concern" class="chip warning">希望医护重点关注</p>
                <p v-if="baseline.response" class="muted">医护回评：{{ baseline.response }}</p>
                <p v-if="baseline.disposition" class="muted">异常处理：{{ baseline.disposition }}</p>
              </template>
            </div>
            <div>
              <label class="field-label" for="compare-picker">对照反馈</label>
              <select id="compare-picker" v-model="compareId" class="field">
                <option v-for="p in activeSeries.points" :key="p.id" :value="p.id">{{ p.date.slice(0, 10) }} · {{ p.title }}</option>
              </select>
              <template v-if="compare">
                <p class="body-text">不适程度：{{ compare.severityLabel }}</p>
                <p v-if="compare.symptoms" class="muted">{{ compare.symptoms }}</p>
                <p v-if="compare.concern" class="chip warning">希望医护重点关注</p>
                <p v-if="compare.response" class="muted">医护回评：{{ compare.response }}</p>
                <p v-if="compare.disposition" class="muted">异常处理：{{ compare.disposition }}</p>
              </template>
            </div>
          </div>
          <p class="trend">{{ trendText }}</p>
          <p class="muted">对照仅比较患者自述的不适程度，不代替医护判断。</p>
        </template>
        <p v-else class="muted">提交随访反馈后，可在这里对照恢复记录。</p>
      </section>
    </template>
  </main>
</template>

<style scoped>
.eyebrow { color: var(--muted); font-size: 12px; letter-spacing: 2px; margin: 0; }
.summary-strip { display: flex; gap: 14px; flex-wrap: wrap; font-size: 13px; color: var(--muted); }
.summary-strip strong { color: var(--ink); font-size: 16px; }
.month-nav .center { text-align: center; }
.nav-btn { padding: 6px 14px; font-size: 18px; }
.month-tools { margin: 8px 0 4px; }
.link-btn { border: 0; background: none; color: var(--brand); font-size: 13px; cursor: pointer; padding: 0; }
.weekday-row { display: grid; grid-template-columns: repeat(7, 1fr); text-align: center; font-size: 12px; color: var(--muted); padding: 6px 0; }
.grid { display: grid; grid-template-columns: repeat(7, 1fr); gap: 2px; }
.day-cell { border: 0; background: none; border-radius: 10px; padding: 6px 0 4px; display: flex; flex-direction: column; align-items: center; gap: 3px; font-size: 14px; color: var(--ink); cursor: pointer; min-height: 44px; }
.day-cell.dim { color: var(--muted); opacity: 0.55; }
.day-cell.today { box-shadow: inset 0 0 0 1px var(--brand); }
.day-cell.selected { background: var(--brand); color: #fff; }
.dots { display: flex; gap: 3px; height: 6px; }
.dot { width: 6px; height: 6px; border-radius: 50%; display: inline-block; background: var(--brand); }
.dot.pending { background: #d97706; }
.dot.done { background: var(--green); }
.dot.review { background: var(--green); }
.legend .dot { vertical-align: middle; margin: 0 2px 0 8px; }
.event-row { border-top: 1px solid var(--line); padding: 10px 0; }
.event-row:first-of-type { border-top: 0; }
.event-title { font-size: 15px; font-weight: 600; margin: 6px 0 2px; }
.body-text { font-size: 14px; line-height: 1.7; white-space: pre-line; }
.overdue { color: var(--amber); font-size: 13px; }
.right { text-align: right; }
.filter-row { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 6px; }
.filter-chip { background: #fff; cursor: pointer; font-size: 12px; padding: 5px 12px; }
.filter-chip.active { color: var(--brand); border-color: var(--brand); background: #eff6ff; font-weight: 600; }
.compare-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.trend { font-size: 15px; font-weight: 600; margin: 12px 0 4px; }
</style>
