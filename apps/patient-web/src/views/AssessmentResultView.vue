<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { errorMessage } from '../api'
import { assessmentApi, type AssessmentTask } from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'
import type { components } from '../../../../packages/contracts/src/schema'

type PublicReview = components['schemas']['AssessmentPublicReview']

const statusLabels: Record<AssessmentTask['status'], string> = {
  draft: '准备照片',
  queued: '等待分析',
  analyzing: '正在分析',
  retry_wait: '等待重试',
  failed: '分析失败',
  awaiting_review: '待医生复核',
  completed: '复核完成',
  rejected: '需补拍或面诊',
  cancelled: '已取消',
}

const route = useRoute()
const router = useRouter()
const store = useSessionStore()

const review = ref<PublicReview | null>(null)
const label = ref('')
const error = ref('')
const needsLogin = ref(false)
const clinicId = ref('')
const reviewedTime = ref('')

const taskId = String(route.query.id || '')
let generation = 0
let visible = false
let timer: ReturnType<typeof setTimeout> | undefined

async function load() {
  if (!visible) return
  if (timer) clearTimeout(timer)
  timer = undefined
  const gen = ++generation
  review.value = null
  label.value = ''
  error.value = ''
  reviewedTime.value = ''
  const session = store.session
  if (!session) {
    needsLogin.value = true
    return
  }
  needsLogin.value = false
  try {
    const view = await assessmentApi.get(taskId, session.patient.id)
    if (gen !== generation) return
    const published = view.publishedReview || null
    review.value = published
    label.value = statusLabels[view.task.status]
    reviewedTime.value = published ? dateTime(published.reviewedAt) : ''
    clinicId.value = view.task.clinicId
  } catch (e) {
    if (gen === generation) {
      error.value = errorMessage(e)
      review.value = null
    }
  } finally {
    if (gen === generation && visible) timer = setTimeout(() => void load(), 5000)
  }
}

onMounted(() => {
  visible = true
  void load()
})
onUnmounted(() => {
  visible = false
  generation++
  if (timer) clearTimeout(timer)
})
</script>

<template>
  <main class="page">
    <div>
      <p class="lede">DOCTOR REVIEWED OBSERVATION</p>
      <h1>辅助评估结果</h1>
    </div>

    <div v-if="error" class="error-box">{{ error }}</div>

    <button v-if="needsLogin" class="primary" @click="router.push('/profile')">前往登录</button>

    <section v-else-if="review" class="card">
      <h2>{{ review.decision === 'rejected' ? '请补充照片或预约面诊' : '经医生复核的辅助观察' }}</h2>
      <p class="muted">{{ review.doctorName }} · {{ reviewedTime }} 北京时间 · 复核版本 {{ review.version }}</p>
      <template v-if="review.finalContent">
        <h2 style="margin-top: 14px">观察概要</h2>
        <p>{{ review.finalContent.summary }}</p>
        <h2 style="margin-top: 14px">可见现象</h2>
        <p v-for="(item, index) in review.finalContent.observations" :key="item.id || index" class="muted">{{ item.description }}</p>
        <h2 style="margin-top: 14px">观察局限</h2>
        <p v-for="item in review.finalContent.limitations" :key="item" class="muted">{{ item }}</p>
        <h2 style="margin-top: 14px">面诊建议</h2>
        <p>{{ review.finalContent.consultationAdvice.reason }}</p>
        <p v-for="item in review.finalContent.consultationAdvice.preparation" :key="item" class="muted">{{ item }}</p>
      </template>
      <h2 style="margin-top: 14px">医生说明</h2>
      <p>{{ review.note }}</p>
      <button
        v-if="review.decision === 'rejected'"
        class="primary"
        style="margin-top: 12px"
        @click="router.push({ path: '/assessment', query: { supersedes: taskId, clinicId } })"
      >
        重新准备照片
      </button>
    </section>

    <section v-else-if="!needsLogin" class="card">
      <strong>{{ label }}</strong>
      <p class="muted">暂无经医生复核的结果，请查看任务进度。</p>
    </section>

    <button class="ghost" @click="router.push({ path: '/assessment/tasks', query: { id: taskId } })">查看任务进度</button>
    <button class="ghost" @click="router.push('/schedule')">预约面诊 ›</button>
    <p class="muted" style="text-align: center">照片辅助观察，具体以面诊为准。</p>
  </main>
</template>
