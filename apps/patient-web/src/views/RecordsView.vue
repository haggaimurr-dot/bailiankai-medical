<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import { errorMessage } from '../api'
import { isSessionExpired, patientApi, type TimelineEvent } from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'

const KIND_LABELS: Record<TimelineEvent['kind'], string> = {
  initial: '问诊', revisit: '复诊', plan: '方案发布', treatment: '实际治疗',
  followup: '随访任务', submitted: '随访反馈', reviewed: '医护回评', appointment: '预约',
}
const STATUS_WORDS: Record<string, string> = {
  waiting: '候补中', pending: '待处理', confirmed: '已确认', completed: '已治疗', rejected: '未确认',
  cancelled: '已取消', expired: '已过期', submitted: '待回评', alert: '待医护关注', reviewed: '已回评', handled: '已处理',
}
const STAGE_WORDS: Record<string, string> = { not_started: '未开始', in_progress: '进行中', completed: '已完成' }
const READABLE_KINDS = ['initial', 'revisit', 'plan']
const FILTERS: { key: string; label: string }[] = [
  { key: '', label: '全部' },
  ...Object.entries(KIND_LABELS).map(([key, label]) => ({ key, label })),
]

function summaryLabel(event: TimelineEvent): string {
  if (event.kind === 'appointment' && event.summary === 'pending') return '待确认'
  if (event.kind === 'followup' && event.summary === 'pending') return '待填写'
  if (event.kind === 'appointment' || event.kind === 'followup') return STATUS_WORDS[event.summary] || event.summary
  return event.summary
}
function stageLabel(event: TimelineEvent): string {
  if (!event.stageIndex) return ''
  return `第 ${event.stageIndex} 阶段${event.stageStatus ? ` · ${STAGE_WORDS[event.stageStatus] || event.stageStatus}` : ''}`
}

const router = useRouter()
const session = useSessionStore()

const loading = ref(true)
const loadingMore = ref(false)
const expired = ref(false)
const error = ref('')
const patientName = ref('')
const loadedPatientId = ref('')
const records = ref<TimelineEvent[]>([])
const page = ref(0)
const total = ref(0)
const kindFilter = ref('')
const expanded = ref<Record<string, boolean>>({})
const confirmingId = ref('')

const hasMore = computed(() => page.value * 20 < total.value)
const visibleRecords = computed(() => (kindFilter.value ? records.value.filter((r) => r.kind === kindFilter.value) : records.value))

// 按类型筛选须不遗漏：筛选只作用于已加载记录，选类型后自动加载剩余页，
// 直至全部记录到齐再过滤；加载出错或超过 100 页即停，避免死循环。
watch(kindFilter, async (value) => {
  if (!value) return
  let guard = 0
  while (hasMore.value && guard < 100) {
    guard++
    const before = records.value.length
    await load(true)
    if (error.value || records.value.length === before) break
  }
})

