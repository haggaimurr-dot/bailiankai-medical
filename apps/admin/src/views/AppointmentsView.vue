<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, ApiError, canManageAppointments, errorMessage, type Appointment, type AppointmentReview } from '../api'
import { auth } from '../auth'
import AppIcon from '../components/AppIcon.vue'
import { shanghaiDate, slotTime } from '../dates'
import { useAppointmentsStore } from '../stores/appointments'
import { useScheduleStore } from '../stores/schedule'

const store = useAppointmentsStore()
const schedule = useScheduleStore()
const statuses: { value: Appointment['status'] | ''; label: string }[] = [
  { value: '', label: '全部预约' },
  { value: 'waiting', label: '候补中' },
  { value: 'pending', label: '待确认' },
  { value: 'confirmed', label: '已确认' },
  { value: 'completed', label: '已治疗' },
  { value: 'rejected', label: '已拒绝' },
  { value: 'cancelled', label: '已取消' },
  { value: 'expired', label: '已过期' },
]
const statusLabels: Record<Appointment['status'], string> = {
  waiting: '候补中', pending: '待确认', confirmed: '已确认', completed: '已治疗', rejected: '已拒绝', cancelled: '已取消', expired: '已过期',
}
const detailOpen = ref(false)
const detail = ref<Appointment | null>(null)
const detailId = ref('')
const detailLoading = ref(false)
const detailError = ref('')
const reviewError = ref('')
const conflictNotice = ref('')
const rejecting = ref(false)
const reason = ref('')
const reasonTouched = ref(false)
const saving = ref(false)
const savingAction = ref<AppointmentReview['action'] | null>(null)
const arrivalTime = ref('')
const arrivalError = ref('')
const arrivalSaving = ref(false)
const arrivalRetryPending = ref(false)
const now = ref(Date.now())
const tableRef = ref()
const selected = ref<Appointment[]>([])
const reminding = ref(false)
const remindResult = ref('')
let detailRequestVersion = 0
let refreshTimer: ReturnType<typeof setInterval> | undefined

const lastRefresh = computed(() => store.refreshedAt ? slotTime(store.refreshedAt.toISOString()) : '尚未同步')
const visibleClinics = computed(() => schedule.clinics.filter((clinic) => !auth.employee || auth.employee.clinicIds.includes(clinic.id)))
const filteredDoctors = computed(() => schedule.doctors.filter((doctor) => !store.clinicId || doctor.clinicId === store.clinicId))
const scopeLabel = computed(() => `${store.clinicId ? visibleClinics.value.find((clinic) => clinic.id === store.clinicId)?.name || '全部院区' : '全部院区'} · ${store.date || '全部日期'} · ${schedule.doctors.find((doctor) => doctor.id === store.doctorId)?.name || '全部医生'}`)
const reasonError = computed(() => !reason.value.trim() ? '请填写拒绝原因，便于患者了解处理结果。' : reason.value.trim().length > 200 ? '拒绝原因不能超过 200 字。' : '')
const reviewAllowed = computed(() => Boolean(detail.value && detail.value.status === 'pending' && detail.value.canReview && new Date(detail.value.confirmationDeadline).getTime() > now.value))
const arrivalAllowed = computed(() => Boolean(detail.value && !detail.value.arrivedAt && detail.value.canRegisterArrival && ['confirmed', 'completed'].includes(detail.value.status) && ['manager', 'customer_service', 'doctor', 'clinician'].includes(auth.employee?.role || '')))

function currentShanghaiTime() {
  const value = new Date()
  const time = new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).format(value)
  return `${shanghaiDate(value)} ${time}`
}

function timestamp(value: string) {
  return `${shanghaiDate(new Date(value))} ${slotTime(value)}`
}

function shortId(id: string) { return id.slice(-8).toUpperCase() }

async function selectStatus(value: Appointment['status'] | '') {
  store.status = value
  await store.applyFilters()
}

async function changeDate(value: string | null) {
  store.date = value || ''
  await store.applyFilters()
}

async function changeClinic(value: string | null) {
  store.clinicId = value || ''
  if (store.doctorId && !filteredDoctors.value.some((doctor) => doctor.id === store.doctorId)) store.doctorId = ''
  await store.applyFilters()
}

async function resetFilters() {
  store.status = ''
  store.date = ''
  store.doctorId = ''
  store.clinicId = ''
  await store.applyFilters()
}

