<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { BrowserQRCodeReader, type IScannerControls } from '@zxing/browser'
import { useRoute } from 'vue-router'
import { api, ApiError, errorMessage, type Appointment, type Slot } from '../api'
import { auth } from '../auth'
import { shanghaiDate, slotTime } from '../dates'

const route = useRoute()
const date = ref(typeof route.query.date === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(route.query.date) ? route.query.date : shanghaiDate()), clinicId = ref(''), doctorId = ref(''), arrival = ref(route.query.arrival === 'reschedule' ? 'reschedule' : 'expected')
const rows = ref<Appointment[]>([]), total = ref(0), page = ref(1), loading = ref(false), error = ref('')
const clinics = ref<Awaited<ReturnType<typeof api.clinics>>>([]), doctors = ref<Awaited<ReturnType<typeof api.doctors>>>([])
const busy = ref(false), token = ref(''), scanned = ref<Appointment | null>(null), scanError = ref(''), camera = ref(false), video = ref<HTMLVideoElement>()
const selected = ref<Appointment | null>(null), slots = ref<Slot[]>([]), slotId = ref(''), reason = ref(''), moveOpen = ref(false), moveError = ref(''), moveLoading = ref(false)
const canArrive = computed(() => ['manager','customer_service','doctor','clinician'].includes(auth.employee?.role || ''))
let generation = 0, controls: IScannerControls | undefined, timer: ReturnType<typeof setInterval> | undefined, disposed = false, moveKey = '', movePayload: string | null = null
const timestamp = (value: string) => `${shanghaiDate(new Date(value))} ${slotTime(value)}`

async function load(reset = false) {
 if (reset) page.value = 1
 const version = ++generation; loading.value = true; error.value = ''
 try {
  const result = await api.appointments({date:date.value,clinicId:clinicId.value,doctorId:doctorId.value,arrival:arrival.value === 'reschedule' ? undefined : arrival.value as 'expected' | 'arrived' | '',needsReschedule:arrival.value === 'reschedule' ? 'true' : undefined,page:page.value,pageSize:20})
  if (version === generation) { rows.value = result.data; total.value = result.meta.total }
 } catch (cause) { if (version === generation) { error.value = errorMessage(cause); rows.value = [] } }
 finally { if (version === generation) loading.value = false }
}
function stopCamera() { controls?.stop(); controls = undefined; camera.value = false; const stream = video.value?.srcObject; if (stream instanceof MediaStream) stream.getTracks().forEach(track => track.stop()) }
async function resolveScan() {
 if (busy.value) return
 stopCamera(); busy.value = true; scanned.value = null; scanError.value = ''
 try { scanned.value = await api.resolveArrivalScan(token.value.trim()) } catch (cause) { scanError.value = errorMessage(cause) } finally { busy.value = false }
}
async function startCamera() {
 scanned.value = null; scanError.value = ''; camera.value = true
 await nextTick()
 try {
  const reader = new BrowserQRCodeReader()
  const nextControls = await reader.decodeFromVideoDevice(undefined,video.value!, (result, _error, activeControls) => {
   if (result && !busy.value) { activeControls.stop(); token.value = result.getText(); void resolveScan() }
  })
  if (disposed || !camera.value) nextControls.stop(); else controls = nextControls
 } catch { stopCamera(); scanError.value = '无法打开摄像头，请检查摄像头权限，或使用扫码枪输入签到码。' }
}
async function confirmScan() {
 if (!scanned.value || busy.value) return
 busy.value = true; scanError.value = ''
 try { scanned.value = await api.confirmArrivalScan(token.value.trim()); await load() } catch (cause) { scanError.value = errorMessage(cause) } finally { busy.value = false }
}
async function manualArrival(row: Appointment) {
 if (busy.value || !row.canRegisterArrival) return
 selected.value = row; moveOpen.value = false
}
const arrivalTime = ref('')
async function saveManual() {
 if (!selected.value || busy.value) return
 if (!arrivalTime.value) arrivalTime.value = new Date().toISOString()
 busy.value = true; scanError.value = ''
 try { await api.registerArrival(selected.value.id,{version:selected.value.version,arrivedAt:arrivalTime.value}); selected.value = null; arrivalTime.value = ''; await load() }
 catch (cause) {
  scanError.value = errorMessage(cause)
  if (cause instanceof ApiError && cause.status === 409 && selected.value) {
   try { selected.value = await api.appointment(selected.value.id); if (selected.value.arrivedAt) { selected.value = null; arrivalTime.value = ''; await load() } } catch { /* Keep the original request for retry. */ }
  }
 } finally { busy.value = false }
}
async function openMove(row: Appointment) {
 selected.value = row; moveOpen.value = true; slotId.value = ''; reason.value = ''; moveError.value = ''; slots.value = []; moveKey = crypto.randomUUID(); movePayload = null; moveLoading.value = true
 try { slots.value = (await api.slots({clinicId:row.clinicId,doctorId:row.doctorId})).filter(slot => slot.status === 'open' && slot.reserved < slot.capacity && Date.parse(slot.startsAt) > Date.now()) }
 catch (cause) { moveError.value = errorMessage(cause) } finally { moveLoading.value = false }
}
async function saveMove() {
 if (!selected.value || !slotId.value || !reason.value.trim() || busy.value) return
 busy.value = true; moveError.value = ''
 movePayload ||= JSON.stringify({slotId:slotId.value,version:selected.value.version,reason:reason.value.trim(),requestKey:moveKey})
 try { await api.suspensionReschedule(selected.value.id,JSON.parse(movePayload)); selected.value = null; await load() }
 catch (cause) {
  moveError.value = errorMessage(cause)
  if (cause instanceof ApiError && cause.status < 500 && selected.value) {
   movePayload = null; moveKey = crypto.randomUUID()
   try { selected.value = await api.appointment(selected.value.id); slots.value = (await api.slots({clinicId:selected.value.clinicId,doctorId:selected.value.doctorId})).filter(slot => slot.status === 'open' && slot.reserved < slot.capacity && Date.parse(slot.startsAt) > Date.now()); await load() } catch { /* Show the original error and allow a refresh. */ }
  }
 } finally { busy.value = false }
}
onMounted(async () => {
 await Promise.allSettled([load(),api.clinics().then(result => clinics.value = result.filter(row => auth.employee?.clinicIds.includes(row.id))),api.doctors().then(result => doctors.value = result)])
 if (!disposed) timer = setInterval(() => { if (document.visibilityState === 'visible' && !busy.value && !selected.value && !scanned.value && !camera.value) void load() },30000)
})
onBeforeUnmount(() => { disposed = true; ++generation; if (timer) clearInterval(timer); stopCamera() })
</script>

