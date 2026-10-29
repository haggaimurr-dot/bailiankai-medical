<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, errorMessage, type Clinic, type Doctor } from '../api'
import { patientApi } from '../patient'
import { doctorPresentation, previewCredentials, type Credential } from '../patient-presentation'
import { useSessionStore } from '../stores/session'
import type { components } from '../../../../packages/contracts/src/schema'

type DoctorReviewPublic = components['schemas']['DoctorReviewPublic']
type DoctorReview = components['schemas']['DoctorReview']
type ReviewableAppointment = components['schemas']['ReviewableAppointment']
type CalendarDay = { date: string; label: string; weekday: string; status: 'loading' | 'open' | 'full' | 'none' }

const WEEKDAYS = ['日', '一', '二', '三', '四', '五', '六']
function shanghaiToday(): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date())
}
function addDays(date: string, days: number): string {
  const [y, m, d] = date.split('-').map(Number)
  const next = new Date(Date.UTC(y, m - 1, d + days))
  return next.toISOString().slice(0, 10)
}
function weekdayOf(date: string): string {
  return WEEKDAYS[new Date(`${date}T12:00:00+08:00`).getDay()]
}

const route = useRoute()
const router = useRouter()

const services = [
  { number: '01', title: '面诊沟通', description: '围绕你的皮肤困扰与个人需求，沟通既往就诊、治疗及用药信息。' },
  { number: '02', title: '个人方案', description: '面诊后，可在预约详情查看医生已发布的个人方案与医嘱。' },
  { number: '03', title: '治疗与随访', description: '治疗后，按随访任务记录恢复感受与选填照片，查看医护回评。' },
]
const notes = [
  '选择出诊日期和可用时段，填写联系人、手机号及希望沟通的问题。',
  '提交预约后需要工作人员确认，请在“我的预约”查看确认结果与到院时间。',
  '到院前整理既往就诊、治疗及用药信息；预约后的面诊照片为选填项。',
  '具体治疗安排以医生面诊和发布的个人方案为准，预约面诊不等于确认治疗。',
]

const session = useSessionStore()
const clinicId = String(route.query.clinicId || '')
const doctorId = String(route.params.id || '')
const calendarDays = ref<CalendarDay[]>([])
const publicReviews = ref<DoctorReviewPublic[]>([])
const myReviews = ref<DoctorReview[]>([])
const reviewable = ref<ReviewableAppointment[]>([])
const reviewForm = ref({ appointmentId: '', rating: 5, content: '' })
const reviewBusy = ref(false)
const reviewMessage = ref('')
const reviewStatusText: Record<string, string> = { pending: '待审核', approved: '已通过', rejected: '已驳回' }

async function loadCalendar(clinic: Clinic, doctor: Doctor) {
  const today = shanghaiToday()
  calendarDays.value = Array.from({ length: 14 }, (_, index) => {
    const date = addDays(today, index)
    return { date, label: date.slice(5), weekday: weekdayOf(date), status: 'loading' as const }
  })
  const results = await Promise.allSettled(calendarDays.value.map((day) => api.slots({ clinicId: clinic.id, doctorId: doctor.id, date: day.date })))
  results.forEach((result, index) => {
    if (result.status !== 'fulfilled') { calendarDays.value[index].status = 'none'; return }
    const slots = result.value
    calendarDays.value[index].status = slots.length === 0 ? 'none' : slots.some((slot) => slot.remaining > 0) ? 'open' : 'full'
  })
}

async function loadReviews() {
  try {
    publicReviews.value = await patientApi.doctorReviews(doctorId)
  } catch { publicReviews.value = [] }
  if (!session.loggedIn) { myReviews.value = []; reviewable.value = []; return }
  try {
    const [mine, canReview] = await Promise.all([patientApi.myReviews(doctorId), patientApi.reviewableAppointments(doctorId)])
    myReviews.value = mine
    reviewable.value = canReview
    if (!reviewForm.value.appointmentId && canReview.length) reviewForm.value.appointmentId = canReview[0].appointmentId
  } catch { myReviews.value = []; reviewable.value = [] }
}

async function submitReview() {
  if (reviewBusy.value || !reviewForm.value.appointmentId || !reviewForm.value.content.trim()) return
  reviewBusy.value = true
  reviewMessage.value = ''
  try {
    await patientApi.createReview(doctorId, { appointmentId: reviewForm.value.appointmentId, rating: reviewForm.value.rating, content: reviewForm.value.content.trim() })
    reviewForm.value.content = ''
    reviewMessage.value = '评价已提交，待审核通过后公开显示。'
    await loadReviews()
  } catch (cause) {
    reviewMessage.value = errorMessage(cause)
  } finally {
    reviewBusy.value = false
  }
}