function onSelectionChange(rows: Appointment[]) {
  selected.value = rows
}

async function remindSelected() {
  if (selected.value.length === 0 || reminding.value) return
  remindResult.value = ''
  try {
    await ElMessageBox.confirm(`将向 ${selected.value.length} 位患者发送就诊提醒（站内消息），确定发送？`, '批量提醒', { confirmButtonText: '发送提醒', cancelButtonText: '取消' })
  } catch {
    return
  }
  reminding.value = true
  try {
    const results = await api.remindAppointments(selected.value.map((row) => row.id))
    const sent = results.filter((r) => r.sent).length
    const failed = results.filter((r) => !r.sent)
    if (failed.length === 0) {
      ElMessage.success(`已向 ${sent} 位患者发送就诊提醒`)
    } else {
      remindResult.value = failed.map((r) => {
        const row = store.appointments.find((item) => item.id === r.appointmentId)
        return row ? `${row.doctorName} ${shanghaiDate(new Date(row.startsAt))} ${slotTime(row.startsAt)}：${r.reason}` : `${r.appointmentId.slice(-8).toUpperCase()}：${r.reason}`
      }).join('；')
      ElMessage.warning(`已提醒 ${sent} 条，${failed.length} 条未提醒，见下方提示`)
    }
    tableRef.value?.clearSelection()
    selected.value = []
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    reminding.value = false
  }
}

async function loadDetail() {
  const version = ++detailRequestVersion
  detailLoading.value = true
  detailError.value = ''
  detail.value = null
  try {
    const result = await api.appointment(detailId.value)
    if (version !== detailRequestVersion || !detailOpen.value) return
    detail.value = result
    now.value = Date.now()
  } catch (cause) {
    if (version !== detailRequestVersion) return
    detailError.value = cause instanceof ApiError && cause.status === 401
      ? '当前查看凭证已失效，请联系项目负责人更新演示配置。'
      : errorMessage(cause)
  } finally {
    if (version === detailRequestVersion) detailLoading.value = false
  }
}

async function openDetail(id: string) {
  detailId.value = id
  detailOpen.value = true
  reviewError.value = ''
  conflictNotice.value = ''
  rejecting.value = false
  reason.value = ''
  reasonTouched.value = false
  arrivalTime.value = currentShanghaiTime()
  arrivalError.value = ''
  arrivalRetryPending.value = false
  await loadDetail()
}

async function refreshDetail() {
  if (saving.value) return
  rejecting.value = false
  reason.value = ''
  reasonTouched.value = false
  reviewError.value = ''
  conflictNotice.value = ''
  await loadDetail()
  await store.refresh({ quiet: true })
}

function beginReject() {
  rejecting.value = true
  reasonTouched.value = false
  reviewError.value = ''
}

async function review(action: AppointmentReview['action']) {
  if (!detail.value || saving.value || !canManageAppointments.value) return
  if (action === 'reject') {
    reasonTouched.value = true
    if (reasonError.value) return
  }
  now.value = Date.now()
  if (!reviewAllowed.value) {
    reviewError.value = '当前预约已无法处理，请刷新详情查看最新状态。'
    return
  }
  saving.value = true
  savingAction.value = action
  reviewError.value = ''
  conflictNotice.value = ''
  try {
    detail.value = await api.reviewAppointment(detail.value.id, {
      action,
      version: detail.value.version,
      ...(action === 'reject' ? { reason: reason.value.trim() } : {}),
    })
    rejecting.value = false
    reason.value = ''
    reasonTouched.value = false
    ElMessage.success(action === 'confirm' ? '预约已确认，患者刷新后可查看结果。' : '预约已拒绝，该时段名额已释放。')
    await Promise.allSettled([store.refresh(), schedule.refresh()])
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 409) {
      rejecting.value = false
      reason.value = ''
      reasonTouched.value = false
      await Promise.allSettled([loadDetail(), store.refresh(), schedule.refresh()])
      conflictNotice.value = detailError.value
        ? '预约状态已变化，最新详情暂未加载成功。请重新加载详情后再处理。'
        : '预约状态已变化，已载入最新详情。请核对当前状态后再处理。'
    } else {
      reviewError.value = cause instanceof ApiError && cause.status === 401
        ? '当前处理凭证已失效，请联系项目负责人更新演示配置。'
        : errorMessage(cause)
    }
  } finally {
    saving.value = false
    savingAction.value = null
  }
}

