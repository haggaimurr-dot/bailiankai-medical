<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showImagePreview } from 'vant'
import { api, ApiError, errorMessage, type Clinic } from '../api'
import { assessmentApi, type AssessmentView } from '../patient'
import { useSessionStore } from '../stores/session'
import type { components } from '../../../../packages/contracts/src/schema'

type AssessmentCreate = components['schemas']['AssessmentCreate']
type PendingAction = {
  action: 'consent' | 'submit' | 'quality-check' | 'cancel'
  body: components['schemas']['AssessmentVersion'] | components['schemas']['AssessmentConsentChange']
  key: string
}
type PendingUpload = { angle: string; file: File; version: number; key: string }

const angles = [
  { value: 'front', label: '正面' },
  { value: 'left', label: '本人左侧45°' },
  { value: 'right', label: '本人右侧45°' },
] as const
// 拍摄指引：示意图（非真人照片，避免用患者照片作示例）。
// 正确示例按三角度各一张；错误示范列出质检最常见的四种失败原因与重拍做法。
const guideCorrect = [
  { value: 'front', label: '正面 · 正确', tip: '面部居中，双眼平视镜头' },
  { value: 'left', label: '本人左侧 45° · 正确', tip: '向左转头，露出左侧面颊' },
  { value: 'right', label: '本人右侧 45° · 正确', tip: '向右转头，露出右侧面颊' },
] as const
const guideMistakes = [
  { id: 'backlit', title: '逆光', cause: '光源在身后，面部发黑，肤色与痘印看不清。', fix: '面向窗户或灯光，让光落在脸上再拍。' },
  { id: 'occluded', title: '遮挡', cause: '头发、手或口罩遮住面部，关键区域缺失。', fix: '把头发拢到耳后，移开遮挡物再拍。' },
  { id: 'tilted', title: '角度倾斜', cause: '头部倾斜或转头过多，左右照片无法对应。', fix: '按角度转头，双眼平视镜头，头摆正。' },
  { id: 'dark', title: '太暗 / 过曝', cause: '光线太暗或太亮，面部细节丢失。', fix: '选自然光充足处，避免直射强光与闪光灯。' },
] as const
const qualityLabels: Record<string, string> = { pending: '等待检查', passed: '质检通过', rejected: '请按提示补拍', unavailable: '检查暂不可用' }
const assessmentKey = () => `ai-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`

const route = useRoute()
const router = useRouter()
const store = useSessionStore()

const clinics = ref<Clinic[]>([])
const clinicId = ref('')
const view = ref<AssessmentView | null>(null)
const error = ref('')
const busy = ref(false)
const needsLogin = ref(false)
const qualityAvailable = ref(false)
const modelAvailable = ref(false)
const actionPending = ref(false)
const uploadPending = ref(false)
const recovering = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

let taskId = String(route.query.id || '')
let supersedes = String(route.query.supersedes || '')
let requestedClinicId = String(route.query.clinicId || '')
let patientId = ''
let generation = 0
let visible = false
let timer: ReturnType<typeof setTimeout> | undefined
let pendingAction: PendingAction | null = null
let pendingUpload: PendingUpload | null = null
let pendingAngle = ''
const previewUrls: string[] = []

const angleRows = computed(() =>
  angles.map((angle) => {
    const photo = view.value?.photos.find((p) => p.angle === angle.value)
    return {
      ...angle,
      photoId: photo?.id || '',
      quality: photo ? qualityLabels[photo.qualityCheck.status] || '检查暂不可用' : '未上传',
      advice: photo?.qualityCheck.retakeAdvice || [],
    }
  }),
)
const canSubmit = computed(() => {
  const current = view.value
  return Boolean(
    current &&
      current.task.status === 'draft' &&
      current.consent.granted &&
      current.photos.length === 3 &&
      current.photos.every((p) => p.qualityCheck.status === 'passed') &&
      modelAvailable.value,
  )
})

function apply(next: AssessmentView) {
  view.value = next
}

function schedule() {
  if (timer) clearTimeout(timer)
  if (visible && taskId) timer = setTimeout(() => void load(true), 5000)
}

