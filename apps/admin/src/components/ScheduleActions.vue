<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElTimeSelect, ElCheckbox, ElCheckboxGroup } from 'element-plus'
import type { components } from '../../../../packages/contracts/src/schema'
import { ApiError, api, canEditSchedule, errorMessage, type Slot, type ScheduleSetting } from '../api'
import { useScheduleStore } from '../stores/schedule'
import { slotTime } from '../dates'
import ScheduleImport from './ScheduleImport.vue'

const props = defineProps<{ selected: Slot[] }>()
const emit = defineEmits<{ changed: [] }>()
const store = useScheduleStore()
const mode = ref<'single' | 'batch' | 'suspend' | ''>('')
const busy = ref(false)
const error = ref('')
const clinicId = ref('')
const doctorId = ref('')
const dates = ref<string[]>([])
const start = ref('09:00')
const end = ref('12:00')
const duration = ref<number | undefined>(30)
const capacity = ref<number | undefined>(3)
const weekdays = ref([1, 2, 3, 4, 5])
const open = ref(true)
const reason = ref('')
const targets = ref<Slot[]>([])
const templateId = ref('')
const setting = ref<ScheduleSetting | null>(null)
const ruleError = ref('')
let ruleRequest = 0
async function loadRules(id: string) {
  const seq = ++ruleRequest; setting.value = null; ruleError.value = ''
  try { const data = await api.scheduleSetting(id); if (seq === ruleRequest) setting.value = data }
  catch (e) { if (seq === ruleRequest) ruleError.value = errorMessage(e) }
}
function applyTemplate(id: string) {
  const t = setting.value?.templates.find(t => t.id === id)
  if (!t) return
  doctorId.value = t.doctorId; weekdays.value = [...t.weekdays]; start.value = t.start; end.value = t.end
  duration.value = t.duration; capacity.value = t.capacity; open.value = t.open
}
const doctors = computed(() => store.doctors.filter(d => d.clinicId === clinicId.value))
const title = computed(() => mode.value === 'single' ? '创建排班' : mode.value === 'batch' ? '批量生成排班' : '停诊处理')
const preview = computed(() => {
  const slots: components['schemas']['SlotCreate'][] = []
  const invalid = (message: string) => ({ slots, message })
  if (!setting.value || setting.value.clinicId !== clinicId.value) return invalid(ruleError.value || '正在加载院区排班规则。')
  if (!clinicId.value || !doctors.value.some(d => d.id === doctorId.value)) return invalid('请选择院区和医生。')
  if (!dates.value[0] || (mode.value === 'batch' && !dates.value[1])) return invalid('请选择排班日期。')
  if (capacity.value === undefined || !Number.isInteger(capacity.value) || capacity.value < 0 || capacity.value > 100) return invalid('容量须为 0–100 的整数。')
  if (!/^\d{2}:\d{2}$/.test(start.value) || !/^\d{2}:\d{2}$/.test(end.value)) return invalid('请填写开始和结束时间。')
  const minute = (v: string) => Number(v.slice(0, 2)) * 60 + Number(v.slice(3))
  const from = minute(start.value), to = minute(end.value)
  if (from >= to || to > 1439) return invalid('结束时间须晚于开始时间，时段须在同一天内。')
  const step = mode.value === 'single' ? to - from : duration.value
  if (step === undefined || !Number.isInteger(step) || step < 1 || step > 1440) return invalid('时段长度须为 1–1440 分钟的整数。')
  if ((to - from) % step !== 0) return invalid('时间范围须能被时段长度整除，请调整结束时间。')
  const first = new Date(`${dates.value[0]}T00:00:00+08:00`)
  const last = new Date(`${mode.value === 'batch' ? dates.value[1] : dates.value[0]}T00:00:00+08:00`)
  if (!Number.isFinite(first.getTime()) || !Number.isFinite(last.getTime()) || last < first || (last.getTime() - first.getTime()) / 86400000 > 90) return invalid('日期范围最多 91 天。')
  for (let day = first.getTime(); day <= last.getTime(); day += 86400000) {
    const date = new Date(day + 8 * 3600000).toISOString().slice(0, 10)
    const rule = setting.value.holidays.find(h => h.date === date)
    if (rule?.kind === 'rest' && open.value) {
      if (mode.value === 'single') return invalid(`该日期为休诊日：${rule.name}。请调整日期或设置为关闭。`)
      continue
    }
    if (mode.value === 'batch' && rule?.kind !== 'work' && !weekdays.value.includes(new Date(day + 8 * 3600000).getUTCDay())) continue
    for (let m = from; m < to; m += step) {
      const begins = day + m * 60000
      if (begins <= Date.now()) return invalid('请选择尚未开始的时段。')
      slots.push({ clinicId: clinicId.value, doctorId: doctorId.value, startsAt: new Date(begins).toISOString(), endsAt: new Date(begins + step * 60000).toISOString(), capacity: capacity.value, status: open.value ? 'open' : 'closed' })
      if (slots.length > 500) return invalid('每批最多生成 500 个时段，请缩小范围。')
    }
  }
  if (!slots.length) return invalid('所选日期没有匹配的星期。')
  return { slots, message: '' }
})
const reserved = computed(() => targets.value.reduce((sum, s) => sum + s.reserved, 0))
const suspensionError = computed(() => !targets.value.length ? '请先选择开放时段。' : !reason.value.trim() || Array.from(reason.value.trim()).length > 200 ? '请填写 1–200 字的停诊原因。' : '')

