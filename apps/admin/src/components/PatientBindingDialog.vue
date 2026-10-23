<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage, type PatientRecordSummary } from '../api'
import { slotTime } from '../dates'
const emit = defineEmits<{ refreshed: [id: string] }>()
const visible = ref(false), busy = ref(false)
const patient = ref<PatientRecordSummary | null>(null)
const code = ref(''), expiresAt = ref(''), error = ref('')
function open(value: PatientRecordSummary) { patient.value = value; code.value = ''; expiresAt.value = ''; error.value = ''; visible.value = true }
async function generate() {
 if (busy.value || !patient.value) return
 busy.value = true; error.value = ''; code.value = ''; expiresAt.value = ''
 try { const result = await api.createPatientBindingCode(patient.value.id); code.value = result.code; expiresAt.value = result.expiresAt }
 catch (cause) { error.value = errorMessage(cause) }
 finally { busy.value = false }
}
async function revoke() {
 if (busy.value || !patient.value) return
 busy.value = true; error.value = ''
 try { await api.revokePatientBindingCode(patient.value.id); code.value = ''; expiresAt.value = ''; ElMessage.success('未使用的绑定码已撤销') }
 catch (cause) { error.value = errorMessage(cause) }
 finally { busy.value = false }
}
async function refresh() {
 if (busy.value || !patient.value) return
 busy.value = true; error.value = ''
 try { patient.value = (await api.patientRecord(patient.value.id)).patient; emit('refreshed', patient.value.id); if (patient.value.wechatBound) { code.value = ''; expiresAt.value = '' } }
 catch (cause) { error.value = errorMessage(cause) }
 finally { busy.value = false }
}
async function copy() {
 try { await navigator.clipboard.writeText(code.value); ElMessage.success('绑定码已复制，请仅交给患者本人') }
 catch { ElMessage.warning('复制失败，请手动选择绑定码复制') }
}
defineExpose({ open })
</script>
<template>
 <el-dialog v-model="visible" title="绑定患者微信身份" width="560px" :close-on-click-modal="false" :show-close="!busy" :close-on-press-escape="!busy" :before-close="(done: () => void) => { if (!busy) done() }">
  <p><strong>{{ patient?.displayName }}</strong> · {{ patient?.id }}</p>
  <el-alert :title="patient?.wechatBound ? '已绑定微信患者身份' : '尚未绑定微信患者身份'" :type="patient?.wechatBound ? 'success' : 'info'" :closable="false" />
  <template v-if="!patient?.wechatBound">
   <p>核对患者本人后生成绑定码，请患者在小程序“我的资料 → 绑定院内档案”粘贴，核对姓名、院区和档案编号后确认。</p>
   <p>绑定码15分钟有效，仅可使用一次。重新生成会使旧码失效。联系方式不会用于自动匹配；微信账号已有业务记录时需人工核对。</p>
   <template v-if="code"><el-input :model-value="code" readonly aria-label="患者微信绑定码" /><p>有效期至：{{ slotTime(expiresAt) }}</p><el-button @click="copy">复制绑定码</el-button></template>
   <div class="binding-actions"><el-button type="primary" :loading="busy" @click="generate">{{ code ? '重新生成绑定码' : '生成绑定码' }}</el-button><el-button :disabled="busy" @click="revoke">撤销未使用的绑定码</el-button></div>
  </template>
  <p v-else>患者重新登录同一微信账号将进入此档案，问诊及历史记录沿用当前患者编号。</p>
  <el-alert v-if="error" :title="error" type="error" :closable="false" />
  <el-button :disabled="busy" @click="refresh">刷新绑定状态</el-button>
 </el-dialog>
</template>
<style scoped>.binding-actions { display: flex; gap: 8px; margin: 16px 0; flex-wrap: wrap; } p { line-height: 1.7; overflow-wrap: anywhere; }</style>
