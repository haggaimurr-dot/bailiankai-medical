<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage } from '../api'
import { auth } from '../auth'

const props = defineProps<{ clinicId: string }>()
const content = ref('')
const enabled = ref(true)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notFound = ref(false)
const isManager = auth.employee?.role === 'manager'

async function load() {
  if (!props.clinicId || !isManager) return
  loading.value = true
  error.value = ''
  notFound.value = false
  try {
    const setting = await api.arrivalGuidance(props.clinicId)
    content.value = setting.content
    enabled.value = setting.enabled
  } catch (cause: any) {
    if (cause?.status === 404) {
      notFound.value = true
      content.value = ''
      enabled.value = true
    } else {
      error.value = errorMessage(cause)
    }
  } finally {
    loading.value = false
  }
}
watch(() => props.clinicId, load)
onMounted(load)

async function save() {
  if (!content.value.trim()) { ElMessage.warning('请填写指引文案。'); return }
  if (content.value.trim().length > 2000) { ElMessage.warning('指引文案至多 2000 个字符。'); return }
  saving.value = true
  error.value = ''
  try {
    await api.saveArrivalGuidance({ clinicId: props.clinicId, content: content.value.trim(), enabled: enabled.value })
    ElMessage.success('到诊指引已保存，登记到诊后将自动推送给患者。')
    notFound.value = false
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section v-if="isManager" class="surface guidance-panel" aria-label="到诊指引配置">
    <div class="panel-heading"><div><h2>到诊指引配置</h2><p>登记到诊后，系统自动向患者推送这条面诊流程指引（站内消息）。关闭后不再推送。</p></div></div>
    <div v-loading="loading">
      <el-alert v-if="error" :title="error" type="error" :closable="false" class="batch-alert" />
      <p v-if="notFound && !loading" class="batch-empty">当前院区尚未配置到诊指引，填写后保存即生效。</p>
      <el-form label-position="top" @submit.prevent="save">
        <el-form-item label="指引文案">
          <el-input v-model="content" type="textarea" :rows="4" maxlength="2000" show-word-limit placeholder="例如：欢迎到院！请先到前台签到，等待叫号后进入诊室就诊。" :disabled="saving" />
        </el-form-item>
        <el-form-item label="启用推送"><el-switch v-model="enabled" :disabled="saving" /></el-form-item>
      </el-form>
      <el-button type="primary" :loading="saving" :disabled="!content.trim()" @click="save">保存指引</el-button>
    </div>
  </section>
</template>

<style scoped>
.guidance-panel { padding: 20px; margin-top: 16px; }
</style>
