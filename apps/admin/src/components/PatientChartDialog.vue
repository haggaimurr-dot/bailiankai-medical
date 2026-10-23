<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage, type Clinic, type PatientRecordSummary } from '../api'
import { shanghaiDate } from '../dates'
const props = defineProps<{ clinics: Clinic[] }>()
const emit = defineEmits<{ saved: [id: string] }>()
const visible = ref(false)
const saving = ref(false)
const patient = ref<PatientRecordSummary | null>(null)
const blank = () => ({ displayName: '', clinicId: '', gender: 'unknown' as 'unknown' | 'female' | 'male', birthDate: '', contactPhone: '', tags: [] as string[], version: 0 })
const form = ref(blank())
function open(value?: PatientRecordSummary, clinicId = '') {
 patient.value = value || null
 form.value = { ...blank(), ...(value?.profile || {}), displayName: value?.displayName || '', clinicId: value?.profile?.clinicId || clinicId || props.clinics[0]?.id || '', tags: [...(value?.profile?.tags || [])] }
 visible.value = true
}
async function save() {
 if (saving.value) return
 const body = { ...form.value, birthDate: form.value.birthDate || '', displayName: form.value.displayName.trim(), contactPhone: form.value.contactPhone.trim(), tags: [...new Set(form.value.tags.map(tag => tag.trim()))] }
 if (!body.displayName || !body.clinicId) { ElMessage.warning('请填写姓名并选择建档院区'); return }
 if (body.contactPhone && !/^1[3-9][0-9]{9}$/.test(body.contactPhone)) { ElMessage.warning('请填写有效的11位手机号'); return }
 if (body.birthDate && body.birthDate > shanghaiDate(new Date())) { ElMessage.warning('出生日期不能晚于今天'); return }
 if (body.tags.some(tag => !tag || [...tag].length > 24)) { ElMessage.warning('标签不能为空且不超过24字'); return }
 saving.value = true
 try {
  let id = patient.value?.id
  if (id) await api.savePatientChart(id, body)
  else { const { version, ...input } = body; id = (await api.createPatientChart(input)).id }
  visible.value = false
  ElMessage.success(patient.value ? '患者资料与标签已保存' : '患者档案已创建')
  emit('saved', id)
 } catch (cause) { ElMessage.error(errorMessage(cause)) }
 finally { saving.value = false }
}
defineExpose({ open })
</script>
<template>
 <el-dialog v-model="visible" :title="patient ? '编辑患者资料与标签' : '直接新建患者档案'" width="560px" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" :before-close="(done: () => void) => { if (!saving) done() }">
  <el-form label-position="top" @submit.prevent="save">
   <el-form-item label="患者姓名（必填）"><el-input v-model="form.displayName" maxlength="64" :disabled="saving" /></el-form-item>
   <el-form-item label="建档院区（必填）"><el-select v-model="form.clinicId" :disabled="saving || !!patient?.profile"><el-option v-for="clinic in clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></el-form-item>
   <div class="chart-grid">
    <el-form-item label="性别"><el-select v-model="form.gender" :disabled="saving"><el-option label="未填写" value="unknown" /><el-option label="女" value="female" /><el-option label="男" value="male" /></el-select></el-form-item>
    <el-form-item label="出生日期"><el-date-picker v-model="form.birthDate" type="date" value-format="YYYY-MM-DD" :disabled="saving" :disabled-date="(date: Date) => shanghaiDate(date) > shanghaiDate(new Date())" /></el-form-item>
   </div>
   <el-form-item label="联系手机号"><el-input v-model="form.contactPhone" maxlength="11" :disabled="saving" /><small>人工登记联系方式，与患者微信手机号授权独立。</small></el-form-item>
   <el-form-item label="患者标签"><el-select v-model="form.tags" multiple filterable allow-create default-first-option :multiple-limit="20" :disabled="saving" placeholder="选择或输入标签后按回车"><el-option v-for="tag in ['痘坑修复', '痘印改善', '敏感肌', '复诊跟进', '需重点随访']" :key="tag" :value="tag" :label="tag" /></el-select><small>最多20个标签，每个不超过24字。</small></el-form-item>
   <el-button type="primary" native-type="submit" :loading="saving">{{ patient ? '保存患者资料' : '创建档案' }}</el-button>
  </el-form>
 </el-dialog>
</template>
<style scoped>
.chart-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
small { color: #7a8a9e; line-height: 1.8; }
.el-select, .el-date-editor { width: 100%; }
@media (max-width: 600px) { .chart-grid { grid-template-columns: 1fr; } }
</style>
