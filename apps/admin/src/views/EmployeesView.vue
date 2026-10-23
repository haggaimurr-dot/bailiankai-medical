<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, errorMessage, type FollowupStaff, type EmployeeEvent, type Clinic, type PermissionRow } from '../api'
import { auth, clearAuth, roleNames, type Employee } from '../auth'

const employees = ref<Employee[]>([]), staff = ref<FollowupStaff[]>([])
const username = ref(''), displayName = ref(''), password = ref(''), role = ref<Employee['role']>('doctor'), staffId = ref('')
const clinics = ref<Clinic[]>([]), clinicIds = ref<string[]>([])
const loading = ref(false), saving = ref(false), error = ref('')
const matrix = ref<PermissionRow[]>([]), editing = ref<Employee | null>(null), editRole = ref<Employee['role']>('doctor'), editStaffId = ref(''), editClinicIds = ref<string[]>([]), editMode = ref<'access' | 'clinics'>('access')
function availableStaff(selectedRole: Employee['role'], accountId?: string) {
 return staff.value.filter(person => person.status === 'on_duty' && (selectedRole !== 'doctor' || person.role === 'doctor') && (selectedRole !== 'nurse' || person.role === 'nurse') && !employees.value.some(account => account.id !== accountId && account.staffId === person.id))
}
const createStaff = computed(() => availableStaff(role.value))
const editStaff = computed(() => availableStaff(editRole.value, editing.value?.id))
watch(role, () => { staffId.value = '' })
watch(editRole, () => { if (!editStaff.value.some(person => person.id === editStaffId.value)) editStaffId.value = '' })
function openEdit(account: Employee, mode: 'access' | 'clinics') { error.value = ''; editing.value = account; editRole.value = account.role; editStaffId.value = account.staffId || ''; editClinicIds.value = [...account.clinicIds]; editing.value = account; editMode.value = mode }
async function saveEdit() {
 if (!editing.value || saving.value) return
 const account = editing.value
 if (editMode.value === 'access' && ['doctor', 'nurse', 'clinician'].includes(editRole.value) && !editStaffId.value || editMode.value === 'clinics' && !editClinicIds.value.length) { error.value = '请完整选择角色、关联人员或授权院区。'; return }
 saving.value = true; error.value = ''
 try {
  if (editMode.value === 'access') await api.setEmployeeAccess(account.id, account.version, editRole.value, editStaffId.value)
  else await api.setEmployeeClinics(account.id, editClinicIds.value, account.version)
  editing.value = null
  if (account.id === auth.employee?.id) { clearAuth(); window.location.assign('/login'); return }
  delete histories.value[account.id]; await load(); ElMessage.success('账号权限已更新，旧会话已失效')
 } catch (cause) { const message = errorMessage(cause); await load(); error.value = message; editing.value = null }
 finally { saving.value = false }
}
const histories = ref<Record<string, EmployeeEvent[]>>({})
const actionNames: Record<EmployeeEvent['action'], string> = { created: '创建账号', activated: '启用账号', deactivated: '停用账号', 'password-reset': '重置密码', 'clinics-updated': '变更院区授权', 'clinic-created': '新增院区并授权', 'doctor-linked': '关联医生资料', 'password-changed': '个人修改密码', 'access-updated': '调整角色与关联人员' }
function timestamp(value: string) { return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'short', timeStyle: 'short' }).format(new Date(value)) }
async function load() {
  loading.value = true; error.value = ''
  try { [employees.value, staff.value, clinics.value, matrix.value] = await Promise.all([api.employees(), api.followupStaff(), api.clinics(), api.employeePermissions()]); clinics.value = clinics.value.filter(clinic => auth.employee?.clinicIds.includes(clinic.id)) }
  catch (cause) { error.value = errorMessage(cause) }
  finally { loading.value = false }
}
async function create() {
  if (saving.value) return
  if (!username.value.trim() || !displayName.value.trim() || password.value.length < 12 || (['clinician', 'doctor', 'nurse'].includes(role.value) && !staffId.value) || !clinicIds.value.length) { error.value = '请填写账号、姓名、至少 12 位密码，并为医护账号关联人员。'; return }
  saving.value = true; error.value = ''
  try {
    await api.createEmployee({ username: username.value.trim(), displayName: displayName.value.trim(), password: password.value, role: role.value, clinicIds: clinicIds.value, staffId: staffId.value || undefined })
    username.value = ''; displayName.value = ''; password.value = ''; staffId.value = ''
    await load(); ElMessage.success('员工账号已创建，请单独告知本人初始密码')
  } catch (cause) { error.value = errorMessage(cause) }
  finally { saving.value = false }
}
async function toggle(account: Employee) {
  if (saving.value || account.id === auth.employee?.id) return
  saving.value = true; error.value = ''
  try {
    await ElMessageBox.confirm(`确认${account.active ? '停用' : '启用'} ${account.displayName} 的账号？${account.active ? '现有会话将立即失效。' : ''}`, '员工账号', { confirmButtonText: '确认', cancelButtonText: '取消' })
    await api.setEmployeeActive(account.id, account.version, !account.active)
    await load()
  } catch (cause) { if (cause !== 'cancel' && cause !== 'close') error.value = errorMessage(cause) }
  finally { saving.value = false }
}
async function resetPassword(account: Employee) {
  if (saving.value) return
  saving.value = true; error.value = ''
  try {
    const result = await ElMessageBox.prompt(`为 ${account.displayName} 设置新密码；该员工现有会话会立即失效。`, '重置密码', { inputType: 'password', inputValidator: value => value.length >= 12 && value.length <= 128, inputErrorMessage: '密码须为 12–128 位', confirmButtonText: '确认重置', cancelButtonText: '取消' })
    await api.resetEmployeePassword(account.id, account.version, result.value)
    if (account.id === auth.employee?.id) { clearAuth(); window.location.assign('/login'); return }
    await load(); ElMessage.success('密码已重置，请单独告知本人')
  } catch (cause) { if (cause !== 'cancel' && cause !== 'close') error.value = errorMessage(cause) }
  finally { saving.value = false }
}
async function showHistory(account: Employee) {
  try { histories.value[account.id] = await api.employeeEvents(account.id) }
  catch (cause) { error.value = errorMessage(cause) }
}
onMounted(() => { void load() })
</script>

