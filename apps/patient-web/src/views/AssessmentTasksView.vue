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
// 任务进度节点：每个节点都有时间说明，进行中的节点显示预计区间。
// 时间全部来自任务本身的真实字段（submittedAt / nextAttemptAt /
// analysisCompletedAt / reviewedAt）与管线已知耗时（质检 30 秒、分析 150 秒
// 超时、worker 约 20 秒一轮）；人工复核无固定时限，不编造 SLA，只给已等待时长。
type TimelineNode = { key: string; label: string; state: 'done' | 'current' | 'todo' | 'blocked'; time: string }
const elapsedText = (from: string | null) => {
  if (!from) return ''
  const mins = Math.max(0, Math.round((Date.now() - Date.parse(from)) / 60000))
  if (mins < 1) return '刚刚'
  if (mins < 60) return `约 ${mins} 分钟`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `约 ${hours} 小时`
  return `约 ${Math.floor(hours / 24)} 天`
}
const timeline = computed<TimelineNode[]>(() => {
  const task = selected.value?.task
  if (!task) return []
  const s = task.status
  const at = (ts: string | null) => (ts ? dateTime(ts) : '—')
  const stageTime = (key: string, state: TimelineNode['state']): string => {
    switch (key) {
      case 'draft':
        return state === 'done' ? `已创建 ${at(task.createdAt)}` : '等待您上传照片并提交'
      case 'queued':
        if (state === 'done') return `已提交 ${at(task.submittedAt)}`
        if (state === 'current') return `预计 1 分钟内开始分析${task.submittedAt ? `（已等待 ${elapsedText(task.submittedAt)}）` : ''}`
        return '约 1 分钟'
      case 'analyzing':
        if (state === 'done') return `分析完成 ${at(task.analysisCompletedAt)}`
        if (state === 'current') return `预计 1–3 分钟完成分析${task.submittedAt ? `（已进行 ${elapsedText(task.submittedAt)}）` : ''}`
        if (state === 'blocked') return `第 ${task.attemptCount} 次尝试失败`
        return '约 1–3 分钟'
      case 'awaiting_review':
        if (state === 'done') return `复核完成 ${at(task.reviewedAt)}`
        if (state === 'current')
          return `分析已于 ${at(task.analysisCompletedAt)} 完成，等待医生复核（人工复核，完成后即展示）${task.analysisCompletedAt ? `（已等待 ${elapsedText(task.analysisCompletedAt)}）` : ''}`
        return '人工复核，完成后即展示'
      case 'completed':
        return state === 'done' ? `已完成 ${at(task.reviewedAt)}` : '复核通过后即展示结果'
      default:
        return ''
    }
  }
  // 各节点在当前状态下的完成情况；cancelled 按已有时间戳回推。
  const doneAt = (key: string): boolean => {
    if (s === 'cancelled') {
      return key === 'draft' || (key === 'queued' && !!task.submittedAt) || (key === 'analyzing' && !!task.analysisCompletedAt) || (key === 'awaiting_review' && !!task.reviewedAt)
    }
    const order = ['draft', 'queued', 'analyzing', 'awaiting_review', 'completed']
    const stage = { draft: 0, queued: 1, analyzing: 2, retry_wait: 2, awaiting_review: 3, completed: 4, rejected: 4, failed: 2, cancelled: -1 }[s]
    return order.indexOf(key) < stage
  }
  const currentAt = (key: string): boolean => {
    if (s === 'cancelled') return false
    return { draft: 'draft', queued: 'queued', analyzing: 'analyzing', retry_wait: 'analyzing', awaiting_review: 'awaiting_review', completed: 'completed', rejected: 'awaiting_review', failed: 'analyzing' }[s] === key
  }
  const nodes: TimelineNode[] = (['draft', 'queued', 'analyzing', 'awaiting_review'] as const).map((key) => {
    let state: TimelineNode['state'] = 'todo'
    if (doneAt(key)) state = 'done'
    else if (currentAt(key)) state = s === 'failed' || (s === 'retry_wait' && key === 'analyzing') ? 'blocked' : 'current'
    return { key, label: statusLabels[key], state, time: stageTime(key, state) }
  })
  if (s === 'retry_wait') {
    nodes.push({
      key: 'retry_wait',
      label: '等待重试',
      state: 'current',
      time: task.nextAttemptAt ? `将于 ${at(task.nextAttemptAt)} 自动重试（第 ${task.attemptCount} / ${task.maxAttempts} 次尝试）` : '系统将自动重试',
    })
  }
  if (s === 'rejected') {
    nodes.push({ key: 'rejected', label: '需补拍或面诊', state: 'blocked', time: `医生复核决定 ${at(task.reviewedAt)}` })
  } else if (s === 'failed') {
    nodes.push({ key: 'failed', label: '分析失败', state: 'blocked', time: `${failureMessage.value || '分析失败'}（第 ${task.attemptCount} / ${task.maxAttempts} 次尝试）` })
  } else if (s === 'cancelled') {
    nodes.push({ key: 'cancelled', label: '已取消', state: 'blocked', time: `已取消 ${at(task.updatedAt)}` })
  } else {
    nodes.push({ key: 'completed', label: '复核完成', state: doneAt('completed') ? 'done' : s === 'completed' ? 'done' : 'todo', time: stageTime('completed', s === 'completed' ? 'done' : 'todo') })
  }
  return nodes
})
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

      <section class="card timeline">
        <h2>进度节点</h2>
        <ol>
          <li v-for="node in timeline" :key="node.key" :class="node.state">
            <span class="dot" aria-hidden="true">{{ node.state === 'done' ? '✓' : node.state === 'blocked' ? '!' : '' }}</span>
            <div>
              <strong>{{ node.label }}</strong>
              <p class="muted">{{ node.time }}</p>
            </div>
          </li>
        </ol>
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

<style scoped>
.timeline ol { list-style: none; margin: 12px 0 0; padding: 0; }
.timeline li { display: flex; gap: 12px; padding: 10px 0; border-top: 1px solid var(--line, #edf1f6); }
.timeline li:first-child { border-top: 0; }
.timeline .dot { flex: none; width: 22px; height: 22px; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 12px; font-weight: 700; margin-top: 2px; border: 2px solid #cbd5e1; color: #fff; background: #fff; }
.timeline li.done .dot { background: #36897d; border-color: #36897d; }
.timeline li.current .dot { background: #1763c6; border-color: #1763c6; }
.timeline li.blocked .dot { background: #c25e5e; border-color: #c25e5e; }
.timeline li.todo { opacity: 0.65; }
.timeline li p { margin: 2px 0 0; }
</style>
