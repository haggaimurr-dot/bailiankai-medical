<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, errorMessage, type Clinic, type Doctor, type Slot } from '../api'
import { clinicTime, upcomingDays, type DayOption } from '../utils/date'

const route = useRoute()
const router = useRouter()

interface SlotView extends Slot {
  timeRange: string
  doctorName: string
  remaining: number
  stateLabel: string
  available: boolean
  waitlistAvailable: boolean
}

const initializing = ref(true)
const loading = ref(true)
const refreshing = ref(false)
const initialError = ref('')
const error = ref('')
const clinics = ref<Clinic[]>([])
const clinic = ref<Clinic | null>(null)
const doctors = ref<Doctor[]>([])
const doctorId = ref('')
const days = ref<DayOption[]>([])
const date = ref('')
const slots = ref<SlotView[]>([])
const availableCount = ref(0)
const clinicPickerShow = ref(false)

let allDoctors: Doctor[] = []
let requestId = 0
let initializationId = 0
let initialSelection: { clinicId?: string; doctorId?: string } = {
  clinicId: String(route.query.clinicId || '') || undefined,
  doctorId: String(route.query.doctorId || '') || undefined,
}

const clinicActions = computed(() => clinics.value.map((row) => ({ name: row.name })))

async function initialize(options: { clinicId?: string; doctorId?: string } = {}) {
  initialSelection = options
  const initId = ++initializationId
  requestId += 1
  initializing.value = true
  loading.value = true
  initialError.value = ''
  error.value = ''
  slots.value = []
  clinic.value = null
  doctors.value = []
  doctorId.value = ''
  availableCount.value = 0
  try {
    const [clinicList, fetchedDoctors] = await Promise.all([api.clinics(), api.doctors()])
    if (initId !== initializationId) return
    allDoctors = fetchedDoctors
    const index = Math.max(0, clinicList.findIndex((item) => item.id === options.clinicId))
    const selected = clinicList[index] || null
    const clinicDoctors = allDoctors.filter((doctor) => doctor.clinicId === selected?.id)
    if (options.clinicId && selected?.id !== options.clinicId) throw new Error('所选院区资料暂不可用，请返回重新选择。')
    if (options.doctorId && !clinicDoctors.some((doctor) => doctor.id === options.doctorId)) throw new Error('所选医生资料暂不可用，请返回医生列表重新选择。')
    clinics.value = clinicList
    clinic.value = selected
    doctors.value = clinicDoctors
    doctorId.value = clinicDoctors.some((doctor) => doctor.id === options.doctorId) ? options.doctorId! : ''
    const dayOptions = upcomingDays()
    days.value = dayOptions
    date.value = dayOptions.some((day) => day.date === date.value) ? date.value : dayOptions[0].date
    initializing.value = false
    await loadSlots()
  } catch (cause) {
    if (initId === initializationId) {
      initialError.value = errorMessage(cause)
      initializing.value = false
      loading.value = false
    }
  }
}

async function loadSlots() {
  const id = ++requestId
  loading.value = true
  error.value = ''
  slots.value = []
  availableCount.value = 0
  if (!clinic.value) {
    loading.value = false
    return
  }
  try {
    const rawSlots = await api.slots({ clinicId: clinic.value.id, doctorId: doctorId.value, date: date.value })
    if (id !== requestId) return
    const views = rawSlots.map((slot): SlotView => {
      const doctor = allDoctors.find((item) => item.id === slot.doctorId)
      // Remaining comes from the API (contract field); fall back to the local
      // derivation only if an older cached response lacks it.
      const remaining = Number.isFinite(slot.remaining) ? slot.remaining : Math.max(0, slot.capacity - slot.reserved)
      const started = new Date(slot.startsAt).getTime() <= Date.now()
      const available = slot.status === 'open' && remaining > 0 && !started
      return {
        ...slot,
        timeRange: `${clinicTime(slot.startsAt)} — ${clinicTime(slot.endsAt)}`,
        doctorName: doctor?.name || '医生资料更新中',
        remaining,
        stateLabel: slot.status === 'closed' ? '已停诊' : started ? '已开始' : remaining === 0 ? '已满' : '可用',
        available,
        waitlistAvailable: slot.status === 'open' && slot.capacity > 0 && remaining === 0 && !started,
      }
    })
    views.sort((a, b) => a.startsAt.localeCompare(b.startsAt) || a.doctorName.localeCompare(b.doctorName))
    slots.value = views
    availableCount.value = views.filter((slot) => slot.available).length
  } catch (cause) {
    if (id === requestId) error.value = errorMessage(cause)
  } finally {
    if (id === requestId) loading.value = false
  }
}

