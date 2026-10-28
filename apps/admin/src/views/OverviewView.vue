<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { api, errorMessage } from '../api'
import { shanghaiDate } from '../dates'
import AppIcon from '../components/AppIcon.vue'

type AnalyticsReport = Awaited<ReturnType<typeof api.analytics>>
type Filters = Parameters<typeof api.analytics>[0]
const today = shanghaiDate()
const filters = reactive({ dateFrom: `${today.slice(0, 7)}-01`, dateTo: today, clinicId: '', doctorId: '', groupBy: 'doctor' as Filters['groupBy'] })
const report = ref<AnalyticsReport | null>(null)
const clinics = ref<AnalyticsReport['clinics']>([]), doctors = ref<AnalyticsReport['doctors']>([])
const lastQuery = ref<Filters | null>(null), resultQuery = ref<Filters | null>(null)
const loading = ref(false), error = ref(''), optionsLoaded = ref(false)
let requestId = 0
const groupLabels = { doctor: '医生', clinic: '院区', day: '每日' }
const eligibleDoctors = computed(() => doctors.value.filter(doctor => !filters.clinicId || doctor.clinicId === filters.clinicId))
const emptyAuthorization = computed(() => optionsLoaded.value && clinics.value.length === 0)
const validRange = computed(() => /^\d{4}-\d{2}-\d{2}$/.test(filters.dateFrom) && /^\d{4}-\d{2}-\d{2}$/.test(filters.dateTo) && filters.dateFrom <= filters.dateTo)
const draftChanged = computed(() => !!resultQuery.value && JSON.stringify(query()) !== JSON.stringify(resultQuery.value))
const cards = computed(() => {
  const metrics = report.value?.summary
  if (!metrics) return []
  return [
    { key: 'appointments', title: '全部预约', value: metrics.appointmentCount, unit: '次', detail: `待确认 ${metrics.pendingAppointmentCount} 次`, icon: 'calendar' as const, color: 'blue' },
    { key: 'confirmed', title: '预约确认', value: metrics.confirmedAppointmentCount, unit: '次', detail: '曾被人工确认，含之后取消', icon: 'check' as const, color: 'blue' },
    { key: 'arrived', title: '实际到诊', value: metrics.arrivedAppointmentCount, unit: '次', detail: `到诊率 ${rate(metrics.arrivalRate)}`, icon: 'location' as const, color: 'teal' },
    { key: 'treatment', title: '实际治疗', value: metrics.treatmentCount, unit: '次', detail: '按实际治疗记录统计', icon: 'doctors' as const, color: 'purple' },
    { key: 'followups', title: '随访完成', value: metrics.followupCompletedCount, unit: '项', detail: `共 ${metrics.followupCount} 项 · 完成率 ${rate(metrics.followupCompletionRate)}`, icon: 'layers' as const, color: 'teal' },
    { key: 'alerts', title: '异常处理', value: metrics.alertHandledCount, unit: '项', detail: `共 ${metrics.alertCount} 项 · 处理率 ${rate(metrics.alertHandlingRate)}`, icon: 'check' as const, color: 'purple' },
  ]
})
const hasData = computed(() => {
  const metrics = report.value?.summary
  return !!metrics && (metrics.appointmentCount > 0 || metrics.treatmentCount > 0 || metrics.followupCount > 0)
})
type MetricKey = 'appointmentCount' | 'confirmedAppointmentCount' | 'arrivedAppointmentCount' | 'treatmentCount' | 'followupCompletedCount' | 'alertHandledCount'
const comparisonRows = computed(() => {
  const summary = report.value?.summary
  if (!summary) return []
  const defs: { key: MetricKey, label: string, unit: string }[] = [
    { key: 'appointmentCount', label: '全部预约', unit: '次' },
    { key: 'confirmedAppointmentCount', label: '预约确认', unit: '次' },
    { key: 'arrivedAppointmentCount', label: '实际到诊', unit: '次' },
    { key: 'treatmentCount', label: '实际治疗', unit: '次' },
    { key: 'followupCompletedCount', label: '随访完成', unit: '项' },
    { key: 'alertHandledCount', label: '异常处理', unit: '项' },
  ]
  return defs.map(def => ({
    ...def,
    current: summary[def.key],
    previous: report.value?.previous?.summary[def.key] ?? null,
    yearAgo: report.value?.yearAgo?.summary[def.key] ?? null,
  }))
})
function deltaLabel(current: number, base: number | null): string {
  if (base === null) return '—'
  if (base === 0) return current === 0 ? '±0' : `+${current}`
  const diff = current - base
  const pct = (diff / base) * 100
  return `${diff > 0 ? '+' : ''}${diff}（${pct > 0 ? '+' : ''}${pct.toFixed(1)}%）`
}
function csvCell(value: string | number | null | undefined): string {
  const text = value === null || value === undefined ? '' : String(value)
  return `"${text.replace(/"/g, '""')}"`
}
function exportCsv() {
  const data = report.value
  if (!data || !resultQuery.value) return
  const lines: (string | number)[][] = []
  lines.push([`运营分析导出 · ${scopeLabel(resultQuery.value)}`])
  lines.push([`生成时间 ${generatedAt(data.generatedAt)} · 北京时间`])
  lines.push([])
  lines.push(['对比', '本期', `环比上期（${data.previous ? `${data.previous.dateFrom} 至 ${data.previous.dateTo}` : '—'}）`, '环比变化', `同比去年（${data.yearAgo ? `${data.yearAgo.dateFrom} 至 ${data.yearAgo.dateTo}` : '—'}）`, '同比变化'])
  for (const row of comparisonRows.value) lines.push([row.label, row.current, row.previous ?? '—', deltaLabel(row.current, row.previous), row.yearAgo ?? '—', deltaLabel(row.current, row.yearAgo)])
  lines.push([])
  const groupHeader = data.groupBy === 'doctor' ? ['医生', '院区'] : data.groupBy === 'clinic' ? ['院区'] : ['日期']
  lines.push([...groupHeader, '全部预约', '待确认', '预约确认', '实际到诊', '到诊率', '实际治疗', '随访任务', '随访完成', '完成率', '逾期未提交', '异常任务', '异常处理', '处理率', '异常待处理'])
  for (const row of data.rows) {
    const group = data.groupBy === 'doctor' ? [row.doctorName || '未归属医生', row.clinicName || '未归属院区'] : data.groupBy === 'clinic' ? [row.clinicName || '未归属院区'] : [row.date]
    const m = row.metrics
    lines.push([...group, m.appointmentCount, m.pendingAppointmentCount, m.confirmedAppointmentCount, m.arrivedAppointmentCount, rate(m.arrivalRate), m.treatmentCount, m.followupCount, m.followupCompletedCount, rate(m.followupCompletionRate), m.followupOverdueCount, m.alertCount, m.alertHandledCount, rate(m.alertHandlingRate), m.pendingAlertCount])
  }
  const csv = '\uFEFF' + lines.map(line => line.map(csvCell).join(',')).join('\r\n')
  const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `运营分析_${data.dateFrom}_${data.dateTo}.csv`
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}
function query(): Filters {
  return { dateFrom: filters.dateFrom, dateTo: filters.dateTo, clinicId: filters.clinicId || undefined, doctorId: filters.doctorId || undefined, groupBy: filters.groupBy }
}
function clinicChanged() {
  if (filters.doctorId && !eligibleDoctors.value.some(doctor => doctor.id === filters.doctorId)) filters.doctorId = ''
}
function rate(value: number | null): string { return value === null ? '—' : `${value.toFixed(1)}%` }
function scopeLabel(values: Filters): string {
  const clinic = values.clinicId ? clinics.value.find(item => item.id === values.clinicId)?.name || values.clinicId : '全部授权院区'
  const doctor = values.doctorId ? doctors.value.find(item => item.id === values.doctorId)?.name || values.doctorId : '全部授权医生'
  return `${values.dateFrom} 至 ${values.dateTo} · ${clinic} · ${doctor} · 按${groupLabels[values.groupBy]}统计`
}
function generatedAt(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value))
}
async function load(values: Filters = query()) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(values.dateFrom) || !/^\d{4}-\d{2}-\d{2}$/.test(values.dateTo) || values.dateFrom > values.dateTo) return
  const currentRequest = ++requestId
  const snapshot = { ...values }
  lastQuery.value = snapshot
  loading.value = true; error.value = ''; report.value = null; resultQuery.value = null
  try {
    const result = await api.analytics(snapshot)
    if (currentRequest !== requestId) return
    clinics.value = result.clinics; doctors.value = result.doctors; optionsLoaded.value = true
    if (filters.clinicId && !clinics.value.some(clinic => clinic.id === filters.clinicId)) filters.clinicId = ''
    if (filters.doctorId && !eligibleDoctors.value.some(doctor => doctor.id === filters.doctorId)) filters.doctorId = ''
    report.value = result
    resultQuery.value = { ...snapshot, dateFrom: result.dateFrom, dateTo: result.dateTo, groupBy: result.groupBy }
  } catch (cause) {
    if (currentRequest === requestId) error.value = errorMessage(cause)
  } finally {
    if (currentRequest === requestId) loading.value = false
  }
}
onMounted(() => void load())
onBeforeUnmount(() => { ++requestId })
</script>

