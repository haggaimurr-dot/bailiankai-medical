<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, errorMessage, type Clinic, type Doctor, type KnowledgeArticle } from '../api'
import { clinicPresentation, doctorPresentation, knowledge, type ClinicPresentation, type DoctorPresentation } from '../patient-presentation'

const router = useRouter()

interface DoctorRow extends Doctor {
  presentation: DoctorPresentation
}

const steps = [
  { number: '01', title: '预约面诊', description: '选择医生与时段，提交预约后等待确认。' },
  { number: '02', title: '到院沟通', description: '与医生沟通皮肤困扰、既往经历和个人需求。' },
  { number: '03', title: '查看方案', description: '面诊后，在预约详情查看医生发布的个人方案。' },
  { number: '04', title: '治疗与随访', description: '治疗后按任务填写恢复反馈，查看医护回评。' },
]

const loading = ref(true)
const refreshing = ref(false)
const error = ref('')
const clinics = ref<Clinic[]>([])
const clinic = ref<Clinic | null>(null)
const doctors = ref<DoctorRow[]>([])
const presentation = ref<ClinicPresentation | null>(null)
const articles = ref<KnowledgeArticle[]>([])
const knowledgeError = ref('')
const clinicPickerShow = ref(false)
// Brand logo: management-configured URL first; bundled in-repo asset as fallback
// (bundled file ships with every build, so a deploy can never wipe the logo).
const logoFailed = ref(false)
const logoGone = ref(false)
const useBundledLogo = computed(() => !presentation.value?.logoUrl || logoFailed.value)
const brandLogoSrc = computed(() => (useBundledLogo.value ? '/brand-logo.png' : (presentation.value?.logoUrl as string)))
function onBrandLogoError() {
  if (useBundledLogo.value) logoGone.value = true
  else logoFailed.value = true
}
// Doctor avatar: management-configured URL first; bundled in-repo asset as fallback
// (bundled file ships with every build, so a deploy can never wipe the avatar).
const avatarFailed = ref<Record<string, boolean>>({})
const avatarGone = ref<Record<string, boolean>>({})
const useBundledAvatar = (doctor: DoctorRow) => !doctor.avatarUrl || !!avatarFailed.value[doctor.id]
const doctorAvatarSrc = (doctor: DoctorRow) => (useBundledAvatar(doctor) ? '/doctor-avatar.jpg' : (doctor.avatarUrl as string))
const avatarVisible = (doctor: DoctorRow) => !!doctor.avatarUrl && !avatarGone.value[doctor.id]
function onAvatarError(doctor: DoctorRow) {
  if (useBundledAvatar(doctor)) avatarGone.value[doctor.id] = true
  else avatarFailed.value[doctor.id] = true
}
let requestId = 0

// Doctor carousel: one full-width card per snap stop. The hint, dots and
// arrows only render when there is more than one doctor to switch between.
const carouselRef = ref<HTMLElement | null>(null)
const carouselIndex = ref(0)
function onCarouselScroll() {
  const el = carouselRef.value
  if (!el || !el.clientWidth) return
  carouselIndex.value = Math.min(doctors.value.length - 1, Math.max(0, Math.round(el.scrollLeft / el.clientWidth)))
}
function scrollToDoctor(index: number) {
  const el = carouselRef.value
  if (!el) return
  const target = Math.min(doctors.value.length - 1, Math.max(0, index))
  carouselIndex.value = target
  el.scrollTo({ left: target * el.clientWidth, behavior: 'smooth' })
}

const clinicActions = computed(() => clinics.value.map((row) => ({ name: row.name })))

async function load(clinicId?: string) {
  const id = ++requestId
  loading.value = true
  error.value = ''
  logoFailed.value = false
  logoGone.value = false
  avatarFailed.value = {}
  avatarGone.value = {}
  doctors.value = []
  carouselIndex.value = 0
  clinics.value = []
  clinic.value = null
  presentation.value = null
  articles.value = []
  knowledgeError.value = ''
  try {
    const list = await api.clinics()
    if (id !== requestId) return
    const index = Math.max(0, list.findIndex((item) => item.id === clinicId))
    const selected = list[index] || null
    clinics.value = list
    clinic.value = selected
    presentation.value = selected ? clinicPresentation(selected) : null
    if (!selected) return
    const [doctorResult, articleResult] = await Promise.allSettled([
      api.doctors(selected.id),
      knowledge.articles(selected.id, '', 1, 2, clinicPresentation(selected).isDemo),
    ])
    if (id !== requestId) return
    doctors.value = doctorResult.status === 'fulfilled'
      ? doctorResult.value.map((row) => ({ ...row, presentation: doctorPresentation(row) }))
      : []
    carouselIndex.value = 0
    carouselRef.value?.scrollTo({ left: 0 })
    error.value = doctorResult.status === 'rejected' ? errorMessage(doctorResult.reason) : ''
    articles.value = articleResult.status === 'fulfilled' ? articleResult.value.data || [] : []
    knowledgeError.value = articleResult.status === 'rejected' ? errorMessage(articleResult.reason) : ''
  } catch (cause) {
    if (id === requestId) error.value = errorMessage(cause)
  } finally {
    if (id === requestId) loading.value = false
  }
}

