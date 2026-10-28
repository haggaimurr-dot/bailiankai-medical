<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, errorMessage, type Clinic } from '../api'
import type { components } from '../../../../packages/contracts/src/schema'
import { auth } from '../auth'
type Template = components['schemas']['MessageTemplate']
type Notice = components['schemas']['Notification']
const clinics = ref<Clinic[]>([]), templates = ref<Template[]>([]), rows = ref<Notice[]>([])
const attempts = ref<components['schemas']['NotificationAttempt'][]>([])
const error = ref(''), loading = ref(false), saving = ref(false), status = ref(''), category = ref(''), page = ref(1), total = ref(0), showAttempts = ref(false)
const form = ref<components['schemas']['MessageTemplateChange']>({ clinicId: '', kind: 'revisit', templateId: '', titleKey: 'thing1', timeKey: 'time2', page: 'pages/recovery-calendar/index', state: 'developer', leadMinutes: 1440, enabled: false, version: 0 })
const labels: Record<string, string> = { queued: '待发送', sending: '发送中', sent: '已发送', retry: '等待重试', blocked: '待授权', failed: '发送失败', unknown: '结果不明', cancelled: '已取消' }
const categoryLabels: Record<string, string> = { authorization: '未授权', template: '模板问题', rate_limit: '频率受限', transient: '临时失败', source: '来源已变更', other: '其他原因' }
const retryableCategory = (row: Notice) => row.status === 'failed' && (row.failureCategory === 'transient' || row.failureCategory === 'rate_limit')
function date(v: string) { return new Date(v).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }) }
function selectTemplate() {
  const found = templates.value.find(t => t.clinicId === form.value.clinicId && t.kind === form.value.kind)
  const { clinicId, kind } = form.value
  form.value = found ? { clinicId: found.clinicId, kind: found.kind, templateId: found.templateId, titleKey: found.titleKey, timeKey: found.timeKey, page: found.page, state: found.state, leadMinutes: found.leadMinutes, enabled: found.enabled, version: found.version } : { clinicId, kind, templateId: '', titleKey: 'thing1', timeKey: 'time2', page: kind === 'revisit' ? 'pages/recovery-calendar/index' : 'pages/followups/index', state: 'developer', leadMinutes: 1440, enabled: false, version: 0 }
}
async function load() {
  loading.value = true; error.value = ''
  try {
    const [ts, cs, queue] = await Promise.all([api.messageTemplates(), api.clinics(), api.notificationQueue(status.value, page.value, category.value)])
    templates.value = ts; clinics.value = cs.filter(c => auth.employee?.clinicIds.includes(c.id)); rows.value = queue.data; total.value = queue.meta.total
    if (!form.value.clinicId && clinics.value[0]) { form.value.clinicId = clinics.value[0].id; selectTemplate() }
  } catch (e) { error.value = errorMessage(e) } finally { loading.value = false }
}
async function save() {
  saving.value = true; error.value = ''
  try { const t = await api.saveMessageTemplate(form.value); form.value.version = t.version; await load() }
  catch (e) { error.value = errorMessage(e) } finally { saving.value = false }
}
async function history(row: Notice) { error.value = ''; try { attempts.value = await api.notificationAttempts(row.id); showAttempts.value = true } catch (e) { error.value = errorMessage(e) } }
async function retry(row: Notice) { saving.value = true; error.value = ''; try { await api.retryNotification(row.id); await load() } catch (e) { error.value = errorMessage(e) } finally { saving.value = false } }
onMounted(load)
</script>

