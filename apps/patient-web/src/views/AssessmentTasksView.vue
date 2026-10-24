<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog } from 'vant'
import { ApiError, errorMessage } from '../api'
import { assessmentApi, type AssessmentTask, type AssessmentView } from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'

type Pending = { action: 'retry' | 'cancel' | 'consent'; version: number; key: string }

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
const failureLabels: Record<string, string> = {
  MODEL_TIMEOUT: '分析超时',
  MODEL_RATE_LIMITED: '服务繁忙，等待重试',
  MODEL_UNAVAILABLE: '分析服务暂不可用',
  WORKER_LEASE_EXPIRED: '任务执行中断，等待恢复',
  MEDIA_UNAVAILABLE: '照片读取失败，分析暂未完成',
  MODEL_OUTPUT_INVALID: '未取得可用的分析结果，请联系工作人员或预约面诊',
  MODEL_CONFIGURATION_ERROR: '辅助观察服务尚未就绪',
  AI_CONSENT_REVOKED: 'AI用途授权已撤回',
  CONSENT_UNAVAILABLE: '授权需要重新确认，任务已停止',
  PATIENT_CANCELLED: '您已取消本任务',
}
const statuses = ['', 'draft', 'queued', 'analyzing', 'retry_wait', 'awaiting_review', 'completed', 'rejected', 'failed', 'cancelled']
const filterLabels = ['全部', '准备照片', '等待分析', '正在分析', '等待重试', '待医生复核', '复核完成', '需补拍或面诊', '分析失败', '已取消']
const qualityLabels: Record<string, string> = { passed: '质检通过', pending: '等待检查', rejected: '请补拍', unavailable: '检查暂不可用' }
const angleLabels: Record<string, string> = { front: '正面', left: '本人左侧', right: '本人右侧' }
const assessmentKey = () => `ai-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`

const route = useRoute()
const router = useRouter()
const store = useSessionStore()

const tasks = ref<AssessmentTask[]>([])
const selected = ref<AssessmentView | null>(null)
const error = ref('')
const needsLogin = ref(false)
const busy = ref(false)
const loading = ref(false)
const pending = ref(false)
const filterIndex = ref(0)
const page = ref(0)
const total = ref(0)

let taskId = String(route.query.id || '')
let patientId = ''
let generation = 0
let visible = false
let timer: ReturnType<typeof setTimeout> | undefined
let operation: Pending | null = null

const detailLabel = computed(() => (selected.value ? statusLabels[selected.value.task.status] : ''))
const failureMessage = computed(() => {
  const code = selected.value?.task.failureCode
  if (!code) return ''
  return failureLabels[code] || '暂未完成，请稍后刷新'
})
const storageKey = () => `assessment-operation-v1:${patientId}:${taskId}`

function schedule() {
  if (timer) clearTimeout(timer)
  if (visible) timer = setTimeout(() => void load(true), 5000)
}

async function load(poll = false, more = false) {
  if (!visible || busy.value || loading.value) {
    schedule()
    return
  }
  const session = store.session
  if (!session) {
    needsLogin.value = true
    selected.value = null
    tasks.value = []
    return
  }
  if (patientId && session.patient.id !== patientId) {
    taskId = ''
    operation = null
    tasks.value = []
    selected.value = null
    pending.value = false
  }
  patientId = session.patient.id
  const gen = ++generation
  loading.value = true
  needsLogin.value = false
  if (!poll) error.value = ''
  try {
    if (taskId) {
      const view = await assessmentApi.get(taskId, patientId)
      if (gen !== generation) return
      selected.value = view
      let stored: Pending | null = null
      try {
        stored = JSON.parse(window.localStorage.getItem(storageKey()) || 'null') as Pending | null
      } catch {
        stored = null
      }
      if (stored) {
        operation = stored
        pending.value = true
      }
    } else {
      const nextPage = more ? page.value + 1 : 1
      const result = await assessmentApi.list(patientId, nextPage, statuses[filterIndex.value] || '')
      if (gen !== generation) return
      const rows = result.data || []
      tasks.value = more ? [...tasks.value, ...rows] : rows
      page.value = nextPage
      total.value = result.meta?.total || 0
    }
  } catch (e) {
    if (gen === generation) {
      error.value = errorMessage(e)
      if (e instanceof ApiError && [401, 403, 404].includes(e.status)) {
        tasks.value = []
        selected.value = null
      }
    }
  } finally {
    if (gen === generation) {
      loading.value = false
      schedule()
    }
  }
}

function openTask(id: string) {
  void router.push({ path: '/assessment/tasks', query: { id } })
}
function backToList() {
  void router.push({ path: '/assessment/tasks' })
}

async function runAction(action: Pending['action']) {
  if (busy.value || operation || !selected.value) return
  if (action === 'cancel' || action === 'consent') {
    try {
      await showConfirmDialog({
        title: action === 'cancel' ? '取消评估任务' : '撤回AI授权',
        message: '该任务将停止后续分析；重新评估需要创建新任务。',
        confirmButtonText: '确定',
        confirmButtonColor: '#1763c6',
      })
    } catch {
      return
    }
  }
  void perform(action)
}