async function load(append = false) {
  if (append) {
    if (loading.value || loadingMore.value || !hasMore.value) return
    loadingMore.value = true
  } else {
    loading.value = true
    error.value = ''
    expired.value = false
  }
  try {
    const identity = await patientApi.me()
    if (append && loadedPatientId.value && identity.id !== loadedPatientId.value) {
      records.value = []
      patientName.value = ''
      total.value = 0
      throw new Error('患者身份已变化，请刷新档案。')
    }
    const nextPage = append ? page.value + 1 : 1
    const envelope = await patientApi.timeline(nextPage, identity.id)
    loadedPatientId.value = identity.id
    patientName.value = identity.displayName
    records.value = append ? [...records.value, ...(envelope.data || [])] : envelope.data || []
    page.value = envelope.meta?.page ?? nextPage
    total.value = envelope.meta?.total ?? records.value.length
  } catch (cause) {
    if (isSessionExpired(cause)) {
      expired.value = true
      records.value = []
    } else {
      error.value = errorMessage(cause)
    }
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

async function relogin() {
  session.expire()
  try {
    await session.ensureSession()
    loadedPatientId.value = ''
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  }
}

async function confirmRead(event: TimelineEvent) {
  if (confirmingId.value || !event.adviceVersion || event.readAt) return
  try {
    await showConfirmDialog({
      title: '确认已阅读医嘱',
      message: event.advice,
      confirmButtonText: '已阅读',
      confirmButtonColor: '#1763c6',
    })
  } catch {
    return
  }
  confirmingId.value = event.id
  try {
    const receipt = await patientApi.confirmAdviceRead(event.kind, event.resourceId, event.adviceVersion, session.patientId)
    records.value = records.value.map((row) => (row.id === event.id ? { ...row, readAt: receipt.readAt } : row))
    showToast('已确认阅读')
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    confirmingId.value = ''
  }
}

function toggleExpand(id: string) {
  expanded.value = { ...expanded.value, [id]: !expanded.value[id] }
}

function openDetail(event: TimelineEvent) {
  if (!event.appointmentId) return
  if (event.kind === 'plan') {
    router.push({ path: '/care/plan', query: { appointmentId: event.appointmentId, versionId: event.resourceId } })
  } else if (event.kind === 'followup' || event.kind === 'submitted' || event.kind === 'reviewed') {
    router.push({ path: '/followups', query: { appointmentId: event.appointmentId, taskId: event.resourceId } })
  } else {
    router.push({ path: `/appointments/${event.appointmentId}` })
  }
}

function actionLabel(event: TimelineEvent): string {
  if (event.kind === 'plan') return '查看完整方案'
  if (event.kind === 'followup' || event.kind === 'submitted' || event.kind === 'reviewed') return '查看随访详情'
  return '查看关联预约'
}

onMounted(() => load())
</script>

<template>
  <main class="page">
    <p class="chip">开发演示 · 仅显示当前患者账号可访问的记录</p>
    <div class="row">
      <div class="grow">
        <p class="eyebrow">MY CARE HISTORY</p>
        <h1>方案与治疗档案</h1>
      </div>
      <span v-if="patientName" class="muted">{{ patientName }}</span>
    </div>
    <p class="lede">问诊、复诊、方案、治疗与随访记录统一按时间倒序显示，医嘱可在此确认阅读。</p>
    <button class="ghost" @click="router.push('/recovery')">恢复日历 · 查看随访与回评</button>

    <div v-if="loading" class="card"><van-loading size="24px">正在整理治疗档案…</van-loading></div>

    <div v-else-if="expired" class="card">
      <h2>登录已失效</h2>
      <p class="muted">演示身份已过期。创建新身份后，旧记录仍属于旧身份，不会自动转入新身份。</p>
      <button class="primary" @click="relogin">重新登录</button>
    </div>

    <div v-else-if="error && !records.length" class="card">
      <h2>档案暂时无法读取</h2>
      <div class="error-box">{{ error }}</div>
      <button class="ghost" style="margin-top: 10px" @click="load()">刷新档案</button>
    </div>

    <template v-else>
      <p class="muted">共 {{ total }} 条记录</p>
      <div class="filter-row">
        <button
          v-for="f in FILTERS"
          :key="f.key"
          class="chip filter-chip"
          :class="{ active: kindFilter === f.key }"
          @click="kindFilter = f.key"
        >
          {{ f.label }}
        </button>
      </div>
      <div v-if="error" class="error-box">{{ error }}</div>

      <van-empty v-if="!records.length" description="暂时没有档案记录" />
      <p v-if="!records.length" class="muted">问诊、复诊、方案与后续治疗记录会统一显示在这里。</p>
      <van-empty v-else-if="!visibleRecords.length" description="当前类型暂无记录" />

      <div class="timeline">
        <article v-for="event in visibleRecords" :key="event.id" class="tl-row">
          <div class="rail"><span class="dot" /><span class="line" /></div>
          <div class="card grow">
            <div class="row">
              <span class="muted">{{ dateTime(event.occurredAt) }}</span>
              <span class="chip" :class="event.kind === 'plan' || event.kind === 'reviewed' ? 'success' : ''">{{ KIND_LABELS[event.kind] }}</span>
            </div>

            <template v-if="event.kind === 'initial' || event.kind === 'revisit'">
              <p class="body-text"><strong>主诉：</strong>{{ event.title }}</p>
              <p v-if="event.history" class="muted preline"><strong>病史与既往情况：</strong>{{ event.history }}</p>
              <p v-if="summaryLabel(event)" class="muted preline"><strong>评估结论：</strong>{{ summaryLabel(event) }}</p>
            </template>
            <template v-else>
              <p class="body-text">{{ event.title }}</p>
              <p v-if="summaryLabel(event)" class="muted preline">{{ summaryLabel(event) }}</p>
            </template>

            <p v-if="stageLabel(event)" class="muted">{{ stageLabel(event) }}</p>
            <p v-if="event.parentConsultationId" class="muted">本次复诊已关联前次问诊</p>
            <p v-if="event.nextVisitAt" class="muted">建议下次复诊：{{ dateTime(event.nextVisitAt) }}</p>

            <div v-if="event.adviceVersion && event.advice" class="advice">
              <h3>医嘱与注意事项</h3>
              <p class="preline" :class="{ collapsed: !expanded[event.id] }">{{ event.advice }}</p>
              <button class="link-btn" @click="toggleExpand(event.id)">{{ expanded[event.id] ? '收起' : '展开全文' }}</button>
              <p class="muted">阅读确认仅表示已阅读医嘱，不作为知情同意或治疗授权。</p>
              <p v-if="event.readAt" class="chip success">已确认阅读 · {{ dateTime(event.readAt) }}</p>
              <button
                v-else-if="READABLE_KINDS.includes(event.kind)"
                class="primary"
                :disabled="confirmingId === event.id"
                @click="confirmRead(event)"
              >
                {{ confirmingId === event.id ? '提交中…' : '我已阅读医嘱' }}
              </button>
            </div>

            <button v-if="event.appointmentId" class="ghost action-btn" @click="openDetail(event)">{{ actionLabel(event) }}</button>
          </div>
        </article>
      </div>

      <button v-if="hasMore" class="ghost" :disabled="loadingMore" @click="load(true)">
        {{ loadingMore ? '加载中…' : '加载更早记录' }}
      </button>
      <p v-else-if="records.length" class="muted center">已显示全部记录</p>
    </template>
  </main>
</template>

<style scoped>
.eyebrow { color: var(--muted); font-size: 12px; margin: 0; letter-spacing: 2px; }
.filter-row { display: flex; gap: 8px; flex-wrap: wrap; }
.filter-chip { background: #fff; cursor: pointer; font-size: 12px; padding: 5px 12px; }
.filter-chip.active { color: var(--brand); border-color: var(--brand); background: #eff6ff; font-weight: 600; }
.timeline { display: flex; flex-direction: column; gap: 12px; }
.tl-row { display: flex; gap: 10px; align-items: stretch; }
.rail { display: flex; flex-direction: column; align-items: center; padding-top: 18px; }
.rail .dot { width: 10px; height: 10px; border-radius: 50%; background: var(--brand); flex: none; }
.rail .line { width: 2px; background: var(--line); flex: 1; margin-top: 4px; }
.body-text { font-size: 14px; line-height: 1.7; }
.preline { white-space: pre-line; line-height: 1.7; font-size: 13px; }
.preline.collapsed { display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
.advice { background: var(--bg); border-radius: 10px; padding: 12px; margin-top: 10px; }
.advice h3 { font-size: 14px; margin: 0 0 6px; }
.link-btn { border: 0; background: none; color: var(--brand); font-size: 13px; cursor: pointer; padding: 0; }
.action-btn { margin-top: 10px; }
.center { text-align: center; }
</style>
