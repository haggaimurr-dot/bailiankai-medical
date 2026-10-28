<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog } from 'vant'
import { api, ApiError, errorMessage, type Slot } from '../api'
import { isSessionExpired, patientApi, randomKey } from '../patient'
import { useSessionStore } from '../stores/session'
import { appointmentView, dateTime, type AppointmentView } from '../utils/appointment'
import { clinicTime, upcomingDays, type DayOption } from '../utils/date'

interface ChangeDraft {
  patientId: string
  key: string
  body: { slotId: string; version: number }
}

interface SlotRow extends Slot {
  label: string
  available: boolean
}

const route = useRoute()
const router = useRouter()
const sessionStore = useSessionStore()
const id = String(route.params.id || '')
const DRAFT_KEY = `booking-change:${id}`

const loading = ref(true)
const loadingSlots = ref(false)
const submitting = ref(false)
const error = ref('')
const slotError = ref('')
const notice = ref('')
const appointment = ref<AppointmentView | null>(null)
const days = ref<DayOption[]>([])
const date = ref('')
const slots = ref<SlotRow[]>([])
const recommendations = ref<SlotRow[]>([])
const selected = ref('')
const recovering = ref(false)

let patientId = ''
let generation = 0
let slotGeneration = 0
let disposed = false
let draft: ChangeDraft | null = null

const title = computed(() => (appointment.value?.status === 'waiting' ? '候补转预约' : '更改预约时间'))