<template>
  <div class="page-heading"><div><h1>消息与复诊提醒</h1><p>配置复诊和随访模板，查看通知队列与发送记录。</p></div><el-button :loading="loading" @click="load">刷新</el-button></div>
  <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
  <el-card class="message-card"><template #header>消息模板</template>
    <el-form label-width="110px" @submit.prevent="save">
      <div class="template-grid">
        <el-form-item label="院区"><el-select v-model="form.clinicId" @change="selectTemplate"><el-option v-for="c in clinics" :key="c.id" :label="c.name" :value="c.id" /></el-select></el-form-item>
        <el-form-item label="提醒类型"><el-select v-model="form.kind" @change="selectTemplate"><el-option label="复诊提醒" value="revisit" /><el-option label="随访提醒" value="followup" /></el-select></el-form-item>
        <el-form-item label="微信模板ID"><el-input v-model="form.templateId" maxlength="100" placeholder="填写微信后台审核通过的模板ID" /></el-form-item>
        <el-form-item label="提前分钟"><el-input-number v-model="form.leadMinutes" :min="1" :max="10080" /></el-form-item>
        <el-form-item label="标题字段"><el-input v-model="form.titleKey" placeholder="thing1" /></el-form-item>
        <el-form-item label="时间字段"><el-input v-model="form.timeKey" placeholder="time2" /></el-form-item>
        <el-form-item label="跳转页面"><el-select v-model="form.page"><el-option label="恢复日历" value="pages/recovery-calendar/index" /><el-option label="随访列表" value="pages/followups/index" /><el-option label="我的预约" value="pages/appointments/index" /></el-select></el-form-item>
        <el-form-item label="小程序版本"><el-select v-model="form.state"><el-option label="开发版" value="developer" /><el-option label="体验版" value="trial" /><el-option label="正式版" value="formal" /></el-select></el-form-item>
      </div>
      <p>模板需包含一项文字字段和一项时间字段，字段编号与微信模板保持一致。患者每次同意订阅提供一次发送机会。</p>
      <el-form-item label="启用提醒"><el-switch v-model="form.enabled" /></el-form-item>
      <el-button type="primary" :loading="saving" :disabled="!form.clinicId" @click="save">保存模板</el-button>
    </el-form>
  </el-card>
  <el-card class="message-card"><template #header><div class="queue-header"><span>通知队列</span><div class="queue-filters"><el-select v-model="category" clearable placeholder="全部失败原因" :disabled="loading" @change="page = 1; load()"><el-option v-for="(label, key) in categoryLabels" :key="key" :label="label" :value="key" /></el-select><el-select v-model="status" clearable placeholder="全部状态" :disabled="loading" @change="page = 1; load()"><el-option v-for="(label, key) in labels" :key="key" :label="label" :value="key" /></el-select></div></div></template>
    <el-table v-loading="loading" :data="rows" empty-text="暂无通知；启用模板后自动生成临近到期的提醒">
      <el-table-column prop="patientId" label="患者编号" min-width="170" />
      <el-table-column label="类型" width="100"><template #default="{ row }">{{ row.kind === 'revisit' ? '复诊' : '随访' }}</template></el-table-column>
      <el-table-column label="到期时间" min-width="180"><template #default="{ row }">{{ date(row.dueAt) }}</template></el-table-column>
      <el-table-column label="计划发送" min-width="180"><template #default="{ row }">{{ date(row.sendAt) }}</template></el-table-column>
      <el-table-column label="状态" width="120"><template #default="{ row }">{{ labels[row.status] }}</template></el-table-column>
      <el-table-column prop="attempts" label="尝试次数" width="100" />
      <el-table-column label="失败原因" min-width="160"><template #default="{ row }"><span v-if="row.failureCategory">{{ categoryLabels[row.failureCategory] || row.failureCategory }}</span><span v-else>—</span><br v-if="row.lastCode" /><small v-if="row.lastCode" class="failure-code">{{ row.lastCode }}</small></template></el-table-column>
      <el-table-column label="操作" width="240"><template #default="{ row }"><el-button link @click="history(row)">发送记录</el-button><el-button v-if="row.status === 'blocked'" link type="primary" :disabled="saving" @click="retry(row)">重新检查授权</el-button><el-button v-if="retryableCategory(row)" link type="primary" :disabled="saving" @click="retry(row)">重新发送</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-model:current-page="page" :total="total" :page-size="20" layout="prev, pager, next, total" :disabled="loading" @current-change="load" />
    <p>临时失败（系统繁忙、限流、凭证失效）自动退避重试，最多5次；失败后仍可按“临时失败 / 频率受限”分类手动重新发送。未授权、模板问题与来源已变更需先处理原因再重试。结果不明的通知保留记录并停止重发。</p>
  </el-card>
  <el-dialog v-model="showAttempts" title="发送记录"><el-table :data="attempts" empty-text="尚未尝试发送"><el-table-column label="时间"><template #default="{ row }">{{ date(row.createdAt) }}</template></el-table-column><el-table-column label="结果"><template #default="{ row }">{{ labels[row.status] || row.status }}</template></el-table-column><el-table-column label="失败原因"><template #default="{ row }">{{ row.failureCategory ? (categoryLabels[row.failureCategory] || row.failureCategory) : '—' }}</template></el-table-column><el-table-column prop="code" label="代码" /></el-table></el-dialog>
</template>
<style scoped>.message-card{margin-top:20px}.template-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 24px}.queue-header{display:flex;align-items:center;justify-content:space-between}.queue-header .el-select{width:180px}.queue-filters{display:flex;gap:10px}.failure-code{color:#8a99ab;font-size:11px}p{color:#677681;font-size:13px}.el-pagination{margin-top:20px}@media(max-width:900px){.template-grid{grid-template-columns:1fr}}</style>
