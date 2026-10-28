<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showImagePreview, showToast } from 'vant'
import { errorMessage } from '../api'
import {
  isSessionExpired,
  patientApi,
  type CarePhoto,
  type FollowupTask,
  type FollowupWorkspace,
} from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'

type Angle = CarePhoto['angle']

const STATUS_FILTERS: { key: string; label: string }[] = [
  { key: '', label: '全部' },
  { key: 'pending', label: '待填写' },
  { key: 'submitted', label: '等待医护回评' },
  { key: 'alert', label: '等待医护处理' },
  { key: 'reviewed', label: '医护已回评' },
  { key: 'handled', label: '异常已处理' },
]
const STATUS_LABELS: Record<FollowupTask['status'], string> = {
  pending: '待填写', submitted: '等待医护回评', alert: '等待医护处理', reviewed: '医护已回评', handled: '异常已处理',
}
const SEVERITIES = ['没有不适', '轻微不适', '明显不适', '严重不适']
const ANGLES: { angle: Angle; label: string }[] = [
  { angle: 'front', label: '正面' },
  { angle: 'left', label: '左侧 45°' },
  { angle: 'right', label: '右侧 45°' },
]

const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const appointmentId = String(route.query.appointmentId || '')
const workspaceMode = Boolean(appointmentId)
const presetTaskId = String(route.query.taskId || '')

const loading = ref(true)
const expired = ref(false)
const error = ref('')
const notice = ref('')
const workspace = ref<FollowupWorkspace | null>(null)
const tasks = ref<FollowupTask[]>([])
const statusFilter = ref('')
const page = ref(1)
const total = ref(0)
const selectedId = ref('')
const symptoms = ref('')
const severity = ref(0)
const concern = ref(false)
const saving = ref(false)
const uploadingAngle = ref<Angle | ''>('')
const consentBusy = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const photoUrls = ref<Record<string, string>>({})

const selected = computed(() => tasks.value.find((t) => t.id === selectedId.value) || null)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / 20)))
const treatment = computed(() => workspace.value?.treatment || null)
const revisions = computed(() => workspace.value?.treatmentRevisions || [])

function statusTheme(status: FollowupTask['status']): string {
  if (status === 'pending') return 'warning'
  if (status === 'alert') return 'danger'
  if (status === 'reviewed' || status === 'handled') return 'success'
  return ''
}

function syncForm(task: FollowupTask | null) {
  symptoms.value = task?.symptoms || ''
  severity.value = task?.severity ?? 0
  concern.value = task?.concern ?? false
}

async function loadTaskPhotos(task: FollowupTask) {
  for (const photo of task.photos) {
    if (photoUrls.value[photo.id]) continue
    try {
      photoUrls.value = { ...photoUrls.value, [photo.id]: await patientApi.followupPhotoFile(photo.id, session.patientId) }
    } catch {
      // Preview failure is non-fatal; the upload slot stays usable.
    }
  }
}

async function selectTask(task: FollowupTask) {
  selectedId.value = task.id
  syncForm(task)
  // In list mode, selecting a task also pulls its appointment workspace
  // (treatment record + revisions), like the miniprogram page does.
  if (!workspaceMode && task.appointmentId !== workspace.value?.treatment?.appointmentId) {
    try {
      workspace.value = await patientApi.followupWorkspace(task.appointmentId, session.patientId)
    } catch {
      workspace.value = null
    }
  }
  await loadTaskPhotos(task)
}

