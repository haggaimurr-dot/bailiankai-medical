<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog } from 'vant'
import { ApiError, errorMessage } from '../api'
import { patientApi, type PatientIdentity, type PrivacyConsent, type PrivacyPolicy } from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'

const router = useRouter()
const store = useSessionStore()

const patient = ref<PatientIdentity | null>(null)
const name = ref('')
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const policy = ref<PrivacyPolicy | null>(null)
const privacy = ref<PrivacyConsent | null>(null)
const bindingCode = ref('')

let generation = 0

const phoneStatus = computed(() => {
  const status = patient.value?.phoneAuthorization
  return status === 'granted' ? '已授权' : status === 'revoked' ? '已撤回' : '未授权'
})
const identityLabel = computed(() => (patient.value?.loginKind === 'wechat' ? '微信患者账号' : '本机演示身份'))

async function confirmDialog(title: string, message: string, confirmText = '确定'): Promise<boolean> {
  try {
    await showConfirmDialog({ title, message, confirmButtonText: confirmText, confirmButtonColor: '#1763c6' })
    return true
  } catch {
    return false
  }
}

function accept(next: PatientIdentity) {
  patient.value = next
  name.value = next.displayName
}

async function handleError(e: unknown) {
  if (e instanceof ApiError && e.code === 'VERSION_CONFLICT') {
    await load()
    error.value = '资料已在其他设备修改，已加载最新资料，请核对后重新操作。'
  } else {
    error.value = errorMessage(e)
    if (!store.session) patient.value = null
  }
}

async function load() {
  const gen = ++generation
  if (!store.session) {
    patient.value = null
    privacy.value = null
    loading.value = true
    error.value = ''
    try {
      const loaded = await patientApi.privacyPolicy()
      if (gen === generation) policy.value = loaded
    } catch (e) {
      if (gen === generation) error.value = errorMessage(e)
    } finally {
      if (gen === generation) loading.value = false
    }
    return
  }
  loading.value = true
  error.value = ''
  try {
    const me = await patientApi.me()
    const [loadedPolicy, loadedPrivacy] = await Promise.all([patientApi.privacyPolicy(), patientApi.privacyConsent(me.id)])
    if (gen === generation) {
      accept(me)
      policy.value = loadedPolicy
      privacy.value = loadedPrivacy
    }
  } catch (e) {
    if (gen === generation) {
      error.value = errorMessage(e)
      if (!store.session) patient.value = null
    }
  } finally {
    if (gen === generation) loading.value = false
  }
}

async function login() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const confirmed = await confirmDialog(
      '创建演示身份',
      '将创建新的演示患者身份。旧身份的预约与记录仍归旧身份，不会自动转入新身份。',
      '创建',
    )
    if (!confirmed) return
    await store.createSession()
    await load()
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}

async function save() {
  const current = patient.value
  if (busy.value || !current) return
  const trimmed = name.value.trim()
  if (!trimmed || Array.from(trimmed).length > 40) {
    error.value = '请填写1–40字姓名。'
    return
  }
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    accept(await patientApi.updateProfile(trimmed, current.version || 1, current.id))
    notice.value = '资料已保存。'
  } catch (e) {
    await handleError(e)
  } finally {
    busy.value = false
  }
}

async function bindChart() {
  const current = patient.value
  if (busy.value || !current) return
  const code = bindingCode.value.trim().toLowerCase()
  if (!/^[a-f0-9]{32}$/.test(code)) {
    error.value = '请粘贴医护提供的完整32位绑定码。'
    return
  }
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const preview = await patientApi.previewBinding(code, current.id)
    const confirmed = await confirmDialog(
      '确认绑定院内档案',
      `姓名：${preview.displayName}\n院区：${preview.clinicName}\n档案编号：${preview.patientId}\n请确认这是您本人的档案。绑定后将使用此档案查看问诊与诊疗记录。`,
      '确认绑定',
    )
    if (!confirmed) return
    await patientApi.bindPatient(code, preview.version, current.id)
    bindingCode.value = ''
    await load()
    notice.value = '院内档案已绑定。重新登录同一微信账号可恢复此档案。'
  } catch (e) {
    await handleError(e)
  } finally {
    busy.value = false
  }
}

async function revokePhone() {
  const current = patient.value
  if (busy.value || !current) return
  busy.value = true
  try {
    const confirmed = await confirmDialog(
      '撤回账号手机号授权',
      '撤回后将清除账号手机号。已提交预约的联系人手机号继续用于该次预约联系。',
      '撤回授权',
    )
    if (!confirmed) return
    error.value = ''
    notice.value = ''
    accept(await patientApi.revokePhone(current.version || 1, current.id))
    notice.value = '已撤回授权并清除账号手机号。'
  } catch (e) {
    await handleError(e)
  } finally {
    busy.value = false
  }
}

async function acceptPrivacy() {
  const current = patient.value
  const currentPolicy = policy.value
  if (busy.value || !current || !currentPolicy) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const confirmed = await confirmDialog(
      '确认隐私说明',
      `请阅读页面所示隐私说明。确认同意版本 ${currentPolicy.version}？手机号及照片授权仍独立选择。`,
      '同意',
    )
    if (!confirmed) return
    await patientApi.setPrivacyConsent(currentPolicy.version, 'accepted', current.id)
    await load()
    notice.value = '已保存隐私同意记录。'
  } catch (e) {
    await handleError(e)
  } finally {
    busy.value = false
  }
}

