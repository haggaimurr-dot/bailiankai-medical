<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog } from 'vant'
import { api, ApiError, errorMessage, type Slot } from '../api'
import {
  clearSubmission, isSessionExpired, loadSubmission, patientApi, saveSubmission,
  type Appointment, type AppointmentCreate, type Submission,
} from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'
import { clinicTime } from '../utils/date'

const route = useRoute()
const router = useRouter()
const sessionStore = useSessionStore()

const waitlist = ref(route.query.waitlist === '1')
const loading = ref(true)
const error = ref('')
const expired = ref(false)
const creatingIdentity = ref(false)
const patientName = ref('')
const accountPhone = ref('')
const officialIdentity = ref(false)
const clinicName = ref('')
const doctorName = ref('')
const timeLabel = ref('')
const slot = ref<Slot | null>(null)
const slotAvailable = ref(false)
// Demo-mode prefill, mirroring the miniprogram's PATIENT_LOGIN_MODE === 'demo' defaults.
const contactName = ref('演示访客')
const contactPhone = ref('13800000000')
const note = ref('')
const validationError = ref('')
const submitError = ref('')
const submitting = ref(false)
const recovering = ref(false)
const recoveryMode = ref(false)
const recoveryMessage = ref('')
const canRetry = ref(false)
const canAbandon = ref(false)
const completedId = ref('')

let generation = 0
let disposed = false
let patientId = ''
let selection: Submission['selection'] = {
  slotId: String(route.query.slotId || ''),
  clinicId: String(route.query.clinicId || ''),
  doctorId: String(route.query.doctorId || ''),
  date: String(route.query.date || ''),
}
let draft: Submission | null = null

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

async function initialize() {
  if (disposed) return
  const gen = ++generation
  loading.value = true
  error.value = ''
  expired.value = false
  submitError.value = ''
  validationError.value = ''
  try {
    const session = await sessionStore.ensureSession()
    const patient = await patientApi.me()
    if (gen !== generation) return
    patientId = session.patient.id
    draft = loadSubmission()
    if (draft && draft.patientId !== patientId) throw new Error('本机还有原患者账号的申请恢复信息，请先查看“我的预约”确认当前身份。')
    if (draft) {
      selection = draft.selection
      waitlist.value = Boolean(draft.body?.waitlist)
    }
    const current = selection
    if (!current.slotId || !current.clinicId || !current.date) throw new Error('请先选择医生和可用时段。')
    patientName.value = patient.displayName
    accountPhone.value = patient.phoneAuthorization === 'granted' ? patient.phone || '' : ''
    officialIdentity.value = patient.loginKind === 'wechat'
    recoveryMode.value = Boolean(draft)
    canRetry.value = Boolean(draft?.body)
    canAbandon.value = false
    if (draft?.body) {
      contactName.value = draft.body.contactName
      contactPhone.value = draft.body.contactPhone
      note.value = draft.body.note || ''
    }
    const [clinics, doctors, slots] = await Promise.all([
      api.clinics(),
      api.doctors(current.clinicId),
      api.slots({ clinicId: current.clinicId, date: current.date, ...(current.doctorId ? { doctorId: current.doctorId } : {}) }),
    ])
    if (gen !== generation) return
    const found = slots.find((item) => item.id === current.slotId) || null
    clinicName.value = clinics.find((item) => item.id === current.clinicId)?.name || '原申请院区'
    doctorName.value = doctors.find((item) => item.id === current.doctorId)?.name || '原申请医生'
    timeLabel.value = found ? `${dateTime(found.startsAt)} — ${clinicTime(found.endsAt)}` : `${current.date}（时段已结束或更新）`
    slot.value = found
    slotAvailable.value = Boolean(found && found.status === 'open' &&
      (waitlist.value ? found.capacity > 0 && found.reserved >= found.capacity : found.capacity > found.reserved) &&
      Date.parse(found.startsAt) > Date.now())
    loading.value = false
    if (draft) await querySubmission()
  } catch (cause) {
    if (gen === generation) {
      error.value = errorMessage(cause)
      expired.value = isSessionExpired(cause)
      loading.value = false
    }
  }
}

function complete(appointment: Appointment, requestKey: string) {
  clearSubmission(requestKey)
  draft = null
  if (disposed) return
  completedId.value = appointment.id
  recoveryMode.value = false
  submitError.value = ''
  void router.replace({ name: 'appointment-detail', params: { id: appointment.id } })
}

