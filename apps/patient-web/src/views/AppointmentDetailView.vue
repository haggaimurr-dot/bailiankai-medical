<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog } from 'vant'
import type { components } from '../../../../packages/contracts/src/schema'
import { ApiError, errorMessage } from '../api'
import { clearSubmission, isSessionExpired, loadSubmission, patientApi } from '../patient'
import { useSessionStore } from '../stores/session'
import { appointmentView, dateTime, type AppointmentView } from '../utils/appointment'

type TimelineEvent = components['schemas']['AppointmentTimelineEvent'] & { label: string; timeLabel: string }

const route = useRoute()
const router = useRouter()
const sessionStore = useSessionStore()
const id = String(route.params.id || '')

const loading = ref(true)
const refreshing = ref(false)
const error = ref('')
const expired = ref(false)
const creatingIdentity = ref(false)
const appointment = ref<AppointmentView | null>(null)
const timeline = ref<TimelineEvent[]>([])
const timelineError = ref('')
const cancelling = ref(false)
const notice = ref('')
const checkInImage = ref('')
const checkInToken = ref('')
const checkInExpiry = ref('')
const checkInLoading = ref(false)
const checkInError = ref('')

let generation = 0
let timelineGeneration = 0
let disposed = false
let checkInTimer: ReturnType<typeof setTimeout> | null = null

const timelineLabels: Record<string, string> = {
  'suspend-move': '停诊改期', submit: '提交预约', waitlist: '登记候补', claim: '候补转为待确认',
  reschedule: '改期申请', confirm: '工作人员确认', reject: '工作人员拒绝', cancel: '患者取消',
  expire: '申请过期', arrival: '登记到诊', arrive: '登记到诊', 'suspension-reschedule': '工作人员协调改期',
  treatment: '记录治疗', complete: '记录治疗',
}

async function confirmDialog(title: string, content: string, confirmText = '确定'): Promise<boolean> {
  try {
    await showConfirmDialog({ title, message: content, confirmButtonText: confirmText, confirmButtonColor: '#1763c6' })
    return true
  } catch {
    return false
  }
}

/** Demo-mode equivalent of the miniprogram's confirmNewSession(). */
async function confirmNewSession(): Promise<boolean> {
  const oldDraft = loadSubmission()
  const confirmed = await confirmDialog(
    '创建新的演示身份',
    '旧预约仍属于旧身份，新身份不会继承这些记录。此操作也会清除本机旧身份的提交恢复信息。是否继续？',
    '创建新身份',
  )
  if (!confirmed) return false
  const session = await sessionStore.createSession()
  const currentDraft = loadSubmission()
  if (oldDraft?.key && currentDraft?.key === oldDraft.key && currentDraft.patientId === oldDraft.patientId && currentDraft.patientId !== session.patient.id) {
    clearSubmission(oldDraft.key)
  }
  return Boolean(session.token)
}

function clearCheckInTimer() {
  if (checkInTimer) {
    clearTimeout(checkInTimer)
    checkInTimer = null
  }
}

async function loadTimeline() {
  const gen = generation
  const tlGen = ++timelineGeneration
  timelineError.value = ''
  try {
    const events = await patientApi.appointmentTimeline(id)
    if (gen !== generation || tlGen !== timelineGeneration || disposed) return
    timeline.value = events.map((event) => ({
      ...event,
      label: timelineLabels[event.action] || '预约状态更新',
      timeLabel: dateTime(event.createdAt),
    }))
  } catch (cause) {
    if (gen !== generation || tlGen !== timelineGeneration || disposed) return
    timelineError.value = errorMessage(cause)
    timeline.value = []
  }
}

async function load() {
  if (disposed) return
  timelineGeneration += 1
  const gen = ++generation
  clearCheckInTimer()
  checkInImage.value = ''
  checkInToken.value = ''
  checkInExpiry.value = ''
  checkInError.value = ''
  loading.value = true
  error.value = ''
  expired.value = false
  try {
    if (!id) throw new Error('缺少预约编号，请从“我的预约”重新进入。')
    const raw = await patientApi.appointment(id)
    if (gen !== generation) return
    appointment.value = appointmentView(raw)
    await loadTimeline()
  } catch (cause) {
    if (gen === generation) {
      error.value = errorMessage(cause)
      expired.value = isSessionExpired(cause)
    }
  } finally {
    if (gen === generation) loading.value = false
  }
}

