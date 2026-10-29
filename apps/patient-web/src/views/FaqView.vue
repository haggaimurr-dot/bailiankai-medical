<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { errorMessage } from '../api'
import { patientApi } from '../patient'
import { useSessionStore } from '../stores/session'

const session = useSessionStore()
const loading = ref(true)
const error = ref('')
const keyword = ref('')
const faqs = ref<any[]>([])
const openId = ref('')

const grouped = computed(() => {
  const map = new Map<string, any[]>()
  for (const f of faqs.value) {
    const list = map.get(f.category) || []
    list.push(f)
    map.set(f.category, list)
  }
  return [...map.entries()]
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    faqs.value = await patientApi.faqs(session.patient?.id, keyword.value.trim()) || []
  } catch (cause) { error.value = errorMessage(cause) }
  finally { loading.value = false }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <h2>常见问题</h2>
    <div class="search"><input v-model="keyword" placeholder="输入关键词搜索，如：复诊、忌口" @keyup.enter="load" /><button @click="load">搜索</button></div>
    <div v-if="loading" class="loading">加载中…</div>
    <div v-else-if="error" class="error">{{ error }}<button @click="load">重试</button></div>
    <div v-else-if="!faqs.length" class="empty">{{ keyword ? '没有找到相关问题，换个关键词试试。' : '暂无常见问题。' }}</div>
    <div v-else>
      <section v-for="[cat, list] in grouped" :key="cat" class="cat">
        <h3>{{ cat }}</h3>
        <div v-for="f in list" :key="f.id" class="qa">
          <button class="q" @click="openId = openId === f.id ? '' : f.id">{{ f.question }}<span class="arrow">{{ openId === f.id ? '▲' : '▼' }}</span></button>
          <div v-if="openId === f.id" class="a">{{ f.answer }}</div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.page { padding: 16px; max-width: 640px; margin: 0 auto; }
.search { display: flex; gap: 8px; margin: 12px 0; }
.search input { flex: 1; padding: 10px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 15px; }
.search button { padding: 10px 20px; background: #07c160; color: #fff; border: none; border-radius: 8px; font-size: 15px; }
.cat { margin-bottom: 20px; }
.cat h3 { font-size: 15px; color: #666; margin: 0 0 8px; }
.qa { border: 1px solid #eee; border-radius: 10px; margin-bottom: 8px; overflow: hidden; }
.q { width: 100%; text-align: left; padding: 12px 14px; background: #fff; border: none; font-size: 15px; display: flex; justify-content: space-between; align-items: center; }
.arrow { color: #999; font-size: 12px; }
.a { padding: 4px 14px 14px; color: #444; font-size: 14px; line-height: 1.7; white-space: pre-wrap; background: #fafafa; }
.loading, .empty { color: #999; padding: 16px 0; }
.error { color: #c00; }
</style>