function retry() {
  if (initialError.value) void initialize(initialSelection)
  else void loadSlots()
}

async function onRefresh() {
  if (initialError.value) await initialize(initialSelection)
  else await initialize({ clinicId: clinic.value?.id, doctorId: doctorId.value })
  refreshing.value = false
}

function onClinicSelect(_action: unknown, index: number) {
  clinicPickerShow.value = false
  const selected = clinics.value[index]
  if (!selected || selected.id === clinic.value?.id) return
  clinic.value = selected
  doctorId.value = ''
  doctors.value = allDoctors.filter((doctor) => doctor.clinicId === selected.id)
  void loadSlots()
}

function onDoctorChange(id: string) {
  if (id === doctorId.value) return
  doctorId.value = id
  void loadSlots()
}

function onDateChange(next: string) {
  if (!next || next === date.value) return
  date.value = next
  void loadSlots()
}

function bookSlot(slot: SlotView) {
  if ((!slot.available && !slot.waitlistAvailable) || loading.value) return
  void router.push({
    path: '/booking',
    query: {
      slotId: slot.id,
      clinicId: slot.clinicId,
      doctorId: slot.doctorId,
      date: date.value,
      ...(slot.waitlistAvailable ? { waitlist: '1' } : {}),
    },
  })
}

onMounted(() => void initialize(initialSelection))
onUnmounted(() => { initializationId += 1; requestId += 1 })
</script>

<template>
  <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
    <main class="page">
      <div class="demo-banner">
        <span class="demo-dot"></span><span>开发演示 · 预约提交后须由工作人员确认</span>
      </div>
      <div>
        <div class="eyebrow">SCHEDULE</div>
        <h1>找到合适的时间</h1>
        <p class="lede">查看未来 7 天的出诊安排<span class="tz">以下时间均为北京时间</span></p>
      </div>

      <div v-if="initializing" class="card state"><van-loading size="24">正在读取排班信息</van-loading></div>
      <div v-else-if="initialError" class="card state">
        <div class="state-title">暂时无法加载</div>
        <p class="muted">{{ initialError }}</p>
        <button class="ghost" @click="retry">重新加载</button>
      </div>
      <template v-else-if="clinic">
        <section class="card">
          <button class="clinic-picker" @click="clinicPickerShow = true">
            <span class="grow">
              <span class="clinic-label">出诊院区</span>
              <span class="clinic-name">{{ clinic.name }}</span>
            </span>
            <van-icon v-if="clinics.length > 1" name="arrow-down" />
          </button>
          <div class="divider"></div>
          <div class="filter-label">选择医生</div>
          <div class="doctor-options">
            <button class="doctor-chip" :class="{ active: !doctorId }" @click="onDoctorChange('')">全部医生</button>
            <button
              v-for="doctor in doctors"
              :key="doctor.id"
              class="doctor-chip"
              :class="{ active: doctorId === doctor.id }"
              @click="onDoctorChange(doctor.id)"
            >{{ doctor.name }}</button>
          </div>
        </section>

        <div class="section-heading"><h2>选择日期</h2><span class="muted">未来 7 天</span></div>
        <div class="days">
          <button
            v-for="day in days"
            :key="day.date"
            class="day"
            :class="{ active: date === day.date }"
            @click="onDateChange(day.date)"
          >
            <span class="day-label">{{ day.label }}</span>
            <span class="day-date">{{ day.shortDate }}</span>
          </button>
        </div>

        <div class="section-heading">
          <h2>{{ date }} 排班</h2>
          <span v-if="!loading && !error" class="muted">{{ availableCount }} 个可用时段</span>
        </div>
        <div v-if="loading" class="card state"><van-loading size="24">正在查询时段</van-loading></div>
        <div v-else-if="error" class="card state">
          <div class="state-title">时段加载失败</div>
          <p class="muted">{{ error }}</p>
          <button class="ghost" @click="retry">重新查询</button>
        </div>
        <template v-else-if="slots.length">
          <section v-for="slot in slots" :key="slot.id" class="card slot-card" :class="{ unavailable: !slot.available }">
            <div class="row">
              <div class="grow">
                <div class="slot-time">{{ slot.timeRange }}</div>
                <div class="muted">{{ slot.doctorName }}</div>
              </div>
              <div class="slot-status">
                <span class="chip" :class="{ success: slot.available }">{{ slot.stateLabel }}</span>
                <div v-if="slot.available" class="remaining">余 {{ slot.remaining }} 个名额</div>
              </div>
            </div>
            <div v-if="slot.available || slot.waitlistAvailable" class="slot-action">
              <button class="ghost small" @click="bookSlot(slot)">{{ slot.waitlistAvailable ? '登记候补' : '申请此时段' }}</button>
            </div>
          </section>
        </template>
        <div v-else class="card state">
          <div class="state-title">当天暂无可查询排班</div>
          <p class="muted">试试其他日期或医生，已开始的时段不再展示。</p>
        </div>

        <p class="refresh-tip">下拉可刷新最新排班</p>
        <section class="card">
          <span class="clinic-label">院区地址</span>
          <p class="clinic-address">{{ clinic.address }}</p>
        </section>
      </template>
      <div v-else class="card state">
        <div class="state-title">暂无院区信息</div>
        <p class="muted">院区资料发布后即可查询排班。</p>
      </div>

      <div class="center"><button class="text-link" @click="router.push('/appointments')">查看我的预约</button></div>
      <p class="footer-note">演示数据仅用于功能验证</p>

      <van-action-sheet v-model:show="clinicPickerShow" :actions="clinicActions" cancel-text="取消" @select="onClinicSelect" />
    </main>
  </van-pull-refresh>
