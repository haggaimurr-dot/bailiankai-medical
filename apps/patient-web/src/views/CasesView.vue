<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { showConfirmDialog, showToast } from 'vant'
import type { components } from '../../../../packages/contracts/src/schema'
import { ApiError, errorMessage } from '../api'
import { patientApi } from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'

type CaseView = components['schemas']['CaseView']

const ANGLE_LABELS: Record<string, string> = { front: '正面', left: '左侧 45°', right: '右侧 45°' }

const session = useSessionStore()

const loading = ref(false)
const loadingMore = ref(false)
const loginBusy = ref(false)
const error = ref('')
const needsLogin = ref(!session.loggedIn)
const cases = ref<CaseView[]>([])
const page = ref(0)
const total = ref(0)
const categoryInput = ref('')
const tagInput = ref('')
const appliedFilters = ref<{ category?: string; tag?: string }>({})

const detail = ref<CaseView | null>(null)
const detailLoading = ref(false)
const imageUrls = ref<Record<string, string>>({})
const imageError = ref('')
const imagesReady = ref(false)
const consentBusy = ref(false)
const sliderPos = ref<Record<number, number>>({})

const hasMore = computed(() => page.value * 20 < total.value)

function consentLine(item: CaseView): string {
  const consent = item.consentRevision === item.revision ? '本版本已授权' : '待授权或已撤回'
  const published = item.publishedAt ? '已发布' : '未发布'
  return `${consent} · ${published}`
}

async function load(append = false) {
  if (!session.loggedIn) {
    needsLogin.value = true
    return
  }
  if (append) loadingMore.value = true
  else loading.value = true
  error.value = ''
  try {
    const nextPage = append ? page.value + 1 : 1
    const envelope = await patientApi.cases(nextPage, session.patientId, appliedFilters.value)
    cases.value = append ? [...cases.value, ...(envelope.data || [])] : envelope.data || []
    page.value = envelope.meta?.page ?? nextPage
    total.value = envelope.meta?.total ?? cases.value.length
    needsLogin.value = false
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 401) {
      needsLogin.value = true
      cases.value = []
    } else {
      error.value = errorMessage(cause)
    }
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

async function demoLogin() {
  loginBusy.value = true
  error.value = ''
  try {
    await session.ensureSession()
    needsLogin.value = false
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loginBusy.value = false
  }
}

function applyFilters() {
  appliedFilters.value = {
    ...(categoryInput.value.trim() ? { category: categoryInput.value.trim() } : {}),
    ...(tagInput.value.trim() ? { tag: tagInput.value.trim() } : {}),
  }
  page.value = 0
  void load()
}

function clearDetailImages() {
  for (const url of Object.values(imageUrls.value)) URL.revokeObjectURL(url)
  imageUrls.value = {}
  imagesReady.value = false
  imageError.value = ''
  sliderPos.value = {}
}

async function openDetail(item: CaseView) {
  detailLoading.value = true
  error.value = ''
  clearDetailImages()
  try {
    const fresh = await patientApi.caseDetail(item.id, session.patientId)
    detail.value = fresh
    // Download every pair image (authenticated), then re-fetch the case:
    // if the revision changed meanwhile, the viewed content is stale and
    // consent must not be given for it.
    const urls: Record<string, string> = {}
    let failed = false
    for (const pair of fresh.content.pairs) {
      for (const imageId of [pair.beforeImageId, pair.afterImageId]) {
        try {
          urls[imageId] = await patientApi.casePhotoFile(fresh.id, imageId, session.patientId)
        } catch {
          failed = true
        }
      }
    }
    imageUrls.value = urls
    const recheck = await patientApi.caseDetail(item.id, session.patientId)
    if (recheck.revision !== fresh.revision || recheck.version !== fresh.version) {
      detail.value = recheck
      imageError.value = '案例已变化，请刷新后查看新版本。'
      imagesReady.value = false
      return
    }
    detail.value = recheck
    if (failed) {
      imageError.value = '图片未完整显示，请刷新后再确认授权。'
      imagesReady.value = false
    } else {
      imagesReady.value = true
    }
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 401) needsLogin.value = true
    else error.value = errorMessage(cause)
    detail.value = null
  } finally {
    detailLoading.value = false
  }
}

function closeDetail() {
  detail.value = null
  clearDetailImages()
}

