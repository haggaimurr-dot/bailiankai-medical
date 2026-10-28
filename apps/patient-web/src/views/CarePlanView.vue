<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import { errorMessage } from '../api'
import { isSessionExpired, patientApi, type CarePlanVersion } from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const appointmentId = String(route.query.appointmentId || '')
const highlightId = String(route.query.versionId || '')

const loading = ref(true)
const expired = ref(false)
const error = ref('')
const versions = ref<CarePlanVersion[]>([])
const readState = ref<Record<string, { adviceVersion: string; readAt: string | null }>>({})
const readError = ref('')
const confirmingId = ref('')
const todosCreated = ref(0)

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
    const list = await patientApi.planVersions(appointmentId, patient.id)
    versions.value = list
    // Per-version advice/read state (only versions that carry instructions).
    const entries = await Promise.allSettled(
      list
        .filter((v) => v.content.instructions.trim())
        .map(async (v) => {
          const event = await patientApi.advice('plan', v.id, patient.id)
          return [v.id, { adviceVersion: event.adviceVersion, readAt: event.readAt }] as const
        }),
    )
    const map: Record<string, { adviceVersion: string; readAt: string | null }> = {}
    for (const entry of entries) if (entry.status === 'fulfilled') map[entry.value[0]] = entry.value[1]
    readState.value = map
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

async function confirmRead(version: CarePlanVersion) {
  const state = readState.value[version.id]
  if (!state || confirmingId.value) return
  try {
    await showConfirmDialog({
      title: '确认已阅读医嘱',
      message: `${version.content.instructions}\n\n阅读确认仅表示已阅读医嘱，不作为知情同意或治疗授权。`,
      confirmButtonText: '我已阅读',
      confirmButtonColor: '#1763c6',
    })
  } catch {
    return
  }
  confirmingId.value = version.id
  readError.value = ''
  try {
    const receipt = await patientApi.confirmAdviceRead('plan', version.id, state.adviceVersion, session.patientId)
    readState.value = { ...readState.value, [version.id]: { adviceVersion: receipt.adviceVersion, readAt: receipt.readAt } }
    todosCreated.value = receipt.todosCreated || 0
    showToast('已确认阅读')
  } catch {
    readError.value = '阅读确认未完成，请重试。若多次失败，请联系负责医护确认。'
  } finally {
    confirmingId.value = ''
  }
}

onMounted(load)
</script>

<template>
  <main class="page">
    <p class="chip">开发演示 · 方案内容为医生录入后发布</p>
    <h1>治疗方案</h1>

    <div v-if="loading" class="card">
      <van-loading size="24px">正在读取治疗方案…</van-loading>
    </div>

    <div v-else-if="expired" class="card">
      <h2>登录已失效</h2>
      <p class="muted">演示身份已过期。创建新身份后，旧记录仍属于旧身份，不会自动转入新身份。</p>
      <button class="primary" @click="relogin">重新登录</button>
    </div>

    <div v-else-if="error" class="card">
      <h2>方案暂时无法读取</h2>
      <div class="error-box">{{ error }}</div>
      <button class="ghost" style="margin-top: 10px" @click="load">重新加载</button>
    </div>

    <template v-else>
      <div v-if="!versions.length" class="card">
        <h2>暂时没有已发布方案</h2>
        <p class="muted">负责医生发布后，方案会显示在这里。</p>
      </div>

      <section v-for="version in versions" :key="version.id" class="card plan-version">
        <div class="row">
          <strong class="grow">方案 V{{ version.version }} · {{ dateTime(version.publishedAt) }}</strong>
          <span v-if="version.id === highlightId" class="chip success">当前查看的方案版本</span>
        </div>
        <p v-if="version.content.summary" class="preline">{{ version.content.summary }}</p>

        <div v-for="(stage, index) in version.content.stages" :key="index" class="stage">
          <h3>{{ stage.title }}</h3>
          <p class="preline muted">{{ stage.body }}</p>
        </div>

        <div v-if="version.content.instructions.trim()" class="instructions">
          <h3>医嘱与注意事项</h3>
          <p class="preline">{{ version.content.instructions }}</p>
          <p class="muted">阅读确认仅表示已阅读医嘱，不作为知情同意或治疗授权。</p>
          <template v-if="readState[version.id]">
            <p v-if="readState[version.id].readAt" class="chip success">已确认阅读 · {{ dateTime(readState[version.id].readAt!) }}</p>
            <button
              v-else
              class="primary"
              :disabled="confirmingId === version.id"
              @click="confirmRead(version)"
            >
              {{ confirmingId === version.id ? '提交中…' : '我已阅读医嘱' }}
            </button>
          </template>
        </div>

        <p class="muted footer-line">
          负责医生 · {{ version.publishedBy }}<template v-if="version.changeReason"> · {{ version.changeReason }}</template>
        </p>
      </section>

      <div v-if="readError" class="error-box">{{ readError }}</div>
      <div v-if="todosCreated > 0" class="card">
        <h2>已按医嘱生成 {{ todosCreated }} 条待办</h2>
        <p class="muted">每条医嘱对应一条待办，可在「我的待办」中查看与完成；也会出现在恢复日历当天。</p>
        <button class="primary" @click="router.push('/followups')">查看我的待办</button>
      </div>
      <p class="muted">方案按版本保留。若对内容有疑问，请联系负责医护。</p>
      <button class="ghost" @click="router.back()">返回</button>
    </template>
  </main>
</template>

<style scoped>
.preline { white-space: pre-line; line-height: 1.7; font-size: 14px; }
.stage { border-top: 1px solid var(--line); padding-top: 10px; margin-top: 10px; }
.stage h3, .instructions h3 { font-size: 14px; margin: 0 0 6px; }
.instructions { background: var(--bg); border-radius: 10px; padding: 12px; margin-top: 12px; }
.footer-line { margin-top: 12px; }
.plan-version .primary { margin-top: 8px; }
</style>