async function registerArrival() {
  if (!detail.value || saving.value || !arrivalAllowed.value) return
  const arrivedAt = new Date(`${arrivalTime.value.replace(' ', 'T')}+08:00`)
  if (!arrivalTime.value || Number.isNaN(arrivedAt.getTime())) {
    arrivalError.value = '请选择实际到诊时间。'
    return
  }
  if (arrivedAt.getTime() < new Date(detail.value.createdAt).getTime() || arrivedAt.getTime() > Date.now()) {
    arrivalError.value = '实际到诊时间须在预约提交时间与当前时间之间。'
    return
  }
  saving.value = true
  arrivalSaving.value = true
  arrivalError.value = ''
  conflictNotice.value = ''
  try {
    detail.value = await api.registerArrival(detail.value.id, { version: detail.value.version, arrivedAt: arrivedAt.toISOString() })
    arrivalRetryPending.value = false
    ElMessage.success('实际到诊已登记。')
    await store.refresh({ quiet: true })
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 409) {
      arrivalRetryPending.value = false
      await Promise.allSettled([loadDetail(), store.refresh({ quiet: true })])
      conflictNotice.value = detailError.value
        ? '预约记录已变化，最新详情暂未加载成功。请重新加载详情后核对到诊记录。'
        : '预约记录已变化，已载入最新详情。请核对到诊记录后再处理。'
    } else {
      // A response can be lost after the server saves it. Keep the exact instant
      // for an idempotent retry instead of submitting a new arrival time.
      arrivalRetryPending.value = !(cause instanceof ApiError) || cause.status >= 500
      arrivalError.value = errorMessage(cause)
    }
  } finally {
    saving.value = false
    arrivalSaving.value = false
  }
}

function beforeClose(done: () => void) { if (!saving.value) done() }

function onDetailClosed() {
  ++detailRequestVersion
  detail.value = null
  detailLoading.value = false
  void store.refresh({ quiet: true })
}

function refreshWhenVisible() {
  now.value = Date.now()
  if (document.visibilityState === 'visible' && !detailOpen.value && !store.refreshing) void store.refresh({ quiet: true })
}

onMounted(() => {
  void schedule.refresh()
  void store.refresh()
  refreshTimer = setInterval(refreshWhenVisible, 30000)
  document.addEventListener('visibilitychange', refreshWhenVisible)
})

onBeforeUnmount(() => {
  ++detailRequestVersion
  if (refreshTimer) clearInterval(refreshTimer)
  document.removeEventListener('visibilitychange', refreshWhenVisible)
})
</script>