async function sendDraft(current: Submission) {
  if (!current.body) return
  try {
    const appointment = await patientApi.create(current.body, current.key, current.patientId)
    complete(appointment, current.key)
  } catch (cause) {
    if (isSessionExpired(cause)) {
      if (!disposed) {
        expired.value = true
        submitError.value = errorMessage(cause)
      }
      return
    }
    if (cause instanceof ApiError && cause.code === 'PATIENT_IDENTITY_CHANGED') {
      if (!disposed) {
        error.value = errorMessage(cause)
        canRetry.value = false
      }
      return
    }
    const definiteFailure = cause instanceof ApiError && [400, 404, 409].includes(cause.status) &&
      !['IDEMPOTENCY_CONFLICT', 'PATIENT_IDENTITY_CHANGED'].includes(cause.code)
    if (definiteFailure) {
      clearSubmission(current.key)
      draft = null
      if (!disposed) {
        recoveryMode.value = false
        submitError.value = errorMessage(cause)
        canRetry.value = false
        slotAvailable.value = !cause.code.startsWith('SLOT_') && slotAvailable.value
      }
    } else if (!disposed) {
      recoveryMode.value = true
      canRetry.value = true
      submitError.value = ''
      recoveryMessage.value = '暂时未能确认提交结果。原申请可能已被接收，请先查询结果；重试会使用同一申请编号和原资料。'
    }
  }
}

async function submit() {
  if (submitting.value || recovering.value || recoveryMode.value || completedId.value || !slotAvailable.value) return
  const body: AppointmentCreate = {
    ...(waitlist.value ? { waitlist: true } : {}),
    slotId: selection.slotId,
    contactName: contactName.value.trim(),
    contactPhone: contactPhone.value.trim(),
    note: note.value.trim(),
  }
  const length = Array.from(body.contactName).length
  const invalid = length < 1 || length > 40
    ? '请填写 1–40 字的联系人姓名。'
    : !/^1[3-9]\d{9}$/.test(body.contactPhone)
      ? '请填写 11 位大陆手机号码。'
      : Array.from(body.note || '').length > 300
        ? '备注不能超过 300 字。'
        : ''
  if (invalid) {
    validationError.value = invalid
    return
  }
  submitting.value = true
  submitError.value = ''
  validationError.value = ''
  try {
    if (loadSubmission()) {
      submitting.value = false
      await initialize()
      return
    }
    const session = await sessionStore.ensureSession()
    if (session.patient.id !== patientId) throw new Error('患者身份已变化，请重新加载页面后提交。')
    draft = saveSubmission(patientId, body, selection)
    if (!disposed) {
      recoveryMode.value = true
      canRetry.value = true
    }
    await sendDraft(draft)
  } catch (cause) {
    if (!disposed) {
      submitError.value = errorMessage(cause)
      expired.value = isSessionExpired(cause)
    }
  } finally {
    if (!disposed) submitting.value = false
  }
}

async function querySubmission() {
  if (submitting.value || recovering.value || !draft) return
  const current = draft
  recovering.value = true
  submitError.value = ''
  canAbandon.value = false
  try {
    const appointment = await patientApi.byRequest(current.key, current.patientId)
    complete(appointment, current.key)
  } catch (cause) {
    if (disposed) return
    if (cause instanceof ApiError && cause.status === 404) {
      recoveryMessage.value = current.body
        ? '暂未查询到结果，原请求可能仍在处理中。可再次查询，或用原申请编号和原资料重试。'
        : '本机联系资料保存已超过 24 小时，已清除。尚未查到原申请，可继续查询；放弃恢复前请先核对“我的预约”。'
      canRetry.value = Boolean(current.body)
      canAbandon.value = !current.body
    } else if (cause instanceof ApiError && cause.code === 'PATIENT_IDENTITY_CHANGED') {
      error.value = errorMessage(cause)
      canRetry.value = false
    } else {
      submitError.value = errorMessage(cause)
      expired.value = isSessionExpired(cause)
    }
  } finally {
    if (!disposed) recovering.value = false
  }
}

async function retrySubmission() {
  if (submitting.value || recovering.value || !draft?.body) return
  submitting.value = true
  submitError.value = ''
  try {
    await sendDraft(draft)
  } finally {
    if (!disposed) submitting.value = false
  }
}

