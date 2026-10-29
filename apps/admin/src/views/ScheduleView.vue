<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ApiError, api, canEditSchedule, errorMessage, type Slot } from '../api'
import type { components } from '../../../../packages/contracts/src/schema'
import AppIcon from '../components/AppIcon.vue'
import ScheduleActions from '../components/ScheduleActions.vue'
import SchedulePatients from '../components/SchedulePatients.vue'
import ScheduleRules from '../components/ScheduleRules.vue'
import ScheduleTemplates from '../components/ScheduleTemplates.vue'
import { displayDate, shanghaiDate, slotTime } from '../dates'
import { useScheduleStore } from '../stores/schedule'

const store = useScheduleStore()
const selectedSlots = ref<Slot[]>([])
const table = ref<{ clearSelection: () => void }>()
function clearSelection() { selectedSlots.value = []; table.value?.clearSelection() }
watch(() => store.slots, clearSelection)
const editorOpen = ref(false)
const editingSlot = ref<Slot | null>(null)
const capacity = ref<number | undefined>(1)
const isOpen = ref(true)
const saving = ref(false)
const saveError = ref('')
const doctorMap = computed(() => new Map(store.doctors.map((doctor) => [doctor.id, doctor])))
const lastRefresh = computed(() => store.refreshedAt ? slotTime(store.refreshedAt.toISOString()) : '尚未同步')
const selectedDateLabel = computed(() => store.date ? displayDate(store.date) : '')
const capacityError = computed(() => {
  if (capacity.value === undefined || !Number.isInteger(capacity.value)) return '请填写整数容量。'
  if (capacity.value < (editingSlot.value?.reserved || 0)) return `容量不能小于已占用的 ${editingSlot.value?.reserved} 个名额。`
  if (capacity.value < 0 || capacity.value > 100) return '每个时段可设置 0–100 个名额。'
  return ''
})

async function openEditor(slot: Slot) {
  if (store.loading) return
  await store.refresh()
  if (store.error) {
    ElMessage.error('最新名额暂未同步，请刷新后再编辑排班。')
    return
  }
  const latestSlot = store.slots.find((item) => item.id === slot.id)
  if (!latestSlot) {
    ElMessage.warning('该时段已变化，请核对当前列表后重新选择。')
    return
  }
  editingSlot.value = { ...latestSlot }
  capacity.value = latestSlot.capacity
  isOpen.value = latestSlot.status === 'open'
  saveError.value = ''
  editorOpen.value = true
}

async function changeDate(value: string | null) {
  store.date = value || shanghaiDate()
  await store.refresh()
}

async function saveSlot() {
  if (!editingSlot.value || capacityError.value || saving.value || !canEditSchedule.value) return
  saving.value = true
  saveError.value = ''
  try {
    await api.updateSlot(editingSlot.value.id, {
      capacity: capacity.value!,
      status: isOpen.value ? 'open' : 'closed',
      version: editingSlot.value.version,
    })
    editorOpen.value = false
    ElMessage.success('排班已更新，小程序刷新后即可看到变更。')
    await store.refresh()
  } catch (error) {
    if (error instanceof ApiError && error.status === 409) {
      editorOpen.value = false
      await store.refresh()
      ElMessage.warning({
        message: store.error
          ? '该时段已被其他操作更新。最新数据暂未获取成功，请刷新列表后再编辑。'
          : '该时段已被其他操作更新，列表已重新加载。请核对最新内容后再次编辑。',
        duration: 6500,
        showClose: true,
      })
    } else {
      saveError.value = error instanceof ApiError && error.status === 401
        ? '当前编辑凭证已失效，请联系项目负责人更新演示配置。'
        : errorMessage(error)
    }
  } finally {
    saving.value = false
  }
}

function beforeClose(done: () => void) { if (!saving.value) done() }

