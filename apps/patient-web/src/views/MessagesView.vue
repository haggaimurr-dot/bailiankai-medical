<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { errorMessage } from '../api'
import { patientApi } from '../patient'
import { useSessionStore } from '../stores/session'
import type { components } from '../../../../packages/contracts/src/schema'

type MessageTemplate = components['schemas']['MessageTemplate']
type MessageConsentChange = components['schemas']['MessageConsentChange']

const statusLabels: Record<string, string> = { accepted: '已授权一次提醒', consumed: '本次授权已使用', rejected: '未同意', revoked: '已撤回' }
const pendingStorageKey = (id: string) => `message-consent-pending:${id}`

interface TemplateRow extends MessageTemplate {
  statusLabel: string
}

const router = useRouter()
const store = useSessionStore()

const templates = ref<TemplateRow[]>([])
const busy = ref(false)
const loading = ref(false)
const error = ref('')
const notice = ref('')
const noSession = ref(false)

let patientId = ''
let generation = 0

async function load() {
  const gen = ++generation
  const session = store.session
  if (!session) {
    templates.value = []
    patientId = ''
    error.value = '请先到“我的资料”登录。'
    noSession.value = true
    return
  }
  noSession.value = false
  patientId = session.patient.id
  busy.value = true
  loading.value = true
  error.value = ''
  try {
    let pending: MessageConsentChange | null = null
    try {
      pending = JSON.parse(window.localStorage.getItem(pendingStorageKey(patientId)) || 'null') as MessageConsentChange | null
    } catch {
      pending = null
    }
    if (pending) {
      // Replay a write-ahead consent change from a previous visit; if this
      // fails the record stays in storage and is retried on the next load.
      await patientApi.setMessageConsent(pending, patientId)
      window.localStorage.removeItem(pendingStorageKey(patientId))
    }
    const [list, consents] = await Promise.all([patientApi.messageTemplates(patientId), patientApi.messageConsents(patientId)])
    if (gen !== generation) return
    templates.value = list.map((template) => ({
      ...template,
      statusLabel: statusLabels[consents.find((c) => c.templateId === template.templateId)?.status || ''] || '尚未授权',
    }))
  } catch (e) {
    if (gen === generation) error.value = errorMessage(e)
  } finally {
    if (gen === generation) {
      busy.value = false
      loading.value = false
    }
  }
}

async function persist(change: MessageConsentChange) {
  window.localStorage.setItem(pendingStorageKey(patientId), JSON.stringify(change))
  await patientApi.setMessageConsent(change, patientId)
  window.localStorage.removeItem(pendingStorageKey(patientId))
}

async function revoke(template: TemplateRow) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await persist({ templateId: template.templateId, status: 'revoked', requestKey: `revoke_${Date.now()}_${Math.random().toString(36).slice(2)}` })
    notice.value = '已停止后续提醒。'
    await load()
  } catch (e) {
    error.value = errorMessage(e)
    busy.value = false
  }
}

onMounted(() => void load())
onUnmounted(() => generation++)
</script>

<template>
  <main class="page">
    <div>
      <h1>消息与复诊提醒</h1>
      <p class="lede">自愿订阅复诊或随访提醒。每次同意可接收一次消息；再次同意不会在本地累加次数。可随时停止后续提醒。</p>
    </div>

    <div v-if="error" class="error-box">{{ error }}</div>
    <div v-if="notice" class="error-box" style="background: var(--mlk-primitive-color-green-50, #ecfdf3); color: var(--green)">{{ notice }}</div>

    <section v-if="noSession" class="card">
      <p class="muted">登录后查看并管理提醒订阅。</p>
      <button class="primary" @click="router.push('/profile')">前往我的资料登录 ›</button>
    </section>

    <template v-else>
      <div class="error-box" style="background: var(--mlk-primitive-color-amber-50, #fff6e5); color: var(--amber)">
        订阅一次提醒需在微信小程序内通过微信订阅消息完成，网页端暂不支持订阅；这里可查看当前订阅状态，并可停止后续提醒。
      </div>
      <van-loading v-if="loading && !templates.length" style="text-align: center; padding: 24px 0">正在读取提醒设置</van-loading>
      <section v-for="template in templates" :key="template.id" class="card">
        <div class="row">
          <strong class="grow">{{ template.kind === 'revisit' ? '复诊提醒' : '随访提醒' }}</strong>
          <span class="chip">{{ template.statusLabel }}</span>
        </div>
        <p class="muted">院区编号：{{ template.clinicId }}</p>
        <div class="row" style="margin-top: 8px">
          <button class="ghost grow" disabled title="请在微信小程序内订阅">订阅一次提醒（小程序内操作）</button>
          <button class="ghost" style="color: var(--danger)" :disabled="busy" @click="revoke(template)">停止后续提醒</button>
        </div>
      </section>
      <van-empty v-if="!templates.length && !loading" description="暂无可订阅的提醒。" />
      <button class="ghost" :disabled="busy" @click="load">刷新</button>
    </template>
  </main>
</template>