async function setConsent(granted: boolean) {
  const item = detail.value
  if (!item || consentBusy.value) return
  try {
    await showConfirmDialog({
      title: granted ? '独立案例展示授权' : '撤回案例展示授权',
      message: granted
        ? `${item.policyContent}\n\n授权仅针对当前版本（案例内容版本 ${item.revision}）；内容更新后需要重新授权。`
        : '撤回后，本案例将不再公开展示。诊疗照片授权与展示授权相互独立，撤回展示授权不影响诊疗记录。',
      confirmButtonText: granted ? '同意本版本展示' : '确认撤回',
      confirmButtonColor: granted ? '#1763c6' : '#b42318',
    })
  } catch {
    return
  }
  consentBusy.value = true
  try {
    const updated = await patientApi.caseConsent(
      item.id,
      { version: item.version, granted, policyVersion: 'case-display-v1' },
      session.patientId,
    )
    detail.value = updated
    cases.value = cases.value.map((c) => (c.id === updated.id ? updated : c))
    showToast(granted ? '已同意本版本展示' : '已撤回展示授权')
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    consentBusy.value = false
  }
}

onMounted(() => {
  if (session.loggedIn) void load()
})
onBeforeUnmount(clearDetailImages)
</script>

<template>
  <main class="page">
    <h1>我的案例展示授权</h1>
    <p class="lede">先查看本版本的全部文字和图片，再自主决定是否授权。诊疗照片授权不能替代展示授权。</p>

    <div v-if="needsLogin" class="card">
      <h2>登录后查看我的案例</h2>
      <p class="muted">案例展示授权与患者账号绑定。演示环境将创建一个演示身份；旧身份的案例不会转入新身份。</p>
      <button class="primary" :disabled="loginBusy" @click="demoLogin">{{ loginBusy ? '登录中…' : '演示登录' }}</button>
      <div v-if="error" class="error-box" style="margin-top: 10px">{{ error }}</div>
    </div>

    <template v-else-if="!detail">
      <section class="card">
        <label class="field-label" for="case-category">分类</label>
        <input id="case-category" v-model="categoryInput" class="field" maxlength="40" placeholder="按分类筛选" @keyup.enter="applyFilters" />
        <label class="field-label" for="case-tag">标签</label>
        <input id="case-tag" v-model="tagInput" class="field" maxlength="30" placeholder="按标签筛选" @keyup.enter="applyFilters" />
        <button class="primary" style="margin-top: 12px" @click="applyFilters">筛选案例</button>
      </section>

      <div v-if="error" class="error-box">{{ error }}</div>
      <div v-if="loading" class="card"><van-loading size="24px">正在加载案例…</van-loading></div>

      <template v-else>
        <van-empty v-if="!cases.length" description="暂未整理您的案例" />
        <section v-for="item in cases" :key="item.id" class="card case-card" @click="openDetail(item)">
          <strong>{{ item.content.title }}</strong>
          <p class="muted">分类：{{ item.content.category || '未分类' }}</p>
          <div class="tag-row">
            <span v-for="tag in item.content.tags" :key="tag" class="chip">{{ tag }}</span>
          </div>
          <p v-if="item.content.summary" class="muted preline">{{ item.content.summary }}</p>
          <p class="muted">{{ consentLine(item) }}</p>
          <p class="link-line">查看文字和治疗前后图片 ›</p>
        </section>
        <button v-if="hasMore" class="ghost" :disabled="loadingMore" @click="load(true)">{{ loadingMore ? '加载中…' : '加载更多' }}</button>
        <button class="ghost" @click="load()">刷新</button>
      </template>
    </template>

    <template v-else>
      <div v-if="detailLoading" class="card"><van-loading size="24px">正在加载案例详情…</van-loading></div>
      <section class="card">
        <strong class="detail-title">{{ detail.content.title }}</strong>
        <p class="muted">分类：{{ detail.content.category || '未分类' }}</p>
        <div class="tag-row">
          <span v-for="tag in detail.content.tags" :key="tag" class="chip">{{ tag }}</span>
        </div>
        <p v-if="detail.content.summary" class="preline">{{ detail.content.summary }}</p>
        <p v-if="detail.content.body" class="preline">{{ detail.content.body }}</p>
      </section>

      <div v-if="imageError" class="error-box">{{ imageError }}</div>

      <section v-for="(pair, index) in detail.content.pairs" :key="index" class="card">
        <div class="pair-grid">
          <figure>
            <img v-if="imageUrls[pair.beforeImageId]" :src="imageUrls[pair.beforeImageId]" :alt="`治疗前 ${ANGLE_LABELS[pair.angle] || pair.angle}`" />
            <div v-else class="img-missing">治疗前图片未加载</div>
            <figcaption class="muted">治疗前 · {{ ANGLE_LABELS[pair.angle] || pair.angle }} · {{ dateTime(pair.beforeAt) }}</figcaption>
          </figure>
          <figure>
            <img v-if="imageUrls[pair.afterImageId]" :src="imageUrls[pair.afterImageId]" :alt="`治疗后 ${ANGLE_LABELS[pair.angle] || pair.angle}`" />
            <div v-else class="img-missing">治疗后图片未加载</div>
            <figcaption class="muted">治疗后 · {{ ANGLE_LABELS[pair.angle] || pair.angle }} · {{ dateTime(pair.afterAt) }}</figcaption>
          </figure>
        </div>
        <template v-if="imagesReady && imageUrls[pair.beforeImageId] && imageUrls[pair.afterImageId]">
          <p class="muted">拖动滑块查看治疗前后 · {{ ANGLE_LABELS[pair.angle] || pair.angle }}</p>
          <div class="compare">
            <img :src="imageUrls[pair.afterImageId]" alt="治疗后对比图" />
            <img
              class="before-layer"
              :src="imageUrls[pair.beforeImageId]"
              alt="治疗前对比图"
              :style="{ clipPath: `inset(0 ${100 - (sliderPos[index] ?? 50)}% 0 0)` }"
            />
            <span class="divider" :style="{ left: `${sliderPos[index] ?? 50}%` }" />
          </div>
          <input v-model.number="sliderPos[index]" type="range" min="0" max="100" class="compare-range" :aria-label="`治疗前后对比 ${index + 1}`" />
        </template>
      </section>

      <p class="muted">审核医生：{{ detail.reviewerName || '待审核' }} · {{ detail.publishedAt ? '已发布' : '未发布' }}</p>

      <section class="card">
        <h2>案例展示独立授权</h2>
        <p class="preline muted">{{ detail.policyContent }}</p>
        <p class="muted">版本：{{ detail.policyVersion }} · 案例内容版本 {{ detail.revision }}</p>
        <template v-if="detail.consentRevision !== detail.revision">
          <button class="primary" :disabled="!imagesReady || consentBusy" @click="setConsent(true)">
            {{ consentBusy ? '提交中…' : '已查看全部内容，同意本版本展示' }}
          </button>
          <p v-if="!imagesReady" class="muted">全部图片完整显示后才能同意授权。</p>
        </template>
        <button v-else class="ghost danger-text" :disabled="consentBusy" @click="setConsent(false)">撤回展示授权</button>
      </section>

      <div v-if="error" class="error-box">{{ error }}</div>
      <button class="ghost" @click="closeDetail">返回案例列表</button>
    </template>
  </main>
</template>

<style scoped>
.preline { white-space: pre-line; line-height: 1.7; font-size: 14px; }
.case-card { cursor: pointer; }
.tag-row { display: flex; gap: 6px; flex-wrap: wrap; margin: 6px 0; }
.link-line { color: var(--brand); font-size: 13px; margin: 6px 0 0; }
.detail-title { font-size: 17px; }
.pair-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.pair-grid figure { margin: 0; }
.pair-grid img { width: 100%; border-radius: 10px; display: block; }
.img-missing { border: 1px dashed var(--line); border-radius: 10px; padding: 30px 8px; text-align: center; color: var(--muted); font-size: 12px; }
.compare { position: relative; border-radius: 10px; overflow: hidden; margin-top: 8px; }
.compare img { width: 100%; display: block; }
.compare .before-layer { position: absolute; inset: 0; height: 100%; object-fit: cover; }
.compare .divider { position: absolute; top: 0; bottom: 0; width: 2px; background: #fff; box-shadow: 0 0 4px rgba(0, 0, 0, 0.5); transform: translateX(-1px); }
.compare-range { width: 100%; margin-top: 8px; }
.danger-text { color: var(--danger); }
</style>
