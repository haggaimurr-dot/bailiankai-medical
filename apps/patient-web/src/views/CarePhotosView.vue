<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { showConfirmDialog, showImagePreview, showToast } from 'vant'
import { errorMessage } from '../api'
import { isSessionExpired, patientApi, type Appointment, type CarePhoto } from '../patient'
import { useSessionStore } from '../stores/session'

type Angle = CarePhoto['angle']

const ANGLES: { angle: Angle; label: string; instruction: string }[] = [
  { angle: 'front', label: '正面', instruction: '头部摆正，双眼看镜头；额头、双颊与下巴完整入镜。' },
  { angle: 'left', label: '左侧 45°', instruction: '鼻尖朝自己的右肩转约 45°，露出左侧面颊；保持头部水平。' },
  { angle: 'right', label: '右侧 45°', instruction: '鼻尖朝自己的左肩转约 45°，露出右侧面颊；保持头部水平。' },
]
const TIPS = [
  '光线均匀：面向自然光或明亮室内灯，避免逆光和浓重阴影。',
  '保留真实肤况：卸妆、关闭美颜与滤镜，不遮挡面部。',
  '保持同一距离：每次拍摄与镜头保持相近距离，方便前后对照。',
  '对焦后再拍：轻点屏幕对焦面部，确认清晰后再拍摄；左右方向以本人为准，请使用原图。',
]

const route = useRoute()
const session = useSessionStore()
const appointmentId = String(route.query.appointmentId || '')

const loading = ref(true)
const expired = ref(false)
const error = ref('')
const actionError = ref('')
const appointment = ref<Appointment | null>(null)
const consentGranted = ref(false)
const consentBusy = ref(false)
const photos = ref<CarePhoto[]>([])
const photoUrls = ref<Partial<Record<Angle, string>>>({})
const photoReadFailed = ref<Partial<Record<Angle, boolean>>>({})
const uploading = ref<Angle | ''>('')
const fileInput = ref<HTMLInputElement | null>(null)

// Pending (picked but not yet uploaded) photo review state.
const pending = ref<{ angle: Angle; file: File; url: string; hint: string; blocked: boolean } | null>(null)
const checkTexture = ref(false)
const checkDirection = ref(false)

const uploadAllowed = computed(() => appointment.value?.status === 'pending' || appointment.value?.status === 'confirmed')
const savedCount = computed(() => photos.value.length)
const pendingLabel = computed(() => ANGLES.find((a) => a.angle === pending.value?.angle)?.label || '')

function revoke(url?: string) {
  if (url) URL.revokeObjectURL(url)
}

async function loadPhotoUrl(photo: CarePhoto) {
  try {
    const url = await patientApi.photoFile(photo.id, session.patientId)
    revoke(photoUrls.value[photo.angle])
    photoUrls.value = { ...photoUrls.value, [photo.angle]: url }
    photoReadFailed.value = { ...photoReadFailed.value, [photo.angle]: false }
  } catch {
    photoReadFailed.value = { ...photoReadFailed.value, [photo.angle]: true }
  }
}