async function abandonRecovery() {
  if (!canAbandon.value || recovering.value || submitting.value || !draft) return
  const confirmed = await confirmDialog(
    '放弃原申请恢复',
    '原请求仍可能完成，放弃恢复不会取消已经创建的预约。请先核对“我的预约”。确认后将清除本机的原申请编号。',
    '确认放弃',
  )
  if (!confirmed) return
  clearSubmission(draft.key)
  backToSchedule()
}

async function newIdentity() {
  if (creatingIdentity.value || submitting.value || recovering.value) return
  creatingIdentity.value = true
  try {
    if (await confirmNewSession()) await initialize()
  } catch (cause) {
    if (!disposed) error.value = errorMessage(cause)
  } finally {
    if (!disposed) creatingIdentity.value = false
  }
}

function useAccountPhone() {
  if (submitting.value || recoveryMode.value || completedId.value || !accountPhone.value) return
  contactPhone.value = accountPhone.value
  validationError.value = ''
}

function backToSchedule() {
  void router.replace('/schedule')
}

function openAppointments() {
  void router.push('/appointments')
}

function openCompleted() {
  if (!disposed && completedId.value) {
    void router.replace({ name: 'appointment-detail', params: { id: completedId.value } })
  }
}

onMounted(() => void initialize())
onUnmounted(() => { generation += 1; disposed = true })
</script>

<template>
  <main class="page">
    <div class="demo-banner">
      <span class="demo-dot"></span><span>预约演示 · 请使用示例资料，不填写真实患者信息</span>
    </div>
    <div>
      <div class="eyebrow">APPOINTMENT</div>
      <h1>{{ waitlist ? '登记候补预约' : '填写预约申请' }}</h1>
      <p class="lede">{{ waitlist ? '候补不占名额；有空位后需主动申请，工作人员确认后生效。' : '提交后暂留名额，工作人员确认后生效。' }}</p>
    </div>

    <div v-if="loading" class="card state"><van-loading size="24">正在核对出诊信息</van-loading></div>
    <div v-else-if="expired" class="card state">
      <div class="state-title">登录已失效</div>
      <p class="muted">{{ error || submitError }}</p>
      <button class="primary" :disabled="creatingIdentity" @click="newIdentity">
        {{ creatingIdentity ? '正在重新登录…' : '重新登录' }}
      </button>
    </div>
    <div v-else-if="error" class="card state">
      <div class="state-title">暂时无法继续</div>
      <p class="muted">{{ error }}</p>
      <button class="ghost" @click="initialize">重新加载</button>
      <div class="secondary"><button class="text-link" @click="openAppointments">查看我的预约</button></div>
      <div class="secondary"><button class="text-link" @click="backToSchedule">返回排班</button></div>
    </div>
    <template v-else>
      <section class="card">
        <h2 class="summary-doctor">{{ doctorName }}</h2>
        <div class="summary-time">{{ timeLabel }}</div>
        <p class="muted">{{ clinicName }} · 北京时间</p>
      </section>

      <p class="identity-note">
        患者账号：{{ patientName }}
        <span class="identity-line">{{ officialIdentity ? '同一微信账号可恢复此患者的资料与预约。' : '本机演示身份，仅用于开发验证。' }}</span>
      </p>

      <div v-if="completedId" class="card state">
        <div class="state-title">申请已提交</div>
        <p class="muted">可进入详情查看工作人员的确认结果。</p>
        <button class="primary" @click="openCompleted">查看申请详情</button>
      </div>
      <template v-else>
        <section v-if="recoveryMode" class="card recovery-card">
          <h2>正在确认原申请结果</h2>
          <p class="muted">{{ recoveryMessage || '已保留原申请编号与资料，请等待提交结果。' }}</p>
          <button class="primary" :disabled="recovering || submitting" @click="querySubmission">
            {{ recovering ? '正在查询…' : '查询提交结果' }}
          </button>
          <button v-if="canRetry" class="ghost block-btn" :disabled="submitting || recovering" @click="retrySubmission">
            {{ submitting ? '正在重试…' : '使用原申请重试' }}
          </button>
          <div v-if="canAbandon" class="secondary">
            <button class="text-link danger" @click="abandonRecovery">放弃原申请恢复</button>
          </div>
        </section>
        <section v-else-if="!slotAvailable" class="card state">
          <div class="state-title">这个时段暂不可预约</div>
          <p class="muted">可能已约满、停诊或开始，请重新选择时段。</p>
          <button class="ghost" @click="backToSchedule">重新选择时段</button>
        </section>

        <section v-if="!recoveryMode || canRetry" class="card">
          <h2>联系信息</h2>
          <p class="muted">本次预约联系人可与患者本人不同。这里的手机号单独保存，不修改患者账号手机号。</p>
          <button v-if="accountPhone && !recoveryMode" class="text-link" :disabled="submitting" @click="useAccountPhone">
            使用账号手机号填写本次联系人
          </button>
          <label class="field-label" for="bk-name">联系人姓名 <span class="required">*</span></label>
          <input
            id="bk-name" v-model="contactName" class="field" maxlength="40"
            :disabled="submitting || recoveryMode" placeholder="演示访客"
            @input="validationError = ''"
          />
          <label class="field-label" for="bk-phone">手机号码 <span class="required">*</span></label>
          <input
            id="bk-phone" v-model="contactPhone" class="field" type="tel" inputmode="numeric" maxlength="11"
            :disabled="submitting || recoveryMode" placeholder="13800000000（演示号码）"
            @input="validationError = ''"
          />
          <label class="field-label" for="bk-note">备注 <span class="muted">选填</span></label>
          <textarea
            id="bk-note" v-model="note" class="field" rows="3" maxlength="300"
            :disabled="submitting || recoveryMode" placeholder="可填写需要协调的到院事项，请勿填写诊疗隐私"
            @input="validationError = ''"
          ></textarea>
          <div class="field-count">{{ note.length }} / 300</div>
        </section>

        <section class="rules">
          <div class="rules-title">{{ waitlist ? '候补须知' : '预约须知' }}</div>
          <template v-if="waitlist">
            <p>仅开放且已满的时段可候补；不保证有空位。</p>
            <p>在详情刷新后主动申请空位，成功后再等待工作人员确认。</p>
            <p>可在开始前取消候补；开始时自动过期。</p>
          </template>
          <template v-else>
            <p>提交后名额按服务端显示的确认截止时间保留，最晚截至时段开始。</p>
            <p>工作人员确认后才生效；未及时确认会自动过期。</p>
            <p>时段开始前，可以取消待确认或已确认的预约。</p>
          </template>
        </section>

        <p v-if="validationError || submitError" class="error-box" role="alert">{{ validationError || submitError }}</p>

        <button v-if="!recoveryMode" class="primary" :disabled="!slotAvailable || submitting" @click="submit">
          {{ submitting ? '正在提交…' : waitlist ? '提交候补申请' : '提交预约申请' }}
        </button>
      </template>

      <div class="secondary center">
        <button class="text-link" @click="openAppointments">查看我的预约</button>
      </div>
    </template>
  </main>
