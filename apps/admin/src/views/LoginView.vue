<script setup lang="ts">
import { ref } from 'vue'
import { api, errorMessage } from '../api'
import { setAuth, employeeHome } from '../auth'

const username = ref(''), password = ref(''), loading = ref(false), error = ref('')
const need2FA = ref(false), pendingEmployeeId = ref(''), tfaCode = ref('')
async function login() {
  if (!username.value.trim() || !password.value || loading.value) return
  loading.value = true; error.value = ''
  try {
    const result = await api.loginEmployee(username.value.trim(), password.value)
    if (result.requires2FA) {
      need2FA.value = true; pendingEmployeeId.value = result.employeeId || ''
      return
    }
    setAuth(result.token!, result.employee!)
    window.location.assign(employeeHome())
  } catch (cause) { error.value = errorMessage(cause) }
  finally { loading.value = false }
}
async function verify2FA() {
  if (tfaCode.value.trim().length !== 6 || loading.value) return
  loading.value = true; error.value = ''
  try {
    const result = await api.verify2FALogin(pendingEmployeeId.value, tfaCode.value.trim())
    setAuth(result.token!, result.employee!)
    window.location.assign(employeeHome())
  } catch (cause) { error.value = errorMessage(cause) }
  finally { loading.value = false }
}
</script>

<template>
  <div class="login-page"><div class="login-card"><div class="eyebrow">BAILIAN KAI</div><h1>员工登录</h1><p>使用个人账号进入管理工作台。所有操作将记录到当前账号。</p><el-alert v-if="error" type="error" :closable="false" :title="error" /><el-form label-position="top" @submit.prevent="login"><el-form-item label="账号"><el-input v-model="username" autocomplete="username" autofocus /></el-form-item><el-form-item label="密码"><el-input v-model="password" type="password" autocomplete="current-password" show-password /></el-form-item><el-button type="primary" native-type="submit" :loading="loading" class="login-button">登录</el-button></el-form>
      <el-form v-if="need2FA" label-position="top" @submit.prevent="verify2FA" style="margin-top: 16px"><el-alert title="该账号开启了双因素验证，请输入验证器 App 的 6 位验证码" type="info" :closable="false" style="margin-bottom: 12px" /><el-form-item label="验证码"><el-input v-model="tfaCode" maxlength="6" autocomplete="one-time-code" /></el-form-item><el-button type="primary" native-type="submit" :loading="loading" :disabled="tfaCode.trim().length !== 6" class="login-button">验证并登录</el-button></el-form></div></div>
</template>

<style scoped>
.login-page{min-height:100vh;display:grid;place-items:center;background:#f1f5fa;padding:24px}.login-card{width:min(420px,100%);padding:36px;background:#fff;border:1px solid #e3eaf2;border-radius:14px;box-shadow:0 14px 40px #253e5a14}.eyebrow{font-size:11px;letter-spacing:.2em;color:#8391a5}.login-card h1{margin:12px 0;color:#304761}.login-card p{font-size:13px;color:#66788b;line-height:1.8;margin-bottom:22px}.login-card .el-alert{margin-bottom:16px}.login-button{width:100%;margin-top:8px}
</style>