async function perform(action?: Pending['action']) {
  const current = selected.value
  if (!current || busy.value) return
  if (!operation) {
    if (!action) return
    operation = { action, version: current.task.version, key: assessmentKey() }
    window.localStorage.setItem(storageKey(), JSON.stringify(operation))
  }
  const pendingOp = operation
  const gen = ++generation
  busy.value = true
  error.value = ''
  pending.value = true
  loading.value = false
  try {
    const next = await assessmentApi.change(
      taskId,
      pendingOp.action,
      pendingOp.action === 'consent'
        ? { version: pendingOp.version, policyVersion: current.consent.version, granted: false }
        : { version: pendingOp.version },
      pendingOp.key,
      patientId,
    )
    if (gen !== generation) return
    window.localStorage.removeItem(storageKey())
    operation = null
    pending.value = false
    selected.value = next
  } catch (e) {
    if (gen === generation) {
      error.value = errorMessage(e)
      if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
        window.localStorage.removeItem(storageKey())
        operation = null
        pending.value = false
      }
    }
  } finally {
    if (gen === generation) {
      busy.value = false
      void load(true)
    }
  }
}

watch(
  () => route.query.id,
  (id) => {
    taskId = String(id || '')
    generation++
    loading.value = false
    selected.value = null
    operation = null
    pending.value = false
    tasks.value = []
    page.value = 0
    void load()
  },
)

onMounted(() => {
  visible = true
  void load()
})
onUnmounted(() => {
  visible = false
  generation++
  if (timer) clearTimeout(timer)
  operation = null
})
</script>

<template>
  <main class="page">
    <div>
      <p class="lede">MY ASSESSMENTS</p>
      <h1>{{ selected ? '评估任务' : '我的评估' }}</h1>
    </div>

    <div v-if="error" class="error-box">{{ error }}</div>

    <section v-if="needsLogin" class="card">
      <p class="muted">请先登录，评估任务归属于您的患者账号。</p>
      <button class="primary" @click="router.push('/profile')">前往登录</button>
    </section>

    <template v-else-if="selected">
      <section class="card">
        <div class="row">
          <strong class="grow">{{ detailLabel }}</strong>
          <span class="chip">{{ selected.task.status }}</span>
        </div>
        <p class="muted">{{ selected.task.id }}</p>
        <p class="muted">分析尝试 {{ selected.task.attemptCount }} / {{ selected.task.maxAttempts }}</p>
        <p v-if="selected.task.status === 'awaiting_review'" class="muted">分析完成，待医生复核。复核后可查看观察内容及建议。</p>
        <p v-if="selected.task.status === 'retry_wait'" class="muted">服务暂时不可用，系统将在稍后自动重试。</p>
        <p v-if="selected.task.failureCode" class="muted">{{ failureMessage }}</p>
        <p v-if="selected.task.status === 'cancelled'" class="muted">任务已取消。可以重新准备照片发起评估。</p>
        <p v-for="photo in selected.photos" :key="photo.id" class="muted">
          {{ angleLabels[photo.angle] || photo.angle }} · {{ qualityLabels[photo.qualityCheck.status] || '检查暂不可用' }}
        </p>
      </section>

      <section class="card" style="display: flex; flex-direction: column; gap: 10px">
        <button v-if="selected.task.status === 'draft'" class="primary" :disabled="busy || pending" @click="router.push({ path: '/assessment', query: { id: selected.task.id } })">
          继续准备照片
        </button>
        <button
          v-if="selected.task.status === 'failed' && selected.task.retryable && selected.task.attemptCount < selected.task.maxAttempts"
          class="primary"
          :disabled="busy || pending"
          @click="runAction('retry')"
        >
          重试分析
        </button>
        <button
          v-if="selected.task.status === 'completed' || selected.task.status === 'rejected'"
          class="primary"
          @click="router.push({ path: '/assessment/result', query: { id: selected.task.id } })"
        >
          查看结果 / 医生说明
        </button>
        <button
          v-if="selected.task.status === 'rejected' || selected.task.status === 'failed' || selected.task.status === 'cancelled'"
          class="ghost"
          :disabled="busy || pending"
          @click="router.push({ path: '/assessment', query: { supersedes: selected.task.id, clinicId: selected.task.clinicId } })"
        >
          重新准备照片
        </button>
        <button v-if="selected.task.status !== 'cancelled'" class="ghost" style="color: var(--danger)" :disabled="busy || pending" @click="runAction('cancel')">
          取消任务
        </button>
        <button v-if="selected.consent.granted" class="ghost" style="color: var(--danger)" :disabled="busy || pending" @click="runAction('consent')">
          撤回AI用途授权
        </button>
        <button v-if="pending" class="primary" :disabled="busy" @click="perform()">重试原操作</button>
      </section>

      <button class="ghost" @click="backToList">返回全部任务</button>
    </template>

    <template v-else-if="!needsLogin">
      <label class="field-label" for="assessment-filter">状态筛选</label>
      <select id="assessment-filter" v-model.number="filterIndex" class="field" @change="page = 0; tasks = []; void load()">
        <option v-for="(label, index) in filterLabels" :key="label" :value="index">{{ label }}</option>
      </select>

      <van-loading v-if="loading && !tasks.length" style="text-align: center; padding: 24px 0">正在读取评估任务</van-loading>
      <section v-for="task in tasks" :key="task.id" class="card" style="cursor: pointer" @click="openTask(task.id)">
        <div class="row">
          <strong class="grow">{{ statusLabels[task.status] }}</strong>
          <span class="muted">查看 ›</span>
        </div>
        <p class="muted">{{ task.id }}</p>
        <p class="muted">更新：{{ dateTime(task.updatedAt) }} 北京时间</p>
      </section>
      <van-empty v-if="!tasks.length && !loading && !error" description="暂无匹配任务" />
      <button v-if="tasks.length < total" class="ghost" :disabled="loading" @click="load(false, true)">加载更多</button>
      <section class="card" style="cursor: pointer" @click="router.push('/assessment')">＋ 开始新的评估</section>
    </template>

    <button class="ghost" @click="router.push('/schedule')">直接预约面诊 ›</button>
    <p class="muted" style="text-align: center">照片辅助观察，具体以面诊为准。</p>
  </main>
</template>