</template>

<style scoped>
.demo-banner { display: flex; gap: 8px; align-items: flex-start; background: #fff6e5; border: 1px solid #f3d9a4; color: var(--amber); border-radius: 10px; padding: 10px 12px; font-size: 12px; line-height: 1.6; }
.demo-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--amber); margin-top: 5px; flex: none; }
.eyebrow { font-size: 11px; letter-spacing: 2px; color: var(--brand); font-weight: 700; }
.state { text-align: center; }
.state-title { font-weight: 700; margin-bottom: 6px; }
.summary-doctor { margin-bottom: 4px; }
.summary-time { font-size: 17px; font-weight: 700; }
.identity-note { background: #eff6ff; border: 1px solid #dbeafe; border-radius: 10px; padding: 10px 12px; font-size: 13px; line-height: 1.6; margin: 0; }
.identity-line { display: block; color: var(--muted); }
.recovery-card .block-btn { width: 100%; margin-top: 10px; }
.secondary { margin-top: 10px; }
.secondary.center { text-align: center; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 14px; cursor: pointer; padding: 4px 0; }
.text-link.danger { color: var(--danger); }
.required { color: var(--danger); }
.field-count { text-align: right; font-size: 12px; color: var(--muted); margin-top: 4px; }
.rules { background: #fff; border: 1px dashed var(--line); border-radius: 14px; padding: 14px 16px; }
.rules-title { font-weight: 700; margin-bottom: 6px; }
.rules p { margin: 4px 0; font-size: 13px; color: var(--muted); line-height: 1.6; }
</style>