async function onRefresh() {
  if (!cancelling.value) await load()
  refreshing.value = false
}

function refresh() {
  if (!cancelling.value) void load()
}

async function showCheckInCode() {
  if (loading.value || checkInLoading.value || cancelling.value) return
  const gen = generation
  timelineGeneration += 1
  checkInLoading.value = true
  checkInError.value = ''
  checkInImage.value = ''
  checkInToken.value = ''
  try {
    const code = await patientApi.checkInCode(id)
    if (disposed || gen !== generation) return
    checkInImage.value = code.imageDataUrl
    checkInToken.value = code.token
    checkInExpiry.value = dateTime(code.expiresAt)
    clearCheckInTimer()
    checkInTimer = setTimeout(() => {
      if (!disposed && checkInImage.value) {
        checkInImage.value = ''
        checkInToken.value = ''
        checkInError.value = '签到码已到期，请重新领取。'
      }
    }, Math.max(0, Date.parse(code.expiresAt) - Date.now()))
  } catch (cause) {
    if (!disposed && gen === generation) checkInError.value = errorMessage(cause)
  } finally {
    if (!disposed) checkInLoading.value = false
  }
}

async function cancelAppointment() {
  const current = appointment.value
  if (!current?.cancellable || loading.value || cancelling.value) return
  cancelling.value = true
  notice.value = ''
  try {
    if (!(await confirmDialog('确认取消预约', '取消后终止本次预约或候补，已占用的名额会释放。如需再次到院，请重新提交申请。', '取消预约'))) return
    const cancelled = await patientApi.cancel(current.id, current.version)
    if (disposed) return
    appointment.value = appointmentView(cancelled)
    notice.value = '预约或候补已取消。'
    await loadTimeline()
  } catch (cause) {
    if (disposed) return
    if (isSessionExpired(cause)) {
      error.value = errorMessage(cause)
      expired.value = true
    } else {
      await load()
      if (disposed) return
      const nowCancelled = appointment.value?.status === 'cancelled'
      notice.value = nowCancelled
        ? '预约或候补已取消。'
        : cause instanceof ApiError && cause.status === 409
          ? '预约状态已发生变化，已刷新最新结果。请核对后再操作。'
          : '暂时未能确认取消结果，请刷新后查看；如仍可取消，可再次操作。'
    }
  } finally {
    if (!disposed) cancelling.value = false
  }
}

async function newIdentity() {
  if (creatingIdentity.value) return
  creatingIdentity.value = true
  try {
    if ((await confirmNewSession()) && !disposed) void router.replace('/appointments')
  } catch (cause) {
    if (!disposed) error.value = errorMessage(cause)
  } finally {
    if (!disposed) creatingIdentity.value = false
  }
}

function openReschedule() {
  if (!loading.value && !cancelling.value && appointment.value?.canReschedule) {
    void router.push({ name: 'reschedule', params: { id } })
  }
}

function openPhotos() {
  if (id) void router.push({ path: '/care/photos', query: { id } })
}

function openCarePlan() {
  if (id) void router.push({ path: '/care/plan', query: { id } })
}

function openFollowups() {
  if (id) void router.push({ path: '/followups', query: { id } })
}

onMounted(() => void load())
onUnmounted(() => {
  generation += 1
  timelineGeneration += 1
  disposed = true
  clearCheckInTimer()
})
</script>

