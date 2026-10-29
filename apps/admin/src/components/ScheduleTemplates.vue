<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ApiError, api, errorMessage } from '../api'
import type { components } from '../../../../packages/contracts/src/schema'
import { displayDate, shanghaiDate, slotTime } from '../dates'
import { useScheduleStore } from '../stores/schedule'

type TemplateRecord = components['schemas']['ScheduleTemplateRecord']
type TemplateEntry = components['schemas']['ScheduleTemplateEntry']
type Holiday = components['schemas']['Holiday']

const store = useScheduleStore()
const TIME_OPTIONS = ['09:00', '10:00', '11:00', '12:00', '14:00', '15:00', '16:00']
const WEEKDAYS = [
  { value: 1, label: '周一' }, { value: 2, label: '周二' }, { value: 3, label: '周三' },
  { value: 4, label: '周四' }, { value: 5, label: '周五' }, { value: 6, label: '周六' }, { value: 7, label: '周日' },
]
const weekdayLabel = (value: number) => WEEKDAYS.find((w) => w.value === value)?.label || `周${value}`

// ---- 模板列表 ----
const templates = ref<TemplateRecord[]>([])
const templatesLoading = ref(false)
const templatesError = ref('')

async function loadTemplates() {
  if (!store.clinicId) return
  templatesLoading.value = true
  templatesError.value = ''
  try {
    templates.value = await api.scheduleTemplates(store.clinicId)
  } catch (error) {
    templatesError.value = errorMessage(error)
  } finally {
    templatesLoading.value = false
  }
}
watch(() => store.clinicId, loadTemplates)
onMounted(loadTemplates)

// ---- 模板编辑 ----
const editorOpen = ref(false)
const editingId = ref<string | null>(null)
const templateName = ref('')
const entries = ref<TemplateEntry[]>([])
const editorError = ref('')
const saving = ref(false)

