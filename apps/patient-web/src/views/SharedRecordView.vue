<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { API_BASE_URL } from '../api'

const route = useRoute()
const loading = ref(true)
const error = ref('')
const record = ref<any>(null)

onMounted(async () => {
  const token = route.params.token as string
  try {
    const res = await fetch(`${API_BASE_URL}/shared-records/${encodeURIComponent(token)}`)
    if (!res.ok) throw new Error('分享链接无效或已过期')
    const body = await res.json()
    record.value = body.data
  } catch (e) { error.value = (e as Error).message }
  finally { loading.value = false }
})
</script>

<template>
  <div class="page">
    <h2>分享的治疗档案</h2>
    <div v-if="loading" class="loading">加载中…</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <div v-else>
      <p class="muted">该分享链接将于 {{ new Date(record.expiresAt).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }) }} 过期</p>
      <div v-for="ev in record.timeline" :key="ev.id" class="card">
        <strong>{{ ev.title || ev.kind }}</strong>
        <p class="muted">{{ ev.occurredAt || '' }}</p>
        <p v-if="ev.summary">{{ ev.summary }}</p>
      </div>
      <p v-if="!record.timeline?.length" class="empty">暂无档案记录</p>
    </div>
  </div>
</template>

<style scoped>
.page { padding: 16px; max-width: 640px; margin: 0 auto; }
.card { border: 1px solid #eee; border-radius: 10px; padding: 12px 14px; margin-bottom: 10px; }
.muted { color: #999; font-size: 13px; }
.loading, .empty { color: #999; padding: 16px 0; }
.error { color: #c00; padding: 16px 0; }
</style>