<template>
  <section class="analytics-page" aria-labelledby="analytics-title">
    <div class="page-heading">
      <div><div class="eyebrow">OPERATIONS ANALYTICS</div><h1 id="analytics-title">医生统计与运营分析</h1><p>按医生、院区和时间查看服务进展，统一确认、到诊、治疗及随访口径。</p></div>
      <span class="analytics-badge"><AppIcon name="location" />仅统计授权院区</span>
    </div>

    <div class="surface analytics-filters">
      <div class="panel-heading"><div><h2>统计范围</h2><p>日期均为北京时间，开始与结束日期均包含在内。</p></div><span class="count-chip">口径 v1</span></div>
      <form class="filter-bar analytics-filter-bar" @submit.prevent="load()">
        <div class="filter-field analytics-date"><label for="analytics-date-from">开始日期</label><input id="analytics-date-from" v-model="filters.dateFrom" type="date" required :max="filters.dateTo || undefined" /></div>
        <div class="filter-field analytics-date"><label for="analytics-date-to">结束日期</label><input id="analytics-date-to" v-model="filters.dateTo" type="date" required :min="filters.dateFrom || undefined" /></div>
        <div class="filter-field"><label id="analytics-clinic-label">院区</label><el-select v-model="filters.clinicId" aria-labelledby="analytics-clinic-label" placeholder="全部授权院区" clearable :disabled="emptyAuthorization" @change="clinicChanged"><el-option v-for="clinic in clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></div>
        <div class="filter-field"><label id="analytics-doctor-label">医生</label><el-select v-model="filters.doctorId" aria-labelledby="analytics-doctor-label" placeholder="全部授权医生" clearable :disabled="emptyAuthorization"><el-option v-for="doctor in eligibleDoctors" :key="doctor.id" :label="doctor.name" :value="doctor.id" /></el-select></div>
        <div class="filter-field analytics-group"><label id="analytics-group-label">统计维度</label><el-select v-model="filters.groupBy" aria-labelledby="analytics-group-label"><el-option label="按医生" value="doctor" /><el-option label="按院区" value="clinic" /><el-option label="按每日" value="day" /></el-select></div>
        <el-button native-type="submit" type="primary" :disabled="!validRange || emptyAuthorization"><AppIcon name="refresh" />查询统计</el-button>
      </form>
      <p v-if="!validRange" class="range-error" role="alert">请选择有效日期范围，结束日期不能早于开始日期。</p>
    </div>

    <div v-if="loading" class="surface analytics-state" role="status" aria-live="polite"><div class="loading-mark"><AppIcon name="refresh" /></div><h2>正在查询统计</h2><p v-if="lastQuery" class="query-snapshot">{{ scopeLabel(lastQuery) }}</p></div>
    <div v-else-if="error" class="surface analytics-state" role="alert"><span class="state-symbol">!</span><h2>统计暂时无法加载</h2><p>{{ error }}</p><p v-if="lastQuery" class="query-snapshot">本次查询：{{ scopeLabel(lastQuery) }}</p><el-button type="primary" @click="load(lastQuery || query())">重试本次查询</el-button></div>
    <div v-else-if="report && emptyAuthorization" class="surface analytics-state"><AppIcon name="location" /><h2>暂无授权院区</h2><p>获得院区授权后，即可查看该院区的医生统计与运营分析。</p></div>
    <template v-else-if="report && resultQuery">
      <div class="analytics-result-meta">
        <div><p class="query-snapshot" data-testid="analytics-result-scope"><strong>当前结果：</strong>{{ scopeLabel(resultQuery) }}</p><p v-if="draftChanged" class="draft-note" role="status">筛选条件已修改，点击“查询统计”后更新结果。</p></div>
        <div class="analytics-result-actions"><span>截至 {{ generatedAt(report.generatedAt) }} · 北京时间</span><el-button type="primary" plain size="small" @click="exportCsv">导出 CSV</el-button></div>
      </div>
      <div class="stat-grid analytics-summary" aria-label="统计汇总">
        <article v-for="card in cards" :key="card.key" class="stat-card" :data-testid="`analytics-card-${card.key}`"><div class="stat-copy"><span>{{ card.title }}</span><strong>{{ card.value }}<small>{{ card.unit }}</small></strong><p>{{ card.detail }}</p></div><span class="stat-icon" :class="card.color"><AppIcon :name="card.icon" /></span></article>
      </div>
      <div class="surface analytics-compare">
        <div class="panel-heading"><div><h2>同比 / 环比</h2><p>环比为本期之前等长周期，同比为去年同期；两个对比窗口与本期使用同一院区、医生与统计口径，不会串期。</p></div></div>
        <div class="analytics-table-wrap" tabindex="0" aria-label="同比环比对照，可横向滚动">
          <table class="analytics-table analytics-compare-table">
            <thead><tr><th scope="col">指标</th><th scope="col">本期（{{ report.dateFrom }} 至 {{ report.dateTo }}）</th><th scope="col">环比上期<span v-if="report.previous">（{{ report.previous.dateFrom }} 至 {{ report.previous.dateTo }}）</span></th><th scope="col">环比变化</th><th scope="col">同比去年<span v-if="report.yearAgo">（{{ report.yearAgo.dateFrom }} 至 {{ report.yearAgo.dateTo }}）</span></th><th scope="col">同比变化</th></tr></thead>
            <tbody><tr v-for="row in comparisonRows" :key="row.key"><th scope="row">{{ row.label }}</th><td>{{ row.current }} {{ row.unit }}</td><td>{{ row.previous === null ? '—' : `${row.previous} ${row.unit}` }}</td><td :class="{ 'compare-up': row.previous !== null && row.current > row.previous, 'compare-down': row.previous !== null && row.current < row.previous }">{{ deltaLabel(row.current, row.previous) }}</td><td>{{ row.yearAgo === null ? '—' : `${row.yearAgo} ${row.unit}` }}</td><td :class="{ 'compare-up': row.yearAgo !== null && row.current > row.yearAgo, 'compare-down': row.yearAgo !== null && row.current < row.yearAgo }">{{ deltaLabel(row.current, row.yearAgo) }}</td></tr></tbody>
          </table>
        </div>
      </div>
      <div class="analytics-queue" aria-label="待处理事项"><span>随访逾期未提交 <strong>{{ report.summary.followupOverdueCount }}</strong></span><span>待确认预约 <strong>{{ report.summary.pendingAppointmentCount }}</strong></span><span>异常待处理 <strong>{{ report.summary.pendingAlertCount }}</strong></span></div>

      <div class="surface analytics-details">
        <div class="panel-heading"><div><h2>按{{ groupLabels[report.groupBy] }}统计 <span class="count-chip">{{ report.rows.length }} 条</span></h2><p>汇总计数与比例，查看各医生、院区或日期的完成与待处理情况。</p></div><span class="table-scroll-hint">可横向滚动查看全部指标</span></div>
        <div v-if="!hasData || report.rows.length === 0" class="analytics-state analytics-empty"><AppIcon name="layers" /><h2>该范围暂无统计数据</h2><p>可调整日期、院区或医生后重新查询。</p></div>
        <div v-else class="analytics-table-wrap" tabindex="0" aria-label="运营统计明细，可横向滚动">
          <table class="analytics-table">
            <thead><tr><th v-if="report.groupBy === 'doctor'" scope="col">医生</th><th v-if="report.groupBy !== 'day'" scope="col">院区</th><th v-if="report.groupBy === 'day'" scope="col">日期</th><th scope="col">全部预约</th><th scope="col">待确认</th><th scope="col">预约确认</th><th scope="col">实际到诊</th><th scope="col">到诊率</th><th scope="col">实际治疗</th><th scope="col">随访任务</th><th scope="col">随访完成</th><th scope="col">完成率</th><th scope="col">逾期未提交</th><th scope="col">异常任务</th><th scope="col">异常处理</th><th scope="col">处理率</th><th scope="col">异常待处理</th></tr></thead>
            <tbody><tr v-for="row in report.rows" :key="row.key"><th v-if="report.groupBy === 'doctor'" scope="row">{{ row.doctorName || '未归属医生' }}</th><td v-if="report.groupBy !== 'day'">{{ row.clinicName || '未归属院区' }}</td><th v-if="report.groupBy === 'day'" scope="row">{{ row.date }}</th><td>{{ row.metrics.appointmentCount }}</td><td>{{ row.metrics.pendingAppointmentCount }}</td><td>{{ row.metrics.confirmedAppointmentCount }}</td><td class="arrived-cell">{{ row.metrics.arrivedAppointmentCount }}</td><td>{{ rate(row.metrics.arrivalRate) }}</td><td>{{ row.metrics.treatmentCount }}</td><td>{{ row.metrics.followupCount }}</td><td>{{ row.metrics.followupCompletedCount }}</td><td>{{ rate(row.metrics.followupCompletionRate) }}</td><td :class="{ 'attention-cell': row.metrics.followupOverdueCount > 0 }">{{ row.metrics.followupOverdueCount }}</td><td>{{ row.metrics.alertCount }}</td><td>{{ row.metrics.alertHandledCount }}</td><td>{{ rate(row.metrics.alertHandlingRate) }}</td><td :class="{ 'attention-cell': row.metrics.pendingAlertCount > 0 }">{{ row.metrics.pendingAlertCount }}</td></tr></tbody>
          </table>
        </div>
        <div class="table-footer"><span>比率以同范围合计计算；分母为 0 时显示“—”，表示不适用。</span><span>仅汇总，不展示患者信息</span></div>
      </div>
    </template>

    <div class="surface analytics-definitions">
      <div class="panel-heading"><div><h2>指标口径</h2><p>先看预约是否确认，再看是否实际到诊。各指标独立统计，不相互推定。</p></div><span class="count-chip">北京时间 UTC+8</span></div>
      <dl>
        <div><dt>预约、确认与到诊</dt><dd>全部预约、待确认、预约确认、实际到诊和实际治疗，按预约的计划就诊日期归入统计范围。预约确认指曾被人工确认的预约，后续取消仍保留确认计数，每个预约只计一次。实际到诊只认独立到诊登记；预约确认或历史治疗记录均不推定到诊。</dd></div>
        <div><dt>实际治疗</dt><dd>有实际治疗记录的预约计入实际治疗，每个预约只计一次。治疗记录与到诊登记分别统计，历史治疗也不会补记实际到诊。</dd></div>
        <div><dt>随访完成与逾期</dt><dd>随访按计划随访日期独立归入统计范围，跨月随访归入对应月份。医护已回评（reviewed）或已处理（handled）才算完成，患者提交资料不算完成。逾期未提交指计划随访时间已过、仍未提交反馈（pending）的任务；已提交待回评或异常待处理不计入此数。</dd></div>
        <div><dt>异常处理</dt><dd>异常任务为异常待处理（alert）或异常已处理（handled）的随访任务；已处理计入异常处理，alert 计入异常待处理。</dd></div>
        <div><dt>比率与归属</dt><dd>到诊率 = 实际登记到诊 ÷ 曾确认预约；随访完成率 = 已回评或处理 ÷ 随访任务；异常处理率 = 异常已处理 ÷ 异常任务。分母为 0 时显示“—”。医生与院区归属沿用预约的接诊医生和院区，随访负责人改派不会改变医生指标。</dd></div>
        <div><dt>查询时点</dt><dd>全部指标按截至查询时的记录与队列状态统计。日期范围决定预约和随访任务的归属，不代表动作发生在该日期内；之后的确认、登记、回评或处理会更新同一范围的结果。</dd></div>
      </dl>
    </div>
  </section>