async function revokePrivacy() {
  const current = patient.value
  const currentPolicy = policy.value
  if (busy.value || !current || !currentPolicy) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const confirmed = await confirmDialog(
      '撤回隐私同意',
      '撤回将记录为不同意，不自动删除既有诊疗记录，也不代替手机号或照片的独立撤回。',
      '撤回',
    )
    if (!confirmed) return
    await patientApi.setPrivacyConsent(currentPolicy.version, 'revoked', current.id)
    await load()
    notice.value = '已记录撤回隐私同意。'
  } catch (e) {
    await handleError(e)
  } finally {
    busy.value = false
  }
}

async function logout() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await patientApi.logout()
    generation++
    patient.value = null
    privacy.value = null
    name.value = ''
    notice.value = '已退出当前设备。重新登录同一微信账号可恢复原资料。'
  } catch (e) {
    await handleError(e)
  } finally {
    busy.value = false
  }
}

onMounted(() => void load())
onUnmounted(() => generation++)
</script>

<template>
  <main class="page">
    <div>
      <p class="lede">MY PROFILE</p>
      <h1>我的资料</h1>
    </div>

    <section class="card" style="cursor: pointer" @click="router.push({ path: '/knowledge', query: { favorites: '1' } })">我的科普收藏 ›</section>
    <section class="card" style="cursor: pointer" @click="router.push('/assessment/tasks')">我的 AI 评估 ›</section>
    <p class="muted">同一微信账号登录，可在不同设备找回资料、预约和治疗记录。</p>

    <section v-if="loading" class="card">
      <van-loading style="text-align: center; padding: 16px 0">正在读取资料</van-loading>
    </section>

    <section v-else-if="!patient" class="card">
      <h2>登录患者账号</h2>
      <p class="muted">登录后管理个人资料。手机号授权可独立选择。</p>
      <button class="primary" :disabled="busy" @click="login">创建演示身份</button>
    </section>

    <template v-else>
      <section class="card">
        <h2>患者资料</h2>
        <p class="muted">{{ identityLabel }} · {{ patient.id }}</p>
        <label class="field-label" for="profile-name">姓名</label>
        <input id="profile-name" v-model="name" class="field" maxlength="40" :disabled="busy" placeholder="请输入姓名" />
        <button class="primary" style="margin-top: 12px" :disabled="busy" @click="save">保存资料</button>
      </section>

      <section class="card">
        <h2>绑定院内档案</h2>
        <p class="muted">如医护已为您人工建档，请向建档医护领取绑定码，核对档案后绑定。绑定码15分钟有效，已有业务记录的微信账号需联系医护核对。</p>
        <label class="field-label" for="binding-code">绑定码</label>
        <input
          id="binding-code"
          :value="bindingCode"
          class="field"
          maxlength="32"
          :disabled="busy"
          placeholder="粘贴医护提供的绑定码"
          @input="bindingCode = ($event.target as HTMLInputElement).value.trim().toLowerCase()"
        />
        <button class="ghost" style="width: 100%; margin-top: 12px" :disabled="busy" @click="bindChart">核对并绑定档案</button>
      </section>

      <section class="card">
        <h2>账号手机号</h2>
        <p><strong>{{ patient.phone || '尚未保存手机号' }}</strong></p>
        <p class="muted">授权状态：{{ phoneStatus }}</p>
        <p class="muted">用于患者账号联系。预约时可另外填写本人或代办联系人的手机号。修改或撤回这里的授权，不会改写已提交预约的联系人信息，也不会撤回诊疗照片授权。</p>
        <p class="muted">手机号授权需在微信小程序内通过微信一键授权完成，网页端暂不支持。</p>
        <button v-if="patient.phoneAuthorization === 'granted'" class="ghost" style="width: 100%; color: var(--danger)" :disabled="busy" @click="revokePhone">
          撤回手机号授权
        </button>
      </section>

      <button class="ghost" :disabled="busy" @click="load">刷新资料</button>

      <section class="card">
        <h2>常用入口</h2>
        <div style="display: flex; flex-direction: column; gap: 10px">
          <button class="ghost" @click="router.push('/appointments')">我的预约 ›</button>
          <button class="ghost" @click="router.push('/records')">治疗档案 ›</button>
          <button class="ghost" @click="router.push('/support')">在线咨询 ›</button>
        </div>
      </section>

      <section class="card" style="cursor: pointer" @click="router.push('/messages')">消息与复诊提醒 ›</section>
      <section class="card" style="cursor: pointer" @click="router.push('/settings/notifications')">提醒偏好设置 ›</section>
      <section class="card" style="cursor: pointer" @click="router.push({ path: '/cases', query: { mine: '1' } })">我的案例 · 展示授权与撤回 ›</section>

      <button class="ghost" :disabled="busy" @click="logout">退出当前设备</button>
    </template>

    <section v-if="policy" class="card">
      <h2>{{ policy.title }}</h2>
      <p class="muted">版本 {{ policy.version }} · {{ policy.effectiveAt }}</p>
      <p class="muted" style="white-space: pre-wrap">{{ policy.content }}</p>
      <template v-if="patient">
        <p class="muted">当前版本：{{ privacy?.accepted ? '已同意' : '尚未同意或已撤回' }}</p>
        <button v-if="!privacy?.accepted" class="primary" :disabled="busy" @click="acceptPrivacy">阅读并同意当前版本</button>
        <button v-else class="ghost" style="width: 100%; color: var(--danger)" :disabled="busy" @click="revokePrivacy">撤回隐私同意</button>
        <p v-for="event in privacy?.events || []" :key="event.id" class="muted">
          {{ dateTime(event.recordedAt) }} · {{ event.policyVersion }} · {{ event.action === 'accepted' ? '同意' : '撤回' }}
        </p>
      </template>
    </section>

    <div v-if="error" class="error-box">{{ error }}</div>
    <div v-if="notice" class="error-box" style="background: var(--mlk-primitive-color-green-50, #ecfdf3); color: var(--green)">{{ notice }}</div>
  </main>
</template>
