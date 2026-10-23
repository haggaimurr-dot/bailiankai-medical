<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElTabs, ElTabPane, ElPopconfirm, ElRadio, ElTimeSelect, ElCheckbox, ElCheckboxGroup } from 'element-plus'
import { api, ApiError, errorMessage } from '../api'
import type { components } from '../../../../packages/contracts/src/schema'
import { useScheduleStore } from '../stores/schedule'
const store = useScheduleStore()
type Template = components['schemas']['ScheduleTemplate']
type Holiday = components['schemas']['HolidayRule']
const editor = ref<'template' | 'holiday' | ''>('')
const editingId = ref('')
const editingDate = ref('')
const busy = ref(false)
const error = ref('')
const clinic = ref('')
const version = ref(0)
const template = ref<Template>({ id: '', name: '', doctorId: '', weekdays: [1, 2, 3, 4, 5], start: '09:00', end: '12:00', duration: 30, capacity: 3, open: true })
const holiday = ref<Holiday>({ date: '', name: '', kind: 'rest' })
const holidayRange = ref<string[]>([])
const useRange = ref(false)
const holidayDates = computed(() => {
  if (!useRange.value) return holiday.value.date ? [holiday.value.date] : []
  const [first, last] = holidayRange.value
  if (!first || !last) return []
  const start = Date.parse(`${first}T00:00:00+08:00`), end = Date.parse(`${last}T00:00:00+08:00`)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start || end - start > 365 * 86400000) return []
  return Array.from({ length: (end - start) / 86400000 + 1 }, (_, i) => new Date(start + i * 86400000 + 28800000).toISOString().slice(0, 10))
})
watch(() => store.clinicId, () => { if (!busy.value) { editor.value = ''; error.value = '' } })
const validation = computed(() => {
  if (editor.value === 'holiday') {
    if (!holidayDates.value.length || !holiday.value.name.trim()) return '请填写日期和规则名称；日期范围最多 366 天。'
    if (store.setting?.holidays.some(h => holidayDates.value.includes(h.date) && h.date !== editingDate.value)) return '所选日期已有规则，请调整范围或编辑已有规则。'
    if ((store.setting?.holidays.filter(h => h.date !== editingDate.value).length || 0) + holidayDates.value.length > 366) return '每个院区最多保存 366 条日期规则，请清理不再使用的规则。'
    return ''
  }
  const t = template.value
  if (!editingId.value && (store.setting?.templates.length || 0) >= 100) return '每个院区最多保存 100 个模板。'
  const minutes = (v: string) => Number(v.slice(0, 2)) * 60 + Number(v.slice(3))
  if (!t.name.trim() || !store.clinicDoctors.some(d => d.id === t.doctorId) || !t.weekdays.length) return '请填写名称、当前院区医生和重复星期。'
  if (!t.start || !t.end || minutes(t.end) <= minutes(t.start)) return '结束时间须晚于开始时间。'
  if (!Number.isInteger(t.duration) || t.duration < 1 || t.duration > 1440 || (minutes(t.end) - minutes(t.start)) % t.duration) return '时段长度须为 1–1440 分钟，且能整除时间范围。'
  if (!Number.isInteger(t.capacity) || t.capacity < 0 || t.capacity > 100) return '容量须为 0–100 的整数。'
  return ''
})
function showTemplate(t?: Template) {
  if (!store.setting) return
  clinic.value = store.clinicId; version.value = store.setting.version; editingId.value = t?.id || ''; error.value = ''
  template.value = t ? { ...t, weekdays: [...t.weekdays] } : { id: crypto.randomUUID(), name: '', doctorId: store.doctorId || store.clinicDoctors[0]?.id || '', weekdays: [1, 2, 3, 4, 5], start: '09:00', end: '12:00', duration: 30, capacity: 3, open: true }
  editor.value = 'template'
}
function showHoliday(h?: Holiday) {
  if (!store.setting) return
  clinic.value = store.clinicId; version.value = store.setting.version; editingDate.value = h?.date || ''; error.value = ''
  holiday.value = h ? { ...h } : { date: store.date, name: '', kind: 'rest' }; useRange.value = false; holidayRange.value = [store.date, store.date]; editor.value = 'holiday'
}
async function persist(templates: Template[], holidays: Holiday[], id: string, expectedVersion: number) {
  busy.value = true; error.value = ''
  try {
    const saved = await api.saveScheduleSetting(id, { templates, holidays, version: expectedVersion })
    if (store.clinicId === id) store.setting = saved
    editor.value = ''; ElMessage.success('排班规则已保存。')
  } catch (e) {
    error.value = errorMessage(e)
    if (e instanceof ApiError && e.status === 409) { await store.loadSetting(id); error.value = '规则已被其他操作修改，最新规则已加载，请关闭后重新编辑。' }
  } finally { busy.value = false }
}
async function save() {
  if (!store.setting || busy.value || validation.value || clinic.value !== store.setting.clinicId || version.value !== store.setting.version) return
  const templates = store.setting.templates.filter(t => editor.value !== 'template' || t.id !== editingId.value)
  const holidays = store.setting.holidays.filter(h => editor.value !== 'holiday' || h.date !== editingDate.value)
  if (editor.value === 'template') templates.push({ ...template.value, name: template.value.name.trim() })
  else holidays.push(...holidayDates.value.map(date => ({ ...holiday.value, date, name: holiday.value.name.trim() })))
  await persist(templates, holidays, clinic.value, version.value)
}
async function remove(kind: 'template' | 'holiday', id: string) {
  const s = store.setting
  if (!s || busy.value) return
  await persist(s.templates.filter(t => kind !== 'template' || t.id !== id), s.holidays.filter(h => kind !== 'holiday' || h.date !== id), s.clinicId, s.version)
}
</script>

