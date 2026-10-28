<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog } from 'vant'
import { errorMessage } from '../api'
import { clearSubmission, isSessionExpired, loadSubmission, patientApi, type WaitlistOffer } from '../patient'
import { useSessionStore } from '../stores/session'
import { appointmentView, dateTime, type AppointmentView } from '../utils/appointment'

const router = useRouter()
const sessionStore = useSessionStore()

const filters = [
  { key: '', label: '全部' },
  { key: 'waiting', label: '候补中' },
  { key: 'pending', label: '待确认' },
  { key: 'confirmed', label: '已确认' },
  { key: 'completed', label: '已治疗' },
  { key: 'rejected', label: '已拒绝' },
  { key: 'cancelled', label: '已取消' },
  { key: 'expired', label: '已过期' },
]

const loading = ref(true)
const loadingMore = ref(false)
const refreshing = ref(false)
const error = ref('')
const expired = ref(false)
const creatingIdentity = ref(false)
const patientName = ref('')
const records = ref<AppointmentView[]>([])
const offers = ref<Record<string, WaitlistOffer>>({})
const confirmingId = ref('')
const status = ref('')
const page = ref(0)
const total = ref(0)
const hasMore = ref(false)
const hasSubmission = ref(false)
let generation = 0

/** Demo-mode equivalent of the miniprogram's confirmNewSession(). */
async function confirmNewSession(): Promise<boolean> {
  const oldDraft = loadSubmission()
  try {
    await showConfirmDialog({
      title: '创建新的演示身份',
      message: '旧预约仍属于旧身份，新身份不会继承这些记录。此操作也会清除本机旧身份的提交恢复信息。是否继续？',
      confirmButtonText: '创建新身份',
      confirmButtonColor: '#1763c6',
    })
  } catch {
    return false
  }
  const session = await sessionStore.createSession()
  const currentDraft = loadSubmission()
  if (oldDraft?.key && currentDraft?.key === oldDraft.key && currentDraft.patientId === oldDraft.patientId && currentDraft.patientId !== session.patient.id) {
    clearSubmission(oldDraft.key)
  }
  return Boolean(session.token)
}