<template>
 <section class="page-heading"><div><div class="eyebrow">ARRIVAL DESK</div><h1>到诊工作台</h1><p>核对预约、登记实际到诊，跟进停诊后的改期安排。</p></div><div><el-button @click="arrival = 'reschedule'; date = ''; load(true)">全部停诊待改期</el-button><el-button :loading="loading" @click="load()">刷新工作台</el-button></div></section>
 <section v-if="canArrive" class="surface arrival-scan">
  <h2>扫码签到</h2><p>患者在预约详情领取当日签到码。扫描后核对姓名、医生和院区，再确认已到院。</p>
  <div class="filter-bar"><el-input v-model="token" :disabled="busy || !!scanned" placeholder="扫码枪输入 / 粘贴签到码后按回车" aria-label="签到码" @keyup.enter="resolveScan" /><el-button type="primary" :loading="busy" @click="resolveScan">核对签到码</el-button><el-button :disabled="busy" @click="camera ? stopCamera() : startCamera()">{{ camera ? '关闭摄像头' : '摄像头扫码' }}</el-button></div>
  <video v-if="camera" ref="video" class="scan-video" muted playsinline />
  <el-alert v-if="scanError" :title="scanError" type="error" :closable="false" />
  <div v-if="scanned" class="scan-result"><strong>{{ scanned.contactName }} · {{ scanned.doctorName }} · {{ scanned.clinicName }}</strong><p>{{ timestamp(scanned.startsAt) }} · {{ scanned.id }}</p><p v-if="scanned.arrivedAt">已签到：{{ timestamp(scanned.arrivedAt) }}</p><el-button v-else type="primary" :loading="busy" @click="confirmScan">核对无误，确认到诊</el-button><el-button :disabled="busy" @click="scanned = null; token = ''; scanError = ''">扫描下一位</el-button></div>
 </section>
 <section class="surface arrival-list">
  <div class="filter-bar"><el-date-picker v-model="date" type="date" value-format="YYYY-MM-DD" :clearable="false" aria-label="到诊日期" @change="load(true)" /><el-select v-model="clinicId" clearable placeholder="全部授权院区" aria-label="院区" @change="doctorId = ''; load(true)"><el-option v-for="row in clinics" :key="row.id" :label="row.name" :value="row.id" /></el-select><el-select v-model="doctorId" clearable placeholder="全部医生" aria-label="医生" @change="load(true)"><el-option v-for="row in doctors.filter(row => !clinicId || row.clinicId === clinicId)" :key="row.id" :label="row.name" :value="row.id" /></el-select><el-select v-model="arrival" aria-label="到诊状态" @change="load(true)"><el-option label="待到诊" value="expected" /><el-option label="已到诊" value="arrived" /><el-option label="停诊待改期" value="reschedule" /><el-option label="全部预约" value="" /></el-select></div>
  <el-alert v-if="error" :title="error" type="error" :closable="false" />
  <el-table v-else v-loading="loading" :data="rows" empty-text="当前筛选下暂无预约">
   <el-table-column label="患者" min-width="140"><template #default="{row}">{{ row.contactName }}<small class="muted"> · {{ row.id.slice(-8).toUpperCase() }}</small></template></el-table-column>
   <el-table-column label="医生 / 院区" min-width="180"><template #default="{row}">{{ row.doctorName }} / {{ row.clinicName }}</template></el-table-column>
   <el-table-column label="预约时间" min-width="180"><template #default="{row}">{{ timestamp(row.startsAt) }}</template></el-table-column>
   <el-table-column label="实际到诊" min-width="170"><template #default="{row}">{{ row.arrivedAt ? timestamp(row.arrivedAt) : '未登记' }}</template></el-table-column>
   <el-table-column label="停诊处理" min-width="170"><template #default="{row}">{{ row.slotStatus === 'closed' ? row.suspensionReason || '时段已关闭' : '—' }}</template></el-table-column>
   <el-table-column label="操作" width="150"><template #default="{row}"><el-button v-if="row.canReschedule" link type="primary" @click="openMove(row)">安排改期</el-button><el-button v-if="row.canRegisterArrival && canArrive && row.slotStatus !== 'closed'" link type="primary" @click="arrivalTime = ''; scanError = ''; manualArrival(row)">登记到诊</el-button></template></el-table-column>
  </el-table>
  <div class="table-footer"><span>按实际到诊记录统计；确认预约不会自动签到。</span><el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load()" /></div>
 </section>
 <el-dialog :model-value="!!selected" :title="moveOpen ? '停诊改期' : '核实实际到诊'" width="520px" :close-on-click-modal="!busy" :close-on-press-escape="!busy" :show-close="!busy" @close="selected = null">
  <template v-if="selected"><p>{{ selected.contactName }} · {{ selected.doctorName }} · {{ selected.clinicName }}</p>
   <template v-if="moveOpen"><el-alert :title="selected.suspensionReason || '原时段已关闭'" type="warning" :closable="false" /><p>先与患者确认新的就诊时间，再保存改期。已确认预约保留确认状态；待确认预约重新计时。</p><el-form label-position="top"><el-form-item label="新时段（同医生、同院区）"><el-select v-model="slotId" :loading="moveLoading" :disabled="busy || !!movePayload" placeholder="选择可用时段"><el-option v-for="slot in slots" :key="slot.id" :value="slot.id" :label="`${timestamp(slot.startsAt)}（余 ${slot.capacity-slot.reserved} 位）`" /></el-select></el-form-item><el-form-item label="患者同意改期的沟通记录"><el-input v-model="reason" type="textarea" maxlength="200" show-word-limit :disabled="busy || !!movePayload" /></el-form-item></el-form><el-alert v-if="moveError" :title="moveError" type="error" :closable="false" /><el-button type="primary" :loading="busy" :disabled="!selected.canReschedule || !slotId || !reason.trim() || moveLoading" @click="saveMove">{{ movePayload ? '重试原改期申请' : '确认改期' }}</el-button></template>
   <template v-else><p>请核实患者已在本院到院。保存时记录当前实际到诊时间。</p><el-alert v-if="scanError" :title="scanError" type="error" :closable="false" /><el-button type="primary" :loading="busy" @click="saveManual">核实并登记到诊</el-button></template>
  </template>
 </el-dialog>
</template>

<style scoped>
.arrival-scan,.arrival-list { padding:24px; margin-bottom:24px }
.arrival-scan h2 { margin-top:0 }.arrival-scan p { color:var(--text-secondary); line-height:1.7 }
.scan-video { width:100%; max-width:420px; max-height:320px; border-radius:12px; background:#172c29 }
.scan-result { padding:18px; margin-top:18px; background:#eef7f4; border-radius:12px }
.filter-bar { flex-wrap:wrap; gap:12px }.arrival-scan .el-input { max-width:480px }.arrival-list .el-select { width:175px }
</style>
