<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, errorMessage, type Clinic, type Doctor } from '../api'
import { clinicPresentation, doctorPresentation, type DoctorPresentation } from '../patient-presentation'

const router = useRouter()

interface DoctorRow extends Doctor {
  presentation: DoctorPresentation
}

const loading = ref(true)
const refreshing = ref(false)
const error = ref('')
const clinics = ref<Clinic[]>([])
const clinic = ref<Clinic | null>(null)
const doctors = ref<DoctorRow[]>([])
const selectedFocus = ref('')
const focusOptions = computed(() => {
  const seen: string[] = []
  for (const doctor of doctors.value) {
    for (const item of doctor.focus || []) {
      if (item && !seen.includes(item)) seen.push(item)
    }
  }
  return seen
})
// 公开接口只返回启用且已发布医生，停用医生不会进入列表，筛选时已一并排除。
const filteredDoctors = computed(() => (selectedFocus.value ? doctors.value.filter((doctor) => (doctor.focus || []).includes(selectedFocus.value)) : doctors.value))
const isDemo = ref(true)
const clinicPickerShow = ref(false)
let requestId = 0

const clinicActions = computed(() => clinics.value.map((row) => ({ name: row.name })))

async function loadDoctors(clinicId?: string) {
  const id = ++requestId
  loading.value = true
  error.value = ''
  doctors.value = []
  selectedFocus.value = ''
  clinic.value = null
  clinics.value = []
  try {
    const list = await api.clinics()
    if (id !== requestId) return
    const index = Math.max(0, list.findIndex((item) => item.id === clinicId))
    const selected = list[index] || null
    clinics.value = list
    clinic.value = selected
    isDemo.value = selected ? clinicPresentation(selected).isDemo : true
    const rows = selected ? await api.doctors(selected.id) : []
    if (id !== requestId) return
    doctors.value = rows.map((row) => ({ ...row, presentation: doctorPresentation(row) }))
  } catch (cause) {
    if (id === requestId) error.value = errorMessage(cause)
  } finally {
    if (id === requestId) loading.value = false
  }
}

async function onRefresh() {
  await loadDoctors(clinic.value?.id)
  refreshing.value = false
}

function onClinicSelect(_action: unknown, index: number) {
  clinicPickerShow.value = false
  const selected = clinics.value[index]
  if (selected) void loadDoctors(selected.id)
}

function openDoctorDetail(doctor: DoctorRow) {
  if (loading.value) return
  void router.push({ name: 'doctor-detail', params: { id: doctor.id }, query: { clinicId: doctor.clinicId } })
}

function openSchedule(doctorId = '') {
  const clinicId = clinic.value?.id
  if (!clinicId) return
  void router.push({ path: '/schedule', query: { clinicId, ...(doctorId ? { doctorId } : {}) } })
}

function openClinic() {
  if (clinic.value && !loading.value) void router.push({ path: '/clinic', query: { clinicId: clinic.value.id } })
}

onMounted(() => void loadDoctors())
onUnmounted(() => { requestId += 1 })
</script>