const loading = ref(true)
const refreshing = ref(false)
const error = ref('')
const doctor = ref<Doctor | null>(null)
const clinic = ref<Clinic | null>(null)
const isDemo = ref(true)
const credentials = ref<Credential[]>([])
const reviewedDate = ref('')
let requestId = 0

async function load() {
  const id = ++requestId
  loading.value = true
  error.value = ''
  doctor.value = null
  clinic.value = null
  credentials.value = []
  reviewedDate.value = ''
  try {
    if (!clinicId || !doctorId) throw new Error('医生入口信息不完整，请返回医生列表重新选择。')
    const [clinics, doctors] = await Promise.all([api.clinics(), api.doctors(clinicId)])
    if (id !== requestId) return
    const foundClinic = clinics.find((item) => item.id === clinicId)
    const foundDoctor = doctors.find((item) => item.id === doctorId && item.clinicId === clinicId)
    if (!foundClinic || !foundDoctor) throw new Error('这位医生的资料暂不可用，请返回医生列表重新选择。')
    const presentation = doctorPresentation(foundDoctor)
    const reviewedAt = Date.parse(foundDoctor.qualificationReviewedAt || '')
    clinic.value = foundClinic
    doctor.value = foundDoctor
    isDemo.value = presentation.isDemo
    credentials.value = presentation.credentials
    void loadCalendar(foundClinic, foundDoctor)
    void loadReviews()
    reviewedDate.value = Number.isFinite(reviewedAt)
      ? new Date(reviewedAt + 8 * 3600000).toISOString().slice(0, 10)
      : ''
  } catch (cause) {
    if (id === requestId) error.value = errorMessage(cause)
  } finally {
    if (id === requestId) loading.value = false
  }
}

async function onRefresh() {
  await load()
  refreshing.value = false
}

function openSchedule() {
  if (loading.value || error.value || !clinic.value || !doctor.value) return
  void router.push({ path: '/schedule', query: { clinicId: clinic.value.id, doctorId: doctor.value.id } })
}

function openClinic() {
  if (clinic.value && !loading.value && !error.value) {
    void router.push({ path: '/clinic', query: { clinicId: clinic.value.id } })
  }
}

function preview(index: number) {
  previewCredentials(credentials.value, index)
}

onMounted(() => void load())
onUnmounted(() => { requestId += 1 })
</script>