</template>

<style scoped>
.demo-banner { display: flex; gap: 8px; align-items: flex-start; background: #fff6e5; border: 1px solid #f3d9a4; color: var(--amber); border-radius: 10px; padding: 10px 12px; font-size: 12px; line-height: 1.6; }
.demo-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--amber); margin-top: 5px; flex: none; }
.eyebrow { font-size: 11px; letter-spacing: 2px; color: var(--brand); font-weight: 700; }
.tz { display: block; font-size: 12px; margin-top: 2px; }
.state { text-align: center; }
.state-title { font-weight: 700; margin-bottom: 6px; }
.clinic-picker { width: 100%; border: 0; background: none; display: flex; align-items: center; text-align: left; cursor: pointer; padding: 0; color: var(--ink); }
.clinic-label { display: block; font-size: 12px; color: var(--muted); }
.clinic-name { display: block; font-size: 16px; font-weight: 700; margin-top: 2px; }
.clinic-address { margin: 6px 0 0; }
.divider { border-top: 1px solid var(--bg); margin: 12px 0; }
.filter-label { font-size: 13px; color: var(--muted); margin-bottom: 8px; }
.doctor-options { display: flex; flex-wrap: wrap; gap: 8px; }
.doctor-chip { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 7px 14px; font-size: 13px; cursor: pointer; color: var(--ink); }
.doctor-chip.active { background: var(--brand); border-color: var(--brand); color: #fff; font-weight: 600; }
.section-heading { display: flex; align-items: center; justify-content: space-between; }
.section-heading h2 { font-size: 16px; margin: 0; }
.days { display: flex; gap: 8px; overflow-x: auto; padding-bottom: 4px; }
.day { flex: none; width: 58px; border: 1px solid var(--line); background: #fff; border-radius: 12px; padding: 8px 0; display: flex; flex-direction: column; align-items: center; gap: 2px; cursor: pointer; color: var(--ink); }
.day.active { border-color: var(--brand); background: #eff6ff; }
.day-label { font-size: 13px; font-weight: 600; }
.day-date { font-size: 11px; color: var(--muted); }
.slot-card.unavailable { opacity: 0.55; }
.slot-time { font-size: 16px; font-weight: 700; }
.slot-status { text-align: right; }
.remaining { font-size: 12px; color: var(--green); margin-top: 4px; }
.slot-action { margin-top: 10px; text-align: right; }
button.ghost.small { padding: 7px 14px; font-size: 13px; color: var(--brand); border-color: var(--brand); }
.refresh-tip { text-align: center; color: var(--muted); font-size: 12px; margin: 0; }
.center { text-align: center; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 14px; cursor: pointer; padding: 4px 0; }
.footer-note { text-align: center; color: var(--muted); font-size: 12px; margin: 0; }
</style>