async function load(poll = false) {
  if (!visible || busy.value) {
    schedule()
    return
  }
  const session = store.session
  if (!session) {
    patientId = ''
    needsLogin.value = true
    view.value = null
    return
  }
  if (patientId && patientId !== session.patient.id) {
    taskId = ''
    pendingAction = null
    pendingUpload = null
    actionPending.value = false
    uploadPending.value = false
  }
  patientId = session.patient.id
  const gen = ++generation
  needsLogin.value = false
  if (!poll) error.value = ''
  try {
    if (!poll) {
      const [clinicList, capabilities] = await Promise.all([api.clinics(), assessmentApi.capabilities()])
      if (gen !== generation) return
      clinics.value = clinicList
      const wanted = requestedClinicId || view.value?.task.clinicId || clinicId.value
      clinicId.value = clinicList.some((c) => c.id === wanted) ? wanted : clinicList[0]?.id || ''
      qualityAvailable.value = capabilities.qualityAvailable
      modelAvailable.value = capabilities.modelAvailable
      recovering.value = Boolean(window.localStorage.getItem(`assessment-create-v1:${patientId}`))
    }
    if (taskId) {
      const next = await assessmentApi.get(taskId, patientId)
      if (gen !== generation) return
      apply(next)
    }
  } catch (e) {
    if (gen === generation) {
      error.value = errorMessage(e)
      if (e instanceof ApiError && [401, 403, 404].includes(e.status)) view.value = null
    }
  } finally {
    if (gen === generation) schedule()
  }
}

async function create() {
  if (busy.value || taskId || !patientId) return
  const storageKey = `assessment-create-v1:${patientId}`
  let body: AssessmentCreate | null = null
  try {
    body = JSON.parse(window.localStorage.getItem(storageKey) || 'null') as AssessmentCreate | null
  } catch {
    body = null
  }
  if (!body) {
    if (!clinicId.value) return
    body = {
      clinicId: clinicId.value,
      requestKey: assessmentKey(),
      ...(supersedes ? { supersedesAssessmentId: supersedes } : {}),
    }
    window.localStorage.setItem(storageKey, JSON.stringify(body))
  }
  const gen = ++generation
  busy.value = true
  error.value = ''
  recovering.value = true
  try {
    const next = await assessmentApi.create(body, patientId)
    if (gen !== generation) return
    taskId = next.task.id
    supersedes = ''
    window.localStorage.removeItem(storageKey)
    recovering.value = false
    apply(next)
    void router.replace({ query: { ...route.query, id: taskId } })
  } catch (e) {
    if (gen === generation) {
      error.value = errorMessage(e)
      if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
        window.localStorage.removeItem(storageKey)
        recovering.value = false
      }
    }
  } finally {
    if (gen === generation) {
      busy.value = false
      schedule()
    }
  }
}

async function perform(action?: PendingAction['action'], granted?: boolean) {
  const current = view.value
  if (!current || busy.value || !patientId) return
  if (!pendingAction) {
    if (!action) return
    pendingAction = {
      action,
      key: assessmentKey(),
      body:
        action === 'consent'
          ? { version: current.task.version, policyVersion: current.consent.version, granted: !!granted }
          : { version: current.task.version },
    }
  }
  const operation = pendingAction
  const gen = ++generation
  busy.value = true
  error.value = ''
  actionPending.value = true
  try {
    const next = await assessmentApi.change(taskId, operation.action, operation.body, operation.key, patientId)
    if (gen !== generation) return
    pendingAction = null
    actionPending.value = false
    apply(next)
  } catch (e) {
    if (gen === generation) {
      error.value = errorMessage(e)
      if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
        pendingAction = null
        actionPending.value = false
      }
    }
  } finally {
    if (gen === generation) {
      busy.value = false
      void load(true)
    }
  }
}

async function toggleConsent() {
  const current = view.value
  if (!current || busy.value) return
  try {
    await showConfirmDialog({
      title: current.consent.granted ? '撤回AI用途授权' : 'AI用途授权',
      message: current.consent.granted
        ? '撤回后停止本任务分析和医护访问，重新评估需创建新任务。'
        : `${current.consent.content}\n版本：${current.consent.version}`,
      confirmButtonText: current.consent.granted ? '撤回' : '同意',
      confirmButtonColor: '#1763c6',
    })
  } catch {
    return
  }
  void perform('consent', !current.consent.granted)
}