// 批量生成排班：先预检冲突清单，确认后只生成无冲突时段，已有排班不被修改。
const batchDoctorId = ref('')
const batchDates = ref<[string, string] | null>(null)
const batchTimes = ref<string[]>(['09:00', '10:00', '11:00', '12:00', '14:00', '15:00', '16:00'])
const batchCapacity = ref<number | undefined>(3)
const batchPreviewing = ref(false)
const batchCreating = ref(false)
const batchError = ref('')
const batchPreview = ref<components['schemas']['SlotBatchPreview'] | null>(null)
const batchFormError = computed(() => {
  if (batchCapacity.value === undefined || !Number.isInteger(batchCapacity.value) || batchCapacity.value < 1 || batchCapacity.value > 100) return '容量须为 1–100 的整数。'
  if (!batchDates.value || !batchDates.value[0] || !batchDates.value[1]) return '请选择日期范围。'
  if (batchTimes.value.length === 0) return '请至少选择一个开始时间。'
  return ''
})
watch([batchDoctorId, batchDates, batchTimes, batchCapacity], () => { batchPreview.value = null })
const groupedToCreate = computed(() => {
  const groups = new Map<string, string[]>()
  for (const draft of batchPreview.value?.toCreate || []) {
    const times = groups.get(draft.date) || []
    times.push(`${slotTime(draft.startsAt)}–${slotTime(draft.endsAt)}`)
    groups.set(draft.date, times)
  }
  return [...groups.entries()].map(([date, times]) => ({ date, times }))
})
function batchReasonLabel(reason: string) {
  return reason === 'past' ? '已过期' : reason === 'holiday' ? '休息日' : '已有排班'
}
function batchGridBody() {
  const doctorId = batchDoctorId.value || store.doctorId || store.clinicDoctors[0]?.id || ''
  if (!doctorId) {
    batchError.value = '请先选择医生。'
    return null
  }
  if (batchFormError.value) {
    batchError.value = batchFormError.value
    return null
  }
  return {
    clinicId: store.clinicId,
    doctorId,
    startDate: batchDates.value![0],
    endDate: batchDates.value![1],
    startTimes: [...batchTimes.value].sort(),
    durationMinutes: 30,
    capacity: batchCapacity.value!,
  }
}
async function previewBatch() {
  const body = batchGridBody()
  if (!body) return
  batchPreviewing.value = true
  batchError.value = ''
  try {
    batchPreview.value = await api.previewSlotBatch(body)
  } catch (error) {
    batchError.value = errorMessage(error)
  } finally {
    batchPreviewing.value = false
  }
}
async function confirmBatch() {
  const body = batchGridBody()
  if (!body) return
  batchCreating.value = true
  batchError.value = ''
  try {
    const result = await api.createSlotBatch(body)
    ElMessage.success(`已生成 ${result.created.length} 个时段，跳过 ${result.skipped.length} 个冲突时段。`)
    batchPreview.value = null
    await store.refresh()
  } catch (error) {
    batchError.value = errorMessage(error)
  } finally {
    batchCreating.value = false
  }
}

onMounted(() => { if (store.initialized) void store.refresh() })
</script>