</template>

<style scoped>
.analytics-page { color: var(--el-text-color-primary); }
.analytics-badge { display: inline-flex; align-items: center; gap: 6px; border: 1px solid #dce9f7; background: #eff6ff; border-radius: 7px; color: #527397; font-size: 11px; padding: 6px 10px; white-space: nowrap; }
.analytics-badge svg { width: 14px; height: 14px; }
.analytics-filters { margin-bottom: 20px; }
.analytics-filter-bar { flex-wrap: wrap; gap: 12px; }
.analytics-filter-bar .filter-field { flex: 1 1 145px; width: auto; min-width: 145px; }
.analytics-filter-bar .analytics-group { flex: 0 1 140px; }
.analytics-filter-bar .analytics-date { flex: 1 1 150px; }
.analytics-date input { height: 37px; width: 100%; border: 1px solid var(--el-border-color); background: #fff; border-radius: var(--el-border-radius-base); color: var(--el-text-color-regular); font-size: 12px; padding: 7px 10px; color-scheme: light; }
.analytics-date input:focus { outline: 2px solid var(--el-color-primary-light-5); outline-offset: 1px; }
.analytics-filter-bar .el-button { min-height: 37px; font-size: 12px; }
.range-error { padding: 0 23px 16px; color: #b25c43; font-size: 12px; }
.analytics-result-meta { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin: 22px 2px 16px; }
.analytics-result-actions { display: flex; align-items: center; gap: 12px; color: #8a99ab; font-size: 10px; white-space: nowrap; padding-top: 3px; }
.analytics-compare { margin-bottom: 22px; }
.analytics-compare-table { min-width: 900px; }
.analytics-compare-table .compare-up { color: #36897d; font-weight: 600; }
.analytics-compare-table .compare-down { color: #b25c43; font-weight: 600; }
.query-snapshot { color: #526981; font-size: 12px; line-height: 1.9; }
.query-snapshot strong { font-weight: 600; color: #334961; }
.analytics-result-meta > span { color: #8a99ab; font-size: 10px; white-space: nowrap; padding-top: 3px; }
.draft-note { font-size: 11px; color: #9a753b; margin-top: 4px; }
.analytics-summary { grid-template-columns: repeat(3, minmax(0, 1fr)); margin-bottom: 16px; }
.analytics-summary .stat-copy p { white-space: normal; }
.analytics-queue { display: flex; flex-wrap: wrap; gap: 12px 30px; padding: 13px 20px; margin-bottom: 22px; border: 1px solid #e4ebf3; border-radius: 9px; background: #f7fafd; color: #71829a; font-size: 11px; }
.analytics-queue strong { margin-left: 10px; font-weight: 600; color: #496787; font-variant-numeric: tabular-nums; }
.analytics-details { margin-bottom: 22px; }
.table-scroll-hint { font-size: 10px; color: #8a99ab; white-space: nowrap; }
.analytics-table-wrap { overflow-x: auto; }
.analytics-table { width: 100%; min-width: 1420px; border-collapse: collapse; font-size: 12px; font-variant-numeric: tabular-nums; }
.analytics-table th, .analytics-table td { padding: 15px 17px; text-align: right; border-bottom: 1px solid #edf1f6; white-space: nowrap; }
.analytics-table thead th { color: #7a8ca2; background: #f7f9fc; font-size: 11px; font-weight: 500; border-top: 1px solid #edf1f6; }
.analytics-table tbody th { text-align: left; color: #334961; font-weight: 550; }
.analytics-table th:first-child, .analytics-table td:first-child { text-align: left; padding-left: 23px; }
.analytics-table tbody tr:hover { background: #f9fbfe; }
.analytics-table .arrived-cell { color: #36897d; font-weight: 600; }
.analytics-table .attention-cell { color: #a47931; }
.analytics-table-wrap:focus-visible { outline: 2px solid var(--el-color-primary-light-5); outline-offset: -2px; }
.analytics-state { min-height: 235px; padding: 30px 24px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; text-align: center; margin-bottom: 22px; }
.analytics-state h2 { font-size: 15px; font-weight: 550; }
.analytics-state > p { font-size: 12px; color: #7a8a9e; }
.analytics-state > svg { width: 29px; height: 29px; color: #91a5bd; margin-bottom: 4px; }
.loading-mark { color: #5c92d7; animation: rotate 1.8s linear infinite; }
.loading-mark svg { width: 25px; height: 25px; }
.analytics-empty { border-top: 1px solid #edf1f6; margin-bottom: 0; min-height: 200px; }
.analytics-definitions { margin-top: 22px; }
.analytics-definitions dl { margin: 0; padding: 0 23px 23px; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px 28px; }
.analytics-definitions dt { font-size: 12px; color: #4b6582; font-weight: 600; margin-bottom: 5px; }
.analytics-definitions dd { margin: 0; font-size: 11px; color: #7a8a9e; line-height: 1.9; }
@keyframes rotate { to { transform: rotate(360deg); } }
@media (max-width: 1100px) { .analytics-result-meta { flex-direction: column; gap: 4px; } }
@media (max-width: 720px) { .analytics-badge { display: none; } .analytics-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); } .analytics-filter-bar .filter-field, .analytics-filter-bar .analytics-date, .analytics-filter-bar .analytics-group { min-width: calc(50% - 6px); flex: 1 1 calc(50% - 6px); } .analytics-filter-bar .el-button { flex: 1; } .analytics-definitions dl { grid-template-columns: 1fr; padding: 0 17px 20px; gap: 16px; } .analytics-queue { gap: 9px 20px; padding: 12px 17px; } .table-scroll-hint { display: none; } .analytics-details .table-footer { flex-direction: column; gap: 5px; } }
</style>