<template>
  <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
    <main class="page">
      <div class="demo-banner">
        <span class="demo-dot"></span><span>开发演示 · 本页展示当前患者账号的申请结果</span>
      </div>

      <div v-if="loading" class="card state"><van-loading size="24">正在读取最新预约状态</van-loading></div>
      <div v-else-if="expired" class="card state">
        <div class="state-title">登录已失效</div>
        <p class="muted">{{ error }}</p>
        <button class="primary" :disabled="creatingIdentity" @click="newIdentity">
          {{ creatingIdentity ? '正在重新登录…' : '重新登录' }}
        </button>
      </div>
      <div v-else-if="error" class="card state">
        <div class="state-title">无法读取预约详情</div>
        <p class="muted">{{ error }}</p>
        <button class="ghost" @click="refresh">重新加载</button>
        <div class="secondary"><button class="text-link" @click="router.replace('/appointments')">返回我的预约</button></div>
      </div>
      <template v-else-if="appointment">
        <section v-if="appointment.slotStatus === 'closed'" class="card">
          <h2>原时段已停诊</h2>
          <p class="copy">{{ appointment.suspensionReason || '工作人员将与您沟通新的就诊时间，请先确认改期安排。' }}</p>
        </section>

        <section v-if="appointment.status === 'confirmed' && !appointment.arrivedAt && appointment.slotStatus !== 'closed'" class="card">
          <h2>到院扫码签到</h2>
          <p class="copy">就诊当天领取签到码，向本院工作人员出示并核对预约。二维码 5 分钟有效。</p>
          <button class="ghost brand-outline" :disabled="checkInLoading" @click="showCheckInCode">
            {{ checkInLoading ? '正在领取…' : checkInImage ? '刷新签到码' : '显示签到码' }}
          </button>
          <p v-if="checkInError" class="notice-line" role="alert">{{ checkInError }}</p>
          <div v-if="checkInImage" class="checkin-box">
            <img class="checkin-img" :src="checkInImage" alt="签到二维码" />
            <div class="checkin-code">{{ checkInToken }}</div>
            <p class="muted">有效至 {{ checkInExpiry }}（北京时间）</p>
          </div>
        </section>

        <section v-if="appointment.arrivedAt" class="card">
          <h2>已登记到诊</h2>
          <p class="copy">工作人员已核实到院，预约不可取消。</p>
        </section>

        <section class="card result-card">
          <span class="chip" :class="appointment.statusTheme === 'default' ? '' : appointment.statusTheme">{{ appointment.statusLabel }}</span>
          <div class="result-title">{{ appointment.statusLabel }}</div>
          <p class="muted">{{ appointment.statusHint }}</p>
          <p v-if="appointment.status === 'pending'" class="deadline">
            确认截止：{{ appointment.deadlineLabel }}
            <span class="deadline-note">以服务端显示的截止时间为准，且不晚于时段开始。</span>
          </p>
          <p v-if="appointment.status === 'rejected'" class="reason">
            拒绝原因：{{ appointment.rejectionReason || '工作人员未确认本次申请' }}
          </p>
        </section>

        <section class="card">
          <h2>到院安排</h2>
          <div class="detail-row"><span class="detail-label">医生</span><span class="detail-value">{{ appointment.doctorName }}</span></div>
          <div class="detail-row"><span class="detail-label">时间</span><span class="detail-value">{{ appointment.timeLabel }} <span class="muted">北京时间</span></span></div>
          <div class="detail-row"><span class="detail-label">院区</span><span class="detail-value">{{ appointment.clinicName }}</span></div>
        </section>

        <section v-if="['pending', 'confirmed', 'completed'].includes(appointment.status)" class="card">
          <h2>面诊资料与方案</h2>
          <p class="copy">照片为选填资料；负责医生发布方案后，可在这里查看。</p>
          <div class="row care-actions">
            <button class="ghost grow" @click="openPhotos">照片资料</button>
            <button class="ghost grow" @click="openCarePlan">治疗方案</button>
          </div>
        </section>

        <section class="card">
          <h2>治疗后随访</h2>
          <p class="copy">记录实际治疗后，会在这里显示随访任务和医护回评。</p>
          <button class="ghost brand-outline" @click="openFollowups">查看随访任务</button>
        </section>

        <section class="card">
          <h2>申请信息</h2>
          <div class="detail-row"><span class="detail-label">联系人</span><span class="detail-value">{{ appointment.contactName }}</span></div>
          <div class="detail-row"><span class="detail-label">手机号</span><span class="detail-value">{{ appointment.contactPhone }}</span></div>
          <div v-if="appointment.note" class="detail-row"><span class="detail-label">备注</span><span class="detail-value">{{ appointment.note }}</span></div>
          <div class="detail-row"><span class="detail-label">提交时间</span><span class="detail-value">{{ appointment.createdLabel }}</span></div>
          <div class="detail-row"><span class="detail-label">申请编号</span><span class="detail-value selectable">{{ appointment.id }}</span></div>
        </section>

        <section class="card">
          <h2>预约状态时间线</h2>
          <p v-if="timelineError" class="muted">
            {{ timelineError }}
            <button class="text-link" @click="loadTimeline">重试加载历史</button>
          </p>
          <p v-else-if="!timeline.length" class="muted">暂无历史记录</p>
          <div v-for="event in timeline" v-else :key="event.id" class="timeline-event">
            <div class="timeline-title">{{ event.label }}</div>
            <p class="muted">{{ event.timeLabel }} · 北京时间</p>
            <p v-if="event.detail" class="timeline-detail">{{ event.detail }}</p>
          </div>
        </section>

        <button v-if="appointment.canReschedule" class="ghost block-btn" :disabled="cancelling" @click="openReschedule">
          {{ appointment.status === 'waiting' ? '查询空位并申请转预约' : '申请改期' }}
        </button>

        <p v-if="notice" class="notice-line" role="alert">{{ notice }}</p>

        <button class="primary" :disabled="cancelling" @click="refresh">刷新预约结果</button>
        <button v-if="appointment.cancellable" class="ghost danger-btn" :disabled="cancelling" @click="cancelAppointment">
          {{ cancelling ? '正在取消…' : '取消预约' }}
        </button>

        <div class="secondary center">
          <button class="text-link" @click="router.replace('/appointments')">查看我的预约</button>
        </div>
        <p class="footer-note">尚未到诊或记录治疗且时段开始前，可取消候补、待确认或已确认预约。</p>
      </template>
    </main>
  </van-pull-refresh>