<template>
  <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
    <main class="page">
      <div v-if="loading" class="card state"><van-loading size="24">正在读取医生资料</van-loading></div>
      <div v-else-if="error" class="card state">
        <div class="state-title">暂时无法查看医生资料</div>
        <p class="muted">{{ error }}</p>
        <button class="ghost" @click="load">重新加载</button>
        <div><button class="text-link" @click="router.push('/doctors')">返回医生列表</button></div>
      </div>
      <template v-else-if="doctor && clinic">
        <div v-if="isDemo" class="demo-banner">
          <span class="demo-dot"></span><span>开发演示 · 医生与院区为虚构资料，仅用于功能验证。</span>
        </div>

        <section class="card">
          <div class="eyebrow">BAILIANKAI · DOCTOR</div>
          <div class="row profile-head">
            <img v-if="doctor.avatarUrl" class="avatar" :src="doctor.avatarUrl" alt="" />
            <span v-else class="avatar initials">{{ doctor.initials }}</span>
            <div class="grow">
              <div class="doctor-name">{{ doctor.name }}</div>
              <div class="muted">{{ doctor.title }}</div>
              <div class="profile-clinic">{{ clinic.name }}</div>
            </div>
          </div>
          <p class="summary">{{ doctor.summary || '医生简介更新中。' }}</p>
        </section>

        <div class="section-heading">
          <h2>执业资质</h2>
          <span v-if="isDemo" class="muted">演示证照样例</span>
        </div>
        <p v-if="reviewedDate" class="muted reviewed-line">{{ isDemo ? '演示核验' : '院方已核验' }} · {{ reviewedDate }}（北京时间）</p>
        <section class="card">
          <p v-if="!credentials.length" class="muted">执业资质资料暂未公布。</p>
          <div v-for="(item, index) in credentials" :key="index" class="credential">
            <div>{{ item.name }}</div>
            <p v-if="item.number" class="muted">证照编号：{{ item.number }}</p>
            <p v-if="item.issuer" class="muted">发证机构：{{ item.issuer }}</p>
            <img v-if="item.imageUrl" class="credential-image" :src="item.imageUrl" alt="证照图片" @click="preview(index)" />
            <p v-if="item.imageUrl" class="muted">点击查看证照原图</p>
          </div>
        </section>

        <div class="section-heading"><h2>专业方向</h2><span class="muted">了解关注领域</span></div>
        <section class="card">
          <div v-if="doctor.focus.length" class="focus-list">
            <span v-for="item in doctor.focus" :key="item" class="chip">{{ item }}</span>
          </div>
          <p v-else class="muted">专业方向资料更新中。</p>
        </section>

        <div class="section-heading"><h2>服务介绍</h2><span class="muted">从沟通到恢复</span></div>
        <section v-if="doctor.serviceIntroduction" class="card service-intro">{{ doctor.serviceIntroduction }}</section>
        <section v-else class="card">
          <div v-for="row in services" :key="row.number" class="service-row">
            <span class="service-number">{{ row.number }}</span>
            <div><div class="service-title">{{ row.title }}</div><p class="muted">{{ row.description }}</p></div>
          </div>
          <p class="muted service-note">以上为院区通用服务流程，个人安排以面诊沟通为准。</p>
        </section>

        <div class="section-heading"><h2>面诊说明</h2><span class="muted">提前准备，从容到院</span></div>
        <section class="card">
          <div v-for="note in notes" :key="note" class="note-row"><span>·</span><p>{{ note }}</p></div>
        </section>

        <section class="card">
          <span class="clinic-label">出诊院区</span>
          <div class="clinic-name">{{ clinic.name }}</div>
          <p class="clinic-address">{{ clinic.address }}</p>
          <button class="text-link" @click="openClinic">院区地图 · 就诊导航 · 机构资质 ›</button>
        </section>

        <div class="section-heading"><h2>出诊日历速览</h2><span class="muted">未来 14 天 · 北京时间</span></div>
        <section class="card">
          <div class="calendar-grid">
            <button v-for="day in calendarDays" :key="day.date" class="calendar-day" :class="day.status" :disabled="day.status !== 'open'" @click="openSchedule">
              <span class="calendar-date">{{ day.label }}</span>
              <span class="calendar-weekday">周{{ day.weekday }}</span>
              <span class="calendar-status">{{ day.status === 'loading' ? '查询中' : day.status === 'open' ? '有号' : day.status === 'full' ? '约满' : '无排班' }}</span>
            </button>
          </div>
          <p class="muted calendar-note">点击有号日期可进入排班预约；日历仅作速览，余号以排班页为准。</p>
        </section>

        <div class="section-heading"><h2>患者评价</h2><span class="muted">仅展示已审核通过的评价</span></div>
        <section class="card">
          <p v-if="!publicReviews.length" class="muted">暂无已审核评价。</p>
          <div v-for="review in publicReviews" :key="review.id" class="review-row">
            <div class="review-head"><span class="review-name">{{ review.displayName || '匿名患者' }}</span><span class="review-stars">{{ '★'.repeat(review.rating) }}</span></div>
            <p class="muted">{{ review.content }}</p>
            <p class="muted review-date">{{ review.createdAt.slice(0, 10) }}</p>
          </div>
        </section>
        <section v-if="session.loggedIn" class="card">
          <h3 class="review-form-title">写评价</h3>
          <p class="muted">仅已治疗的就诊可评，每单一次；提交后待审核通过才公开显示。</p>
          <div v-if="myReviews.length" class="my-reviews">
            <div v-for="review in myReviews" :key="review.id" class="review-row">
              <div class="review-head"><span class="review-name">{{ reviewStatusText[review.status] || review.status }}</span><span class="review-stars">{{ '★'.repeat(review.rating) }}</span></div>
              <p class="muted">{{ review.content }}</p>
              <p v-if="review.status === 'rejected' && review.reviewNote" class="muted">驳回原因：{{ review.reviewNote }}</p>
            </div>
          </div>
          <div v-if="reviewable.length" class="review-form">
            <label class="form-label">选择就诊
              <select v-model="reviewForm.appointmentId" class="form-select">
                <option v-for="item in reviewable" :key="item.appointmentId" :value="item.appointmentId">就诊 {{ item.startsAt.slice(0, 10) }} {{ item.startsAt.slice(11, 16) }}</option>
              </select>
            </label>
            <label class="form-label">评分
              <select v-model.number="reviewForm.rating" class="form-select">
                <option v-for="n in 5" :key="n" :value="n">{{ n }} 分</option>
              </select>
            </label>
            <label class="form-label">评价内容
              <textarea v-model="reviewForm.content" class="form-textarea" rows="3" maxlength="1000" placeholder="分享你的就诊感受（1–1000 字）" />
            </label>
            <button class="primary" :disabled="reviewBusy || !reviewForm.content.trim()" @click="submitReview">提交评价</button>
            <p v-if="reviewMessage" class="muted">{{ reviewMessage }}</p>
          </div>
          <p v-else class="muted">暂无可评价的就诊。</p>
        </section>
        <section v-else class="card">
          <p class="muted">登录后可对已治疗的就诊写评价，每单一次。</p>
        </section>

        <section class="booking-bar">
          <div class="row">
            <div class="grow">
              <div class="booking-title">预约 {{ doctor.name }}</div>
              <span class="muted">查看未来 7 天排班 · 北京时间</span>
            </div>
          </div>
          <button class="primary" @click="openSchedule">查看排班 · 预约面诊 <van-icon name="arrow" size="14" /></button>
        </section>
      </template>
    </main>
  </van-pull-refresh>