function choosePhoto(angle: string) {
  const current = view.value
  if (!current?.consent.granted || current.task.status !== 'draft' || busy.value || pendingAction || pendingUpload) return
  pendingAngle = angle
  fileInput.value?.click()
}

function onFilePicked(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  const current = view.value
  if (!file || !current || !pendingAngle) return
  pendingUpload = { angle: pendingAngle, file, version: current.task.version, key: assessmentKey() }
  void upload()
}

async function upload() {
  const operation = pendingUpload
  if (!operation || busy.value) return
  const gen = ++generation
  busy.value = true
  error.value = ''
  uploadPending.value = true
  try {
    const next = await assessmentApi.upload(taskId, operation.angle, operation.file, operation.version, operation.key, patientId)
    if (gen !== generation) return
    pendingUpload = null
    uploadPending.value = false
    apply(next)
  } catch (e) {
    if (gen === generation) {
      error.value = errorMessage(e)
      if (e instanceof ApiError && e.status === 409 && e.code !== 'PATIENT_IDENTITY_CHANGED') {
        try {
          const current = await assessmentApi.get(taskId, patientId)
          if (gen !== generation) return
          apply(current)
          pendingUpload = { ...operation, version: current.task.version, key: assessmentKey() }
        } catch {
          pendingUpload = null
          uploadPending.value = false
        }
      } else if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
        pendingUpload = null
        uploadPending.value = false
      }
    }
  } finally {
    if (gen === generation) {
      busy.value = false
      schedule()
    }
  }
}

async function preview(photoId: string) {
  if (!photoId) return
  const gen = generation
  try {
    const url = await assessmentApi.photo(taskId, photoId, patientId)
    if (gen !== generation || !visible) {
      URL.revokeObjectURL(url)
      return
    }
    previewUrls.push(url)
    showImagePreview({ images: [url], closeable: true, onClose: () => URL.revokeObjectURL(url) })
  } catch (e) {
    if (gen === generation) error.value = errorMessage(e)
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
  pendingAction = null
  pendingUpload = null
  for (const url of previewUrls) URL.revokeObjectURL(url)
})
</script>

