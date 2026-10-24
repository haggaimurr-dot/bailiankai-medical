<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import { api, errorMessage, type Clinic } from '../api'
import { clinicPresentation, previewCredentials, validLocation, type ClinicPresentation } from '../patient-presentation'

const route = useRoute()
const router = useRouter()
const clinicId = String(route.query.clinicId || '')

const loading = ref(true)
const refreshing = ref(false)
const error = ref('')
const clinic = ref<Clinic | null>(null)
const presentation = ref<ClinicPresentation | null>(null)
let generation = 0

const location = computed(() => presentation.value?.location)
const hasLocation = computed(() => validLocation(location.value))
const canCall = computed(() =>
  Boolean(clinic.value && presentation.value && !presentation.value.isDemo && /^[+\d][\d\s()-]{4,31}$/.test(clinic.value.phone)),
)
const callout = computed(() => (presentation.value?.isDemo ? '演示位置 · 非真实院址' : clinic.value?.name || ''))
const mapUrl = computed(() => {
  if (!clinic.value || !validLocation(location.value)) return ''
  const name = encodeURIComponent(presentation.value?.isDemo ? '演示位置（非真实院址）' : clinic.value.name)
  return `https://uri.amap.com/marker?position=${location.value.longitude},${location.value.latitude}&name=${name}`
})

async function load() {
  const gen = ++generation
  loading.value = true
  error.value = ''
  clinic.value = null
  presentation.value = null
  try {
    if (!clinicId) throw new Error('院区入口信息不完整，请返回首页重新选择。')
    const found = (await api.clinics()).find((row) => row.id === clinicId)
    if (gen !== generation) return
    if (!found) throw new Error('院区资料暂不可用，请返回首页重新选择。')
    clinic.value = found
    presentation.value = clinicPresentation(found)
  } catch (cause) {
    if (gen === generation) error.value = errorMessage(cause)
  } finally {
    if (gen === generation) loading.value = false
  }
}

async function onRefresh() {
  await load()
  refreshing.value = false
}

async function openLocation() {
  if (loading.value || !clinic.value || !presentation.value || !hasLocation.value) return
  if (presentation.value.isDemo) {
    try {
      await showConfirmDialog({
        title: '打开演示位置',
        message: '此坐标仅用于地图功能验证，并非真实院址。',
        confirmButtonText: '查看示例',
        confirmButtonColor: '#1763c6',
      })
    } catch {
      return
    }
  }
  window.open(mapUrl.value, '_blank')
}

async function copyAddress() {
  if (!clinic.value) return
  try {
    await navigator.clipboard.writeText(clinic.value.address)
    showToast('地址已复制')
  } catch {
    showToast('复制失败，请长按地址复制')
  }
}

function preview(index: number) {
  previewCredentials(presentation.value?.credentials || [], index)
}

onMounted(() => void load())
onUnmounted(() => { generation += 1 })
</script>

<template>
  <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
    <main class="page">
      <div v-if="loading" class="card state"><van-loading size="24">正在读取院区资料</van-loading></div>
      <div v-else-if="error" class="card state">
        <div class="state-title">院区资料暂不可用</div>
        <p class="muted">{{ error }}</p>
        <button class="ghost" @click="load">重新加载</button>
      </div>
      <template v-else-if="clinic && presentation">
        <div v-if="presentation.isDemo" class="demo-banner">
          <span>Mock 演示资料 · 地图坐标与证照样例不代表真实院址和资质</span>
        </div>

        <section class="card">
          <h1 class="clinic-title">{{ clinic.name }}</h1>
          <p class="lede">{{ clinic.address }}</p>
          <p class="muted">营业时间：{{ presentation.hours || '暂未公布' }}</p>
          <p class="muted">联系电话：{{ clinic.phone }}</p>
          <div class="row actions">
            <button class="ghost small" @click="copyAddress">复制地址</button>
            <a v-if="canCall" class="ghost small tel" :href="`tel:${clinic.phone}`">联系院区</a>
          </div>
        </section>

        <section class="card">
          <h2>院区地图</h2>
          <div v-if="hasLocation && location" class="map-box">
            <div class="map-pin">＋</div>
            <div class="map-callout">{{ callout }}</div>
            <p class="muted">坐标：{{ location.latitude }}, {{ location.longitude }}（GCJ-02）</p>
          </div>
          <p v-else class="muted">院区尚未公布地图位置，可复制地址或联系院区。</p>
          <button v-if="hasLocation" class="primary" @click="openLocation">
            {{ presentation.isDemo ? '查看演示地图位置' : '打开地图 · 就诊导航' }}
          </button>
        </section>

        <section class="card">
          <h2>交通与停车</h2>
          <p class="copy">{{ presentation.transport || '交通信息暂未公布。' }}</p>
        </section>

        <section class="card">
          <h2>到院指引</h2>
          <p class="copy">{{ presentation.arrivalGuide || '到院前请确认预约结果与时段，到院后向前台出示预约信息。' }}</p>
          <button class="text-link" @click="router.push('/appointments')">查看我的预约 ›</button>
        </section>

        <section class="card">
          <h2>机构资质</h2>
          <p v-if="!presentation.credentials.length" class="muted">机构资质资料暂未公布。</p>
          <div v-for="(item, index) in presentation.credentials" :key="index" class="credential">
            <div>{{ item.name }}</div>
            <p v-if="item.number" class="muted">证照编号：{{ item.number }}</p>
            <p v-if="item.issuer" class="muted">发证机构：{{ item.issuer }}</p>
            <img v-if="item.imageUrl" class="credential-image" :src="item.imageUrl" alt="证照图片" @click="preview(index)" />
            <p v-if="item.imageUrl" class="muted">点击查看证照原图</p>
          </div>
        </section>
      </template>
    </main>
  </van-pull-refresh>
</template>

<style scoped>
.state { text-align: center; }
.state-title { font-weight: 700; margin-bottom: 6px; }
.demo-banner { background: #fff6e5; border: 1px solid #f3d9a4; color: var(--amber); border-radius: 10px; padding: 10px 12px; font-size: 12px; line-height: 1.6; }
.clinic-title { font-size: 20px; }
.copy { line-height: 1.7; margin: 0; }
.actions { margin-top: 10px; flex-wrap: wrap; }
button.ghost.small, a.ghost.small { padding: 7px 12px; font-size: 13px; text-decoration: none; display: inline-block; }
.map-box { text-align: center; padding: 18px 0 6px; background: var(--bg); border-radius: 12px; margin-bottom: 12px; }
.map-pin { font-size: 30px; color: var(--brand); font-weight: 800; }
.map-callout { display: inline-block; background: #fff; border: 1px solid var(--line); border-radius: 8px; padding: 4px 10px; font-size: 13px; margin-top: 6px; }
.credential { padding: 8px 0; border-top: 1px solid var(--bg); }
.credential:first-of-type { border-top: 0; }
.credential p { margin: 2px 0; }
.credential-image { max-width: 100%; max-height: 180px; object-fit: contain; margin-top: 6px; cursor: pointer; }
.text-link { border: 0; background: none; color: var(--brand); font-size: 13px; cursor: pointer; padding: 4px 0; }
</style>
