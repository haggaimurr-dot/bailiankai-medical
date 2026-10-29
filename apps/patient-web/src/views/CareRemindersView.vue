<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from 'vant'
import { errorMessage } from '../api'
import { patientApi, type CareReminder, type ReminderCheckin } from '../patient'
import { useSessionStore } from '../stores/session'

const session = useSessionStore()
const loading = ref(true)
const error = ref('')
const reminders = ref<CareReminder[]>([])
const history = ref<Record<string, ReminderCheckin[]>>({})
const expandedId = ref('')
const checkingId = ref('')

const today = new Date().toISOString().slice(0, 10)

function patientId() { return session.patient?.id || '' }

async function load() {
  loading.value = true
  error.value = ''
  try { reminders.value = await patientApi.careReminders(patientId()) }
  catch (cause) { error.value = errorMessage(cause) }
  finally { loading.value = false }
}

async function toggleHistory(id: string) {
  if (expandedId.value === id) { expandedId.value = ''; return }
  expandedId.value = id
  if (!history.value[id]) {
    try { history.value[id] = await patientApi.reminderCheckins(id, patientId()) }
    catch (cause) { showToast(errorMessage(cause)) }
  }
}

async function checkin(rem: CareReminder) {
  if (checkingId.value) return
  checkingId.value = rem.id
  try {
    await patientApi.checkinReminder(rem.id, today, patientId())
    rem.doneToday = true
    delete history.value[rem.id]
    showToast('打卡成功')
  } catch (cause) { showToast(errorMessage(cause)) }
  finally { checkingId.value = '' }
}

const kindLabel = (k: string) => k === 'medication' ? '用药' : '护理'
const doneCount = computed(() => reminders.value.filter(r => r.doneToday).length)

onMounted(load)
</script>

<template>
  <div class="page">
    <h2>用药 / 护理打卡</h2>
    <p class="desc">今日 {{ doneCount }} / {{ reminders.length }} 已打卡</p>
    <div v-if="loading" class="loading">加载中…</div>
    <div v-else-if="error" class="error">{{ error }}<button @click="load">重试</button></div>
    <div v-else-if="!reminders.length" class="empty">暂无提醒，医生会根据你的方案添加。</div>
    <div v-else class="list">
      <div v-for="rem in reminders" :key="rem.id" class="card">
        <div class="card-head">
          <span class="kind">{{ kindLabel(rem.kind) }}</span>
          <strong>{{ rem.title }}</strong>
        </div>
        <p v-if="rem.detail" class="detail">{{ rem.detail }}</p>
        <p class="meta">{{ rem.startDate }} 起<span v-if="rem.endDate"> 至 {{ rem.endDate }}</span></p>
        <div class="actions">
          <button :disabled="rem.doneToday || checkingId === rem.id" @click="checkin(rem)">
            {{ rem.doneToday ? '今日已打卡' : (checkingId === rem.id ? '打卡中…' : '打卡') }}
          </button>
          <button class="ghost" @click="toggleHistory(rem.id)">{{ expandedId === rem.id ? '收起记录' : '打卡记录' }}</button>
        </div>
        <div v-if="expandedId === rem.id" class="history">
          <span v-for="h in (history[rem.id] || [])" :key="h.date" :class="['day', h.done ? 'done' : 'miss']">{{ h.date.slice(5) }}</span>
          <span v-if="!(history[rem.id] || []).length" class="none">暂无打卡记录</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { padding: 16px; max-width: 640px; margin: 0 auto; }
.desc { color: #666; margin: 4px 0 12px; }
.list { display: flex; flex-direction: column; gap: 12px; }
.card { border: 1px solid #eee; border-radius: 12px; padding: 12px 14px; background: #fff; }
.card-head { display: flex; gap: 8px; align-items: center; }
.kind { font-size: 12px; color: #fff; background: #07c160; border-radius: 8px; padding: 2px 8px; }
.detail { color: #444; margin: 8px 0; font-size: 14px; }
.meta { color: #999; font-size: 12px; margin: 0 0 8px; }
.actions { display: flex; gap: 8px; }
.actions button { flex: 1; padding: 10px; border: none; border-radius: 8px; background: #07c160; color: #fff; font-size: 15px; }
.actions button:disabled { background: #ccc; }
.actions button.ghost { background: #f2f2f2; color: #333; flex: 0 0 auto; padding: 10px 16px; }
.history { margin-top: 10px; display: flex; flex-wrap: wrap; gap: 6px; }
.day { font-size: 12px; padding: 4px 8px; border-radius: 6px; background: #f5f5f5; color: #999; }
.day.done { background: #e6f7ee; color: #07c160; }
.none, .empty, .loading { color: #999; padding: 12px 0; }
.error { color: #c00; }
</style>
