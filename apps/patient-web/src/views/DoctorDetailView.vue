<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, errorMessage, type Clinic, type Doctor } from '../api'
import { doctorPresentation, previewCredentials, type Credential } from '../patient-presentation'

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

const clinicId = String(route.query.clinicId || '')
const doctorId = String(route.params.id || '')

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
</style>