function readDraft(): ChangeDraft | null {
  try {
    const raw = window.localStorage.getItem(DRAFT_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as ChangeDraft
    return parsed?.key && parsed?.body ? parsed : null
  } catch {
    return null
  }
}

function removeDraft() {
  window.localStorage.removeItem(DRAFT_KEY)
}

async function confirmDialog(title: string, content: string, confirmText = '确定'): Promise<boolean> {
  try {
    await showConfirmDialog({ title, message: content, confirmButtonText: confirmText, confirmButtonColor: '#1763c6' })
    return true
  } catch {
    return false
  }
}

async function load() {
  const gen = ++generation
  slotGeneration += 1
  loading.value = true
  error.value = ''
  slotError.value = ''
  slots.value = []
  try {
    if (!id) throw new Error('请从预约详情进入。')
    const session = await sessionStore.ensureSession()
    const raw = await patientApi.appointment(id)
    if (gen !== generation) return
    patientId = session.patient.id
    const saved = readDraft()
    if (saved && saved.patientId !== patientId) throw new Error('当前患者身份已变化，请使用原账号查询改期结果。')
    draft = saved || null
    const dayOptions = upcomingDays()
    const preferred = date.value || dateTime(raw.startsAt).slice(0, 10)
    appointment.value = appointmentView(raw)
    days.value = dayOptions
    date.value = dayOptions.some((day) => day.date === preferred) ? preferred : dayOptions[0].date
    recovering.value = Boolean(draft)
    selected.value = draft?.body.slotId || ''
    await loadSlots()
    await loadRecommendations(raw.startsAt)
  } catch (cause) {
    if (gen === generation) error.value = errorMessage(cause)
  } finally {
    if (gen === generation) loading.value = false
  }
}

async function loadSlots() {
  const current = appointment.value
  if (!current) return
  const gen = ++slotGeneration
  loadingSlots.value = true
  slotError.value = ''
  slots.value = []
  try {
    const rows = await api.slots({ clinicId: current.clinicId, doctorId: current.doctorId, date: date.value })
    if (gen !== slotGeneration) return
    slots.value = rows
      .filter((slot) => slot.id !== current.slotId || current.status === 'waiting')
      .map((slot) => ({
        ...slot,
        label: `${dateTime(slot.startsAt)} — ${clinicTime(slot.endsAt)}`,
        available: slot.status === 'open' && (slot.remaining ?? slot.capacity - slot.reserved) > 0 && Date.parse(slot.startsAt) > Date.now(),
      }))
  } catch (cause) {
    if (gen === slotGeneration) slotError.value = errorMessage(cause)
  } finally {
    if (gen === slotGeneration) loadingSlots.value = false
  }
}

function toRow(slot: Slot): SlotRow {
  return {
    ...slot,
    label: `${dateTime(slot.startsAt)} — ${clinicTime(slot.endsAt)}`,
    available: slot.status === 'open' && (slot.remaining ?? slot.capacity - slot.reserved) > 0 && Date.parse(slot.startsAt) > Date.now(),
  }
}

async function loadRecommendations(after: string) {
  const current = appointment.value
  if (!current) return
  try {
    const rows = await api.slotRecommendations({ clinicId: current.clinicId, doctorId: current.doctorId, after, limit: 3 })
    recommendations.value = rows.filter((slot) => slot.id !== current.slotId).map(toRow)
  } catch {
    // Recommendations are a convenience; the full calendar below stays usable.
    recommendations.value = []
  }
}

async function chooseRecommendation(slot: SlotRow) {
  if (submitting.value || loadingSlots.value || draft) return
  if (!slot.available) return
  date.value = slot.startsAt.slice(0, 10)
  selected.value = ''
  await loadSlots()
  if (slots.value.some((row) => row.id === slot.id && row.available)) {
    selected.value = slot.id
    notice.value = ''
  }
}

function changeDate(next: string) {
  if (submitting.value || draft) return
  if (!days.value.some((day) => day.date === next)) return
  date.value = next
  selected.value = ''
  void loadSlots()
}

function selectSlot(slotId: string) {
  if (submitting.value || loadingSlots.value || draft) return
  if (slots.value.some((slot) => slot.id === slotId && slot.available)) {
    selected.value = slotId
    notice.value = ''
  }
}

async function submit() {
  const current = appointment.value
  if (!current || loading.value || submitting.value) return
  if (!draft && (!current.canReschedule || !slots.value.some((slot) => slot.id === selected.value && slot.available))) return
  submitting.value = true
  notice.value = ''
  try {
    if (!draft) {
      const chosen = slots.value.find((slot) => slot.id === selected.value)!
      const confirmed = await confirmDialog(
        current.status === 'waiting' ? '申请预约名额' : '确认改期',
        `${chosen.label}。成功后需工作人员重新确认；失败时保留原申请。`,
        '确认申请',
      )
      if (!confirmed) return
      if (disposed) return
      const key = randomKey()
      sessionStore.requireIdentity(patientId)
      if (readDraft()) throw new Error('已有改期申请待确认，请刷新后重试。')
      const created: ChangeDraft = { patientId, key, body: { slotId: selected.value, version: current.version } }
      window.localStorage.setItem(DRAFT_KEY, JSON.stringify(created))
      draft = created
      recovering.value = true
    }
    const active = draft!
    await patientApi.reschedule(id, active.body, active.key, active.patientId)
    removeDraft()
    draft = null
    if (!disposed) openDetail()
  } catch (cause) {
    if (disposed) return
    const definite = cause instanceof ApiError && [400, 404, 409].includes(cause.status) && cause.code !== 'IDEMPOTENCY_CONFLICT'
    if (definite) {
      removeDraft()
      draft = null
      await load()
    }
    if (disposed) return
    notice.value = isSessionExpired(cause)
      ? '登录已失效，请重新登录原账号后再确认结果。'
      : draft
        ? '暂时未能确认结果。请使用原申请重试，不能另选时段重复提交。'
        : errorMessage(cause)
    recovering.value = Boolean(draft)
  } finally {
    if (!disposed) submitting.value = false
  }
}

function openDetail() {
  void router.replace({ name: 'appointment-detail', params: { id } })
}

onMounted(() => void load())
onUnmounted(() => {
  disposed = true
  generation += 1
  slotGeneration += 1
})
</script>

<template>
  <main class="page">
    <h1>{{ title }}</h1>

    <div v-if="loading" class="card state"><van-loading size="24">正在读取预约与排班</van-loading></div>
    <div v-else-if="error" class="card state">
      <p class="muted">{{ error }}</p>
      <button class="ghost" @click="load">重试</button>
    </div>
    <template v-else-if="appointment">
      <section class="card">
        <h2>{{ appointment.doctorName }} · {{ appointment.clinicName }}</h2>
        <div>原时间：{{ appointment.timeLabel }}</div>
        <p class="muted">{{ appointment.statusLabel }} · 北京时间</p>
      </section>

      <p class="rules">同一医生、同一院区内选择新时间。成功后重新进入待确认；新时段无法申请时保留原预约。候补不占名额，可选择原时段空位或其他可用时段。</p>

      <section v-if="recovering" class="card">
        <h2>原申请等待确认结果</h2>
        <p class="muted">已保存原申请，重试会复用原编号与时段。</p>
        <button class="primary" :disabled="submitting" @click="submit">
          {{ submitting ? '正在重试…' : '使用原申请重试' }}
        </button>
      </section>
      <template v-else-if="appointment.canReschedule">
        <section v-if="recommendations.length" class="card recommendations">
          <h2>最近可约 · 同医生</h2>
          <p class="muted">原时间之后最近的有空位时段，点一下直接选中。</p>
          <button
            v-for="slot in recommendations"
            :key="slot.id"
            class="rec-row"
            :class="{ selected: selected === slot.id }"
            :disabled="!slot.available || submitting"
            @click="chooseRecommendation(slot)"
          >{{ slot.label }} · 余 {{ slot.remaining ?? slot.capacity - slot.reserved }} 个名额</button>
        </section>

        <div class="days">
          <button
            v-for="day in days"
            :key="day.date"
            class="day"
            :class="{ active: date === day.date }"
            :disabled="submitting"
            @click="changeDate(day.date)"
          >{{ day.label }} {{ day.shortDate }}</button>
        </div>

        <div v-if="loadingSlots" class="card state"><van-loading size="20">查询空位</van-loading></div>
        <div v-else-if="slotError" class="card state">
          <p class="muted">{{ slotError }}</p>
          <button class="ghost" @click="loadSlots">重新查询</button>
        </div>
        <template v-else>
          <button
            v-for="slot in slots"
            :key="slot.id"
            class="card slot-row"
            :class="{ selected: selected === slot.id }"
            :disabled="!slot.available || submitting"
            @click="selectSlot(slot.id)"
          >{{ slot.label }} · {{ slot.available ? '可申请' : '暂无空位' }}</button>
          <div v-if="!slots.length" class="card state"><p class="muted">当天没有可选时段，请换一天。</p></div>
        </template>

        <button class="primary" :disabled="!selected || loadingSlots || !!slotError || submitting" @click="submit">
          {{ submitting ? '正在提交…' : appointment.status === 'waiting' ? '申请转为待确认预约' : '确认改期申请' }}
        </button>
      </template>
      <section v-else class="card">
        <p class="muted">当前预约状态已变化，无法申请改期或候补转预约。</p>
      </section>

      <p v-if="notice" class="error-box" role="alert">{{ notice }}</p>
    </template>

    <div class="secondary center">
      <button class="text-link" :disabled="submitting" @click="openDetail">返回预约详情</button>
    </div>
  </main>
</template>

<style scoped>
.state { text-align: center; }
.rules { background: #fff; border: 1px dashed var(--line); border-radius: 14px; padding: 12px 16px; font-size: 13px; color: var(--muted); line-height: 1.7; margin: 0; }
.days { display: flex; gap: 8px; overflow-x: auto; padding-bottom: 4px; }
.day { flex: none; border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 8px 14px; font-size: 13px; cursor: pointer; color: var(--ink); }
.day.active { background: var(--brand); border-color: var(--brand); color: #fff; font-weight: 600; }
.slot-row { width: 100%; text-align: left; font-size: 14px; cursor: pointer; color: var(--ink); }
.slot-row.selected { border-color: var(--brand); background: #eff6ff; }
.slot-row:disabled { opacity: 0.55; cursor: default; }
.recommendations h2 { margin: 0 0 4px; }
.recommendations .muted { margin: 0 0 10px; }
.rec-row { display: block; width: 100%; text-align: left; border: 1px solid var(--line); border-radius: 10px; background: #f0fdf4; padding: 10px 12px; font-size: 14px; cursor: pointer; color: var(--ink); margin-top: 8px; }
.rec-row.selected { border-color: var(--brand); background: #eff6ff; }
.rec-row:disabled { opacity: 0.55; cursor: default; }
.secondary { margin-top: 4px; }
.secondary.center { text-align: center; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 14px; cursor: pointer; padding: 4px 0; }
.text-link:disabled { opacity: 0.55; }
</style>