</template>

<style scoped>
.demo-banner { display: flex; gap: 8px; align-items: flex-start; background: #fff6e5; border: 1px solid #f3d9a4; color: var(--amber); border-radius: 10px; padding: 10px 12px; font-size: 12px; line-height: 1.6; }
.demo-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--amber); margin-top: 5px; flex: none; }
.state { text-align: center; }
.state-title { font-weight: 700; margin-bottom: 6px; }
.copy { line-height: 1.6; margin: 0 0 10px; }
.brand-outline { color: var(--brand); border-color: var(--brand); }
.checkin-box { text-align: center; margin-top: 14px; }
.checkin-img { width: 280px; max-width: 100%; display: block; margin: 0 auto; }
.checkin-code { font-size: 20px; font-weight: 800; letter-spacing: 2px; margin-top: 10px; word-break: break-all; }
.notice-line { background: #eff6ff; border: 1px solid #dbeafe; color: var(--brand-dark); border-radius: 10px; padding: 10px 12px; font-size: 13px; line-height: 1.6; margin: 0; }
.result-card .result-title { font-size: 22px; font-weight: 800; margin: 8px 0 4px; }
.deadline { color: var(--amber); font-size: 13px; line-height: 1.6; }
.deadline-note { display: block; color: var(--muted); font-size: 12px; }
.reason { color: var(--danger); font-size: 13px; }
.detail-row { display: flex; gap: 12px; padding: 7px 0; border-top: 1px solid var(--bg); }
.detail-row:first-of-type { border-top: 0; }
.detail-label { flex: none; width: 64px; color: var(--muted); font-size: 13px; }
.detail-value { flex: 1; min-width: 0; line-height: 1.6; }
.selectable { user-select: text; word-break: break-all; }
.care-actions { gap: 10px; }
.timeline-event { padding: 8px 0; border-top: 1px solid var(--bg); }
.timeline-event:first-of-type { border-top: 0; }
.timeline-title { font-weight: 600; }
.timeline-event p { margin: 2px 0 0; }
.timeline-detail { font-size: 13px; line-height: 1.6; }
.block-btn { width: 100%; color: var(--brand); border-color: var(--brand); font-weight: 600; }
.danger-btn { width: 100%; color: var(--danger); border-color: #f3c2bd; font-weight: 600; }
.secondary { margin-top: 4px; }
.secondary.center { text-align: center; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 13px; cursor: pointer; padding: 4px 0; }
.footer-note { text-align: center; color: var(--muted); font-size: 12px; margin: 0; }
</style>