async function onRefresh() {
  await load(clinic.value?.id)
  refreshing.value = false
}

function onClinicSelect(_action: unknown, index: number) {
  clinicPickerShow.value = false
  const selected = clinics.value[index]
  if (selected) void load(selected.id)
}

function openDoctors() {
  void router.push('/doctors')
}

function openDoctorDetail(doctor: DoctorRow) {
  if (loading.value) return
  void router.push({ name: 'doctor-detail', params: { id: doctor.id }, query: { clinicId: doctor.clinicId } })
}

function openSchedule(doctorId = '') {
  const clinicId = clinic.value?.id
  if (!clinicId) {
    openDoctors()
    return
  }
  void router.push({ path: '/schedule', query: { clinicId, ...(doctorId ? { doctorId } : {}) } })
}

function openClinic() {
  if (clinic.value && !loading.value) void router.push({ path: '/clinic', query: { clinicId: clinic.value.id } })
}

function openKnowledge() {
  void router.push({ path: '/knowledge', query: clinic.value ? { clinicId: clinic.value.id } : {} })
}

function openArticle(row: KnowledgeArticle) {
  void router.push({ name: 'knowledge-detail', params: { id: row.id } })
}

function openFlow() {
  document.getElementById('care-flow')?.scrollIntoView({ behavior: 'smooth' })
}

onMounted(() => void load())
onUnmounted(() => { requestId += 1 })
</script>