function show(next: 'single' | 'batch' | 'suspend') {
  clinicId.value = store.clinicId
  doctorId.value = store.doctorId || doctors.value[0]?.id || ''
  dates.value = [store.date, store.date]
  targets.value = props.selected.filter(s => s.status === 'open').map(s => ({ ...s }))
  reason.value = ''
  templateId.value = ''
  error.value = ''
  mode.value = next
  if (next !== 'suspend') void loadRules(clinicId.value)
}
async function save() {
  if (busy.value || !canEditSchedule.value || (mode.value === 'suspend' ? suspensionError.value : preview.value.message)) return
  busy.value = true
  error.value = ''
  try {
    if (mode.value === 'suspend') await api.suspendSlots(targets.value.map(s => ({ id: s.id, version: s.version })), reason.value.trim())
    else {
      await api.createSlots(preview.value.slots)
      store.clinicId = clinicId.value
      store.doctorId = doctorId.value
      store.date = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date(preview.value.slots[0]!.startsAt))
    }
    const suspended = mode.value === 'suspend'
    mode.value = ''
    ElMessage.success(suspended ? '所选时段已停诊，已有预约保留，请联系受影响患者。' : '排班已创建。')
    emit('changed')
    await store.refresh()
  } catch (e) {
    if (mode.value === 'suspend' && e instanceof ApiError && e.status === 409) {
      mode.value = ''
      emit('changed')
      await store.refresh()
      ElMessage.warning('排班已变化，本次停诊未保存，请刷新数据后重新选择时段。')
    } else error.value = errorMessage(e)
  } finally { busy.value = false }
}
function beforeClose(done: () => void) { if (!busy.value) done() }
</script>