<template>
  <main class="page">
    <div>
      <p class="lede">PHOTO ASSISTED OBSERVATION</p>
      <h1>AI 辅助评估</h1>
      <p class="lede">准备正面、本人左侧及右侧45°照片，分析后由医生复核。</p>
    </div>

    <div v-if="error" class="error-box">{{ error }}</div>

    <section v-if="needsLogin" class="card">
      <p class="muted">请先登录，任务和照片归属于您的患者账号。</p>
      <button class="primary" @click="router.push('/profile')">前往登录</button>
    </section>

    <template v-else>
      <div v-if="!qualityAvailable || !modelAvailable" class="error-box" style="background: var(--mlk-primitive-color-amber-50, #fff6e5); color: var(--amber)">
        辅助观察服务尚未就绪。您可以保存准备资料，也可以直接预约面诊。
      </div>

      <section v-if="!view" class="card">
        <label class="field-label" for="assessment-clinic">评估院区</label>
        <select id="assessment-clinic" v-model="clinicId" class="field" :disabled="recovering || busy">
          <option v-for="clinic in clinics" :key="clinic.id" :value="clinic.id">{{ clinic.name }}</option>
        </select>
        <p v-if="!clinics.length" class="muted">暂无可预约院区</p>
        <button class="primary" style="margin-top: 12px" :disabled="busy || !clinics.length" @click="create">
          {{ recovering ? '恢复原创建申请' : '创建评估任务' }}
        </button>
      </section>

      <template v-else>
        <section class="card">
          <p class="muted">任务编号：{{ view.task.id }}</p>
          <p class="muted">任务版本 {{ view.task.version }}</p>
          <h2>独立AI用途授权</h2>
          <p class="muted">{{ view.consent.granted ? '已同意AI用途' : '尚未同意或已撤回' }}</p>
          <button
            class="ghost"
            style="width: 100%"
            :disabled="busy || actionPending || uploadPending || (!view.consent.granted && view.task.status !== 'draft')"
            @click="toggleConsent"
          >
            {{ view.consent.granted ? '查看并撤回授权' : '阅读并同意AI用途' }}
          </button>
        </section>

        <section class="card">
          <h2>三角度照片</h2>
          <p class="muted">自然光、无遮挡、关闭美颜；本人左右方向请核对。短边至少512像素。</p>
          <div class="guide">
            <h3>正确示例</h3>
            <div class="guide-row">
              <figure v-for="item in guideCorrect" :key="item.value" class="guide-fig">
                <svg v-if="item.value === 'front'" viewBox="0 0 72 72" class="guide-svg" aria-hidden="true">
                  <rect width="72" height="72" rx="10" fill="#eef4fb" />
                  <circle cx="59" cy="13" r="6" fill="#f6c453" />
                  <path d="M14 64 Q14 48 36 48 Q58 48 58 64 Z" fill="#9db8d6" />
                  <circle cx="36" cy="33" r="16" fill="#f2c9a0" />
                  <circle cx="30" cy="31" r="1.8" fill="#333" />
                  <circle cx="42" cy="31" r="1.8" fill="#333" />
                  <path d="M31 40 Q36 43 41 40" stroke="#b07a4a" stroke-width="1.5" fill="none" />
                </svg>
                <svg v-else-if="item.value === 'left'" viewBox="0 0 72 72" class="guide-svg" aria-hidden="true">
                  <rect width="72" height="72" rx="10" fill="#eef4fb" />
                  <circle cx="59" cy="13" r="6" fill="#f6c453" />
                  <path d="M14 64 Q14 48 36 48 Q58 48 58 64 Z" fill="#9db8d6" />
                  <circle cx="35" cy="33" r="16" fill="#f2c9a0" />
                  <path d="M49 30 Q56 33 49 37 Z" fill="#f2c9a0" />
                  <circle cx="41" cy="31" r="1.8" fill="#333" />
                  <path d="M38 40 Q42 42 46 40" stroke="#b07a4a" stroke-width="1.5" fill="none" />
                </svg>
                <svg v-else viewBox="0 0 72 72" class="guide-svg" aria-hidden="true">
                  <rect width="72" height="72" rx="10" fill="#eef4fb" />
                  <circle cx="59" cy="13" r="6" fill="#f6c453" />
                  <path d="M14 64 Q14 48 36 48 Q58 48 58 64 Z" fill="#9db8d6" />
                  <circle cx="37" cy="33" r="16" fill="#f2c9a0" />
                  <path d="M23 30 Q16 33 23 37 Z" fill="#f2c9a0" />
                  <circle cx="31" cy="31" r="1.8" fill="#333" />
                  <path d="M26 40 Q30 42 34 40" stroke="#b07a4a" stroke-width="1.5" fill="none" />
                </svg>
                <figcaption>
                  <span class="guide-ok">✓ {{ item.label }}</span>
                  <small>{{ item.tip }}</small>
                </figcaption>
              </figure>
            </div>
            <h3>错误示范 · 对照重拍</h3>
            <div class="guide-row mistakes">
              <figure v-for="item in guideMistakes" :key="item.id" class="guide-fig wrong">
                <svg v-if="item.id === 'backlit'" viewBox="0 0 72 72" class="guide-svg" aria-hidden="true">
                  <rect width="72" height="72" rx="10" fill="#fff3c4" />
                  <circle cx="58" cy="14" r="10" fill="#f6c453" />
                  <path d="M14 64 Q14 48 36 48 Q58 48 58 64 Z" fill="#5a5a5a" />
                  <circle cx="36" cy="33" r="16" fill="#3f3f3f" />
                </svg>
                <svg v-else-if="item.id === 'occluded'" viewBox="0 0 72 72" class="guide-svg" aria-hidden="true">
                  <rect width="72" height="72" rx="10" fill="#eef4fb" />
                  <path d="M14 64 Q14 48 36 48 Q58 48 58 64 Z" fill="#9db8d6" />
                  <circle cx="36" cy="33" r="16" fill="#f2c9a0" />
                  <path d="M20 34 Q18 14 36 14 Q54 14 52 32 Q50 22 42 20 Q28 18 24 34 Z" fill="#5b4636" />
                  <circle cx="30" cy="31" r="1.8" fill="#333" />
                </svg>
                <svg v-else-if="item.id === 'tilted'" viewBox="0 0 72 72" class="guide-svg" aria-hidden="true">
                  <rect width="72" height="72" rx="10" fill="#eef4fb" />
                  <g transform="rotate(20 36 36)">
                    <path d="M14 64 Q14 48 36 48 Q58 48 58 64 Z" fill="#9db8d6" />
                    <circle cx="36" cy="33" r="16" fill="#f2c9a0" />
                    <circle cx="30" cy="31" r="1.8" fill="#333" />
                    <circle cx="42" cy="31" r="1.8" fill="#333" />
                    <path d="M31 40 Q36 43 41 40" stroke="#b07a4a" stroke-width="1.5" fill="none" />
                  </g>
                </svg>
                <svg v-else viewBox="0 0 72 72" class="guide-svg" aria-hidden="true">
                  <rect width="72" height="72" rx="10" fill="#3a3a3a" />
                  <path d="M14 64 Q14 48 36 48 Q58 48 58 64 Z" fill="#555" />
                  <circle cx="36" cy="33" r="16" fill="#6b6b6b" />
                  <circle cx="30" cy="31" r="1.5" fill="#444" />
                  <circle cx="42" cy="31" r="1.5" fill="#444" />
                </svg>
                <figcaption>
                  <span class="guide-bad">✕ {{ item.title }}</span>
                  <small>原因：{{ item.cause }}</small>
                  <small>正确做法：{{ item.fix }}</small>
                </figcaption>
              </figure>
            </div>
            <p class="muted">质检不通过时，按提示找到对应的错误示范重拍。</p>
          </div>
          <div v-for="row in angleRows" :key="row.value" class="card" style="margin-top: 10px">
            <strong>{{ row.label }} · {{ row.quality }}</strong>
            <p v-for="line in row.advice" :key="line" class="muted">{{ line }}</p>
            <div class="row" style="margin-top: 8px">
              <button
                class="primary grow"
                :disabled="busy || actionPending || uploadPending || !view.consent.granted || view.task.status !== 'draft'"
                @click="choosePhoto(row.value)"
              >
                {{ row.photoId ? '替换照片' : '选择照片' }}
              </button>
              <button v-if="row.photoId" class="ghost" @click="preview(row.photoId)">预览</button>
            </div>
          </div>

          <div style="display: flex; flex-direction: column; gap: 10px; margin-top: 14px">
            <button
              v-if="view.task.status === 'draft'"
              class="ghost"
              :disabled="busy || !qualityAvailable || actionPending || uploadPending"
              @click="perform('quality-check')"
            >
              重新检查照片
            </button>
            <button v-if="uploadPending" class="primary" :disabled="busy" @click="upload">重试原照片上传</button>
            <button v-if="actionPending" class="primary" :disabled="busy" @click="perform()">重试原操作</button>
            <button v-if="view.task.status === 'draft'" class="primary" :disabled="busy || !canSubmit || actionPending || uploadPending" @click="perform('submit')">
              提交辅助评估
            </button>
            <button class="ghost" @click="router.push({ path: '/assessment/tasks', query: { id: view.task.id } })">查看任务进度</button>
          </div>
        </section>
      </template>
    </template>

    <input ref="fileInput" type="file" accept="image/*" style="display: none" @change="onFilePicked" />
    <button class="ghost" @click="router.push('/schedule')">直接预约面诊 ›</button>
    <p class="muted" style="text-align: center">照片辅助观察，具体以面诊为准。</p>
  </main>
</template>

<style scoped>
.guide { margin-top: 12px; }
.guide h3 { font-size: 14px; margin: 14px 0 8px; }
.guide-row { display: flex; flex-wrap: wrap; gap: 8px; }
.guide-fig { flex: 1 1 30%; min-width: 100px; margin: 0; }
.guide-row.mistakes .guide-fig { flex-basis: 45%; }
.guide-svg { width: 100%; height: auto; display: block; border-radius: 10px; }
.guide-fig figcaption { margin-top: 6px; font-size: 12px; line-height: 1.6; }
.guide-fig figcaption small { display: block; color: var(--muted); }
.guide-ok { color: var(--green); font-weight: 700; }
.guide-bad { color: var(--danger); font-weight: 700; }
</style>