async function load() {
  loading.value = true
  expired.value = false
  error.value = ''
  try {
    const patient = await patientApi.me()
    if (workspaceMode) {
      workspace.value = await patientApi.followupWorkspace(appointmentId, patient.id)
      tasks.value = workspace.value.tasks
      total.value = tasks.value.length
    } else {
      const envelope = await patientApi.followups(statusFilter.value, page.value, patient.id)
      tasks.value = envelope.data || []
      total.value = envelope.meta?.total ?? tasks.value.length
    }
    const initial = tasks.value.find((t) => t.id === presetTaskId) || tasks.value[0] || null
    if (initial) await selectTask(initial)
    else {
      selectedId.value = ''
      syncForm(null)
    }
  } catch (cause) {
    if (isSessionExpired(cause)) expired.value = true
    else error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

async function relogin() {
  session.expire()
  try {
    await session.ensureSession()
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  }
}

async function changeFilter(key: string) {
  if (workspaceMode || statusFilter.value === key) return
  statusFilter.value = key
  page.value = 1
  await load()
}

async function changePage(next: number) {
  if (next < 1 || next > totalPages.value) return
  page.value = next
  await load()
}

async function toggleConsent(granted: boolean) {
  const task = selected.value
  if (!task || consentBusy.value) return
  consentBusy.value = true
  notice.value = ''
  try {
    await patientApi.setPhotoConsent(task.appointmentId, granted, session.patientId)
    tasks.value = tasks.value.map((t) => (t.appointmentId === task.appointmentId ? { ...t, photosGranted: granted } : t))
    showToast(granted ? '已授权' : '已撤回授权')
  } catch (cause) {
    notice.value = errorMessage(cause)
  } finally {
    consentBusy.value = false
  }
}

function pickPhoto(angle: Angle) {
  const task = selected.value
  if (!task || task.status !== 'pending' || !task.photosGranted || uploadingAngle.value) return
  if (fileInput.value) {
    fileInput.value.dataset.angle = angle
    fileInput.value.value = ''
    fileInput.value.click()
  }
}

async function onPhotoChosen(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const angle = (input.dataset.angle || '') as Angle
  const task = selected.value
  if (!file || !angle || !task) return
  uploadingAngle.value = angle
  notice.value = ''
  try {
    const saved = await patientApi.uploadFollowupPhoto(task.id, angle, file, session.patientId)
    const updated = { ...task, photos: [...task.photos.filter((p) => p.angle !== angle), saved] }
    tasks.value = tasks.value.map((t) => (t.id === task.id ? updated : t))
    await loadTaskPhotos(updated)
    showToast('照片已保存')
  } catch (cause) {
    notice.value = errorMessage(cause)
  } finally {
    uploadingAngle.value = ''
  }
}

function previewPhoto(photo: CarePhoto) {
  const task = selected.value
  if (!task) return
  const urls = task.photos.map((p) => photoUrls.value[p.id]).filter((u): u is string => Boolean(u))
  const current = photoUrls.value[photo.id]
  if (current) showImagePreview({ images: urls, startPosition: Math.max(0, urls.indexOf(current)) })
}

async function submit() {
  const task = selected.value
  if (!task || saving.value) return
  if (!symptoms.value.trim()) {
    showToast('请填写症状与恢复情况')
    return
  }
  saving.value = true
  notice.value = ''
  try {
    const updated = await patientApi.submitFollowup(
      task.id,
      { version: task.version, symptoms: symptoms.value.trim(), severity: severity.value, concern: concern.value },
      session.patientId,
    )
    tasks.value = tasks.value.map((t) => (t.id === task.id ? updated : t))
    syncForm(updated)
    notice.value = updated.status === 'alert' ? '已提交并进入医护关注队列。' : '反馈已提交，请等待医护回评。'
    showToast('反馈已提交')
  } catch (cause) {
    notice.value = `${errorMessage(cause)} 可重试相同反馈或刷新查看提交结果。`
  } finally {
    saving.value = false
  }
}

// Advice-derived todos (titles prefixed 【护理】/【复诊】/【注意事项】) are
// plain care steps, not symptom reports: the patient marks them done without
// having to invent symptoms. The submit records '已按医嘱完成' as the
// response so staff can tell a completed todo from a symptom report.
const isAdviceTodo = (task: FollowupTask) => /^(【护理】|【复诊】|【注意事项】)/.test(task.title)
async function markAdviceDone() {
  const task = selected.value
  if (!task || saving.value || !isAdviceTodo(task)) return
  saving.value = true
  notice.value = ''
  try {
    const updated = await patientApi.submitFollowup(
      task.id,
      { version: task.version, symptoms: '已按医嘱完成', severity: 0, concern: false },
      session.patientId,
    )
    tasks.value = tasks.value.map((t) => (t.id === task.id ? updated : t))
    syncForm(updated)
    notice.value = '已标记完成，等待医护回评。'
    showToast('已标记完成')
  } catch (cause) {
    notice.value = `${errorMessage(cause)} 可重试或刷新查看提交结果。`
  } finally {
    saving.value = false
  }
}

function openCarePlan() {
  const t = treatment.value
  if (!t) return
  router.push({ path: '/care/plan', query: { appointmentId: t.appointmentId, ...(t.planVersionId ? { versionId: t.planVersionId } : {}) } })
}

onMounted(load)
onBeforeUnmount(() => {
  for (const url of Object.values(photoUrls.value)) URL.revokeObjectURL(url)
})
</script>

<template>
  <main class="page">
    <p class="chip">开发演示 · 请使用虚构症状及照片</p>
    <h1>治疗后随访</h1>
    <p class="lede">治疗后按计划填写恢复反馈，医护会回评；如有明显不适或异常，请直接联系机构，不要只等待随访。</p>

    <div v-if="loading" class="card"><van-loading size="24px">正在读取随访…</van-loading></div>

    <div v-else-if="expired" class="card">
      <h2>登录已失效</h2>
      <p class="muted">演示身份已过期。创建新身份后，旧记录仍属于旧身份，不会自动转入新身份。</p>
      <button class="primary" @click="relogin">重新登录</button>
    </div>

    <div v-else-if="error" class="card">
      <h2>随访暂时无法读取</h2>
      <div class="error-box">{{ error }}</div>
      <button class="ghost" style="margin-top: 10px" @click="load">刷新查看</button>
    </div>

    <template v-else>
      <section v-if="treatment" class="card">
        <h2>实际治疗记录 · V{{ treatment.version }}</h2>
        <p v-if="treatment.summary" class="body-text">{{ treatment.summary }}</p>
        <p v-if="treatment.notes" class="muted">{{ treatment.notes }}</p>
        <p class="muted">治疗时间：{{ dateTime(treatment.treatedAt) }}（北京时间）</p>
        <button class="ghost" @click="openCarePlan">查看关联预约的已发布方案</button>
        <div v-if="revisions.length > 1" class="revisions">
          <h3>更正历史</h3>
          <div v-for="rev in revisions" :key="rev.version" class="revision">
            <p class="muted">V{{ rev.version }} · {{ dateTime(rev.changedAt) }} · {{ rev.correctionReason || '原始记录' }} · 治疗时间 {{ dateTime(rev.treatedAt) }}</p>
            <p v-if="rev.summary" class="body-text">{{ rev.summary }}</p>
            <p v-if="rev.notes" class="muted">{{ rev.notes }}</p>
          </div>
        </div>
      </section>

      <div v-if="!workspaceMode" class="filter-row">
        <button
          v-for="f in STATUS_FILTERS"
          :key="f.key"
          class="chip filter-chip"
          :class="{ active: statusFilter === f.key }"
          @click="changeFilter(f.key)"
        >
          {{ f.label }}
        </button>
      </div>

      <div v-if="!tasks.length" class="card">
        <h2>暂时没有随访任务</h2>
        <p class="muted">医护记录实际治疗后，会生成随访任务。</p>
      </div>

      <section
        v-for="task in tasks"
        :key="task.id"
        class="card task-card"
        :class="{ selected: task.id === selectedId }"
        @click="selectTask(task)"
      >
        <div class="row">
          <strong class="grow">{{ task.title }}</strong>
          <span class="chip" :class="statusTheme(task.status)">{{ STATUS_LABELS[task.status] }}</span>
        </div>
        <p class="muted">计划时间：{{ dateTime(task.dueAt) }} · 北京时间</p>
        <p class="muted">随访负责人：{{ task.assigneeName || '待分配' }}</p>
        <p v-if="task.overdue && task.status === 'pending'" class="overdue">已超过计划时间，仍可提交反馈。</p>
      </section>

      <div v-if="!workspaceMode && tasks.length" class="row pager">
        <button class="ghost" :disabled="page <= 1" @click="changePage(page - 1)">上一页</button>
        <span class="muted grow center">第 {{ page }} 页 / 共 {{ total }} 项</span>
        <button class="ghost" :disabled="page >= totalPages" @click="changePage(page + 1)">下一页</button>
      </div>

      <section v-if="selected" class="card">
        <div class="row">
          <h2 class="grow">{{ selected.title }}</h2>
          <span class="chip" :class="statusTheme(selected.status)">{{ STATUS_LABELS[selected.status] }}</span>
        </div>

        <template v-if="selected.status === 'pending'">
          <label class="field-label">当前感受</label>
          <div class="filter-row">
            <button
              v-for="(label, index) in SEVERITIES"
              :key="label"
              class="chip filter-chip"
              :class="{ active: severity === index }"
              @click="severity = index"
            >
              {{ label }}
            </button>
          </div>
          <label class="field-label" for="symptoms">症状与恢复情况 *</label>
          <textarea id="symptoms" v-model="symptoms" class="field" rows="4" maxlength="2000" placeholder="请描述目前的恢复情况（演示环境请勿填写真实隐私）" />
          <div class="row switch-row">
            <span class="grow">希望医护重点关注</span>
            <van-switch v-model="concern" size="22" />
          </div>
        </template>
        <template v-else>
          <p class="body-text">已提交反馈：{{ SEVERITIES[selected.severity] || '未记录' }}<template v-if="selected.concern"> · 希望医护重点关注</template></p>
          <p v-if="selected.symptoms" class="muted">{{ selected.symptoms }}</p>
        </template>

        <div class="photo-block">
          <h3>随访照片</h3>
          <p class="muted">任务照片按任务保存，使用本次预约的照片授权；提交反馈后不能再替换。</p>
          <template v-if="selected.photosGranted">
            <p class="chip success">照片已授权</p>
            <button class="ghost" :disabled="consentBusy" @click="toggleConsent(false)">撤回授权</button>
          </template>
          <button v-else class="ghost" :disabled="consentBusy" @click="toggleConsent(true)">同意照片用于诊疗及随访</button>
          <div class="photo-grid">
            <div v-for="item in ANGLES" :key="item.angle" class="photo-cell">
              <img
                v-if="selected.photos.find((p) => p.angle === item.angle) && photoUrls[selected.photos.find((p) => p.angle === item.angle)!.id]"
                :src="photoUrls[selected.photos.find((p) => p.angle === item.angle)!.id]"
                :alt="`${item.label}随访照片`"
                @click="previewPhoto(selected.photos.find((p) => p.angle === item.angle)!)"
              />
              <div v-else class="photo-placeholder">{{ item.label }}</div>
              <button
                v-if="selected.status === 'pending'"
                class="ghost small"
                :disabled="!selected.photosGranted || uploadingAngle !== ''"
                @click="pickPhoto(item.angle)"
              >
                {{ uploadingAngle === item.angle ? '上传中…' : `${selected.photos.find((p) => p.angle === item.angle) ? '替换' : '选择'}${item.label}` }}
              </button>
            </div>
          </div>
        </div>

        <p v-if="selected.status === 'alert'" class="error-box">已进入医护关注队列。如需及时帮助，请直接联系机构。</p>
        <div v-if="selected.response" class="response-block">
          <h3>医护回评</h3>
          <p class="body-text">{{ selected.response }}</p>
        </div>
        <div v-if="selected.disposition" class="response-block">
          <h3>异常处理记录</h3>
          <p class="body-text">{{ selected.disposition }}</p>
        </div>

        <div v-if="notice" class="notice-box">{{ notice }}</div>
        <button v-if="selected.status === 'pending'" class="primary" :disabled="saving" @click="submit">
          {{ saving ? '提交中…' : '提交随访反馈' }}
        </button>
        <button v-if="selected.status === 'pending' && isAdviceTodo(selected)" class="ghost" :disabled="saving" style="margin-top: 8px" @click="markAdviceDone">
          {{ saving ? '提交中…' : '标记已完成' }}
        </button>
        <button class="ghost" style="margin-top: 8px" @click="load">刷新随访结果</button>
        <input ref="fileInput" type="file" accept="image/*" hidden @change="onPhotoChosen" />
      </section>
    </template>
  </main>
</template>

<style scoped>
.body-text { font-size: 14px; line-height: 1.7; white-space: pre-line; }
.revisions { border-top: 1px solid var(--line); margin-top: 12px; padding-top: 10px; }
.revisions h3, .photo-block h3, .response-block h3 { font-size: 14px; margin: 0 0 6px; }
.revision { border-top: 1px dashed var(--line); padding: 6px 0; }
.filter-row { display: flex; gap: 8px; flex-wrap: wrap; }
.filter-chip { background: #fff; cursor: pointer; font-size: 12px; padding: 5px 12px; }
.filter-chip.active { color: var(--brand); border-color: var(--brand); background: #eff6ff; font-weight: 600; }
.task-card { cursor: pointer; }
.task-card.selected { border-color: var(--brand); }
.overdue { color: var(--amber); font-size: 13px; }
.pager .center { text-align: center; }
.switch-row { margin: 12px 0; font-size: 14px; }
.photo-block { border-top: 1px solid var(--line); margin-top: 14px; padding-top: 12px; }
.photo-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; margin-top: 10px; }
.photo-cell img { width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: 10px; display: block; }
.photo-placeholder { aspect-ratio: 1; border: 1px dashed var(--line); border-radius: 10px; display: flex; align-items: center; justify-content: center; color: var(--muted); font-size: 12px; }
.photo-cell .small { width: 100%; margin-top: 6px; padding: 7px 4px; font-size: 12px; }
.response-block { background: var(--bg); border-radius: 10px; padding: 10px 12px; margin-top: 10px; }
.notice-box { background: #eff6ff; color: var(--brand-dark); border-radius: 10px; padding: 10px 12px; font-size: 13px; line-height: 1.6; margin: 10px 0; }
</style>