<template>
  <section class="surface rules-panel">
    <div class="panel-heading"><div><h2>排班模板与节假日规则</h2><p>按当前院区保存；规则用于新排班，已有排班需单独调整。</p></div><el-button :loading="busy" @click="store.loadSetting()">刷新规则</el-button></div>
    <el-alert v-if="store.settingError" :title="store.settingError" type="error" :closable="false" />
    <el-alert v-if="error && !editor" :title="error" type="error" :closable="false" />
    <el-tabs>
      <el-tab-pane label="排班模板">
        <el-button type="primary" :disabled="!store.setting || busy" @click="showTemplate()">新增模板</el-button>
        <el-table :data="store.setting?.templates || []" empty-text="暂无模板，保存常用医生、星期和时段参数后可一键应用。">
          <el-table-column prop="name" label="模板名称" />
          <el-table-column label="医生"><template #default="{ row }">{{ store.doctors.find(d => d.id === row.doctorId)?.name || row.doctorId }}</template></el-table-column>
          <el-table-column label="重复星期"><template #default="{ row }">{{ [...row.weekdays].sort((a, b) => (a || 7) - (b || 7)).map(d => `周${['日', '一', '二', '三', '四', '五', '六'][d]}`).join('、') }}</template></el-table-column>
          <el-table-column label="时段"><template #default="{ row }">{{ row.start }}–{{ row.end }} · {{ row.duration }} 分钟 / {{ row.capacity }} 个</template></el-table-column>
          <el-table-column label="开放状态" width="100"><template #default="{ row }">{{ row.open ? '开放' : '关闭' }}</template></el-table-column>
          <el-table-column label="操作" width="160"><template #default="{ row }"><el-button link type="primary" :disabled="busy" @click="showTemplate(row)">编辑</el-button><el-popconfirm title="确认删除该模板？" @confirm="remove('template', row.id)"><template #reference><el-button link type="danger" :disabled="busy">删除</el-button></template></el-popconfirm></template></el-table-column>
        </el-table>
      </el-tab-pane>
      <el-tab-pane label="节假日规则">
        <el-button type="primary" :disabled="!store.setting || busy" @click="showHoliday()">新增日期规则</el-button>
        <el-table :data="[...(store.setting?.holidays || [])].sort((a, b) => a.date.localeCompare(b.date))" empty-text="暂无日期规则，按重复星期生成排班。">
          <el-table-column prop="date" label="日期" /><el-table-column prop="name" label="规则名称" />
          <el-table-column label="规则"><template #default="{ row }">{{ row.kind === 'rest' ? '休诊：跳过开放排班' : '补班：优先于重复星期' }}</template></el-table-column>
          <el-table-column label="操作" width="160"><template #default="{ row }"><el-button link type="primary" :disabled="busy" @click="showHoliday(row)">编辑</el-button><el-popconfirm title="确认删除该日期规则？" @confirm="remove('holiday', row.date)"><template #reference><el-button link type="danger" :disabled="busy">删除</el-button></template></el-popconfirm></template></el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
    <el-dialog :model-value="Boolean(editor)" :title="editor === 'template' ? '排班模板' : '节假日规则'" width="580px" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @update:model-value="(v: boolean) => { if (!v && !busy) editor = '' }">
      <el-form label-position="top" :disabled="busy">
        <template v-if="editor === 'template'">
          <el-form-item label="模板名称"><el-input v-model="template.name" maxlength="50" aria-label="模板名称" /></el-form-item>
          <el-form-item label="医生"><el-select v-model="template.doctorId" aria-label="模板医生"><el-option v-for="d in store.clinicDoctors" :key="d.id" :value="d.id" :label="d.name" /></el-select></el-form-item>
          <el-form-item label="重复星期"><el-checkbox-group v-model="template.weekdays"><el-checkbox v-for="(label, i) in ['日', '一', '二', '三', '四', '五', '六']" :key="i" :value="i">周{{ label }}</el-checkbox></el-checkbox-group></el-form-item>
          <div class="rule-grid"><el-form-item label="开始时间"><el-time-select v-model="template.start" start="00:00" end="23:45" step="00:15" /></el-form-item><el-form-item label="结束时间"><el-time-select v-model="template.end" start="00:15" end="23:45" step="00:15" /></el-form-item><el-form-item label="每时段分钟数"><el-input-number v-model="template.duration" :min="1" :max="1440" :precision="0" /></el-form-item><el-form-item label="每时段容量"><el-input-number v-model="template.capacity" :min="0" :max="100" :precision="0" /></el-form-item></div>
          <el-form-item label="开放状态"><el-switch v-model="template.open" active-text="开放" inactive-text="关闭" /></el-form-item>
        </template>
        <template v-else>
          <el-form-item label="规则名称"><el-input v-model="holiday.name" maxlength="50" aria-label="规则名称" /></el-form-item>
          <el-form-item v-if="!editingDate" label="配置方式"><el-switch v-model="useRange" active-text="连续日期" inactive-text="单日" aria-label="连续日期" /></el-form-item>
          <el-form-item label="规则日期"><el-date-picker v-if="!useRange" v-model="holiday.date" value-format="YYYY-MM-DD" :clearable="false" aria-label="规则日期" /><el-date-picker v-else v-model="holidayRange" type="daterange" value-format="YYYY-MM-DD" :clearable="false" start-placeholder="规则开始日期" end-placeholder="规则结束日期" /></el-form-item>
          <el-form-item label="日期规则"><el-radio-group v-model="holiday.kind"><el-radio value="rest">休诊日</el-radio><el-radio value="work">补班日</el-radio></el-radio-group></el-form-item>
          <el-alert title="保存规则不会关闭已有排班或变更预约。已有排班请使用停诊处理。" type="info" :closable="false" />
          <p v-if="useRange && holidayDates.length">将新增 {{ holidayDates.length }} 条日期规则，已有日期规则冲突时整批不保存。</p>
        </template>
      </el-form>
      <el-alert v-if="error || validation" :title="error || validation" :type="error ? 'error' : 'info'" :closable="false" />
      <el-alert v-if="store.setting && version !== store.setting.version" title="规则版本已变化，请关闭弹窗后重新编辑。" type="warning" :closable="false" />
      <template #footer><el-button :disabled="busy" @click="editor = ''">取消</el-button><el-button type="primary" :loading="busy" :disabled="Boolean(validation) || version !== store.setting?.version" @click="save">保存规则</el-button></template>
    </el-dialog>
  </section>
</template>
<style scoped>
.rules-panel { padding: 24px; margin-top: 24px; }
.rule-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 20px; }
.el-alert { margin: 12px 0; }
</style>