</template>

<style scoped>
.state { text-align: center; }
.state-title { font-weight: 700; margin-bottom: 6px; }
.demo-banner { display: flex; gap: 8px; align-items: flex-start; background: #fff6e5; border: 1px solid #f3d9a4; color: var(--amber); border-radius: 10px; padding: 10px 12px; font-size: 12px; line-height: 1.6; }
.demo-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--amber); margin-top: 5px; flex: none; }
.eyebrow { font-size: 11px; letter-spacing: 2px; color: var(--brand); font-weight: 700; margin-bottom: 10px; }
.profile-head { align-items: flex-start; }
.avatar { width: 56px; height: 56px; border-radius: 50%; object-fit: cover; flex: none; }
.avatar.initials { background: var(--brand); color: #fff; display: inline-flex; align-items: center; justify-content: center; font-weight: 700; font-size: 18px; }
.doctor-name { font-size: 19px; font-weight: 700; }
.profile-clinic { color: var(--brand); font-size: 13px; margin-top: 2px; }
.summary { line-height: 1.7; margin: 12px 0 0; }
.section-heading { display: flex; align-items: center; justify-content: space-between; }
.section-heading h2 { font-size: 16px; margin: 0; }
.reviewed-line { margin: 0; }
.credential { padding: 8px 0; border-top: 1px solid var(--bg); }
.credential:first-of-type { border-top: 0; padding-top: 0; }
.credential p { margin: 2px 0; }
.credential-image { max-width: 100%; max-height: 180px; object-fit: contain; margin-top: 6px; cursor: pointer; }
.focus-list { display: flex; flex-wrap: wrap; gap: 6px; }
.service-intro { line-height: 1.7; }
.service-row { display: flex; gap: 12px; padding: 8px 0; }
.service-number { color: var(--brand); font-weight: 800; }
.service-title { font-weight: 600; }
.service-row p { margin: 2px 0 0; }
.service-note { border-top: 1px solid var(--bg); padding-top: 8px; }
.note-row { display: flex; gap: 8px; padding: 4px 0; }
.note-row span { color: var(--brand); font-weight: 800; }
.note-row p { margin: 0; line-height: 1.6; }
.clinic-label { font-size: 12px; color: var(--muted); }
.clinic-name { font-size: 16px; font-weight: 700; margin-top: 2px; }
.clinic-address { margin: 6px 0 2px; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 13px; cursor: pointer; padding: 4px 0; }
.booking-bar { background: #fff; border: 1px solid var(--line); border-radius: 14px; padding: 14px 16px; display: flex; flex-direction: column; gap: 10px; }
.booking-title { font-weight: 700; }
.calendar-grid { display: grid; grid-template-columns: repeat(7, 1fr); gap: 6px; }
.calendar-day { border: 1px solid var(--line); background: #fff; border-radius: 10px; padding: 8px 2px; display: flex; flex-direction: column; align-items: center; gap: 2px; cursor: pointer; color: var(--ink); }
.calendar-day:disabled { cursor: default; opacity: 0.75; }
.calendar-day.open { border-color: var(--teal); }
.calendar-day.open .calendar-status { color: var(--teal); font-weight: 700; }
.calendar-date { font-size: 13px; font-weight: 600; }
.calendar-weekday { font-size: 11px; color: var(--muted); }
.calendar-status { font-size: 11px; color: var(--muted); }
.calendar-note { margin: 10px 0 0; }
.review-row { padding: 10px 0; border-top: 1px solid var(--bg); }
.review-row:first-of-type { border-top: 0; padding-top: 2px; }
.review-head { display: flex; justify-content: space-between; align-items: center; }
.review-name { font-weight: 600; font-size: 14px; }
.review-stars { color: #d97706; letter-spacing: 1px; }
.review-row p { margin: 6px 0 0; line-height: 1.7; }
.review-date { font-size: 12px; }
.review-form-title { font-size: 15px; margin: 0 0 6px; }
.my-reviews { margin-bottom: 12px; }
.review-form { display: flex; flex-direction: column; gap: 10px; margin-top: 12px; }
.form-label { font-size: 13px; font-weight: 600; display: flex; flex-direction: column; gap: 6px; }
.form-select, .form-textarea { width: 100%; border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; font-size: 14px; background: #fff; color: var(--ink); box-sizing: border-box; }
</style>
