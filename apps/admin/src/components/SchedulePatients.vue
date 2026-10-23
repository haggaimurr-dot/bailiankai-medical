<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElRadio } from 'element-plus'
import { api, ApiError, errorMessage, type SchedulePatientTask, type Slot } from '../api'
import { useScheduleStore } from '../stores/schedule'
import { displayDate, slotTime } from '../dates'
const store = useScheduleStore()
const rows = ref<SchedulePatientTask[]>([])
const loading = ref(false)
const error = ref('')
const filter = ref('active')
let request = 0
const selected = ref<SchedulePatientTask | null>(null)
const mode = ref<'contact' | 'reschedule' | ''>('')
const busy = ref(false)
const dialogError = ref('')
const contactStatus = ref<'todo' | 'contacted' | 'unreachable'>('todo')
const note = ref('')
const date = ref('')
const slots = ref<Slot[]>([])
const target = ref('')
const candidateLoading = ref(false)
let candidateRequest = 0
const key = ref('')
function renewKey() { key.value = crypto.randomUUID() }
const closedStatuses = ['cancelled', 'expired', 'rejected', 'completed']
function state(row: SchedulePatientTask) {
  if (row.currentSlotId !== row.slotId) return '已改期'
  if (row.slotStatus === 'open') return '已恢复排班'
  if (closedStatuses.includes(row.appointmentStatus)) return '预约已结束'
  return ({ todo: '待联系', contacted: '已联系', unreachable: '暂未联系上' })[row.contactStatus]
}
function active(row: SchedulePatientTask) { return row.slotStatus !== 'open' && row.currentSlotId === row.slotId && !closedStatuses.includes(row.appointmentStatus) }
const remaining = computed(() => rows.value.filter(active).length)
const visible = computed(() => rows.value.filter(row => filter.value === 'all' || (filter.value === 'active' ? active(row) : !active(row))))
async function refresh() {
  const seq = ++request, clinic = store.clinicId
  rows.value = []; error.value = ''; loading.value = true
  try { if (clinic) { const data = await api.schedulePatients(clinic); if (seq === request) rows.value = data } }
  catch (e) { if (seq === request) error.value = errorMessage(e) }
  finally { if (seq === request) loading.value = false }
}
watch(() => [store.clinicId, store.refreshedAt], () => { void refresh() }, { immediate: true })
async function show(row: SchedulePatientTask, next: 'contact' | 'reschedule') {
  selected.value = { ...row }; mode.value = next; dialogError.value = ''; contactStatus.value = row.contactStatus; note.value = row.note
  date.value = store.date; target.value = ''; slots.value = []; key.value = crypto.randomUUID()
  if (next === 'reschedule') await loadCandidates()
}
async function loadCandidates() {
  const seq = ++candidateRequest, row = selected.value
  target.value = ''; slots.value = []; dialogError.value = ''
  if (!row || !date.value) return
  candidateLoading.value = true
  try {
    const data = await api.slots({ clinicId: store.clinicId, doctorId: row.doctorId, date: date.value })
    if (seq === candidateRequest) slots.value = data.filter(s => s.id !== row.slotId && s.doctorId === row.doctorId && s.status === 'open' && s.reserved < s.capacity && new Date(s.startsAt).getTime() > Date.now())
  } catch (e) { if (seq === candidateRequest) dialogError.value = errorMessage(e) }
  finally { if (seq === candidateRequest) candidateLoading.value = false }
}
async function save() {
  const row = selected.value
  if (!row || busy.value || mode.value === 'reschedule' && !target.value) return
  busy.value = true; dialogError.value = ''
  try {
    if (mode.value === 'contact') await api.updateScheduleContact(row.id, { version: row.version, contactStatus: contactStatus.value, note: note.value.trim() })
    else await api.rescheduleAffectedPatient(row.id, target.value, row.appointmentVersion, key.value)
    ElMessage.success(mode.value === 'contact' ? '联系记录已保存。' : '改期成功，原名额已释放，新预约待确认。')
    mode.value = ''; await store.refresh(); await refresh()
  } catch (e) {
    dialogError.value = errorMessage(e)
    if (e instanceof ApiError && e.status === 409) { await refresh(); dialogError.value += ' 请关闭窗口，核对最新清单后重新操作。' }
  } finally { busy.value = false }
}
</script>
<template>
  <section class="surface patients-panel">
    <div class="panel-heading"><div><h2>受影响患者处理清单 <span class="count-chip">{{ loading || error ? '—' : remaining }}</span></h2><p>汇总当前院区停诊预约，记录联系结果并安排改期。</p></div><el-button :loading="loading" @click="refresh">刷新清单</el-button></div>
    <el-radio-group v-model="filter"><el-radio-button value="active">待处理</el-radio-button><el-radio-button value="resolved">已处理</el-radio-button><el-radio-button value="all">全部</el-radio-button></el-radio-group>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-table v-else v-loading="loading" :data="visible" empty-text="当前筛选暂无受影响患者。">
      <el-table-column label="患者" min-width="150"><template #default="{ row }"><strong>{{ row.contactName }}</strong><div>{{ row.contactPhone }}</div></template></el-table-column>
      <el-table-column label="原就诊安排" min-width="210"><template #default="{ row }">{{ store.doctors.find(d => d.id === row.doctorId)?.name }}<div>{{ displayDate(row.startsAt.slice(0, 10)) }} {{ slotTime(row.startsAt) }}–{{ slotTime(row.endsAt) }}</div></template></el-table-column>
      <el-table-column prop="reason" label="停诊原因" min-width="160" />
      <el-table-column label="处理状态" min-width="130"><template #default="{ row }"><el-tag :type="active(row) ? 'warning' : 'success'">{{ state(row) }}</el-tag></template></el-table-column>
      <el-table-column prop="note" label="联系备注" min-width="170" />
      <el-table-column label="操作" width="170"><template #default="{ row }"><el-button link type="primary" :disabled="busy" @click="show(row, 'contact')">联系记录</el-button><el-button link type="primary" :disabled="busy || !active(row)" @click="show(row, 'reschedule')">安排改期</el-button></template></el-table-column>
    </el-table>
    <el-dialog :model-value="Boolean(mode)" :title="mode === 'contact' ? '联系记录' : '安排改期'" width="540px" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @update:model-value="(v: boolean) => { if (!v && !busy) { mode = ''; candidateRequest++ } }">
      <p>{{ selected?.contactName }} · {{ selected?.contactPhone }}</p>
      <el-form label-position="top" :disabled="busy">
        <template v-if="mode === 'contact'">
          <el-form-item label="联系结果"><el-radio-group v-model="contactStatus"><el-radio value="todo">待联系</el-radio><el-radio value="contacted">已联系</el-radio><el-radio value="unreachable">暂未联系上</el-radio></el-radio-group></el-form-item>
          <el-form-item label="联系备注"><el-input v-model="note" type="textarea" :rows="3" maxlength="500" show-word-limit aria-label="联系备注" /></el-form-item>
        </template>
        <template v-else>
          <el-alert title="请先与患者确认新时间。仅支持同院区、同医生；改期后释放原名额、占用新名额，预约重新进入待确认。" type="warning" :closable="false" />
          <el-form-item label="目标日期"><el-date-picker v-model="date" value-format="YYYY-MM-DD" :clearable="false" aria-label="目标日期" @change="loadCandidates" /></el-form-item>
          <el-form-item label="可改期时段"><el-select v-model="target" :loading="candidateLoading" :disabled="candidateLoading" placeholder="选择有剩余名额的时段" aria-label="可改期时段" @change="renewKey"><el-option v-for="s in slots" :key="s.id" :value="s.id" :label="`${slotTime(s.startsAt)}–${slotTime(s.endsAt)} · 剩余 ${s.capacity - s.reserved} 个`" /></el-select></el-form-item>
          <p v-if="!candidateLoading && !slots.length">该日期暂无可改期时段，请选择其他日期。</p>
        </template>
      </el-form>
      <el-alert v-if="dialogError" :title="dialogError" type="error" :closable="false" />
      <template #footer><el-button :disabled="busy" @click="mode = ''; candidateRequest++">取消</el-button><el-button type="primary" :loading="busy" :disabled="mode === 'reschedule' && (!target || candidateLoading)" @click="save">{{ mode === 'contact' ? '保存联系记录' : '确认改期' }}</el-button></template>
    </el-dialog>
  </section>
</template>
<style scoped>
.patients-panel { padding: 24px; margin-top: 24px; }
.el-alert { margin: 14px 0; }
</style>