function blankEntry(): TemplateEntry {
  return { doctorId: store.clinicDoctors[0]?.id || '', weekday: 1, startTimes: ['09:00'], durationMinutes: 30, capacity: 3 }
}
function openCreate() {
  editingId.value = null
  templateName.value = ''
  entries.value = [blankEntry()]
  editorError.value = ''
  editorOpen.value = true
}
function openEdit(row: TemplateRecord) {
  editingId.value = row.id
  templateName.value = row.name
  entries.value = row.pattern.entries.map((e) => ({ ...e, startTimes: [...e.startTimes] }))
  if (!entries.value.length) entries.value = [blankEntry()]
  editorError.value = ''
  editorOpen.value = true
}
function entryError(e: TemplateEntry): string {
  if (!e.doctorId) return '请选择医生。'
  if (!e.startTimes.length) return '请至少选择一个开始时间。'
  if (!Number.isInteger(e.capacity) || e.capacity < 1 || e.capacity > 100) return '容量须为 1–100 的整数。'
  return ''
}
const editorFormError = computed(() => {
  if (!templateName.value.trim()) return '请填写模板名称。'
  if (templateName.value.trim().length > 100) return '模板名称至多 100 个字符。'
  if (!entries.value.length) return '请至少添加一条每周排班。'
  if (entries.value.length > 50) return '每周排班至多 50 条。'
  for (const e of entries.value) {
    const err = entryError(e)
    if (err) return err
  }
  return ''
})
async function saveTemplate() {
  if (editorFormError.value || saving.value) return
  saving.value = true
  editorError.value = ''
  const body = {
    name: templateName.value.trim(),
    pattern: { entries: entries.value.map((e) => ({ doctorId: e.doctorId, weekday: e.weekday, startTimes: [...e.startTimes].sort(), durationMinutes: 30, capacity: e.capacity })) },
  }
  try {
    if (editingId.value) {
      await api.updateScheduleTemplate(editingId.value, body)
      ElMessage.success('排班模板已更新。')
    } else {
      await api.createScheduleTemplate({ clinicId: store.clinicId, ...body })
      ElMessage.success('排班模板已保存。')
    }
    editorOpen.value = false
    await loadTemplates()
  } catch (error) {
    editorError.value = errorMessage(error)
  } finally {
    saving.value = false
  }
}
async function removeTemplate(row: TemplateRecord) {
  try {
    await ElMessageBox.confirm(`删除模板「${row.name}」？已生成的排班不受影响。`, '删除排班模板', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
  } catch { return }
  try {
    await api.deleteScheduleTemplate(row.id)
    ElMessage.success('排班模板已删除。')
    await loadTemplates()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  }
}

// ---- 一键套用 ----
const applyTemplateId = ref('')
const applyDates = ref<[string, string] | null>(null)
const applyPreviewing = ref(false)
const applyCreating = ref(false)
const applyError = ref('')
const applyPreview = ref<components['schemas']['TemplateApplyPreview'] | null>(null)
watch([applyTemplateId, applyDates], () => { applyPreview.value = null })
const applyFormError = computed(() => {
  if (!applyTemplateId.value) return '请选择要套用的模板。'
  if (!applyDates.value || !applyDates.value[0] || !applyDates.value[1]) return '请选择套用日期范围。'
  return ''
})
const applyGrouped = computed(() => {
  const groups = new Map<string, string[]>()
  const doctorNames = new Map(store.clinicDoctors.map((d) => [d.id, d.name]))
  for (const draft of applyPreview.value?.toCreate || []) {
    const times = groups.get(draft.date) || []
    times.push(`${doctorNames.get(draft.doctorId) || '医生'} ${slotTime(draft.startsAt)}–${slotTime(draft.endsAt)}`)
    groups.set(draft.date, times)
  }
  return [...groups.entries()].map(([date, times]) => ({ date, times }))
})
function conflictLabel(reason: string) {
  return reason === 'past' ? '已过期' : reason === 'holiday' ? '节假日停诊' : '已有排班'
}
async function previewApply() {
  if (applyFormError.value) { applyError.value = applyFormError.value; return }
  applyPreviewing.value = true
  applyError.value = ''
  try {
    applyPreview.value = await api.previewTemplateApply(applyTemplateId.value, { startDate: applyDates.value![0], endDate: applyDates.value![1] })
  } catch (error) {
    applyError.value = errorMessage(error)
  } finally {
    applyPreviewing.value = false
  }
}
async function confirmApply() {
  if (applyFormError.value || !applyPreview.value) return
  applyCreating.value = true
  applyError.value = ''
  try {
    const result = await api.applyScheduleTemplate(applyTemplateId.value, { startDate: applyDates.value![0], endDate: applyDates.value![1] })
    ElMessage.success(`已生成 ${result.created.length} 个时段，跳过 ${result.skipped.length} 个冲突时段（含节假日）。`)
    applyPreview.value = null
  } catch (error) {
    if (error instanceof ApiError && error.status === 409) {
      applyError.value = '套用中途发生冲突，整批已回滚，请重新预览后确认。'
    } else {
      applyError.value = errorMessage(error)
    }
  } finally {
    applyCreating.value = false
  }
}

// ---- 节假日 ----
const holidayFrom = computed(() => {
  const today = shanghaiDate()
  return `${today.slice(0, 7)}-01`
})
const holidayTo = computed(() => {
  const [y, m] = holidayFrom.value.split('-').map(Number)
  const next = new Date(y, m, 0)
  return `${y}-${String(m).padStart(2, '0')}-${String(next.getDate()).padStart(2, '0')}`
})
const holidays = ref<Holiday[]>([])
const holidaysLoading = ref(false)
const holidaysError = ref('')
const newHolidayDate = ref('')
const newHolidayName = ref('')
const holidaySaving = ref(false)

async function loadHolidays() {
  holidaysLoading.value = true
  holidaysError.value = ''
  try {
    holidays.value = await api.holidays(holidayFrom.value, holidayTo.value)
  } catch (error) {
    holidaysError.value = errorMessage(error)
  } finally {
    holidaysLoading.value = false
  }
}
onMounted(loadHolidays)
async function addHoliday() {
  if (!newHolidayDate.value) { ElMessage.warning('请选择节假日日期。'); return }
  if (!newHolidayName.value.trim()) { ElMessage.warning('请填写节假日名称。'); return }
  holidaySaving.value = true
  try {
    await api.createHoliday({ date: newHolidayDate.value, name: newHolidayName.value.trim() })
    ElMessage.success(`${displayDate(newHolidayDate.value)} 已设为节假日，当天自动停诊。`)
    newHolidayDate.value = ''
    newHolidayName.value = ''
    await loadHolidays()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    holidaySaving.value = false
  }
}
async function removeHoliday(row: Holiday) {
  try {
    await ElMessageBox.confirm(`取消 ${displayDate(row.holidayDate)}（${row.name}）的节假日？`, '取消节假日', { type: 'warning', confirmButtonText: '取消节假日', cancelButtonText: '保留' })
  } catch { return }
  try {
    await api.deleteHoliday(row.holidayDate)
    ElMessage.success('节假日已取消。')
    await loadHolidays()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  }
}
</script>

<template>
  <section class="surface template-panel" aria-label="排班模板">
    <div class="panel-heading">
      <div><h2>排班模板一键套用</h2><p>保存每周固定排班为模板，一键套用到任意日期范围；节假日当天自动停诊不生成号源。</p></div>
      <el-button type="primary" @click="openCreate">新建模板</el-button>
    </div>

    <div v-if="templatesError" class="state-block error-state" role="alert"><span class="state-symbol">!</span><h3>模板暂未加载</h3><p>{{ templatesError }}</p><el-button type="primary" :loading="templatesLoading" @click="loadTemplates">重新加载</el-button></div>
    <div v-else v-loading="templatesLoading" class="template-list">
      <p v-if="!templates.length && !templatesLoading" class="batch-empty">当前院区暂无排班模板，点击「新建模板」保存每周固定排班。</p>
      <article v-for="tpl in templates" :key="tpl.id" class="template-card">
        <div class="template-meta"><strong>{{ tpl.name }}</strong><small>{{ tpl.pattern.entries.length }} 条每周排班</small></div>
        <ul class="template-entries">
          <li v-for="(e, i) in tpl.pattern.entries" :key="i">
            {{ weekdayLabel(e.weekday) }} · {{ (store.clinicDoctors.find((d) => d.id === e.doctorId)?.name) || '医生' }} · {{ [...e.startTimes].sort().join('、') }} · 容量 {{ e.capacity }}
          </li>
        </ul>
        <div class="template-actions">
          <el-button link type="primary" @click="applyTemplateId = tpl.id; applyPreview = null">套用</el-button>
          <el-button link type="primary" @click="openEdit(tpl)">编辑</el-button>
          <el-button link type="danger" @click="removeTemplate(tpl)">删除</el-button>
        </div>
      </article>
    </div>

    <div class="apply-block">
      <h3>套用模板</h3>
      <div class="filter-bar">
        <div class="filter-field"><label for="apply-template">模板</label><el-select id="apply-template" v-model="applyTemplateId" placeholder="选择模板"><el-option v-for="tpl in templates" :key="tpl.id" :label="tpl.name" :value="tpl.id" /></el-select></div>
        <div class="filter-field date-filter"><label for="apply-dates">套用日期范围</label><el-date-picker id="apply-dates" v-model="applyDates" type="daterange" value-format="YYYY-MM-DD" format="YYYY 年 MM 月 DD 日" range-separator="至" start-placeholder="开始日期" end-placeholder="结束日期" /></div>
      </div>
      <el-alert v-if="applyError" :title="applyError" type="error" show-icon :closable="false" class="batch-alert" />
      <div class="batch-actions"><el-button type="primary" :loading="applyPreviewing" :disabled="applyCreating" @click="previewApply">预览套用</el-button></div>
      <div v-if="applyPreview" class="batch-preview">
        <p class="batch-summary">预检完成：可新建 <strong>{{ applyPreview.toCreate.length }}</strong> 个，冲突 <strong>{{ applyPreview.conflicts.length }}</strong> 个（节假日当天自动跳过）。确认后只生成无冲突时段。</p>
        <div v-if="applyGrouped.length" class="batch-group"><h3>可新建</h3><ul><li v-for="group in applyGrouped" :key="group.date">{{ displayDate(group.date) }}：{{ group.times.join('、') }}</li></ul></div>
        <div v-if="applyPreview.conflicts.length" class="batch-group"><h3>冲突（确认后跳过）</h3><ul><li v-for="conflict in applyPreview.conflicts" :key="`${conflict.date}-${conflict.doctorId}-${conflict.startsAt}`">{{ displayDate(conflict.date) }} {{ slotTime(conflict.startsAt) }}–{{ slotTime(conflict.endsAt) }} · {{ conflictLabel(conflict.reason) }}</li></ul></div>
        <p v-if="!applyPreview.toCreate.length && !applyPreview.conflicts.length" class="batch-empty">该范围无可生成时段。</p>
        <div class="batch-actions"><el-button type="primary" :loading="applyCreating" :disabled="!applyPreview.toCreate.length" @click="confirmApply">确认套用</el-button><el-button :disabled="applyCreating" @click="applyPreview = null">取消</el-button></div>
      </div>
    </div>
  </section>

  <section class="surface holiday-panel" aria-label="节假日管理">
    <div class="panel-heading"><div><h2>节假日自动停诊</h2><p>设为节假日的日期，批量生成与模板套用都会自动跳过，手工建号也会被拒绝。</p></div></div>
    <div class="filter-bar">
      <div class="filter-field date-filter"><label for="holiday-date">节假日日期</label><el-date-picker id="holiday-date" v-model="newHolidayDate" type="date" value-format="YYYY-MM-DD" format="YYYY 年 MM 月 DD 日" placeholder="选择日期" /></div>
      <div class="filter-field"><label for="holiday-name">名称</label><el-input id="holiday-name" v-model="newHolidayName" maxlength="100" placeholder="例如：国庆节" /></div>
      <div class="filter-field filter-action"><el-button type="primary" :loading="holidaySaving" @click="addHoliday">设为节假日</el-button></div>
    </div>
    <div v-if="holidaysError" class="state-block error-state" role="alert"><span class="state-symbol">!</span><h3>节假日暂未加载</h3><p>{{ holidaysError }}</p><el-button type="primary" :loading="holidaysLoading" @click="loadHolidays">重新加载</el-button></div>
    <div v-else v-loading="holidaysLoading">
      <p v-if="!holidays.length && !holidaysLoading" class="batch-empty">{{ holidayFrom.slice(0, 7) }} 暂无节假日。</p>
      <ul v-else class="holiday-list">
        <li v-for="h in holidays" :key="h.holidayDate"><strong>{{ displayDate(h.holidayDate) }}</strong><span>{{ h.name }}</span><el-button link type="danger" @click="removeHoliday(h)">取消</el-button></li>
      </ul>
    </div>
  </section>

  <el-dialog v-model="editorOpen" title="排班模板" width="640px" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
    <el-form label-position="top" @submit.prevent="saveTemplate">
      <el-form-item label="模板名称"><el-input v-model="templateName" maxlength="100" placeholder="例如：张医生常规周排班" :disabled="saving" /></el-form-item>
      <div v-for="(entry, index) in entries" :key="index" class="entry-card">
        <div class="entry-head"><strong>每周排班 {{ index + 1 }}</strong><el-button v-if="entries.length > 1" link type="danger" :disabled="saving" @click="entries.splice(index, 1)">移除</el-button></div>
        <div class="entry-grid">
          <div class="filter-field"><label :for="`entry-doctor-${index}`">医生</label><el-select :id="`entry-doctor-${index}`" v-model="entry.doctorId" placeholder="选择医生" :disabled="saving"><el-option v-for="doctor in store.clinicDoctors" :key="doctor.id" :label="doctor.name" :value="doctor.id" /></el-select></div>
          <div class="filter-field"><label :for="`entry-weekday-${index}`">星期</label><el-select :id="`entry-weekday-${index}`" v-model="entry.weekday" :disabled="saving"><el-option v-for="w in WEEKDAYS" :key="w.value" :label="w.label" :value="w.value" /></el-select></div>
          <div class="filter-field"><label :for="`entry-capacity-${index}`">容量</label><el-input-number :id="`entry-capacity-${index}`" v-model="entry.capacity" :min="1" :max="100" :step="1" step-strictly :precision="0" :disabled="saving" aria-label="容量" /></div>
        </div>
        <div class="batch-times"><span class="batch-times-label">开始时间（每个 30 分钟）</span><el-checkbox-group v-model="entry.startTimes" :disabled="saving"><el-checkbox v-for="time in TIME_OPTIONS" :key="time" :value="time">{{ time }}</el-checkbox></el-checkbox-group></div>
      </div>
      <el-button :disabled="saving || entries.length >= 50" @click="entries.push(blankEntry())">添加每周排班</el-button>
      <el-alert v-if="editorFormError" :title="editorFormError" type="warning" show-icon :closable="false" class="batch-alert" />
      <el-alert v-if="editorError" :title="editorError" type="error" show-icon :closable="false" class="batch-alert" />
    </el-form>
    <template #footer><el-button :disabled="saving" @click="editorOpen = false">取消</el-button><el-button type="primary" :loading="saving" :disabled="Boolean(editorFormError)" @click="saveTemplate">保存模板</el-button></template>
  </el-dialog>
</template>

<style scoped>
.template-panel, .holiday-panel { margin-top: 16px; padding: 20px; }
.template-list { display: grid; gap: 12px; margin-top: 12px; }
.template-card { border: 1px solid var(--el-border-color-lighter); border-radius: 12px; padding: 14px 16px; }
.template-meta { display: flex; align-items: baseline; gap: 10px; }
.template-meta small { color: var(--el-text-color-secondary); }
.template-entries { margin: 8px 0; padding-left: 18px; color: var(--el-text-color-regular); font-size: 13px; }
.template-actions { display: flex; gap: 4px; }
.apply-block { margin-top: 18px; border-top: 1px dashed var(--el-border-color); padding-top: 16px; }
.apply-block h3 { margin: 0 0 12px; font-size: 15px; }
.holiday-list { list-style: none; margin: 12px 0 0; padding: 0; display: grid; gap: 8px; }
.holiday-list li { display: flex; align-items: center; gap: 12px; border: 1px solid var(--el-border-color-lighter); border-radius: 10px; padding: 8px 14px; }
.holiday-list span { color: var(--el-text-color-secondary); flex: 1; }
.entry-card { border: 1px solid var(--el-border-color-lighter); border-radius: 12px; padding: 12px 14px; margin-bottom: 12px; }
.entry-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.entry-grid { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 12px; }
.filter-action { align-self: end; }
@media (max-width: 640px) { .entry-grid { grid-template-columns: 1fr; } }
</style>