<template>
  <div v-if="canEditSchedule" class="schedule-actions">
    <el-button :disabled="store.loading || Boolean(store.error) || !store.clinicId" @click="show('single')">创建排班</el-button>
    <el-button type="primary" :disabled="store.loading || Boolean(store.error) || !store.clinicId" @click="show('batch')">批量生成</el-button>
    <ScheduleImport />
    <el-button type="danger" plain :disabled="store.loading || !selected.some(s => s.status === 'open')" @click="show('suspend')">停诊处理（{{ selected.filter(s => s.status === 'open').length }}）</el-button>
  </div>
  <el-dialog :model-value="Boolean(mode)" :title="title" width="640px" :before-close="beforeClose" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" destroy-on-close @update:model-value="(value: boolean) => { if (!value && !busy) mode = '' }">
    <template v-if="mode === 'suspend'">
      <el-alert :title="`将关闭 ${targets.length} 个时段，涉及 ${reserved} 个已占用名额。已有预约及审核流程保留，请人工联系患者安排后续就诊。`" type="warning" :closable="false" show-icon />
      <el-table :data="targets" max-height="240"><el-table-column label="医生"><template #default="{ row }">{{ store.doctors.find(d => d.id === row.doctorId)?.name }}</template></el-table-column><el-table-column label="时段"><template #default="{ row }">{{ slotTime(row.startsAt) }}–{{ slotTime(row.endsAt) }}</template></el-table-column><el-table-column prop="reserved" label="已占用" /></el-table>
      <el-form label-position="top"><el-form-item label="停诊原因" :error="suspensionError"><el-input v-model="reason" type="textarea" :rows="3" maxlength="200" show-word-limit :disabled="busy" aria-label="停诊原因" /></el-form-item></el-form>
    </template>
    <template v-else-if="mode">
      <el-form label-position="top" :disabled="busy" @submit.prevent="save">
        <div class="schedule-form-grid">
          <el-form-item label="院区"><el-select v-model="clinicId" aria-label="创建排班院区" @change="doctorId = ''; templateId = ''; loadRules(clinicId)"><el-option v-for="c in store.clinics" :key="c.id" :label="c.name" :value="c.id" /></el-select></el-form-item>
          <el-form-item label="医生"><el-select v-model="doctorId" aria-label="创建排班医生"><el-option v-for="d in doctors" :key="d.id" :label="d.name" :value="d.id" /></el-select></el-form-item>
        </div>
        <el-form-item v-if="mode === 'batch'" label="使用排班模板"><el-select v-model="templateId" clearable placeholder="选择模板后自动填入参数" aria-label="使用排班模板" @change="applyTemplate"><el-option v-for="t in setting?.templates || []" :key="t.id" :label="t.name" :value="t.id" /></el-select><p>休诊日自动跳过，补班日优先于重复星期。</p></el-form-item>
        <el-form-item label="排班日期"><el-date-picker v-if="mode === 'single'" :model-value="dates[0]" type="date" value-format="YYYY-MM-DD" @update:model-value="(v: string | null) => dates = v ? [v, v] : []" /><el-date-picker v-else v-model="dates" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始日期" end-placeholder="结束日期" :clearable="false" /></el-form-item>
        <el-form-item v-if="mode === 'batch'" label="重复星期"><el-checkbox-group v-model="weekdays"><el-checkbox v-for="(label, i) in ['日', '一', '二', '三', '四', '五', '六']" :key="i" :value="i">周{{ label }}</el-checkbox></el-checkbox-group></el-form-item>
        <div class="schedule-form-grid">
          <el-form-item label="开始时间"><el-time-select v-model="start" start="00:00" end="23:45" step="00:15" /></el-form-item>
          <el-form-item label="结束时间"><el-time-select v-model="end" start="00:15" end="23:45" step="00:15" /></el-form-item>
          <el-form-item v-if="mode === 'batch'" label="每时段分钟数"><el-input-number v-model="duration" :min="1" :max="1440" :precision="0" aria-label="每时段分钟数" /></el-form-item>
          <el-form-item label="每时段容量"><el-input-number v-model="capacity" :min="0" :max="100" :precision="0" aria-label="每时段容量" /></el-form-item>
        </div>
        <el-form-item label="开放状态"><el-switch v-model="open" active-text="开放" inactive-text="关闭" /></el-form-item>
      </el-form>
      <el-alert :title="preview.message || `预览：将创建 ${preview.slots.length} 个时段，共 ${preview.slots.length * (capacity || 0)} 个名额。重复或重叠时整批拒绝。`" :type="preview.message ? 'warning' : 'info'" :closable="false" />
      <el-table v-if="!preview.message" :data="preview.slots" max-height="200"><el-table-column label="日期"><template #default="{ row }">{{ new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date(row.startsAt)) }}</template></el-table-column><el-table-column label="时段"><template #default="{ row }">{{ slotTime(row.startsAt) }}–{{ slotTime(row.endsAt) }}</template></el-table-column><el-table-column prop="capacity" label="容量" /></el-table>
    </template>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <template #footer><el-button :disabled="busy" @click="mode = ''">取消</el-button><el-button :type="mode === 'suspend' ? 'danger' : 'primary'" :loading="busy" :disabled="Boolean(mode === 'suspend' ? suspensionError : preview.message)" @click="save">{{ mode === 'suspend' ? '确认停诊' : '确认创建' }}</el-button></template>
  </el-dialog>
</template>

<style scoped>
.schedule-actions { display: flex; flex-wrap: wrap; gap: 10px; margin-bottom: 18px; }
.schedule-actions .el-button { margin-left: 0; }
.schedule-form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 20px; }
.el-alert { margin: 14px 0; }
</style>
