<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showImagePreview } from 'vant'
import { api, errorMessage, type Clinic } from '../api'
import { patientApi } from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'
import type { components } from '../../../../packages/contracts/src/schema'

type Ticket = components['schemas']['SupportTicket']
type SupportEvent = components['schemas']['SupportEvent']
type SupportCreate = components['schemas']['SupportCreate']
type SupportAction = components['schemas']['SupportAction']
type SupportUpload = components['schemas']['SupportUpload']
type Pending = { kind: 'create'; body: SupportCreate } | { kind: 'reply'; id: string; body: SupportAction }

const statusLabels: Record<string, string> = { open: '等待客服接手', in_progress: '正在处理', waiting_patient: '客服已回复', resolved: '已解决', closed: '已关闭' }
const categoryValues: SupportCreate['category'][] = ['general', 'booking', 'aftercare', 'complaint']
const categoryLabels = ['一般咨询', '预约咨询', '术后服务', '意见反馈']
const pendingStorageKey = (id: string) => `support-pending:${id}`
const requestKey = (prefix: string) => `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2)}`

const route = useRoute()
const router = useRouter()
const store = useSessionStore()

const tickets = ref<Ticket[]>([])
const selected = ref<Ticket | null>(null)
const events = ref<SupportEvent[]>([])
const clinics = ref<Clinic[]>([])
const clinicId = ref('')
const categoryIndex = ref(0)
const subject = ref('')
const content = ref('')
const reply = ref('')
const uploads = ref<SupportUpload[]>([])
const rating = ref(5)
const ratingComment = ref('')
const busy = ref(false)
const loading = ref(false)
const error = ref('')
const notice = ref('')
const noSession = ref(false)
const page = ref(1)
const total = ref(0)
const eventPage = ref(1)
const eventTotal = ref(0)
const pending = ref(false)
const attachInput = ref<HTMLInputElement | null>(null)

let patientId = ''
let generation = 0
let detailGeneration = 0
let polling = false
let timer: ReturnType<typeof setInterval> | undefined

const mode = computed(() => (route.query.ticket ? 'detail' : route.query.new ? 'create' : 'list'))
const canRate = computed(() => Boolean(selected.value && !selected.value.rating && (selected.value.status === 'resolved' || selected.value.status === 'closed')))

function readPending(): Pending | null {
  try {
    return JSON.parse(window.localStorage.getItem(pendingStorageKey(patientId)) || 'null') as Pending | null
  } catch {
    return null
  }
}
function clearPending() {
  window.localStorage.removeItem(pendingStorageKey(patientId))
}

async function load() {
  const gen = ++generation
  const session = store.session
  if (!session) {
    patientId = ''
    tickets.value = []
    selected.value = null
    events.value = []
    uploads.value = []
    reply.value = ''
    error.value = '请先到“我的资料”登录，再发起在线咨询。'
    noSession.value = true
    return
  }
  noSession.value = false
  if (patientId !== session.patient.id) {
    patientId = session.patient.id
    tickets.value = []
    selected.value = null
    events.value = []
    subject.value = ''
    content.value = ''
    reply.value = ''
    rating.value = 5
    ratingComment.value = ''
    uploads.value = []
  }
  busy.value = true
  loading.value = true
  error.value = ''
  pending.value = Boolean(window.localStorage.getItem(pendingStorageKey(patientId)))
  try {
    const [result, clinicList] = await Promise.all([patientApi.supportTickets(patientId), api.clinics()])
    if (gen !== generation) return
    tickets.value = result.data || []
    total.value = result.meta?.total || 0
    page.value = 1
    clinics.value = clinicList
    if (!clinicId.value && clinicList.length) clinicId.value = clinicList[0].id
    if (selected.value) await detail(selected.value.id)
    else if (route.query.ticket) await detail(String(route.query.ticket))
  } catch (e) {
    if (gen === generation) error.value = errorMessage(e)
  } finally {
    if (gen === generation) {
      busy.value = false
      loading.value = false
    }
  }
}

async function detail(id: string, nextPage = 1) {
  const gen = generation
  const dg = ++detailGeneration
  const pid = patientId
  try {
    const result = await patientApi.supportDetail(id, pid, nextPage)
    if (gen !== generation || dg !== detailGeneration || store.session?.patient.id !== pid) return
    const data = result.data!
    selected.value = data.ticket
    events.value = data.events || []
    eventPage.value = nextPage
    eventTotal.value = result.meta?.total || 0
  } catch (e) {
    if (gen === generation) error.value = errorMessage(e)
  }
}