<template>
  <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
    <main class="page">
      <div class="brand-line">
        <div class="grow">
          <div class="brand-title">{{ presentation?.brandName || '百莲凯医疗美容' }}</div>
          <div class="muted">{{ presentation?.tagline || '用心沟通 · 陪伴每一步' }}</div>
        </div>
        <picture v-if="!logoGone" class="brand-logo-pic">
          <source v-if="useBundledLogo" srcset="/brand-logo.webp" type="image/webp" />
          <img class="brand-logo" :src="brandLogoSrc" @error="onBrandLogoError" alt="百莲凯医疗美容" />
        </picture>
        <span v-else class="brand-seal">＋</span>
      </div>

      <section class="hero">
        <div class="eyebrow light">BAILIANKAI · SKIN CARE</div>
        <div class="hero-title">关心你的皮肤<br />也关心你的感受</div>
        <p class="hero-sub">痘坑痘印面诊与恢复管理<br />从一次充分的沟通开始。</p>
        <button class="hero-btn" @click="openSchedule()">预约面诊 <van-icon name="arrow" /></button>
        <div class="hero-tags"><span>医生面诊</span><span>个人方案</span><span>治疗随访</span></div>
      </section>

      <section v-if="presentation?.introduction" class="card">
        <h2>关于{{ presentation.brandName }}</h2>
        <p class="muted">{{ presentation.introduction }}</p>
      </section>
      <section v-if="presentation?.environmentImages?.length" class="card">
        <h2>门店实景</h2>
        <van-swipe class="env-swipe" :autoplay="4000" indicator-color="#0f766e" lazy-render>
          <van-swipe-item v-for="(image, index) in presentation.environmentImages" :key="index">
            <img class="env-image" :src="image.imageUrl" :alt="image.caption || '门店实景'" />
            <p v-if="image.caption" class="env-caption">{{ image.caption }}</p>
          </van-swipe-item>
        </van-swipe>
      </section>
      <section v-if="presentation?.featuredReviews?.length" class="card">
        <h2>患者评价精选</h2>
        <div v-for="(review, index) in presentation.featuredReviews" :key="index" class="review-row">
          <div class="review-head"><span class="review-name">{{ review.displayName || '匿名患者' }}</span><span v-if="review.rating" class="review-stars">{{ '★'.repeat(review.rating) }}</span></div>
          <p class="muted">{{ review.content }}</p>
        </div>
      </section>

      <button class="ghost profile-entry" @click="router.push('/profile')">我的资料 · 登录与手机号授权</button>

      <div class="quick-grid">
        <button class="quick-entry" @click="router.push('/assessment')">
          <span class="quick-label">AI 辅助评估</span><span class="quick-caption">照片观察 · 医生复核</span>
        </button>
        <button class="quick-entry" @click="router.push('/support')">
          <span class="quick-label">在线咨询</span><span class="quick-caption">留言 · 服务记录</span>
        </button>
        <button class="quick-entry" @click="router.push('/cases')">
          <span class="quick-label">案例库</span><span class="quick-caption">治疗前后对比</span>
        </button>
        <button class="quick-entry" @click="openDoctors">
          <van-icon name="user-o" size="22" class="quick-icon" /><span class="quick-label">找医生</span><span class="quick-caption">了解团队</span>
        </button>
        <button class="quick-entry" @click="openFlow">
          <van-icon name="todo-list-o" size="22" class="quick-icon teal" /><span class="quick-label">看流程</span><span class="quick-caption">就诊指南</span>
        </button>
        <button class="quick-entry" @click="router.push('/appointments')">
          <van-icon name="calendar-o" size="22" class="quick-icon" /><span class="quick-label">我的预约</span><span class="quick-caption">查看进度</span>
        </button>
        <button class="quick-entry" @click="router.push('/records')">
          <van-icon name="records-o" size="22" class="quick-icon purple" /><span class="quick-label">治疗档案</span><span class="quick-caption">方案与随访</span>
        </button>
      </div>

      <section class="card">
        <h2>恢复日历</h2>
        <p class="muted">按日期查看随访安排、待填写反馈和医护回评。</p>
        <button class="text-link" @click="router.push('/recovery')">查看我的恢复日历 ›</button>
      </section>

      <section id="care-flow" class="card">
        <div class="row">
          <div class="grow"><h2>从面诊到随访</h2><p class="muted">每一步，都有清晰的安排</p></div>
          <van-icon name="like-o" size="20" color="#1763c6" />
        </div>
        <div v-for="step in steps" :key="step.number" class="flow-step">
          <span class="step-number">{{ step.number }}</span>
          <div><div class="step-title">{{ step.title }}</div><p class="muted">{{ step.description }}</p></div>
        </div>
      </section>

      <div class="section-heading">
        <h2>认识医生团队</h2>
        <button class="text-link" @click="openDoctors">查看全部 <van-icon name="arrow" size="12" /></button>
      </div>

      <section v-if="clinics.length && clinic" class="card">
        <button class="clinic-picker" :disabled="loading" @click="clinicPickerShow = true">
          <span class="grow">
            <span class="clinic-label">当前院区{{ presentation?.isDemo ? ' · 演示资料' : '' }}</span>
            <span class="clinic-name">{{ clinic.name }}</span>
          </span>
          <van-icon v-if="clinics.length > 1" name="arrow-down" />
        </button>
        <p class="clinic-address">{{ clinic.address }}</p>
        <p class="muted">营业时间：{{ presentation?.hours || '暂未公布' }}</p>
        <button class="text-link" @click="openClinic">院区地图 · 就诊导航 · 机构资质 ›</button>
      </section>

      <div v-if="loading" class="card state"><van-loading size="24">正在读取团队资料</van-loading></div>
      <div v-else-if="error" class="card state">
        <div class="state-title">团队资料暂时无法加载</div>
        <p class="muted">{{ error }}</p>
        <button class="ghost" @click="load(clinic?.id)">重新加载</button>
      </div>
      <template v-else-if="doctors.length">
        <div class="doctor-carousel" data-testid="doctor-carousel">
          <div ref="carouselRef" class="doctor-track" @scroll="onCarouselScroll">
            <section v-for="doctor in doctors" :key="doctor.id" class="card doctor-card" data-testid="doctor-card" :data-doctor-id="doctor.id" @click="openDoctorDetail(doctor)">
              <div class="row">
                <img v-if="avatarVisible(doctor)" class="avatar" :src="doctorAvatarSrc(doctor)" loading="lazy" alt="" @error="onAvatarError(doctor)" />
                <span v-else class="avatar initials">{{ doctor.initials }}</span>
                <div class="grow">
                  <div class="doctor-name">{{ doctor.name }}</div>
                  <div class="muted">{{ doctor.title }}</div>
                  <span v-if="doctor.presentation.isDemo" class="chip warning">演示医生</span>
                </div>
              </div>
              <p class="doctor-summary">{{ doctor.summary }}</p>
              <div class="focus-list"><span v-for="item in doctor.focus" :key="item" class="chip">{{ item }}</span></div>
              <div class="row doctor-action">
                <span class="muted grow">专业方向 · 服务与面诊说明</span>
                <button class="ghost small" @click.stop="openDoctorDetail(doctor)">查看医生详情</button>
                <button class="primary small" @click.stop="openSchedule(doctor.id)">立即预约</button>
              </div>
            </section>
          </div>
          <div v-if="doctors.length > 1" class="carousel-nav">
            <button class="carousel-arrow" :disabled="carouselIndex === 0" aria-label="上一位医生" @click="scrollToDoctor(carouselIndex - 1)">‹</button>
            <div class="carousel-dots">
              <button v-for="(doctor, i) in doctors" :key="doctor.id" class="carousel-dot" :class="{ active: i === carouselIndex }" :aria-label="`第 ${i + 1} 位医生`" @click="scrollToDoctor(i)" />
            </div>
            <button class="carousel-arrow" :disabled="carouselIndex === doctors.length - 1" aria-label="下一位医生" @click="scrollToDoctor(carouselIndex + 1)">›</button>
          </div>
          <p v-if="doctors.length > 1" class="swipe-hint">← 左右滑动查看更多医生 →</p>
        </div>
      </template>
      <div v-else class="card state">
        <div class="state-title">暂无医生资料</div>
        <p class="muted">当前院区还未发布团队信息，请稍后查看。</p>
      </div>

      <div class="section-heading">
        <h2>皮肤科普</h2>
        <button class="text-link" @click="openKnowledge">查看全部 ›</button>
      </div>
      <div v-if="loading" class="card state"><p class="muted">正在读取科普内容</p></div>
      <div v-else-if="knowledgeError" class="card state">
        <p class="muted">{{ knowledgeError }}</p>
        <button class="text-link" @click="openKnowledge">进入科普栏目重试 ›</button>
      </div>
      <div v-else-if="!articles.length" class="card state"><p class="muted">暂无已发布科普内容</p></div>
      <section v-for="row in articles" v-else :key="row.id" class="card article-card" @click="openArticle(row)">
        <h2>{{ row.title }}</h2>
        <p class="muted">{{ row.summary }}</p>
        <p class="muted">审核：{{ row.reviewerName }}</p>
        <span class="text-link">阅读全文 ›</span>
      </section>

      <section class="card">
        <h2>面诊前，准备好这些</h2>
        <div class="prep-row"><span>01</span><p>整理希望与医生沟通的问题和个人需求。</p></div>
        <div class="prep-row"><span>02</span><p>准备既往就诊、治疗及用药信息，方便面诊沟通。</p></div>
        <div class="prep-row"><span>03</span><p>在“我的预约”查看确认结果和到院时段。</p></div>
        <p class="muted prep-note">本页提供就诊流程介绍，具体方案以医生面诊及发布的方案为准。</p>
      </section>

      <div>
        <button class="primary" @click="openSchedule()">选择时间 · 预约面诊</button>
        <p class="footer-note">提交后需工作人员确认，可在“我的预约”查看进度</p>
      </div>

      <div v-if="presentation?.isDemo" class="demo-banner">
        <span class="demo-dot"></span><span>Mock 演示 · 院区与医生为虚构资料，示例位置与证照不代表真实资料。</span>
      </div>

      <van-action-sheet v-model:show="clinicPickerShow" :actions="clinicActions" cancel-text="取消" @select="onClinicSelect" />
    </main>
  </van-pull-refresh>
