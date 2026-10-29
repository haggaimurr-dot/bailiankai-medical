<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showToast } from 'vant'
import { errorMessage } from '../api'
import { patientApi } from '../patient'
import { useSessionStore } from '../stores/session'

const session = useSessionStore()
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const prefs = ref({ appointmentReminder: true, followupReminder: true, careReminder: true, leadHours: 24 })

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r: any = await patientApi.notificationPreferences(session.patient?.id)
    if (r) prefs.value = r
  } catch (cause) { error.value = errorMessage(cause) }
  finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    await patientApi.updateNotificationPreferences({ ...prefs.value }, session.patient?.id)
    showToast('已保存')
  } catch (cause) { showToast(errorMessage(cause)) }
  finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <h2>提醒偏好</h2>
    <p class="desc">设置各类提醒的开关与提前通知时间。</p>
    <div v-if="loading" class="loading">加载中…</div>
    <div v-else-if="error" class="error">{{ error }}<button @click="load">重试</button></div>
    <div v-else class="card">
      <label class="row"><span>预约提醒</span><input type="checkbox" v-model="prefs.appointmentReminder" /></label>
      <label class="row"><span>随访提醒</span><input type="checkbox" v-model="prefs.followupReminder" /></label>
      <label class="row"><span>用药/护理提醒</span><input type="checkbox" v-model="prefs.careReminder" /></label>
      <label class="row"><span>提前通知</span>
        <select v-model.number="prefs.leadHours">
          <option :value="1">1 小时</option>
          <option :value="3">3 小时</option>
          <option :value="12">12 小时</option>
          <option :value="24">24 小时</option>
          <option :value="48">48 小时</option>
          <option :value="72">72 小时</option>
        </select>
      </label>
      <button class="primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
    </div>
  </div>
</template>

<style scoped>
.page { padding: 16px; max-width: 640px; margin: 0 auto; }
.desc { color: #666; font-size: 13px; margin: 4px 0 12px; }
.card { background: #fff; border: 1px solid #eee; border-radius: 12px; padding: 16px; }
.row { display: flex; justify-content: space-between; align-items: center; padding: 12px 0; border-bottom: 1px solid #f5f5f5; font-size: 15px; }
.row input[type="checkbox"] { width: 22px; height: 22px; }
.row select { padding: 8px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 15px; }
.primary { width: 100%; margin-top: 16px; padding: 12px; background: #07c160; color: #fff; border: none; border-radius: 8px; font-size: 16px; }
.loading { color: #999; padding: 16px 0; }
.error { color: #c00; }
</style>
