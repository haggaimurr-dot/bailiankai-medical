<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage, type Appointment, type Clinic, type FollowupTemplate, type NursingFeedback } from '../api'
import { auth } from '../auth'
import FollowupDays from './FollowupDays.vue'
const props = defineProps<{ mode: string }>()
const emit = defineEmits<{ 'templates-changed': [] }>()
const templates = ref<FollowupTemplate[]>([]), clinics = ref<Clinic[]>([]), appointments = ref<Appointment[]>([]), records = ref<NursingFeedback[]>([])
const busy = ref(false), loading = ref(false), error = ref(''), page = ref(1), total = ref(0), appointmentId = ref(''), status = ref<Appointment['status']>('completed')
const dates = ref<[string, string] | null>(null)
const emptyTemplate = () => ({ id: '', clinicId: clinics.value[0]?.id || '', name: '', offsets: [1, 3, 7], response: '', disposition: '', standardStatus: 'draft' as 'draft' | 'formal', applicableTreatment: '', nursingInstructions: '', followupChecklist: '', active: false, version: 0 })
const form = ref(emptyTemplate()), editing = ref(false)
const selectedStandard = ref('')
const nursingStandards = computed(() => templates.value.filter(item => item.active && item.standardStatus === 'formal' && item.clinicId === appointments.value.find(row => row.id === appointmentId.value)?.clinicId))
const nursingStandard = computed(() => nursingStandards.value.find(item => item.id === selectedStandard.value))
function localTime() { return new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date()).replace(' ', 'T') }
function timestamp(value: string) { return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
const observedAt = ref(localTime()), content = ref(''), action = ref(''), requestKey = ref(''), submittedDraft = ref('')
let generation = 0
async function load() {
  const current = ++generation; loading.value = true; error.value = ''
  try {
    if (props.mode === 'templates') {
      const [ts, cs] = await Promise.all([api.followupTemplates(), api.clinics()])
      if (current !== generation) return
      templates.value = ts; clinics.value = cs.filter(item => auth.employee?.clinicIds.includes(item.id))
    } else {
      const [result, standards] = await Promise.all([api.appointments({ status: status.value, page: page.value, pageSize: 20 }), api.followupTemplates()])
      if (current !== generation) return
      appointments.value = result.data; total.value = result.meta.total
      templates.value = standards
    }
  } catch (cause) { if (current === generation) error.value = errorMessage(cause) }
  finally { if (current === generation) loading.value = false }
}
function edit(item?: FollowupTemplate) { form.value = item ? { ...emptyTemplate(), ...item, standardStatus: item.standardStatus || 'draft', active: item.standardStatus === 'formal' && item.active, offsets: [...item.offsets] } : emptyTemplate(); editing.value = true; error.value = '' }
async function saveTemplate() {
  if (busy.value) return
  if (!form.value.name.trim() || !form.value.clinicId || !form.value.offsets.length) { error.value = '请填写院区、模板名称和随访天数'; return }
  if (form.value.standardStatus === 'formal' && [form.value.applicableTreatment, form.value.nursingInstructions, form.value.followupChecklist, form.value.response, form.value.disposition].some(value => !value.trim())) { error.value = '正式规范须填写适用治疗、护理要求、随访检查要点、回评文本及异常处理'; return }
  busy.value = true; error.value = ''
  try {
    const { id, clinicId, name, offsets, response, disposition, standardStatus, applicableTreatment, nursingInstructions, followupChecklist, active, version } = form.value
    await api.saveFollowupTemplate(id, { clinicId, name: name.trim(), offsets, response, disposition, standardStatus, applicableTreatment, nursingInstructions, followupChecklist, active: standardStatus === 'formal' && active, version })
    editing.value = false; await load(); emit('templates-changed'); ElMessage.success('护理与随访规范已保存')
  } catch (cause) { error.value = errorMessage(cause) }
  finally { busy.value = false }
}
let historyGeneration = 0
async function history() {
  const id = appointmentId.value, current = ++historyGeneration
  if (!id) { records.value = []; return }
  try { const rows = await api.nursingFeedback(id, dates.value ? { dateFrom: dates.value[0], dateTo: dates.value[1] } : undefined); if (current === historyGeneration) records.value = rows }
  catch (cause) { if (current === historyGeneration) error.value = errorMessage(cause) }
}
function selectAppointment(item: Appointment) {
  selectedStandard.value = ''; appointmentId.value = item.id; records.value = []; content.value = ''; action.value = ''; observedAt.value = localTime(); requestKey.value = ''; submittedDraft.value = ''; error.value = ''; void history()
}
async function saveFeedback() {
  if (busy.value || !appointmentId.value) return
  const date = new Date(observedAt.value + '+08:00')
  if (!content.value.trim() || !action.value.trim() || !Number.isFinite(date.getTime()) || date.getTime() > Date.now()) { error.value = '请填写护理情况、实际措施及有效的非未来时间'; return }
  const body = { observedAt: date.toISOString(), content: content.value.trim(), action: action.value.trim() }, draft = JSON.stringify(body)
  if (draft !== submittedDraft.value) { requestKey.value = crypto.randomUUID(); submittedDraft.value = draft }
  busy.value = true; error.value = ''
  try {
    await api.recordNursingFeedback(appointmentId.value, { ...body, requestKey: requestKey.value })
    content.value = ''; action.value = ''; submittedDraft.value = ''; requestKey.value = ''; await history(); ElMessage.success('护理反馈已保存')
  } catch (cause) { error.value = errorMessage(cause) }
  finally { busy.value = false }
}
watch(() => props.mode, () => { appointmentId.value = ''; records.value = []; editing.value = false; page.value = 1; void load() })
onMounted(load)
</script>
<template>
  <section class="tools">
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <template v-if="mode === 'templates'">
      <div class="toolbar"><div><h2>护理与随访规范</h2><p>主管维护本院区规范。完整填写后设为正式规范并启用，医护应用前核对适用治疗。</p></div><el-button v-if="auth.employee?.role === 'manager'" type="primary" :disabled="busy" @click="edit()">新增模板</el-button><el-button :disabled="busy" @click="load">刷新模板</el-button></div>
      <el-skeleton v-if="loading" :rows="3" animated />
      <el-empty v-else-if="!templates.length" description="暂无护理与随访规范，请由主管新增配置" />
      <article v-for="item in templates" :key="item.id" class="entry"><div class="toolbar"><strong>{{ item.name }} · {{ clinics.find(c => c.id === item.clinicId)?.name || item.clinicId }}</strong><el-tag :type="item.active && item.standardStatus === 'formal' ? 'success' : 'info'">{{ item.active && item.standardStatus === 'formal' ? '启用' : '停用' }}</el-tag><el-button v-if="auth.employee?.role === 'manager'" :disabled="busy" @click="edit(item)">编辑模板</el-button></div><el-tag :type="item.standardStatus === 'formal' ? 'success' : 'warning'">{{ item.standardStatus === 'formal' ? '正式规范' : '草稿待完善' }}</el-tag><p>适用治疗：{{ item.applicableTreatment || '待配置' }}</p><p>治疗后 {{ item.offsets.join('、') }} 天</p><h4>护理要求</h4><p class="body">{{ item.nursingInstructions || '待配置' }}</p><h4>随访检查要点</h4><p class="body">{{ item.followupChecklist || '待配置' }}</p><p class="body">{{ item.response || '未配置回评文本' }}</p><h4>异常处理参考</h4><p class="body">{{ item.disposition || '待配置' }}</p><small>V{{ item.version }} · {{ timestamp(item.updatedAt) }}</small></article>
      <el-dialog v-model="editing" title="护理与随访规范配置" width="min(620px, 95vw)" :close-on-click-modal="false" :show-close="!busy" :close-on-press-escape="!busy">
        <el-alert v-if="error" :title="error" type="error" :closable="false" />
        <el-form label-position="top"><el-form-item label="院区"><el-select v-model="form.clinicId" :disabled="busy || !!form.id"><el-option v-for="clinic in clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></el-form-item><el-form-item label="模板名称"><el-input v-model="form.name" maxlength="100" :disabled="busy" /></el-form-item><el-form-item label="适用治疗"><el-input v-model="form.applicableTreatment" maxlength="200" show-word-limit :disabled="busy" placeholder="填写治疗项目及适用范围" /></el-form-item><el-form-item label="护理要求"><el-input v-model="form.nursingInstructions" type="textarea" :rows="4" maxlength="2000" show-word-limit :disabled="busy" placeholder="填写经院区确认的护理要求及注意事项" /></el-form-item><el-form-item label="随访检查要点"><el-input v-model="form.followupChecklist" type="textarea" :rows="4" maxlength="2000" show-word-limit :disabled="busy" placeholder="填写各阶段需要询问、观察和记录的事项" /></el-form-item><el-form-item label="随访天数"><FollowupDays v-model="form.offsets" :disabled="busy" /></el-form-item><el-form-item label="回评文本"><el-input v-model="form.response" type="textarea" :rows="3" maxlength="2000" show-word-limit :disabled="busy" /></el-form-item><el-form-item label="异常处理参考文本"><el-input v-model="form.disposition" type="textarea" :rows="2" maxlength="1000" show-word-limit :disabled="busy" /></el-form-item><el-form-item label="规范状态"><el-radio-group v-model="form.standardStatus" :disabled="busy" @change="form.active = false"><el-radio-button value="draft">草稿</el-radio-button><el-radio-button value="formal">正式规范</el-radio-button></el-radio-group></el-form-item><el-form-item label="启用模板"><el-switch v-model="form.active" :disabled="busy || form.standardStatus !== 'formal'" /><small>仅内容完整的正式规范可启用；停用不会改变已有随访任务。</small></el-form-item></el-form>
        <template #footer><el-button :disabled="busy" @click="editing = false">取消</el-button><el-button type="primary" :loading="busy" @click="saveTemplate">保存模板</el-button></template>
      </el-dialog>
    </template>
    <template v-else>
      <div class="toolbar"><div><h2>独立护理反馈</h2><p>按预约追加护理观察与实际措施，保留记录人和时间。记录供员工查看。</p></div><el-button :disabled="busy" @click="load">刷新预约</el-button></div>
      <div class="nursing-layout"><div><el-select v-model="status" :disabled="busy" @change="page = 1; appointmentId = ''; records = []; load()"><el-option label="已治疗预约" value="completed" /><el-option label="已确认预约" value="confirmed" /></el-select><el-skeleton v-if="loading" :rows="3" animated /><el-empty v-else-if="!appointments.length" description="暂无预约" /><button v-for="item in appointments" :key="item.id" class="appointment" :class="{ selected: appointmentId === item.id }" :disabled="busy" @click="selectAppointment(item)"><strong>{{ item.contactName }} · {{ item.doctorName }}</strong><span>{{ timestamp(item.startsAt) }}</span></button><el-pagination v-model:current-page="page" :disabled="busy" :total="total" :page-size="20" layout="prev, pager, next" @current-change="load" /></div>
      <div v-if="appointmentId"><section class="standard-reference"><h3>护理规范参考</h3><el-select v-model="selectedStandard" clearable placeholder="选择本院区已启用正式规范" :disabled="busy"><el-option v-for="item in nursingStandards" :key="item.id" :value="item.id" :label="item.name" /></el-select><div v-if="nursingStandard"><p>适用治疗：{{ nursingStandard.applicableTreatment }} · V{{ nursingStandard.version }}</p><h4>护理要求</h4><p class="body">{{ nursingStandard.nursingInstructions }}</p><h4>随访检查要点</h4><p class="body">{{ nursingStandard.followupChecklist }}</p><h4>异常处理参考</h4><p class="body">{{ nursingStandard.disposition }}</p></div><p v-else>选择适用规范后核对实际情况，并在下方记录已执行的护理措施。</p></section><el-form label-position="top"><el-form-item label="护理发生时间（北京时间）"><input v-model="observedAt" type="datetime-local" step="1" :max="localTime()" :disabled="busy" /></el-form-item><el-form-item label="护理情况 / 患者反馈"><el-input v-model="content" type="textarea" :rows="4" maxlength="2000" show-word-limit :disabled="busy" /></el-form-item><el-form-item label="实际护理措施 / 跟进安排"><el-input v-model="action" type="textarea" :rows="3" maxlength="1000" show-word-limit :disabled="busy" /></el-form-item><el-button type="primary" :loading="busy" @click="saveFeedback">保存护理反馈</el-button></el-form><h3>护理反馈历史</h3><el-date-picker v-model="dates" type="daterange" value-format="YYYY-MM-DD" start-placeholder="护理开始日期" end-placeholder="护理结束日期" @change="records = []; history()" /><el-empty v-if="!records.length" description="当前日期范围没有护理反馈" /><article v-for="item in records" :key="item.id" class="entry"><strong>{{ timestamp(item.observedAt) }} · {{ item.actorName }}</strong><p class="body">{{ item.content }}</p><p class="body">护理措施：{{ item.action }}</p><small>录入于 {{ timestamp(item.createdAt) }}</small></article></div><el-empty v-else description="选择预约录入或查看护理反馈" /></div>
    </template>
  </section>
</template>
<style scoped>
.standard-reference{padding:16px;background:#f7faff;border:1px solid #e3eaf2;border-radius:8px;margin-bottom:20px}.standard-reference h3{margin-top:0}.standard-reference p{font-size:13px;line-height:1.8}.entry h4,.standard-reference h4{margin:12px 0 4px;color:#526a86}.tools{margin-top:20px;background:white;border:1px solid #e3eaf2;border-radius:12px;padding:22px}.toolbar{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:16px}.toolbar>div:first-child{flex:1}.toolbar h2{font-size:17px}.toolbar p,small{color:#8391a5;font-size:12px}.entry{padding:16px 0;border-top:1px solid #edf1f6;font-size:13px}.body{white-space:pre-wrap;line-height:1.8}.nursing-layout{display:grid;grid-template-columns:300px minmax(0,1fr);gap:24px}.appointment{display:block;width:100%;background:white;border:0;border-bottom:1px solid #edf1f6;padding:16px;text-align:left;cursor:pointer}.appointment span{display:block;margin-top:8px;font-size:12px;color:#8391a5}.selected{background:#eff6ff}.el-select{width:100%}input{font:inherit;border:1px solid #dce3ee;border-radius:5px;padding:10px;width:100%}h3{margin:24px 0 16px}.el-date-editor{max-width:100%}@media(max-width:1000px){.nursing-layout{grid-template-columns:1fr}}
</style>