async function load(append = false) {
  if (append && (loadingMore.value || loading.value || !hasMore.value)) return
  const gen = ++generation
  const nextPage = append ? page.value + 1 : 1
  loading.value = !append
  loadingMore.value = append
  error.value = ''
  expired.value = false
  if (!append) records.value = []
  try {
    const identity = await patientApi.me()
    const result = await patientApi.appointments(status.value, nextPage)
    if (gen !== generation) return
    const rows = (append ? records.value : []).concat((result.data || []).map(appointmentView))
    const meta = result.meta || { page: nextPage, pageSize: 10, total: rows.length }
    patientName.value = identity.displayName
    records.value = rows
    page.value = meta.page
    total.value = meta.total
    hasMore.value = rows.length < meta.total
    hasSubmission.value = Boolean(loadSubmission())
    // 候补 offer 另取：失败不阻塞预约列表本身。
    try {
      const wl = await patientApi.waitlist()
      if (gen === generation) {
        const map: Record<string, WaitlistOffer> = {}
        for (const entry of wl.data || []) if (entry.offer) map[entry.appointment.id] = entry.offer
        offers.value = map
      }
    } catch { /* ignore: waitlist info is best-effort here */ }
  } catch (cause) {
    if (gen === generation) {
      error.value = errorMessage(cause)
      expired.value = isSessionExpired(cause)
    }
  } finally {
    if (gen === generation) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

async function onRefresh() {
  await load()
  refreshing.value = false
}

function onStatusChange(next: string) {
  if (next === status.value || !filters.some((item) => item.key === next)) return
  status.value = next
  void load()
}

function openDetail(id: string) {
  if (id) void router.push({ name: 'appointment-detail', params: { id } })
}

async function confirmOffer(id: string) {
  if (confirmingId.value) return
  confirmingId.value = id
  try {
    await patientApi.confirmWaitlist(id)
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    confirmingId.value = ''
  }
}

async function newIdentity() {
  if (creatingIdentity.value) return
  creatingIdentity.value = true
  try {
    if (await confirmNewSession()) await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    creatingIdentity.value = false
  }
}

onMounted(() => void load())
onUnmounted(() => { generation += 1 })
</script>

<template>
  <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
    <main class="page">
      <div class="row top-actions">
        <button class="ghost grow" @click="router.push('/followups')">我的随访任务</button>
        <button class="ghost grow" @click="router.push('/records')">治疗档案</button>
      </div>

      <div class="demo-banner">
        <span class="demo-dot"></span><span>开发演示 · 仅显示当前患者账号的预约</span>
      </div>

      <div class="row">
        <div class="grow">
          <div class="eyebrow">MY APPOINTMENTS</div>
          <h1>我的预约</h1>
        </div>
        <button class="ghost small" @click="router.push('/schedule')">查看排班</button>
      </div>

      <p v-if="patientName && !expired" class="identity-note">当前患者账号：{{ patientName }}</p>

      <section v-if="hasSubmission && !expired" class="card recovery-row">
        <span class="grow">有一笔申请的提交结果待确认</span>
        <button class="text-link" @click="router.push('/booking')">查询原申请</button>
      </section>

      <div class="filter-scroll">
        <button
          v-for="item in filters"
          :key="item.key"
          class="status-chip"
          :class="{ active: status === item.key }"
          @click="onStatusChange(item.key)"
        >{{ item.label }}</button>
      </div>

      <div v-if="loading" class="card state"><van-loading size="24">正在读取预约记录</van-loading></div>
      <div v-else-if="expired" class="card state">
        <div class="state-title">登录已失效</div>
        <p class="muted">{{ error }}</p>
        <button class="primary" :disabled="creatingIdentity" @click="newIdentity">
          {{ creatingIdentity ? '正在重新登录…' : '重新登录' }}
        </button>
      </div>
      <div v-else-if="error && !records.length" class="card state">
        <div class="state-title">预约记录加载失败</div>
        <p class="muted">{{ error }}</p>
        <button class="ghost" @click="load()">重新加载</button>
      </div>
      <template v-else-if="records.length">
        <p class="record-count">共 {{ total }} 条记录 · 下拉可刷新结果</p>
        <section v-for="item in records" :key="item.id" class="card">
          <div class="row">
            <span class="doctor-name grow">{{ item.doctorName }}</span>
            <span class="chip" :class="item.statusTheme === 'default' ? '' : item.statusTheme">{{ item.statusLabel }}</span>
          </div>
          <div class="appt-time">{{ item.timeLabel }}</div>
          <p class="muted">{{ item.clinicName }} · 北京时间</p>
          <p v-if="item.status === 'pending'" class="deadline">待工作人员确认，最晚 {{ item.deadlineLabel }}</p>
          <div v-if="item.status === 'waiting' && offers[item.id]" class="offer-banner">
            <span class="grow">有空位了！请在 {{ dateTime(offers[item.id].expiresAt) }} 前确认，超时将顺延给下一位候补患者。</span>
            <button class="primary small" :disabled="confirmingId === item.id" @click="confirmOffer(item.id)">{{ confirmingId === item.id ? '确认中…' : '确认占位' }}</button>
          </div>
          <p v-if="item.status === 'rejected'" class="reason">拒绝原因：{{ item.rejectionReason || '工作人员未确认本次申请' }}</p>
          <div class="row appt-footer">
            <span class="muted grow">{{ item.statusHint }}</span>
            <button class="text-link" @click="openDetail(item.id)">查看详情</button>
          </div>
        </section>
        <p v-if="error" class="error-box" role="alert">{{ error }}</p>
        <button v-if="hasMore" class="ghost block-btn" :disabled="loadingMore" @click="load(true)">
          {{ loadingMore ? '正在加载…' : '加载更多' }}
        </button>
        <p v-else class="footer-note">已显示全部记录</p>
      </template>
      <div v-else class="card state">
        <div class="state-title">暂无{{ status ? '此状态的' : '' }}预约</div>
        <p class="muted">提交申请后，可以在这里查看确认结果。</p>
        <button class="ghost" @click="router.push('/schedule')">查看可约时段</button>
      </div>
    </main>
  </van-pull-refresh>
</template>

<style scoped>
.top-actions { gap: 10px; }
.demo-banner { display: flex; gap: 8px; align-items: flex-start; background: #fff6e5; border: 1px solid #f3d9a4; color: var(--amber); border-radius: 10px; padding: 10px 12px; font-size: 12px; line-height: 1.6; }
.demo-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--amber); margin-top: 5px; flex: none; }
.eyebrow { font-size: 11px; letter-spacing: 2px; color: var(--brand); font-weight: 700; }
.identity-note { background: #eff6ff; border: 1px solid #dbeafe; border-radius: 10px; padding: 10px 12px; font-size: 13px; margin: 0; }
.recovery-row { display: flex; align-items: center; gap: 10px; }
.filter-scroll { display: flex; gap: 8px; overflow-x: auto; padding-bottom: 4px; }
.status-chip { flex: none; border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 7px 14px; font-size: 13px; cursor: pointer; color: var(--ink); }
.status-chip.active { background: var(--brand); border-color: var(--brand); color: #fff; font-weight: 600; }
.state { text-align: center; }
.state-title { font-weight: 700; margin-bottom: 6px; }
.record-count { color: var(--muted); font-size: 12px; margin: 0; }
.doctor-name { font-size: 16px; font-weight: 700; }
.appt-time { font-size: 15px; font-weight: 600; margin-top: 8px; }
.deadline { color: var(--amber); font-size: 13px; margin: 4px 0 0; }
.offer-banner { display: flex; gap: 10px; align-items: center; background: #ecfdf5; border: 1px solid #a7f3d0; border-radius: 10px; padding: 10px 12px; font-size: 13px; line-height: 1.6; margin-top: 8px; }
.reason { color: var(--danger); font-size: 13px; margin: 4px 0 0; }
.appt-footer { margin-top: 10px; border-top: 1px solid var(--bg); padding-top: 8px; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 13px; cursor: pointer; padding: 4px 0; white-space: nowrap; }
button.ghost.small { padding: 7px 12px; font-size: 13px; }
.block-btn { width: 100%; }
.footer-note { text-align: center; color: var(--muted); font-size: 12px; margin: 0; }
</style>
