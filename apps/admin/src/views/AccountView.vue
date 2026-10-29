<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage } from '../api'
import { auth, clearAuth } from '../auth'
const currentPassword = ref(''), password = ref(''), confirmation = ref(''), saving = ref(false), error = ref('')
// 行 115：双因素验证
const tfaEnabled = ref(false), tfaSecret = ref(''), tfaUrl = ref(''), tfaCode = ref(''), tfaBusy = ref(false), tfaError = ref(''), tfaStep = ref<'idle' | 'setup'>('idle')
async function loadTfaStatus() { try { const r: any = await api.tfaStatus(); tfaEnabled.value = !!r.data?.enabled } catch {} }
async function startTfaSetup() {
  tfaBusy.value = true; tfaError.value = ''
  try { const r: any = await api.tfaSetup(); tfaSecret.value = r.data.secret; tfaUrl.value = r.data.otpauthUrl; tfaStep.value = 'setup' }
  catch (e) { tfaError.value = errorMessage(e) }
  finally { tfaBusy.value = false }
}
async function confirmTfa() {
  tfaBusy.value = true; tfaError.value = ''
  try { await api.tfaEnable(tfaCode.value.trim()); tfaEnabled.value = true; tfaStep.value = 'idle'; tfaCode.value = ''; ElMessage.success('双因素验证已开启，下次登录需输入验证码') }
  catch (e) { tfaError.value = errorMessage(e) }
  finally { tfaBusy.value = false }
}
async function disableTfa() {
  tfaBusy.value = true; tfaError.value = ''
  try { await api.tfaDisable(tfaCode.value.trim()); tfaEnabled.value = false; tfaCode.value = ''; ElMessage.success('双因素验证已关闭') }
  catch (e) { tfaError.value = errorMessage(e) }
  finally { tfaBusy.value = false }
}
loadTfaStatus()
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
  <section class="account-panel"><h1>双因素验证</h1>
    <p>开启后，登录时除密码外还需输入验证器 App（Google Authenticator / 微软 Authenticator 等）的 6 位验证码。</p>
    <el-alert v-if="tfaError" :title="tfaError" type="error" :closable="false" />
    <div v-if="!tfaEnabled && tfaStep === 'idle'"><el-button type="primary" :loading="tfaBusy" @click="startTfaSetup">开启双因素验证</el-button></div>
    <div v-if="!tfaEnabled && tfaStep === 'setup'">
      <p>用验证器 App 扫描下方密钥（或手动输入）：</p>
      <p class="tfa-secret">{{ tfaSecret }}</p>
      <el-input v-model="tfaCode" maxlength="6" placeholder="输入 6 位验证码" style="max-width: 200px" />
      <div style="margin-top: 12px"><el-button type="primary" :loading="tfaBusy" :disabled="tfaCode.trim().length !== 6" @click="confirmTfa">验证并开启</el-button>
      <el-button :disabled="tfaBusy" @click="tfaStep = 'idle'">取消</el-button></div>
    </div>
    <div v-if="tfaEnabled">
      <el-tag type="success">已开启</el-tag>
      <p>关闭需输入当前验证码：</p>
      <el-input v-model="tfaCode" maxlength="6" placeholder="输入 6 位验证码" style="max-width: 200px" />
      <div style="margin-top: 12px"><el-button type="danger" plain :loading="tfaBusy" :disabled="tfaCode.trim().length !== 6" @click="disableTfa">关闭双因素验证</el-button></div>
    </div>
  </section>
</template>
<style scoped>.account-panel{max-width:560px;padding:28px;background:white;border:1px solid #e3eaf2;border-radius:12px}.account-panel h1{font-size:24px;color:#304761}.account-panel p{margin:14px 0;color:#66788b;font-size:13px}.account-panel .el-alert{margin-bottom:16px}.tfa-secret{font-family:monospace;background:#f5f7fa;padding:8px 12px;border-radius:6px;word-break:break-all}</style>
