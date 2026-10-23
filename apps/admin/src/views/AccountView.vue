<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage } from '../api'
import { auth, clearAuth } from '../auth'
const currentPassword = ref(''), password = ref(''), confirmation = ref(''), saving = ref(false), error = ref('')
async function save() {
  if (saving.value) return
  error.value = ''
  const bytes = new TextEncoder().encode(password.value).length
  if (!currentPassword.value || bytes < 12 || bytes > 72 || password.value !== confirmation.value || password.value === currentPassword.value) { error.value = '请填写原密码；新密码须为 12–72 字节，与原密码不同，且两次输入一致。'; return }
  saving.value = true
  try { await api.changePassword(currentPassword.value, password.value); currentPassword.value = ''; password.value = ''; confirmation.value = ''; clearAuth(); ElMessage.success('密码已修改，请重新登录'); window.location.assign('/login') }
  catch (cause) { error.value = errorMessage(cause) }
  finally { saving.value = false }
}
</script>
<template>
  <section class="account-panel"><h1>个人修改密码</h1><p>{{ auth.employee?.displayName }} · {{ auth.employee?.username }}</p><p>修改后所有已登录会话立即失效，请使用新密码重新登录。</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-form label-position="top" @submit.prevent="save">
      <el-form-item label="原密码"><el-input v-model="currentPassword" type="password" show-password autocomplete="current-password" :disabled="saving" /></el-form-item>
      <el-form-item label="新密码"><el-input v-model="password" type="password" show-password autocomplete="new-password" placeholder="12–72 字节" :disabled="saving" /></el-form-item>
      <el-form-item label="确认新密码"><el-input v-model="confirmation" type="password" show-password autocomplete="new-password" :disabled="saving" /></el-form-item>
      <el-button type="primary" native-type="submit" :loading="saving">保存新密码</el-button>
    </el-form>
  </section>
</template>
<style scoped>.account-panel{max-width:560px;padding:28px;background:white;border:1px solid #e3eaf2;border-radius:12px}.account-panel h1{font-size:24px;color:#304761}.account-panel p{margin:14px 0;color:#66788b;font-size:13px}.account-panel .el-alert{margin-bottom:16px}</style>