<template>
  <div class="employees-page"><div class="heading"><div><div class="eyebrow">EMPLOYEE ACCESS</div><h1>员工与权限</h1><p>主管开通个人账号，并关联随访人员。账号停用后会话立即失效。</p></div><el-button :disabled="saving" @click="load">刷新</el-button></div>
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <div class="grid"><section class="panel"><h2>开通账号</h2><el-form label-position="top"><el-form-item label="登录账号"><el-input v-model="username" maxlength="40" placeholder="小写字母开头，可用数字、点、横线" /></el-form-item><el-form-item label="员工姓名"><el-input v-model="displayName" maxlength="80" /></el-form-item><el-form-item label="初始密码"><el-input v-model="password" type="password" show-password autocomplete="new-password" placeholder="至少 12 位，请线下交给本人" /></el-form-item><el-form-item label="角色"><el-select v-model="role"><el-option v-for="(label, value) in roleNames" :key="value" :label="label" :value="value" /></el-select></el-form-item><el-form-item :label="['clinician', 'doctor', 'nurse'].includes(role) ? '关联随访人员（必选）' : '关联随访人员（可选）'"><el-select v-model="staffId" clearable placeholder="选择人员"><el-option v-for="person in createStaff" :key="person.id" :label="person.displayName" :value="person.id" /></el-select></el-form-item><el-form-item label="授权院区（必选）"><el-select v-model="clinicIds" multiple><el-option v-for="clinic in clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></el-form-item><el-button type="primary" :loading="saving" @click="create">创建员工账号</el-button></el-form></section>
      <section class="panel"><h2>已开通账号</h2><el-skeleton v-if="loading" :rows="4" animated /><div v-else-if="!employees.length" class="empty">暂无员工账号</div><div v-for="account in employees" :key="account.id" class="account"><div><strong>{{ account.displayName }}</strong><p>{{ account.username }} · {{ roleNames[account.role] }}<span v-if="account.staffId"> · {{ staff.find(item => item.id === account.staffId)?.displayName || account.staffId }}</span></p><el-button size="small" text @click="showHistory(account)">查看账号操作记录</el-button><div v-for="event in histories[account.id] || []" :key="event.id" class="account-event">{{ timestamp(event.createdAt) }} · {{ event.actorName }} · {{ actionNames[event.action] }}</div></div><div><el-tag :type="account.active ? 'success' : 'info'">{{ account.active ? '启用' : '停用' }}</el-tag><el-button size="small" :disabled="saving" @click="openEdit(account, 'clinics')">院区授权</el-button><el-button size="small" :disabled="saving" @click="openEdit(account, 'access')">角色与人员</el-button><el-button size="small" :disabled="saving" @click="resetPassword(account)">重置密码</el-button><el-button v-if="account.id !== auth.employee?.id" size="small" :disabled="saving" @click="toggle(account)">{{ account.active ? '停用' : '启用' }}</el-button></div></div></section></div>
    <section class="panel matrix"><h2>角色权限矩阵</h2><p>允许表示角色具备操作权限；患者资料仍受授权院区及本人负责范围限制。兼容旧账号的医护角色沿用医生权限。</p><el-table :data="matrix" border><el-table-column prop="label" label="页面 / 操作" min-width="180" /><el-table-column v-for="(label, value) in roleNames" :key="value" :label="label" min-width="120"><template #default="{ row }"><el-tag :type="row.roles[value] ? 'success' : 'info'">{{ row.roles[value] ? '允许' : '无权' }}</el-tag></template></el-table-column></el-table></section>
    <el-dialog :model-value="!!editing" :title="editMode === 'access' ? '调整角色与关联人员' : '调整院区授权'" width="520px" :close-on-click-modal="false" :show-close="!saving" @close="editing = null">
      <el-alert title="保存后该员工所有会话立即失效；关联人员调整不会自动转移诊疗与随访任务。" type="warning" :closable="false" />
      <el-alert v-if="error" :title="error" type="error" :closable="false" />
      <el-form label-position="top"><template v-if="editMode === 'access'">
        <el-form-item label="角色"><el-select v-model="editRole" :disabled="saving"><el-option v-for="(label, value) in roleNames" :key="value" :label="label" :value="value" :disabled="editing?.id === auth.employee?.id && value !== 'manager'" /></el-select></el-form-item>
        <el-form-item :label="['doctor', 'nurse', 'clinician'].includes(editRole) ? '关联人员（必选）' : '关联人员（可选）'"><el-select v-model="editStaffId" clearable :disabled="saving"><el-option v-for="person in editStaff" :key="person.id" :label="person.displayName" :value="person.id" /></el-select></el-form-item>
      </template><el-form-item v-else label="授权院区"><el-select v-model="editClinicIds" multiple :disabled="saving"><el-option v-for="clinic in clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></el-form-item></el-form>
      <template #footer><el-button :disabled="saving" @click="editing = null">取消</el-button><el-button type="primary" :loading="saving" @click="saveEdit">保存调整</el-button></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.heading{display:flex;align-items:center;justify-content:space-between;margin-bottom:22px}.heading h1{font-size:25px;color:#304761}.heading p{font-size:13px;color:#8391a5;margin-top:8px}.grid{display:grid;grid-template-columns:minmax(280px,400px) minmax(0,1fr);gap:20px;margin-top:20px}.panel{padding:22px;border:1px solid #e3eaf2;border-radius:12px;background:white}.panel h2{font-size:17px;margin-bottom:18px}.panel .el-select{width:100%}.account{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:15px 0;border-bottom:1px solid #edf1f6}.account p{color:#8391a5;font-size:12px;margin-top:6px}.account .el-button{margin-left:9px}.empty{color:#8391a5;padding:30px;text-align:center}@media(max-width:900px){.grid{grid-template-columns:1fr}}
.matrix{margin-top:20px}.matrix p{font-size:13px;color:#66788b;margin-bottom:16px}.account{flex-wrap:wrap}.account-event{font-size:12px;color:#66788b;padding:5px 0}
</style>