async function persist(record: Pending) {
  if (!patientId) return
  const gen = generation
  detailGeneration++
  window.localStorage.setItem(pendingStorageKey(patientId), JSON.stringify(record))
  pending.value = true
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const ticket = record.kind === 'create' ? await patientApi.createSupport(record.body, patientId) : await patientApi.supportAction(record.id, record.body, patientId)
    clearPending()
    if (gen !== generation) return
    pending.value = false
    subject.value = ''
    content.value = ''
    reply.value = ''
    uploads.value = []
    ratingComment.value = ''
    notice.value = '已提交，消息每3秒自动刷新。'
    if (route.query.ticket !== ticket.id || mode.value !== 'detail') {
      await router.replace({ query: { ticket: ticket.id } })
    }
    await detail(ticket.id)
    void load()
  } catch (e) {
    if (gen === generation) error.value = errorMessage(e)
  } finally {
    if (gen === generation) busy.value = false
  }
}

async function submitCreate() {
  if (busy.value) return
  if (pending.value) {
    error.value = '上次提交结果待确认，请先重试上次提交。'
    return
  }
  if (!clinicId.value || !subject.value.trim() || !content.value.trim()) {
    error.value = '请选择院区，并填写标题和咨询内容。'
    return
  }
  await persist({
    kind: 'create',
    body: {
      clinicId: clinicId.value,
      category: categoryValues[categoryIndex.value]!,
      subject: subject.value.trim(),
      content: content.value.trim(),
      requestKey: requestKey('support'),
    },
  })
}

async function sendReply() {
  if (busy.value || !selected.value) return
  if (pending.value) {
    error.value = '上次提交结果待确认，请先重试上次提交。'
    return
  }
  if (!reply.value.trim() && !uploads.value.length) {
    error.value = '请填写回复内容。'
    return
  }
  await persist({
    kind: 'reply',
    id: selected.value.id,
    body: {
      action: 'message',
      content: reply.value.trim(),
      ...(uploads.value.length ? { attachments: uploads.value } : {}),
      version: selected.value.version,
      requestKey: requestKey('reply'),
    },
  })
}

async function rate() {
  if (busy.value || pending.value || !selected.value) return
  await persist({
    kind: 'reply',
    id: selected.value.id,
    body: {
      action: 'rate',
      rating: rating.value,
      content: ratingComment.value.trim() || '服务评分',
      version: selected.value.version,
      requestKey: requestKey('rating'),
    },
  })
}

async function retryPending() {
  if (busy.value) return
  const record = readPending()
  if (record) await persist(record)
  else await load()
}

async function discardPending() {
  if (busy.value) return
  try {
    await showConfirmDialog({
      title: '核对后重新编辑',
      message: '请先刷新服务记录，核对上次内容是否已保存。确认后清除待重试请求，编辑内容仍保留。',
      confirmButtonText: '确定',
      confirmButtonColor: '#1763c6',
    })
  } catch {
    return
  }
  clearPending()
  pending.value = false
  void load()
}

function readAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '')
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

