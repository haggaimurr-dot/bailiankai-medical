<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showToast } from 'vant'
import { assessmentApi, type AssessmentTask } from '../patient'
import { useSessionStore } from '../stores/session'

// 新增-P2 行 101：历史评估对比。只列出本人已发布（completed）的评估，按时间正序，
// 选择 2–3 条后同屏对照结论与照片。
const router = useRouter()
const session = useSessionStore()
const patientId = session.patient?.id || ''
const tasks = ref<AssessmentTask[]>([])
const loading = ref(false)
const error = ref('')
const selected = ref<string[]>([])
const comparing = ref(false)
const compareData = ref<{ id: string; date: string; conclusion: string; photos: { label: string; url: string }[] }[]>([])

async function load() {
  if (!patientId) return
  loading.value = true
  error.value = ''
  try {
    // 只取已完成的（已发布）评估。
    const result = await assessmentApi.list(patientId, 1, 'completed')
    const list = result.data || []
    // 按创建时间正序（时间正确的顺序）。
    tasks.value = [...list].sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt))
  } catch (e) {
    error.value = '加载评估列表失败，请稍后重试。'
  } finally {
    loading.value = false
  }
}
onMounted(load)

function toggle(id: string) {
  const i = selected.value.indexOf(id)
  if (i >= 0) selected.value.splice(i, 1)
  else if (selected.value.length < 3) selected.value.push(id)
  else showToast('最多同时对比 3 条评估')
}

async function startCompare() {
  if (selected.value.length < 2) { showToast('请至少选择 2 条评估进行对比'); return }
  comparing.value = true
  compareData.value = []
  try {
    // 按选择时的任务时间顺序排列。
    const ordered = selected.value
      .map((id) => tasks.value.find((t) => t.id === id)!)
      .filter(Boolean)
      .sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt))
    for (const task of ordered) {
      const view = await assessmentApi.get(task.id, patientId!)
      const conclusion = view.publishedReview?.note || view.publishedReview?.decision || '已发布'
      const photos: { label: string; url: string }[] = []
      for (const photo of view.photos || []) {
        try {
          const url = await assessmentApi.photo(task.id, photo.id, patientId!)
          photos.push({ label: photo.angle, url })
        } catch { /* 单张加载失败不阻断对比 */ }
      }
      compareData.value.push({
        id: task.id,
        date: new Date(task.createdAt).toLocaleDateString('zh-CN'),
        conclusion,
        photos,
      })
    }
  } catch {
    showToast('加载对比数据失败')
    compareData.value = []
  } finally {
    comparing.value = false
  }
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })
}
</script>

<template>
  <main class="page">
    <h1>历史评估对比</h1>
    <p class="muted">只对比您本人已发布的评估结果，按时间先后排列，可同屏对照结论与照片。</p>

    <div v-if="error" class="error-box">{{ error }}</div>

    <section v-if="!compareData.length" class="card">
      <h2>选择评估（{{ selected.length }}/3）</h2>
      <div v-if="loading" class="muted">加载中…</div>
      <div v-else-if="!tasks.length" class="muted">暂无已发布的评估。</div>
      <label v-for="task in tasks" :key="task.id" class="task-option">
        <input type="checkbox" :checked="selected.includes(task.id)" @change="toggle(task.id)" />
        <span>{{ formatDate(task.createdAt) }}</span>
        <small class="muted">{{ task.id.slice(-6).toUpperCase() }}</small>
      </label>
      <button class="primary" :disabled="selected.length < 2 || comparing" @click="startCompare">
        {{ comparing ? '加载中…' : '开始对比' }}
      </button>
    </section>

    <section v-else class="compare-grid">
      <article v-for="item in compareData" :key="item.id" class="card compare-col">
        <h2>{{ item.date }}</h2>
        <p class="conclusion">{{ item.conclusion }}</p>
        <div class="photos">
          <figure v-for="(photo, i) in item.photos" :key="i">
            <img :src="photo.url" :alt="photo.label" loading="lazy" />
            <figcaption>{{ photo.label }}</figcaption>
          </figure>
        </div>
        <button class="ghost" @click="router.push({ path: '/assessment/result', query: { id: item.id } })">查看完整报告</button>
      </article>
      <button class="ghost" @click="compareData = []; selected = []">重新选择</button>
    </section>
  </main>
</template>

<style scoped>
.page { padding: 16px; max-width: 900px; margin: 0 auto; }
.muted { color: var(--muted); font-size: 13px; }
.card { background: #fff; border-radius: 12px; padding: 16px; margin: 12px 0; box-shadow: 0 1px 4px rgba(0,0,0,.06); }
.task-option { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid #f0f0f0; cursor: pointer; }
.compare-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px; }
.compare-col h2 { margin: 0 0 8px; font-size: 15px; }
.conclusion { font-size: 14px; line-height: 1.7; background: #f7f9f8; border-radius: 8px; padding: 10px; }
.photos { display: flex; gap: 8px; flex-wrap: wrap; margin: 10px 0; }
.photos figure { margin: 0; width: 80px; }
.photos img { width: 80px; height: 80px; object-fit: cover; border-radius: 8px; }
.photos figcaption { font-size: 11px; text-align: center; color: var(--muted); }
.primary, .ghost { margin-top: 12px; }
</style>