async function load() {
  loading.value = true
  expired.value = false
  error.value = ''
  if (!appointmentId) {
    error.value = '缺少预约编号，请从预约详情重新进入。'
    loading.value = false
    return
  }
  try {
    const patient = await patientApi.me()
    const [appt, consent, list] = await Promise.all([
      patientApi.appointment(appointmentId),
      patientApi.photoConsent(appointmentId, patient.id),
      patientApi.photos(appointmentId, patient.id),
    ])
    appointment.value = appt
    consentGranted.value = consent.granted
    photos.value = list
    await Promise.allSettled(list.map((photo) => loadPhotoUrl(photo)))
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

async function toggleConsent(granted: boolean) {
  try {
    await showConfirmDialog({
      title: granted ? '同意照片用于本次诊疗' : '撤回照片授权',
      message: granted
        ? '授权后，本次预约的面诊照片可供负责医护在诊疗中查看。授权会记录版本与时间，可随时撤回。'
        : '撤回后，医护将无法再查看这些照片。已保存的照片不会用于其他用途。',
      confirmButtonText: granted ? '同意授权' : '确认撤回',
      confirmButtonColor: granted ? '#1763c6' : '#b42318',
    })
  } catch {
    return
  }
  consentBusy.value = true
  actionError.value = ''
  try {
    const result = await patientApi.setPhotoConsent(appointmentId, granted, session.patientId)
    consentGranted.value = result.granted
    showToast(granted ? '已授权' : '已撤回授权')
  } catch (cause) {
    actionError.value = errorMessage(cause)
  } finally {
    consentBusy.value = false
  }
}

function pick(angle: Angle) {
  if (!consentGranted.value || !uploadAllowed.value || uploading.value) return
  pending.value && revoke(pending.value.url)
  pending.value = null
  if (fileInput.value) {
    fileInput.value.dataset.angle = angle
    fileInput.value.value = ''
    fileInput.value.click()
  }
}

async function onFileChosen(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const angle = (input.dataset.angle || '') as Angle
  if (!file || !angle) return
  // On-device advisory inspection (resolution only, never diagnostic):
  // short edge < 320 px blocks, < 640 px warns — same thresholds as the
  // miniprogram's photo-quality checks; angle/clarity stay manual.
  const url = URL.createObjectURL(file)
  let hint = '已读取照片，请对照下方提示人工核对清晰度与方向。'
  let blocked = false
  try {
    const bitmap = await createImageBitmap(file)
    const shortEdge = Math.min(bitmap.width, bitmap.height)
    bitmap.close()
    if (shortEdge < 320) {
      blocked = true
      hint = `照片短边仅 ${shortEdge} 像素，低于 320 像素下限，这张照片暂时不能上传，请选择更清晰的原图或重新拍摄。`
    } else if (shortEdge < 640) {
      hint = `照片短边 ${shortEdge} 像素，略低于建议的 640 像素，请确认面颊纹理清晰后再保存。`
    } else {
      hint = `照片分辨率 ${shortEdge} 像素（短边），达到建议标准；清晰度与光线请人工核对。`
    }
  } catch {
    blocked = true
    hint = '照片读取失败，这张照片暂时不能上传，请重新选择。'
  }
  checkTexture.value = false
  checkDirection.value = false
  pending.value = { angle, file, url, hint, blocked }
}

function discardPending() {
  if (pending.value) revoke(pending.value.url)
  pending.value = null
}

async function confirmUpload() {
  const item = pending.value
  if (!item || item.blocked || uploading.value) return
  if (!checkTexture.value || !checkDirection.value) {
    showToast('请先完成两项人工核对')
    return
  }
  uploading.value = item.angle
  actionError.value = ''
  try {
    // Fresh gating, as in the miniprogram: consent and appointment status
    // are re-read immediately before the upload.
    const [consent, appt] = await Promise.all([
      patientApi.photoConsent(appointmentId, session.patientId),
      patientApi.appointment(appointmentId),
    ])
    consentGranted.value = consent.granted
    appointment.value = appt
    if (!consent.granted) {
      actionError.value = '照片授权已撤回，请重新授权后选择照片。'
      return
    }
    if (!(appt.status === 'pending' || appt.status === 'confirmed')) {
      actionError.value = '预约状态已更新，面诊照片当前仅供查看。'
      return
    }
    const saved = await patientApi.uploadPhoto(appointmentId, item.angle, item.file, session.patientId)
    photos.value = [...photos.value.filter((p) => p.angle !== item.angle), saved]
    discardPending()
    await loadPhotoUrl(saved)
    showToast('照片已保存')
  } catch (cause) {
    actionError.value = errorMessage(cause)
  } finally {
    uploading.value = ''
  }
}

function preview(angle: Angle) {
  const urls = ANGLES.map((a) => photoUrls.value[a.angle]).filter((u): u is string => Boolean(u))
  const current = photoUrls.value[angle]
  if (!current) return
  showImagePreview({ images: urls, startPosition: Math.max(0, urls.indexOf(current)) })
}

function retryRead(angle: Angle) {
  const photo = photos.value.find((p) => p.angle === angle)
  if (photo) void loadPhotoUrl(photo)
}

function photoOf(angle: Angle) {
  return photos.value.find((p) => p.angle === angle)
}

onMounted(load)
onBeforeUnmount(() => {
  for (const url of Object.values(photoUrls.value)) revoke(url)
  if (pending.value) revoke(pending.value.url)
})
</script>

<template>
  <main class="page">
    <p class="chip">开发演示 · 请勿上传真实患者照片</p>
    <h1>面诊照片</h1>
    <p class="lede">选填项目，不影响预约。流程：01 拍摄/选图 › 02 本机检查 › 03 确认保存。每个角度保留一张，保存后会替换该角度的照片。</p>

    <div v-if="loading" class="card"><van-loading size="24px">正在读取照片资料…</van-loading></div>

    <div v-else-if="expired" class="card">
      <h2>登录已失效</h2>
      <p class="muted">演示身份已过期。创建新身份后，旧记录仍属于旧身份，不会自动转入新身份。</p>
      <button class="primary" @click="relogin">重新登录</button>
    </div>

    <div v-else-if="error" class="card">
      <h2>照片资料暂时无法读取</h2>
      <div class="error-box">{{ error }}</div>
      <button class="ghost" style="margin-top: 10px" @click="load">重新读取</button>
    </div>

    <template v-else>
      <section class="card">
        <h2>拍摄前，做好这四件事</h2>
        <ol class="tips">
          <li v-for="tip in TIPS" :key="tip">{{ tip }}</li>
        </ol>
      </section>

      <section class="card">
        <h2>照片用途授权</h2>
        <p class="muted">授权会被记录版本与时间；撤回后，医护将无法再查看这些照片。照片授权与手机号授权相互独立。</p>
        <template v-if="consentGranted">
          <p class="chip success">已授权 · 已保存 {{ savedCount }} / 3</p>
          <button class="ghost danger-text" :disabled="consentBusy" @click="toggleConsent(false)">撤回授权</button>
        </template>
        <button v-else class="primary" :disabled="consentBusy" @click="toggleConsent(true)">同意照片用于本次诊疗</button>
      </section>

      <div v-if="actionError" class="error-box">{{ actionError }}</div>

      <section v-if="pending" class="card">
        <div class="row">
          <h2 class="grow">检查这张照片</h2>
          <span class="chip warning">尚未上传</span>
        </div>
        <p class="muted">{{ pendingLabel }} · 本机检查（仅提示，不作诊断）</p>
        <img :src="pending.url" class="pending-img" alt="待保存照片预览" @click="showImagePreview([pending.url])" />
        <p :class="pending.blocked ? 'error-box' : 'muted'">{{ pending.hint }}</p>
        <template v-if="!pending.blocked">
          <label class="check"><input v-model="checkTexture" type="checkbox" /> 面颊纹理清楚，没有明显模糊或遮挡</label>
          <label class="check"><input v-model="checkDirection" type="checkbox" /> 符合{{ pendingLabel }}指引，已核对本人左右方向</label>
          <button class="primary" :disabled="uploading !== ''" @click="confirmUpload">
            {{ uploading ? '保存中…' : '确认并保存照片' }}
          </button>
        </template>
        <button class="ghost" style="margin-top: 8px" @click="discardPending">放弃这张，重新选择</button>
      </section>

      <h2 class="section-title">三个拍摄角度（{{ savedCount }} / 3）</h2>
      <section v-for="item in ANGLES" :key="item.angle" class="card">
        <div class="row">
          <strong class="grow">{{ item.label }}</strong>
          <span class="chip" :class="photoOf(item.angle) ? 'success' : ''">{{ photoOf(item.angle) ? '已保存' : '尚未上传' }}</span>
        </div>
        <p class="muted">{{ item.instruction }}</p>
        <img
          v-if="photoOf(item.angle) && photoUrls[item.angle]"
          :src="photoUrls[item.angle]"
          class="saved-img"
          :alt="`${item.label}照片`"
          @click="preview(item.angle)"
        />
        <div v-else-if="photoOf(item.angle) && photoReadFailed[item.angle]" class="read-failed">
          <p class="muted">照片已保存，预览暂时无法显示。</p>
          <button class="ghost" @click="retryRead(item.angle)">重试读取</button>
        </div>
        <div v-else-if="photoOf(item.angle)" class="read-failed"><van-loading size="20px">正在读取预览…</van-loading></div>
        <div v-else class="pose-placeholder">示意轮廓 · {{ item.label }}</div>
        <div class="row" style="margin-top: 10px">
          <button
            class="primary grow"
            :disabled="!consentGranted || !uploadAllowed || uploading !== ''"
            @click="pick(item.angle)"
          >
            {{ photoOf(item.angle) ? '重新拍摄或选图' : '选择照片' }}
          </button>
        </div>
        <p v-if="photoOf(item.angle)" class="muted">重新拍摄或选图，确认保存后替换当前照片。</p>
      </section>

      <p v-if="!uploadAllowed" class="muted">当前预约的面诊照片仅供查看；治疗后的新照片请在随访任务中补充。</p>
      <p v-else-if="!consentGranted" class="muted">同意照片用途授权后即可选择照片。</p>
      <input ref="fileInput" type="file" accept="image/*" hidden @change="onFileChosen" />
    </template>
  </main>
</template>

<style scoped>
.tips { margin: 0; padding-left: 20px; line-height: 1.8; font-size: 13px; color: var(--muted); }
.section-title { font-size: 16px; margin: 4px 0 0; }
.pending-img, .saved-img { width: 100%; border-radius: 10px; display: block; margin: 8px 0; }
.pose-placeholder, .read-failed { border: 1px dashed var(--line); border-radius: 10px; padding: 26px 12px; text-align: center; color: var(--muted); font-size: 13px; margin: 8px 0; }
.check { display: flex; gap: 8px; align-items: flex-start; font-size: 14px; line-height: 1.6; margin: 8px 0; }
.danger-text { color: var(--danger); }
</style>