<template>
  <section class="page-heading">
    <div><div class="eyebrow">SCHEDULE OVERVIEW</div><h1>排班工作台</h1><p>安排好每一段时间，让就诊更从容。</p></div>
    <el-button class="refresh-button" :loading="store.loading" @click="store.refresh()"><AppIcon v-if="!store.loading" name="refresh" />刷新数据</el-button>
  </section>

  <section class="welcome-strip" aria-label="当前院区">
    <div class="clinic-emblem"><AppIcon name="location" /></div>
    <div class="welcome-copy"><span class="strip-kicker">当前工作院区</span><h2>{{ store.selectedClinic?.name || '等待院区信息' }}</h2><p>{{ store.selectedClinic?.address || '连接服务后显示院区与排班' }}</p></div>
    <div class="welcome-date"><AppIcon name="calendar" /><span>{{ selectedDateLabel }}</span><small>所选排班日期</small></div>
    <div class="strip-decoration" aria-hidden="true"><span></span><span></span><span></span></div>
  </section>

  <section class="stat-grid" aria-label="所选日期排班统计">
    <article class="stat-card"><div class="stat-copy"><span>排班时段</span><strong>{{ store.loading || store.error ? '—' : store.stats.total }}<small>个</small></strong><p>当前筛选结果</p></div><span class="stat-icon blue"><AppIcon name="calendar" /></span></article>
    <article class="stat-card"><div class="stat-copy"><span>已开放时段</span><strong>{{ store.loading || store.error ? '—' : store.stats.open }}<small>个</small></strong><p>小程序展示为开放</p></div><span class="stat-icon teal"><AppIcon name="check" /></span></article>
    <article class="stat-card"><div class="stat-copy"><span>开放剩余容量</span><strong>{{ store.loading || store.error ? '—' : store.stats.available }}<small>个</small></strong><p>开放时段的剩余名额</p></div><span class="stat-icon purple"><AppIcon name="layers" /></span></article>
    <article class="stat-card"><div class="stat-copy"><span>已关闭时段</span><strong>{{ store.loading || store.error ? '—' : store.stats.closed }}<small>个</small></strong><p>小程序展示为关闭</p></div><span class="stat-icon gray"><AppIcon name="pause" /></span></article>
  </section>

  <section class="schedule-panel surface">
    <ScheduleActions :selected="selectedSlots" @changed="clearSelection" />
    <div class="panel-heading"><div><h2>时段管理 <span class="count-chip">{{ store.loading || store.error ? '—' : store.slots.length }}</span></h2><p>调整时段容量与开放状态，变更会同步至小程序。</p></div><span class="sync-meta"><span></span>最近同步 {{ lastRefresh }}</span></div>
    <div class="filter-bar">
      <div class="filter-field"><label for="clinic-filter">院区</label><el-select id="clinic-filter" :model-value="store.clinicId" placeholder="选择院区" @change="store.selectClinic"><el-option v-for="clinic in store.clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></div>
      <div class="filter-field"><label for="doctor-filter">医生</label><el-select id="doctor-filter" v-model="store.doctorId" clearable placeholder="全部医生" @change="store.refresh()"><el-option v-for="doctor in store.clinicDoctors" :key="doctor.id" :label="doctor.name" :value="doctor.id" /></el-select></div>
      <div class="filter-field date-filter"><label for="date-filter">排班日期</label><el-date-picker id="date-filter" :model-value="store.date" type="date" value-format="YYYY-MM-DD" format="YYYY 年 MM 月 DD 日" :clearable="false" placeholder="选择日期" @update:model-value="changeDate" /></div>
      <el-button class="today-button" :disabled="store.date === shanghaiDate()" @click="changeDate(shanghaiDate())">回到今天</el-button>
    </div>

    <div v-if="store.error" class="state-block error-state" role="alert"><span class="state-symbol">!</span><h3>排班数据暂未加载</h3><p>{{ store.error }}</p><el-button type="primary" :loading="store.loading" @click="store.refresh()">重新加载</el-button></div>
    <div v-else v-loading="store.loading" class="schedule-table-wrap" :aria-busy="store.loading">
      <el-table ref="table" :data="store.visibleSlots" row-key="id" class="schedule-table" :empty-text="store.loading ? '正在加载排班…' : '该日期暂无排班，可切换日期或医生查看。'" @selection-change="(rows: Slot[]) => selectedSlots = rows">
        <el-table-column v-if="canEditSchedule" type="selection" width="48" :selectable="(row: Slot) => row.status === 'open' && !store.loading" />
        <el-table-column label="医生" min-width="210"><template #default="{ row }"><div class="doctor-cell"><span class="doctor-avatar">{{ doctorMap.get(row.doctorId)?.initials || '医' }}</span><div><strong>{{ doctorMap.get(row.doctorId)?.name || '医生信息待同步' }}</strong><small>{{ doctorMap.get(row.doctorId)?.title || '—' }}</small></div></div></template></el-table-column>
        <el-table-column label="就诊时段" min-width="185"><template #default="{ row }"><div class="time-cell"><AppIcon name="clock" /><strong>{{ slotTime(row.startsAt) }}<span>—</span>{{ slotTime(row.endsAt) }}</strong></div></template></el-table-column>
        <el-table-column label="名额使用" min-width="195"><template #default="{ row }"><div class="capacity-cell"><div><strong>{{ row.reserved }}<span> / {{ row.capacity }}</span></strong><small>剩余 {{ Math.max(0, row.capacity - row.reserved) }} 个</small></div><div class="capacity-track"><span :style="{ width: `${Math.min(100, row.reserved / Math.max(row.capacity, 1) * 100)}%` }"></span></div></div></template></el-table-column>
        <el-table-column label="开放状态" min-width="145"><template #default="{ row }"><span class="status-badge" :class="row.status === 'closed' ? 'closed' : row.reserved >= row.capacity ? 'full' : 'open'"><span></span>{{ row.status === 'closed' ? '已关闭' : row.reserved >= row.capacity ? '已满额' : '已开放' }}</span></template></el-table-column>
        <el-table-column label="操作" width="102" align="right"><template #default="{ row }"><el-button type="primary" link :disabled="store.loading" @click="openEditor(row)">编辑排班<AppIcon name="chevron" /></el-button></template></el-table-column>
      </el-table>
    </div>
    <div class="table-footer"><span>统计基于当前院区、医生与日期筛选</span><span>共 {{ store.loading || store.error ? '—' : store.slots.length }} 个时段</span></div>
  </section>

  <section v-if="canEditSchedule" class="surface batch-panel" aria-label="批量生成排班">
    <div class="panel-heading"><div><h2>批量生成排班</h2><p>按日期范围与开始时间批量生成时段；先预览冲突清单，确认后只生成无冲突时段，已有排班不会被修改。</p></div></div>
    <div class="filter-bar">
      <div class="filter-field"><label for="batch-doctor">医生</label><el-select id="batch-doctor" v-model="batchDoctorId" :placeholder="store.doctorId || store.clinicDoctors[0]?.name || '选择医生'"><el-option v-for="doctor in store.clinicDoctors" :key="doctor.id" :label="doctor.name" :value="doctor.id" /></el-select></div>
      <div class="filter-field date-filter"><label for="batch-dates">日期范围</label><el-date-picker id="batch-dates" v-model="batchDates" type="daterange" value-format="YYYY-MM-DD" format="YYYY 年 MM 月 DD 日" range-separator="至" start-placeholder="开始日期" end-placeholder="结束日期" /></div>
      <div class="filter-field"><label for="batch-capacity">每个时段容量</label><el-input-number id="batch-capacity" v-model="batchCapacity" :min="1" :max="100" :step="1" step-strictly :precision="0" aria-label="每个时段容量" /></div>
    </div>
    <div class="batch-times"><span class="batch-times-label">开始时间（每个 30 分钟）</span><el-checkbox-group v-model="batchTimes"><el-checkbox v-for="time in ['09:00', '10:00', '11:00', '12:00', '14:00', '15:00', '16:00']" :key="time" :value="time">{{ time }}</el-checkbox></el-checkbox-group></div>
    <el-alert v-if="batchError" :title="batchError" type="error" show-icon :closable="false" class="batch-alert" />
    <div class="batch-actions"><el-button type="primary" :loading="batchPreviewing" :disabled="batchCreating" @click="previewBatch">预览冲突</el-button></div>
    <div v-if="batchPreview" class="batch-preview">
      <p class="batch-summary">预检完成：可新建 <strong>{{ batchPreview.toCreate.length }}</strong> 个，冲突 <strong>{{ batchPreview.conflicts.length }}</strong> 个。确认后只生成无冲突时段。</p>
      <div v-if="groupedToCreate.length" class="batch-group"><h3>可新建</h3><ul><li v-for="group in groupedToCreate" :key="group.date">{{ displayDate(group.date) }}：{{ group.times.join('、') }}</li></ul></div>
      <div v-if="batchPreview.conflicts.length" class="batch-group"><h3>冲突（确认后跳过）</h3><ul><li v-for="conflict in batchPreview.conflicts" :key="`${conflict.date}-${conflict.startsAt}`">{{ displayDate(conflict.date) }} {{ slotTime(conflict.startsAt) }}–{{ slotTime(conflict.endsAt) }} · {{ batchReasonLabel(conflict.reason) }}<span v-if="conflict.existingSlotId">（容量 {{ conflict.existingCapacity }}，已占 {{ conflict.existingReserved }}）</span></li></ul></div>
      <p v-if="!batchPreview.toCreate.length && !batchPreview.conflicts.length" class="batch-empty">该范围无可生成时段。</p>
      <div class="batch-actions"><el-button type="primary" :loading="batchCreating" :disabled="!batchPreview.toCreate.length" @click="confirmBatch">确认生成</el-button><el-button :disabled="batchCreating" @click="batchPreview = null">取消</el-button></div>
    </div>
  </section>

  <div class="bottom-note"><span class="note-icon">i</span><p>待确认与已确认预约均占用名额。关闭时段后，小程序会显示“已关闭”；已占用名额继续保留。</p></div>
  <ScheduleTemplates v-if="canEditSchedule" />
  <SchedulePatients v-if="canEditSchedule" />
  <ScheduleRules v-if="canEditSchedule" />

  <el-dialog v-model="editorOpen" title="编辑排班" width="480px" :before-close="beforeClose" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
    <template v-if="editingSlot">
      <div class="editor-summary"><span class="doctor-avatar">{{ doctorMap.get(editingSlot.doctorId)?.initials || '医' }}</span><div><strong>{{ doctorMap.get(editingSlot.doctorId)?.name || '医生' }}</strong><p>{{ selectedDateLabel }} · {{ slotTime(editingSlot.startsAt) }}–{{ slotTime(editingSlot.endsAt) }}</p></div></div>
      <el-alert v-if="!canEditSchedule" title="当前环境未配置排班编辑权限，请联系项目负责人。" type="warning" :closable="false" show-icon class="editor-alert" />
      <el-form label-position="top" @submit.prevent="saveSlot">
        <el-form-item label="时段总容量" :error="capacityError"><el-input-number v-model="capacity" :min="editingSlot.reserved" :max="100" :step="1" step-strictly :precision="0" :disabled="saving" aria-label="时段总容量" /><p class="field-help">当前已占用 {{ editingSlot.reserved }} 个名额，容量不能低于已占用数量。</p></el-form-item>
        <el-form-item label="开放状态"><div class="switch-row"><div><strong>{{ isOpen ? '开放时段' : '关闭时段' }}</strong><p>{{ isOpen ? '小程序将此时段显示为开放' : '小程序将此时段显示为关闭' }}</p></div><el-switch v-model="isOpen" :disabled="saving" aria-label="开放时段" /></div></el-form-item>
      </el-form>
      <el-alert v-if="saveError" :title="saveError" type="error" show-icon :closable="false" />
    </template>
    <template #footer><el-button :disabled="saving" @click="editorOpen = false">取消</el-button><el-button type="primary" :loading="saving" :disabled="!canEditSchedule || Boolean(capacityError)" @click="saveSlot">保存修改</el-button></template>
  </el-dialog>
</template>
