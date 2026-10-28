<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage, type Slot } from '../api'
import type { components } from '../../../../packages/contracts/src/schema'
import { shanghaiDate, slotTime } from '../dates'
import { useScheduleStore } from '../stores/schedule'

type WaitlistEntry = components['schemas']['WaitlistEntry']

const schedule = useScheduleStore()
const doctorId = ref('')
const date = ref(shanghaiDate())
const slots = ref<Slot[]>([])
const slotsLoading = ref(false)
const slotId = ref('')
const queue = ref<WaitlistEntry[]>([])
const queueLoading = ref(false)
const error = ref('')
const sweeping = ref(false)
const sweepResult = ref('')

const selectedSlot = computed(() => slots.value.find((slot) => slot.id === slotId.value))

async function loadSlots() {
  slots.value = []
  slotId.value = ''
  queue.value = []
  if (!doctorId.value || !date.value) return
  slotsLoading.value = true
  error.value = ''
  try {
    slots.value = await api.slots({ doctorId: doctorId.value, date: date.value })
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    slotsLoading.value = false
  }
}

async function loadQueue() {
  queue.value = []
  sweepResult.value = ''
  if (!slotId.value) return
  queueLoading.value = true
  error.value = ''
  try {
    queue.value = await api.waitlistQueue(slotId.value)
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    queueLoading.value = false
  }
}

async function sweep() {
  sweeping.value = true
  try {
    const result = await api.sweepWaitlist()
    sweepResult.value = `结算完成：过期 ${result.expired} 条，新 offer ${result.offered} 条。`
    await loadQueue()
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    sweeping.value = false
  }
}

onMounted(async () => {
  if (!schedule.initialized) await schedule.refresh()
  if (schedule.doctors.length && !doctorId.value) doctorId.value = schedule.doctors[0].id
  await loadSlots()
})
</script>

<template>
  <main class="page">
    <h1>候补队列</h1>
    <p class="lede">空位释放时系统自动通知最早一位候补患者，并给其限时确认；超时未确认自动顺延给下一位。offer 本身不占名额，有效期内他人无法预约该时段。</p>

    <section class="card filters">
      <label>医生
        <select v-model="doctorId" @change="loadSlots">
          <option v-for="doctor in schedule.doctors" :key="doctor.id" :value="doctor.id">{{ doctor.name }}</option>
        </select>
      </label>
      <label>日期
        <input v-model="date" type="date" @change="loadSlots" />
      </label>
      <label>时段
        <select v-model="slotId" :disabled="slotsLoading || !slots.length" @change="loadQueue">
          <option value="" disabled>{{ slotsLoading ? '加载中…' : slots.length ? '请选择时段' : '该日无排班' }}</option>
          <option v-for="slot in slots" :key="slot.id" :value="slot.id">{{ slotTime(slot.startsAt) }}—{{ slotTime(slot.endsAt) }}（{{ slot.reserved }}/{{ slot.capacity }}）</option>
        </select>
      </label>
      <button type="button" :disabled="sweeping" @click="sweep">{{ sweeping ? '结算中…' : '结算过期 offer' }}</button>
    </section>

    <p v-if="error" class="error-box" role="alert">{{ error }}</p>
    <p v-if="sweepResult" class="notice">{{ sweepResult }}</p>

    <section v-if="selectedSlot" class="card">
      <h2>队列 · {{ slotTime(selectedSlot.startsAt) }}—{{ slotTime(selectedSlot.endsAt) }}</h2>
      <p v-if="queueLoading" class="muted">正在读取队列…</p>
      <p v-else-if="!queue.length" class="muted">该时段暂无候补登记。</p>
      <table v-else class="table">
        <thead><tr><th>登记顺序</th><th>患者</th><th>联系电话</th><th>登记时间</th><th>限时 offer</th></tr></thead>
        <tbody>
          <tr v-for="(entry, index) in queue" :key="entry.appointment.id">
            <td>{{ index + 1 }}</td>
            <td>{{ entry.appointment.contactName }}</td>
            <td>{{ entry.appointment.contactPhone }}</td>
            <td>{{ slotTime(entry.appointment.createdAt) }}</td>
            <td>{{ entry.offer ? `确认截止 ${slotTime(entry.offer.expiresAt)}` : '等待空位' }}</td>
          </tr>
        </tbody>
      </table>
    </section>
  </main>
</template>

<style scoped>
.page { padding: 20px; }
.lede { color: #666; line-height: 1.7; }
.filters { display: flex; flex-wrap: wrap; gap: 12px; align-items: end; }
.filters label { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
.filters select, .filters input { padding: 6px 8px; }
.notice { background: #ecfdf5; border: 1px solid #a7f3d0; border-radius: 8px; padding: 10px 12px; }
.table { width: 100%; border-collapse: collapse; }
.table th, .table td { text-align: left; padding: 8px; border-bottom: 1px solid #eee; }
</style>