<template>
  <section class="page-heading">
    <div><div class="eyebrow">APPOINTMENT CENTER</div><h1>预约管理</h1><p>及时处理每一次预约，让患者安心安排就诊。</p></div>
    <el-button class="refresh-button" :loading="store.refreshing" @click="store.refresh()"><AppIcon v-if="!store.refreshing" name="refresh" />刷新预约</el-button>
  </section>

  <section class="appointment-overview" aria-label="待确认预约概览">
    <div class="appointment-overview-icon"><AppIcon name="clock" /></div>
    <div class="appointment-overview-count"><span>待确认预约</span><strong data-testid="pending-count">{{ store.loading || store.pendingTotal === null ? '—' : store.pendingTotal }}<small>条</small></strong></div>
    <div class="appointment-overview-copy"><h2>提交后先占用名额，确认后才显示已确认</h2><p>待确认名额最多保留 2 小时，具体时间以预约详情中的确认截止为准。</p><span>{{ scopeLabel }}<template v-if="store.countError"> · 数量暂未同步，请刷新重试</template></span></div>
    <el-button class="pending-filter-button" @click="selectStatus('pending')">查看待确认<AppIcon name="arrow" /></el-button>
  </section>

  <section class="surface appointment-panel">
    <div class="panel-heading"><div><h2>预约列表 <span class="count-chip">{{ store.loading || store.error ? '—' : store.total }}</span></h2><p>患者联系信息仅在预约详情中展示。</p></div><span class="sync-meta"><span></span>最近同步 {{ lastRefresh }}</span></div>
    <div class="appointment-status-tabs" role="tablist" aria-label="预约状态筛选"><button v-for="item in statuses" :key="item.value" type="button" role="tab" :aria-selected="store.status === item.value" :class="{ active: store.status === item.value }" @click="selectStatus(item.value)">{{ item.label }}</button></div>
    <div class="filter-bar appointment-filter-bar">
      <div class="filter-field"><label for="appointment-clinic-filter">院区</label><el-select id="appointment-clinic-filter" v-model="store.clinicId" clearable placeholder="全部院区" aria-label="预约院区筛选" @change="changeClinic"><el-option v-for="clinic in visibleClinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></div>
      <div class="filter-field"><label for="appointment-doctor-filter">医生</label><el-select id="appointment-doctor-filter" v-model="store.doctorId" clearable placeholder="全部医生" aria-label="预约医生筛选" @change="store.applyFilters()"><el-option v-for="doctor in filteredDoctors" :key="doctor.id" :label="doctor.name" :value="doctor.id" /></el-select></div>
      <div class="filter-field date-filter"><label for="appointment-date-filter">就诊日期</label><el-date-picker id="appointment-date-filter" :model-value="store.date" type="date" value-format="YYYY-MM-DD" format="YYYY 年 MM 月 DD 日" clearable placeholder="全部日期" aria-label="预约日期筛选" @update:model-value="changeDate" /></div>
      <el-button class="today-button" :disabled="!store.date && !store.doctorId && !store.clinicId && !store.status" @click="resetFilters">重置筛选</el-button>
      <el-button v-if="canManageAppointments" type="primary" :disabled="selected.length === 0 || reminding" :loading="reminding" @click="remindSelected">批量提醒{{ selected.length ? `（${selected.length}）` : '' }}</el-button>
      <span class="appointment-filter-note">按提交时间从新到旧排序</span>
    </div>
    <el-alert v-if="remindResult" :title="`部分提醒未发送：${remindResult}`" type="warning" show-icon closable class="remind-result-alert" @close="remindResult = ''" />

    <div v-if="store.error" class="state-block error-state" role="alert"><span class="state-symbol">!</span><h3>预约数据暂未加载</h3><p>{{ store.error }}</p><el-button type="primary" :loading="store.refreshing" @click="store.refresh()">重新加载预约</el-button></div>
    <div v-else v-loading="store.loading" class="schedule-table-wrap" :aria-busy="store.loading">
      <el-table ref="tableRef" :data="store.appointments" row-key="id" class="schedule-table appointment-table" :empty-text="store.loading ? '正在加载预约…' : '当前筛选下暂无预约，可调整筛选条件或稍后刷新。'" @selection-change="onSelectionChange">
        <el-table-column v-if="canManageAppointments" type="selection" width="48" />
        <el-table-column label="预约编号" min-width="145"><template #default="{ row }"><div class="appointment-code" :data-appointment-id="row.id" :title="row.id">{{ shortId(row.id) }}</div><small class="appointment-row-demo">演示预约</small></template></el-table-column>
        <el-table-column label="医生 / 院区" min-width="170"><template #default="{ row }"><div class="appointment-doctor-cell"><strong>{{ row.doctorName }}</strong><small>{{ row.clinicName }}</small></div></template></el-table-column>
        <el-table-column label="就诊时间" min-width="173"><template #default="{ row }"><div class="appointment-time-cell"><strong>{{ shanghaiDate(new Date(row.startsAt)) }}</strong><small>{{ slotTime(row.startsAt) }}–{{ slotTime(row.endsAt) }}</small></div></template></el-table-column>
        <el-table-column label="预约状态" min-width="119"><template #default="{ row }"><span class="appointment-status" :class="row.status"><span></span>{{ statusLabels[row.status as Appointment['status']] }}</span></template></el-table-column>
        <el-table-column label="实际到诊" min-width="180"><template #default="{ row }"><div class="appointment-time-cell" :data-testid="`arrival-status-${row.id}`"><strong>{{ row.arrivedAt ? '已登记到诊' : '未登记到诊' }}</strong><small v-if="row.arrivedAt">{{ timestamp(row.arrivedAt) }}</small></div></template></el-table-column>
        <el-table-column label="确认截止 / 提交时间" min-width="176"><template #default="{ row }"><div class="appointment-deadline-cell"><strong v-if="row.status === 'pending'">{{ timestamp(row.confirmationDeadline) }}</strong><strong v-else class="muted">—</strong><small>提交于 {{ timestamp(row.createdAt) }}</small></div></template></el-table-column>
        <el-table-column label="操作" width="103" align="right"><template #default="{ row }"><el-button type="primary" link :data-testid="`appointment-detail-${row.id}`" :aria-label="`查看预约 ${row.id}`" @click="openDetail(row.id)">查看详情<AppIcon name="chevron" /></el-button></template></el-table-column>
      </el-table>
    </div>
    <div class="table-footer appointment-pagination"><span>仅使用虚构演示数据</span><el-pagination :current-page="store.page" :page-size="store.pageSize" :total="store.total" :disabled="store.loading || Boolean(store.error)" layout="total, prev, pager, next" small background @current-change="store.selectPage" /></div>
  </section>

  <div class="bottom-note appointment-rules"><span class="note-icon">i</span><p>待确认超时后，系统会将预约标记为已过期并释放名额。未登记到诊的患者可在就诊开始前取消；确认截止前请及时处理。页面可见时每 30 秒自动刷新，查看详情期间暂停自动刷新。</p></div>

  <el-drawer v-model="detailOpen" title="预约详情" size="540px" class="appointment-drawer" :before-close="beforeClose" :close-on-click-modal="!saving" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close @closed="onDetailClosed">
    <div v-if="conflictNotice" class="detail-alert"><el-alert :title="conflictNotice" type="warning" show-icon :closable="false" /></div>
    <div v-if="detailError" class="state-block error-state" role="alert"><span class="state-symbol">!</span><h3>预约详情暂未加载</h3><p>{{ detailError }}</p><el-button type="primary" :loading="detailLoading" @click="loadDetail">重新加载详情</el-button></div>
    <div v-else v-loading="detailLoading" class="appointment-detail-content" :aria-busy="detailLoading">
      <template v-if="detail">
        <div class="detail-summary"><div><span class="detail-kicker">演示预约 · {{ shortId(detail.id) }}</span><h2>{{ detail.doctorName }}</h2><p>{{ detail.clinicName }}</p></div><span class="appointment-status" :class="detail.status"><span></span>{{ statusLabels[detail.status] }}</span></div>
        <div class="detail-visit-time"><AppIcon name="calendar" /><div><strong>{{ shanghaiDate(new Date(detail.startsAt)) }}</strong><span>{{ slotTime(detail.startsAt) }}–{{ slotTime(detail.endsAt) }} · 北京时间</span></div></div>
        <section v-if="detail.status === 'pending'" class="confirmation-notice"><div class="confirmation-notice-heading"><AppIcon name="clock" /><strong>确认截止 {{ timestamp(detail.confirmationDeadline) }}</strong></div><p>当前已占用 1 个名额，尚未确认。请在截止前处理；超过截止时间后系统释放名额。</p><p v-if="!reviewAllowed" class="deadline-passed">当前预约已无法处理，请刷新详情查看最新状态。</p></section>
        <section v-else class="appointment-outcome" :class="detail.status"><strong>{{ detail.status === 'waiting' ? '候补中，未占用名额' : detail.status === 'completed' ? '实际治疗已记录，进入随访' : detail.status === 'confirmed' ? '预约已确认，名额继续保留' : detail.status === 'rejected' ? '预约已拒绝，名额已释放' : detail.status === 'cancelled' ? '患者已取消，名额已释放' : '预约已过期，名额已释放' }}</strong><p v-if="detail.status === 'rejected'" class="rejection-result">{{ detail.rejectionReason || '未填写拒绝原因' }}</p><p v-else-if="detail.status === 'expired'">未在确认截止前完成处理，患者可重新选择时段预约。</p></section>

        <section v-if="detail.slotStatus === 'closed'" class="detail-section"><el-alert :title="detail.suspensionReason || '原时段已停诊'" type="warning" :closable="false" /><p>原预约保留，请先与患者沟通改期。</p><RouterLink v-if="detail.canReschedule" :to="{path: '/arrivals', query: {date: shanghaiDate(new Date(detail.startsAt)), arrival: 'reschedule'}}">前往到诊工作台安排改期</RouterLink></section>
        <section class="detail-section" data-testid="arrival-section"><h3>实际到诊</h3><dl v-if="detail.arrivedAt" class="detail-fields"><div><dt>已登记到诊</dt><dd data-testid="arrival-recorded-time">{{ timestamp(detail.arrivedAt) }} · 北京时间</dd></div></dl><p v-else data-testid="arrival-not-recorded">尚未登记到诊</p><p class="detail-privacy-note">核实患者已经到院后登记。预约确认、治疗记录不会自动计为到诊；已到诊预约不可取消。</p><el-form v-if="arrivalAllowed" label-position="top" @submit.prevent="registerArrival"><el-form-item label="实际到诊时间（北京时间）" :error="arrivalError"><el-date-picker v-model="arrivalTime" type="datetime" value-format="YYYY-MM-DD HH:mm:ss" format="YYYY-MM-DD HH:mm:ss" :disabled="saving || arrivalRetryPending" :clearable="false" placeholder="选择实际到诊时间" aria-label="实际到诊时间（北京时间）" /></el-form-item><p v-if="arrivalRetryPending" class="detail-privacy-note">本次提交时间已保留，请重试以确认登记结果。</p><el-button type="primary" :loading="arrivalSaving" :disabled="saving" data-testid="register-arrival" @click="registerArrival">{{ arrivalRetryPending ? '重试到诊登记' : '登记实际到诊' }}</el-button></el-form></section>

        <section class="detail-section"><h3>联系信息</h3><dl class="detail-fields"><div><dt>联系姓名</dt><dd data-testid="appointment-contact-name">{{ detail.contactName }}</dd></div><div><dt>联系电话</dt><dd data-testid="appointment-contact-phone">{{ detail.contactPhone }}</dd></div></dl><p class="detail-privacy-note">用于本次预约核对，联系信息由患者提交。</p></section>
        <section class="detail-section"><h3>预约备注</h3><p class="appointment-note" data-testid="appointment-note">{{ detail.note || '患者未填写备注' }}</p></section>
        <section class="detail-section"><RouterLink :to="{ path: '/patients', query: { patientId: detail.patientId } }">查看该患者统一档案</RouterLink></section>
        <section class="detail-section"><h3>预约记录</h3><dl class="detail-fields"><div><dt>提交时间</dt><dd>{{ timestamp(detail.createdAt) }}</dd></div><div><dt>更新时间</dt><dd>{{ timestamp(detail.updatedAt) }}</dd></div><div><dt>完整编号</dt><dd class="full-appointment-id">{{ detail.id }}</dd></div></dl></section>
        <div class="detail-cancel-note"><AppIcon name="clock" /><p>患者取消规则：就诊开始前可取消未登记到诊的待确认或已确认预约；待确认预约还须未超过确认截止时间。</p></div>

        <section v-if="rejecting && reviewAllowed" class="reject-form"><h3>填写拒绝原因</h3><p>提交后会释放名额，患者可查看此原因。</p><el-form label-position="top" @submit.prevent="review('reject')"><el-form-item label="拒绝原因" :error="reasonTouched ? reasonError : ''"><el-input v-model="reason" type="textarea" :rows="4" maxlength="200" show-word-limit :disabled="saving" placeholder="请说明本次无法确认预约的原因" aria-label="拒绝原因" @blur="reasonTouched = true" /></el-form-item></el-form></section>
        <div v-if="!canManageAppointments && detail.status === 'pending'" class="detail-alert"><el-alert title="当前账号没有预约确认或拒绝权限。" type="warning" show-icon :closable="false" /></div>
        <div v-if="reviewError" class="detail-alert" role="alert"><el-alert :title="reviewError" type="error" show-icon :closable="false" /></div>
      </template>
    </div>
    <template #footer>
      <div class="appointment-detail-footer">
        <el-button :loading="detailLoading" :disabled="saving || (rejecting && reviewAllowed)" @click="refreshDetail"><AppIcon v-if="!detailLoading" name="refresh" />刷新详情</el-button>
        <div v-if="detail && reviewAllowed" class="review-actions"><template v-if="rejecting"><el-button :disabled="saving" @click="rejecting = false">暂不拒绝</el-button><el-button type="danger" :loading="savingAction === 'reject'" :disabled="saving || !canManageAppointments" @click="review('reject')">提交拒绝</el-button></template><template v-else><el-button class="reject-trigger" :disabled="saving || !canManageAppointments" @click="beginReject">拒绝预约</el-button><el-button type="primary" :loading="savingAction === 'confirm'" :disabled="saving || !canManageAppointments || detail.slotStatus === 'closed'" @click="review('confirm')">确认预约</el-button></template></div>
        <el-button v-else :disabled="saving" @click="detailOpen = false">关闭详情</el-button>
      </div>
    </template>
  </el-drawer>
</template>
