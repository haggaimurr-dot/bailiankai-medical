<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, canEditSchedule, errorMessage, type ScheduleSetting } from '../api'
import { useScheduleStore } from '../stores/schedule'
import { shanghaiDate, slotTime } from '../dates'
import { slotCsvHeader, validateSlotCsv } from '../schedule-import'

const store = useScheduleStore()
const opened = ref(false), busy = ref(false), reading = ref(false)
const clinicId = ref(''), content = ref(''), filename = ref(''), error = ref('')
const setting = ref<ScheduleSetting | null>(null)
const doctors = computed(() => store.doctors.filter(d => d.clinicId === clinicId.value))
const preview = computed(() => {
  if (!content.value || !setting.value) return { slots: [], errors: [] }
  try { return validateSlotCsv(content.value, clinicId.value, doctors.value.map(d => d.id), setting.value) }
  catch (e) { return { slots: [], errors: [{ row: 0, message: errorMessage(e) }] } }
})
const ready = computed(() => !!setting.value && !!content.value && preview.value.slots.length > 0 && !preview.value.errors.length && !busy.value && !reading.value)
async function show() {
  clinicId.value = store.clinicId; content.value = ''; filename.value = ''; error.value = ''; setting.value = null
  opened.value = true; reading.value = true
  try { setting.value = await api.scheduleSetting(clinicId.value) }
  catch (e) { error.value = errorMessage(e) }
  finally { reading.value = false }
}
function download() {
  const blob = new Blob(['\uFEFF' + slotCsvHeader + '\r\n'], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob), link = document.createElement('a')
  link.href = url; link.download = '院方正式号源模板.csv'; link.click(); URL.revokeObjectURL(url)
}
async function choose(event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0]
  input.value = ''; content.value = ''; filename.value = ''; error.value = ''
  if (!file) return
  if (!/\.csv$/i.test(file.name) || file.size > 1024 * 1024) { error.value = '请选择不超过 1 MB 的 UTF-8 CSV 文件。'; return }
  reading.value = true
  try { content.value = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer()); filename.value = file.name }
  catch { error.value = '文件读取失败，请使用 UTF-8 编码重新导出 CSV。' }
  finally { reading.value = false }
}
async function save() {
  if (!ready.value || !canEditSchedule.value) return
  busy.value = true; error.value = ''
  try {
    // Revalidate against the latest date rules before submitting. The API also
    // checks permissions, holidays and existing overlaps in one transaction.
    setting.value = await api.scheduleSetting(clinicId.value)
    const result = validateSlotCsv(content.value, clinicId.value, doctors.value.map(d => d.id), setting.value)
    if (result.errors.length) { error.value = '最新规则与文件冲突，请核对下方校验结果。'; return }
    await api.createSlots(result.slots)
    store.clinicId = clinicId.value; store.doctorId = ''; store.date = shanghaiDate(new Date(result.slots[0]!.startsAt))
    opened.value = false; ElMessage.success(`已导入 ${result.slots.length} 个院方号源时段。`)
    await store.refresh()
  } catch (e) { error.value = errorMessage(e) }
  finally { busy.value = false }
}
</script>

<template>
  <el-button v-if="canEditSchedule" :disabled="store.loading || !!store.error || !store.clinicId" @click="show">导入院方号源</el-button>
  <el-dialog :model-value="opened" title="导入院方正式号源" width="860px" :close-on-click-modal="false" :close-on-press-escape="!busy && !reading" :show-close="!busy && !reading" @update:model-value="(v: boolean) => { if (!busy && !reading) opened = v }">
    <el-alert title="请使用院方核定的号源文件。按北京时间导入当前院区，每行一个时段；全部校验通过后整批保存，重复或重叠时整批拒绝。" type="info" :closable="false" />
    <p>当前院区ID：<strong>{{ clinicId }}</strong>。容量为时段总名额，开放状态填写“开放”或“关闭”。</p>
    <el-table :data="doctors" size="small" max-height="150"><el-table-column prop="name" label="可排班医生" /><el-table-column prop="id" label="医生ID（填入文件）" /></el-table>
    <div class="import-controls"><el-button @click="download">下载 CSV 模板</el-button><label>选择号源文件 <input type="file" accept=".csv,text/csv" aria-label="院方号源文件" :disabled="busy || reading || !setting" @change="choose" /></label><span>{{ filename }}</span></div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <p v-if="reading">正在读取文件或排班规则…</p>
    <template v-if="content">
      <el-alert :title="preview.errors.length ? `发现 ${preview.errors.length} 项错误，请修正文件后重新选择。` : `校验通过：${preview.slots.length} 个时段，共 ${preview.slots.reduce((sum, s) => sum + s.capacity, 0)} 个名额，其中 ${preview.slots.filter(s => s.status === 'open').length} 个开放时段。`" :type="preview.errors.length ? 'error' : 'success'" :closable="false" />
      <el-table v-if="preview.errors.length" :data="preview.errors" max-height="240"><el-table-column label="数据行" width="100"><template #default="{ row }">{{ row.row || '文件' }}</template></el-table-column><el-table-column prop="message" label="校验结果" /></el-table>
      <el-table v-else :data="preview.slots" max-height="280"><el-table-column label="医生"><template #default="{ row }">{{ doctors.find(d => d.id === row.doctorId)?.name }}</template></el-table-column><el-table-column label="日期"><template #default="{ row }">{{ shanghaiDate(new Date(row.startsAt)) }}</template></el-table-column><el-table-column label="时段"><template #default="{ row }">{{ slotTime(row.startsAt) }}–{{ slotTime(row.endsAt) }}</template></el-table-column><el-table-column prop="capacity" label="容量" /><el-table-column label="状态"><template #default="{ row }">{{ row.status === 'open' ? '开放' : '关闭' }}</template></el-table-column></el-table>
    </template>
    <template #footer><el-button :disabled="busy || reading" @click="opened = false">取消</el-button><el-button type="primary" :disabled="!ready" :loading="busy" @click="save">确认导入</el-button></template>
  </el-dialog>
</template>

<style scoped>
.import-controls { display: flex; align-items: center; flex-wrap: wrap; gap: 16px; margin: 20px 0; }
.el-alert { margin: 12px 0; }
</style>