</template>

<style scoped>
.brand-line { display: flex; align-items: center; gap: 10px; }
.brand-title { font-size: 19px; font-weight: 700; }
.brand-logo { height: 34px; width: auto; max-width: 140px; object-fit: contain; }
.brand-logo-pic { display: inline-flex; flex: none; }
.brand-seal { font-size: 26px; color: var(--brand); font-weight: 800; }
.hero { border-radius: 16px; padding: 20px 18px; color: #fff; background: linear-gradient(135deg, #1763c6, #0d4c9c); }
.eyebrow { font-size: 11px; letter-spacing: 2px; color: var(--brand); font-weight: 700; }
.eyebrow.light { color: #bcd7f7; }
.hero-title { font-size: 25px; font-weight: 800; line-height: 1.35; margin: 10px 0 8px; }
.hero-sub { font-size: 13px; line-height: 1.7; color: #dbeafe; margin: 0 0 16px; }
.hero-btn { border: 0; border-radius: 999px; background: #fff; color: var(--brand); font-weight: 700; font-size: 15px; padding: 10px 22px; cursor: pointer; display: inline-flex; align-items: center; gap: 6px; }
.hero-tags { display: flex; gap: 16px; margin-top: 16px; font-size: 12px; color: #dbeafe; }
.profile-entry { width: 100%; color: var(--brand); font-weight: 600; }
.quick-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
.quick-entry { border: 1px solid var(--line); background: #fff; border-radius: 12px; padding: 12px 6px; display: flex; flex-direction: column; align-items: center; gap: 3px; cursor: pointer; color: var(--ink); }
.quick-icon { color: var(--brand); }
.quick-icon.teal { color: var(--teal); }
.quick-icon.purple { color: #7c3aed; }
.quick-label { font-size: 13px; font-weight: 600; }
.quick-caption { font-size: 10px; color: var(--muted); text-align: center; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 13px; cursor: pointer; padding: 4px 0; display: inline-flex; align-items: center; gap: 2px; }
.flow-step { display: flex; gap: 12px; padding: 10px 0; border-top: 1px solid var(--bg); }
.step-number { color: var(--brand); font-weight: 800; }
.step-title { font-weight: 600; }
.flow-step p { margin: 2px 0 0; }
.section-heading { display: flex; align-items: center; justify-content: space-between; }
.section-heading h2 { font-size: 17px; margin: 0; }
.clinic-picker { width: 100%; border: 0; background: none; display: flex; align-items: center; text-align: left; cursor: pointer; padding: 0; color: var(--ink); }
.clinic-label { display: block; font-size: 12px; color: var(--muted); }
.clinic-name { display: block; font-size: 16px; font-weight: 700; margin-top: 2px; }
.clinic-address { margin: 8px 0 2px; }
.state { text-align: center; }
.state-title { font-weight: 700; margin-bottom: 6px; }
.doctor-card { cursor: pointer; }
.doctor-track { display: flex; overflow-x: auto; scroll-snap-type: x mandatory; -webkit-overflow-scrolling: touch; scrollbar-width: none; }
.doctor-track::-webkit-scrollbar { display: none; }
.doctor-track .doctor-card { flex: 0 0 100%; min-width: 100%; scroll-snap-align: start; box-sizing: border-box; }
.carousel-nav { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 10px; }
.carousel-arrow { width: 32px; height: 32px; border-radius: 50%; border: 1px solid #ddd; background: #fff; font-size: 20px; line-height: 1; cursor: pointer; }
.carousel-arrow:disabled { opacity: 0.35; cursor: default; }
.carousel-dots { display: flex; gap: 8px; }
.carousel-dot { width: 8px; height: 8px; padding: 0; border-radius: 50%; border: none; background: #ddd; cursor: pointer; }
.carousel-dot.active { background: #333; }
.swipe-hint { text-align: center; color: #888; font-size: 13px; margin: 6px 0 0; }
.avatar { width: 48px; height: 48px; border-radius: 50%; object-fit: cover; flex: none; }
.avatar.initials { background: var(--brand); color: #fff; display: inline-flex; align-items: center; justify-content: center; font-weight: 700; }
.doctor-name { font-size: 16px; font-weight: 700; }
.doctor-summary { line-height: 1.6; margin: 10px 0; }
.focus-list { display: flex; flex-wrap: wrap; gap: 6px; }
.doctor-action { margin-top: 10px; }
.env-swipe { border-radius: 12px; overflow: hidden; }
.env-image { width: 100%; height: 200px; object-fit: cover; display: block; }
.env-caption { font-size: 13px; color: var(--muted); margin: 8px 0 0; text-align: center; }
.review-row { padding: 10px 0; border-top: 1px solid var(--bg); }
.review-row:first-of-type { border-top: 0; padding-top: 2px; }
.review-head { display: flex; justify-content: space-between; align-items: center; }
.review-name { font-weight: 600; font-size: 14px; }
.review-stars { color: #d97706; letter-spacing: 1px; }
.review-row p { margin: 6px 0 0; line-height: 1.7; }
button.ghost.small { padding: 7px 12px; font-size: 13px; }
.article-card { cursor: pointer; }
.prep-row { display: flex; gap: 12px; padding: 8px 0; }
.prep-row span { color: var(--brand); font-weight: 800; }
.prep-row p { margin: 0; line-height: 1.6; }
.prep-note { border-top: 1px solid var(--bg); padding-top: 8px; }
.footer-note { text-align: center; color: var(--muted); font-size: 12px; margin: 10px 0 0; }
.demo-banner { display: flex; gap: 8px; align-items: flex-start; background: #fff6e5; border: 1px solid #f3d9a4; color: var(--amber); border-radius: 10px; padding: 10px 12px; font-size: 12px; line-height: 1.6; }
.demo-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--amber); margin-top: 5px; flex: none; }
</style>