async function onAttachPicked(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (!files.length || busy.value || pending.value || !selected.value) return
  if (files.some((file) => file.size === 0 || file.size > 2 * 1024 * 1024)) {
    error.value = '附件每个不超过2MB'
    return
  }
  busy.value = true
  try {
    const added: SupportUpload[] = []
    for (const file of files.slice(0, 3 - uploads.value.length)) {
      added.push({ name: file.name, data: await readAsBase64(file) })
    }
    uploads.value = [...uploads.value, ...added]
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}

async function openAttachment(attachmentId: string, name: string) {
  if (!selected.value) return
  try {
    const url = await patientApi.supportAttachment(selected.value.id, attachmentId, patientId)
    if (/\.pdf$/i.test(name)) {
      window.open(url, '_blank')
    } else {
      showImagePreview({ images: [url], closeable: true, onClose: () => URL.revokeObjectURL(url) })
    }
  } catch (e) {
    error.value = errorMessage(e)
  }
}

async function moreTickets() {
  if (busy.value) return
  const gen = generation
  busy.value = true
  try {
    const result = await patientApi.supportTickets(patientId, page.value + 1)
    if (gen === generation) {
      tickets.value = [...tickets.value, ...(result.data || [])]
      page.value += 1
      total.value = result.meta?.total || 0
    }
  } catch (e) {
    if (gen === generation) error.value = errorMessage(e)
  } finally {
    if (gen === generation) busy.value = false
  }
}

async function poll() {
  if (polling || busy.value || !selected.value || eventPage.value !== 1 || store.session?.patient.id !== patientId) return
  polling = true
  const previousTotal = eventTotal.value
  try {
    await detail(selected.value.id)
    if (eventTotal.value > previousTotal) notice.value = '收到新消息，服务记录已更新。'
  } finally {
    polling = false
  }
}

function openTicket(id: string) {
  generation++
  selected.value = null
  events.value = []
  reply.value = ''
  ratingComment.value = ''
  uploads.value = []
  void router.push({ query: { ticket: id } })
}
function backToList() {
  generation++
  selected.value = null
  events.value = []
  error.value = ''
  void router.push({ query: {} })
}
function newTicket() {
  if (busy.value) return
  selected.value = null
  error.value = ''
  notice.value = ''
  void router.push({ query: { new: '1' } })
}

watch(
  () => route.query.ticket,
  (ticket) => {
    if (ticket && patientId) {
      selected.value = null
      events.value = []
      void detail(String(ticket))
    }
  },
)

onMounted(() => {
  void load()
  timer = setInterval(() => void poll(), 3000)
})
onUnmounted(() => {
  generation++
  detailGeneration++
  if (timer) clearInterval(timer)
})
</script>

<template>
  <main class="page">
    <div>
      <h1>在线咨询</h1>
      <p class="lede">向院区客服提交问题，可在同一工单内持续沟通。</p>
    </div>

    <div v-if="error" class="error-box">{{ error }}</div>
    <div v-if="notice" class="error-box" style="background: var(--mlk-primitive-color-green-50, #ecfdf3); color: var(--green)">{{ notice }}</div>

    <section v-if="noSession" class="card">
      <p class="muted">登录后可发起在线咨询并查看服务记录。</p>
      <button class="primary" @click="router.push('/profile')">前往我的资料登录 ›</button>
    </section>

    <section v-if="pending && !noSession" class="card">
      <h2>上次提交结果待确认</h2>
      <p class="muted">为避免重复提交，请先重试上次提交，或核对记录后重新编辑。</p>
      <div class="row">
        <button class="primary grow" :disabled="busy" @click="retryPending">重试上次提交</button>
        <button class="ghost" :disabled="busy" @click="discardPending">核对后重新编辑</button>
      </div>
    </section>

    <!-- create mode -->
    <section v-if="mode === 'create' && !noSession" class="card">
      <h2>发起咨询</h2>
      <label class="field-label" for="support-clinic">服务院区</label>
      <select id="support-clinic" v-model="clinicId" class="field" :disabled="busy">
        <option v-for="clinic in clinics" :key="clinic.id" :value="clinic.id">{{ clinic.name }}</option>
      </select>
      <label class="field-label">咨询类型</label>
      <div class="row" style="flex-wrap: wrap">
        <button
          v-for="(label, index) in categoryLabels"
          :key="label"
          class="chip"
          :class="{ success: categoryIndex === index }"
          style="cursor: pointer; font-size: 13px; padding: 6px 12px"
          :disabled="busy"
          @click="categoryIndex = index"
        >
          {{ label }}
        </button>
      </div>
      <label class="field-label" for="support-subject">咨询标题</label>
      <input id="support-subject" v-model="subject" class="field" maxlength="100" :disabled="busy" placeholder="请简要描述问题" />
      <label class="field-label" for="support-content">咨询内容</label>
      <textarea id="support-content" v-model="content" class="field" rows="5" maxlength="2000" :disabled="busy" placeholder="请填写具体问题" />
      <button class="primary" style="margin-top: 12px" :disabled="busy" @click="submitCreate">提交咨询</button>
      <button class="ghost" style="width: 100%; margin-top: 10px" :disabled="busy" @click="backToList">返回服务记录</button>
    </section>

    <!-- detail mode -->
    <template v-else-if="mode === 'detail' && selected">
      <section class="card">
        <div class="row">
          <strong class="grow">{{ selected.subject }}</strong>
          <span class="chip warning">{{ statusLabels[selected.status] }}</span>
        </div>
        <p class="muted">工单：{{ selected.id }}</p>
        <p class="muted">最近更新：{{ dateTime(selected.updatedAt) }}（北京时间）</p>
        <p v-if="selected.rating" class="muted">服务评分：{{ selected.rating }} / 5 星<span v-if="selected.ratingComment"> · {{ selected.ratingComment }}</span></p>
      </section>

      <section v-if="canRate" class="card">
        <h2>服务评价</h2>
        <div class="row" role="radiogroup" aria-label="服务评分">
          <button
            v-for="star in 5"
            :key="star"
            class="ghost"
            style="font-size: 22px; padding: 4px 10px; color: var(--amber)"
            :disabled="busy"
            @click="rating = star"
          >
            {{ star <= rating ? '★' : '☆' }}
          </button>
          <span class="muted">{{ rating }} / 5 星</span>
        </div>
        <label class="field-label" for="rating-comment">评价内容（选填）</label>
        <textarea id="rating-comment" v-model="ratingComment" class="field" rows="2" maxlength="500" :disabled="busy" />
        <button class="primary" style="margin-top: 10px" :disabled="busy" @click="rate">提交评分</button>
      </section>

      <section class="card">
        <div class="row">
          <h2 class="grow" style="margin: 0">咨询与服务记录</h2>
          <span class="muted">每3秒自动刷新</span>
          <button class="ghost" :disabled="busy" @click="detail(selected.id)">刷新回复</button>
        </div>
        <p class="muted">第 {{ eventPage }} 页 · 共 {{ eventTotal }} 条，最新记录在前</p>
        <div v-for="event in events" :key="event.id" class="card" style="margin-top: 10px">
          <div class="row">
            <strong class="grow">{{ event.actorType === 'patient' ? '我' : '客服' }}</strong>
            <span class="muted">{{ dateTime(event.createdAt) }}</span>
          </div>
          <p v-if="event.content" style="white-space: pre-wrap">{{ event.content }}</p>
          <div v-for="attachment in event.attachments || []" :key="attachment.id" style="margin-top: 6px">
            <button class="ghost" @click="openAttachment(attachment.id, attachment.name)">查看附件：{{ attachment.name }}</button>
          </div>
        </div>
        <button v-if="eventPage * 20 < eventTotal" class="ghost" style="width: 100%; margin-top: 10px" :disabled="busy" @click="detail(selected.id, eventPage + 1)">
          查看更早记录
        </button>
      </section>

      <section v-if="selected.status !== 'closed'" class="card">
        <label class="field-label" for="support-reply">回复客服</label>
        <textarea id="support-reply" v-model="reply" class="field" rows="4" maxlength="2000" :disabled="busy" placeholder="填写回复内容" />
        <div class="row" style="flex-wrap: wrap; margin-top: 8px">
          <span v-for="(upload, index) in uploads" :key="index" class="chip">
            {{ upload.name }}
            <button style="border: 0; background: none; cursor: pointer; color: var(--danger)" :disabled="busy || pending" @click="uploads.splice(index, 1)">移除</button>
          </span>
        </div>
        <div class="row" style="margin-top: 10px">
          <button class="ghost" :disabled="busy || pending || uploads.length >= 3" @click="attachInput?.click()">添加附件</button>
          <span class="muted">JPG/PNG/PDF，每个 ≤2 MB，最多 3 个</span>
        </div>
        <input ref="attachInput" type="file" accept=".jpg,.jpeg,.png,.pdf" multiple style="display: none" @change="onAttachPicked" />
        <button class="primary" style="margin-top: 12px" :disabled="busy" @click="sendReply">发送</button>
      </section>
      <p v-else class="muted">本工单已关闭，历史记录保留。如有新的问题，可发起新咨询。</p>

      <button class="ghost" @click="backToList">返回我的服务记录</button>
    </template>

    <!-- list mode -->
    <template v-else-if="mode === 'list' && !noSession">
      <button class="primary" :disabled="busy" @click="newTicket">发起咨询</button>
      <h2 style="margin: 4px 0 0">我的服务记录</h2>
      <van-loading v-if="loading && !tickets.length" style="text-align: center; padding: 24px 0">正在读取服务记录</van-loading>
      <section v-for="ticket in tickets" :key="ticket.id" class="card" style="cursor: pointer" @click="openTicket(ticket.id)">
        <div class="row">
          <strong class="grow">{{ ticket.subject }}</strong>
          <span class="chip warning">{{ statusLabels[ticket.status] }}</span>
        </div>
        <p class="muted">{{ dateTime(ticket.updatedAt) }} · 查看记录 ›</p>
      </section>
      <van-empty v-if="!tickets.length && !loading" description="暂无咨询记录，点击“发起咨询”提交问题。" />
      <button v-if="tickets.length < total" class="ghost" :disabled="busy" @click="moreTickets">加载更多服务记录</button>
    </template>
  </main>
</template>
