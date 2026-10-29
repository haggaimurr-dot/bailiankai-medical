<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { errorMessage } from '../api'
import { patientApi, type FollowupTask } from '../patient'
import { useSessionStore } from '../stores/session'

const session = useSessionStore()
const loading = ref(true)
const error = ref('')
const timeline = ref<any[]>([])
const patientName = ref('')

onMounted(async () => {
  try {
    const identity = await patientApi.me()
    patientName.value = identity.displayName || ''
    // 拉取全部时间线（分页取完）
    const all: any[] = []
    let page = 1
    for (;;) {
      const env: any = await patientApi.timeline(page, identity.id)
      const items = env.data || []
      all.push(...items)
      if (items.length < 20 || page >= 25) break
      page++
    }
    timeline.value = all
  } catch (cause) { error.value = errorMessage(cause) }
  finally {
    loading.value = false
    // 数据就绪后自动调起打印（用户选“另存为 PDF”）
    setTimeout(() => { if (!error.value) window.print() }, 600)
  }
})

function fmtDate(v: string) {
  if (!v) return ''
  return new Date(v).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
}
</script>

<template>
  <div class="export-page">
    <div v-if="loading" class="loading">正在生成档案…</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <div v-else>
      <header class="export-header">
        <h1>百莲凯医疗美容 · 治疗档案</h1>
        <p>患者：{{ patientName }} · 导出时间：{{ fmtDate(new Date().toISOString()) }}</p>
      </header>
      <section v-for="ev in timeline" :key="ev.id" class="export-item">
        <h3>{{ ev.title || ev.kind }} <small>{{ fmtDate(ev.occurredAt) }}</small></h3>
        <p v-if="ev.summary">{{ ev.summary }}</p>
        <p v-if="ev.detail" class="detail">{{ ev.detail }}</p>
      </section>
      <p v-if="!timeline.length" class="empty">暂无档案记录</p>
      <p class="no-print footer-note">提示：在打印对话框中选择“另存为 PDF”即可导出 PDF 文件。</p>
    </div>
  </div>
</template>

<style scoped>
.export-page { padding: 24px; max-width: 800px; margin: 0 auto; color: #000; background: #fff; }
.export-header { border-bottom: 2px solid #000; padding-bottom: 12px; margin-bottom: 16px; }
.export-header h1 { font-size: 20px; margin: 0 0 8px; }
.export-item { break-inside: avoid; border-bottom: 1px solid #ddd; padding: 10px 0; }
.export-item h3 { font-size: 15px; margin: 0 0 6px; }
.export-item h3 small { color: #666; font-weight: normal; margin-left: 8px; }
.detail { white-space: pre-wrap; font-size: 13px; color: #333; }
.footer-note { color: #666; font-size: 13px; margin-top: 20px; }
.loading, .empty { color: #999; padding: 16px 0; }
.error { color: #c00; }
@media print {
  .no-print { display: none; }
  .export-page { padding: 0; max-width: none; }
}
</style>