<template>
  <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
    <main class="page">
      <div v-if="isDemo" class="demo-banner">
        <span class="demo-dot"></span><span>开发演示 · 院区与医生均为虚构数据，仅用于功能验证</span>
      </div>

      <section class="hero">
        <div class="eyebrow light">BAILIANKAI · CARE</div>
        <h1 class="hero-title">了解医生<br />从容安排到院</h1>
        <p class="hero-sub">查看团队介绍与近期排班，<br />为下一次面诊做好准备。</p>
        <div class="hero-tags"><span>百莲凯医疗美容</span><span>医生与排班</span></div>
      </section>

      <button class="ghost block-btn" @click="router.push('/appointments')">我的预约 · 查看申请进度</button>

      <section v-if="clinics.length && clinic" class="card">
        <button class="clinic-picker" :disabled="loading" @click="clinicPickerShow = true">
          <span class="grow">
            <span class="clinic-label">当前院区</span>
            <span class="clinic-name">{{ clinic.name }}</span>
          </span>
          <van-icon v-if="clinics.length > 1" name="arrow-down" />
        </button>
        <p class="clinic-address">{{ clinic.address }}</p>
        <button class="text-link" @click="openClinic">院区地图 · 就诊导航 · 机构资质 ›</button>
      </section>

      <div class="section-heading">
        <h2>医生团队</h2>
        <span v-if="!loading && !error" class="muted">{{ filteredDoctors.length }} 位医生</span>
      </div>

      <div v-if="!loading && !error && focusOptions.length" class="filter-row">
        <button class="filter-chip" :class="{ active: !selectedFocus }" @click="selectedFocus = ''">全部</button>
        <button v-for="item in focusOptions" :key="item" class="filter-chip" :class="{ active: selectedFocus === item }" @click="selectedFocus = item">{{ item }}</button>
      </div>

      <div v-if="loading" class="card state"><van-loading size="24">正在读取医生资料</van-loading></div>
      <div v-else-if="error" class="card state">
        <div class="state-title">暂时无法加载</div>
        <p class="muted">{{ error }}</p>
        <button class="ghost" @click="loadDoctors(clinic?.id)">重新加载</button>
      </div>
      <template v-else-if="filteredDoctors.length">
        <section v-for="doctor in filteredDoctors" :key="doctor.id" class="card">
          <div class="row doctor-head" @click="openDoctorDetail(doctor)">
            <img v-if="doctor.avatarUrl" class="avatar" :src="doctor.avatarUrl" alt="" />
            <span v-else class="avatar initials">{{ doctor.initials }}</span>
            <div class="grow">
              <div class="doctor-name">{{ doctor.name }}</div>
              <div class="muted">{{ doctor.title }}</div>
              <span v-if="doctor.presentation.isDemo" class="chip warning">演示资料</span>
            </div>
          </div>
          <p class="doctor-summary">{{ doctor.summary }}</p>
          <div class="focus-list">
            <van-tag v-for="item in doctor.focus" :key="item" type="primary" plain>{{ item }}</van-tag>
          </div>
          <div class="row doctor-action">
            <button class="text-link" @click="openDoctorDetail(doctor)">医生详情</button>
            <span class="grow"></span>
            <button class="ghost small" @click="openSchedule(doctor.id)">查看排班</button>
          </div>
        </section>
        <button class="primary" @click="openSchedule()">查看全部排班</button>
      </template>
      <div v-else class="card state">
        <div class="state-title">{{ doctors.length ? '暂无匹配医生' : '暂无医生资料' }}</div>
        <p class="muted">{{ doctors.length ? `没有擅长「${selectedFocus}」的医生，换个方向试试。` : '当前院区还未发布医生信息，请稍后再来查看。' }}</p>
        <button v-if="doctors.length" class="ghost" @click="selectedFocus = ''">查看全部医生</button>
      </div>

      <p class="footer-note">预约需工作人员确认，可在“我的预约”查看进度</p>

      <van-action-sheet v-model:show="clinicPickerShow" :actions="clinicActions" cancel-text="取消" @select="onClinicSelect" />
    </main>
  </van-pull-refresh>
</template>

<style scoped>
.demo-banner { display: flex; gap: 8px; align-items: flex-start; background: #fff6e5; border: 1px solid #f3d9a4; color: var(--amber); border-radius: 10px; padding: 10px 12px; font-size: 12px; line-height: 1.6; }
.demo-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--amber); margin-top: 5px; flex: none; }
.hero { border-radius: 16px; padding: 20px 18px; color: #fff; background: linear-gradient(135deg, #1763c6, #0d4c9c); }
.eyebrow { font-size: 11px; letter-spacing: 2px; font-weight: 700; }
.eyebrow.light { color: #bcd7f7; }
.hero-title { font-size: 24px; font-weight: 800; line-height: 1.35; margin: 10px 0 8px; color: #fff; }
.hero-sub { font-size: 13px; line-height: 1.7; color: #dbeafe; margin: 0; }
.hero-tags { display: flex; gap: 16px; margin-top: 14px; font-size: 12px; color: #dbeafe; }
.block-btn { width: 100%; color: var(--brand); font-weight: 600; }
.clinic-picker { width: 100%; border: 0; background: none; display: flex; align-items: center; text-align: left; cursor: pointer; padding: 0; color: var(--ink); }
.clinic-label { display: block; font-size: 12px; color: var(--muted); }
.clinic-name { display: block; font-size: 16px; font-weight: 700; margin-top: 2px; }
.clinic-address { margin: 8px 0 2px; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 13px; cursor: pointer; padding: 4px 0; }
.section-heading { display: flex; align-items: center; justify-content: space-between; }
.section-heading h2 { font-size: 17px; margin: 0; }
.state { text-align: center; }
.state-title { font-weight: 700; margin-bottom: 6px; }
.doctor-head { cursor: pointer; }
.avatar { width: 48px; height: 48px; border-radius: 50%; object-fit: cover; flex: none; }
.avatar.initials { background: var(--brand); color: #fff; display: inline-flex; align-items: center; justify-content: center; font-weight: 700; }
.doctor-name { font-size: 16px; font-weight: 700; }
.doctor-summary { line-height: 1.6; margin: 10px 0; }
.focus-list { display: flex; flex-wrap: wrap; gap: 6px; }
.filter-row { display: flex; flex-wrap: wrap; gap: 6px; margin: 10px 0 4px; }
.filter-chip { border: 1px solid var(--line); background: #fff; color: var(--muted); border-radius: 999px; padding: 5px 12px; font-size: 12px; cursor: pointer; }
.filter-chip.active { background: var(--brand); border-color: var(--brand); color: #fff; font-weight: 600; }
.doctor-action { margin-top: 10px; }
button.ghost.small { padding: 7px 12px; font-size: 13px; }
.footer-note { text-align: center; color: var(--muted); font-size: 12px; margin: 4px 0 0; }
</style>
