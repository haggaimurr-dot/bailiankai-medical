<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage } from '../api'

interface TemplateStat { templateId: string; sent: number; delivered: number; clicked: number; failed: number }
interface RouteStat { route: string; method: string; statDate: string; total: number; err4xx: number; err5xx: number; sumMs: number; maxMs: number }
interface SlowAlert { id: string; route: string; method: string; status: number; durationMs: number; thresholdMs: number; createdAt: string }
interface DailyRow { tableName: string; rowCount: number; delta?: number }
interface SecurityAlert { id: string; actorType: string; actorId: string; kind: string; detail: string; alertedAt: string; readAt?: string }

const tab = ref('templates')
const loading = ref(false)
const error = ref('')

// 模板效果
const templates = ref<{ id: string; kind: string }[]>([])
const templateStats = ref<Record<string, TemplateStat>>({})
// 接口监控
const routeStats = ref<RouteStat[]>([])
const slowAlerts = ref<SlowAlert[]>([])
const thresholdMs = ref(2000)
const newThreshold = ref('2000')
// 数据日报
const dailyDate = ref(new Date().toISOString().slice(0, 10))
const dailyRows = ref<DailyRow[]>([])
// 安全告警
const alerts = ref<SecurityAlert[]>([])

async function loadTemplates() {
  try {
    const rows = await api.messageTemplates()
    templates.value = rows.map((t: any) => ({ id: t.id, kind: t.kind }))
    for (const t of templates.value) {
      try { templateStats.value[t.id] = await api.templateEffectStats(t.id) } catch { /* 单个失败不影响其他 */ }
    }
  } catch (cause) { error.value = errorMessage(cause) }
}

async function loadMonitoring() {
  try {
    routeStats.value = await api.monitoringRoutes()
    slowAlerts.value = await api.monitoringSlowAlerts()
    const s = await api.getSlowThreshold()
    thresholdMs.value = s.thresholdMs
    newThreshold.value = String(s.thresholdMs)
  } catch (cause) { error.value = errorMessage(cause) }
}

async function saveThreshold() {
  const v = parseInt(newThreshold.value, 10)
  if (!v || v <= 0) { ElMessage.error('阈值须为正整数毫秒'); return }
  try {
    await api.setSlowThreshold(v)
    thresholdMs.value = v
    ElMessage.success('慢请求阈值已更新')
  } catch (cause) { ElMessage.error(errorMessage(cause)) }
}

async function loadDaily() {
  loading.value = true
  try {
    const res = await api.getDataDaily(dailyDate.value)
    dailyRows.value = res.rows
  } catch (cause) { error.value = errorMessage(cause) } finally { loading.value = false }
}

async function generateDaily() {
  loading.value = true
  try {
    await api.generateDataDaily(dailyDate.value)
    ElMessage.success('日报已生成')
    await loadDaily()
  } catch (cause) { ElMessage.error(errorMessage(cause)) } finally { loading.value = false }
}

async function loadAlerts() {
  try { alerts.value = await api.securityAlerts() } catch (cause) { error.value = errorMessage(cause) }
}

async function markAlertRead(id: string) {
  try {
    await api.readSecurityAlert(id)
    await loadAlerts()
  } catch (cause) { ElMessage.error(errorMessage(cause)) }
}

function avgMs(r: RouteStat) { return r.total ? Math.round(r.sumMs / r.total) : 0 }

onMounted(async () => {
  loading.value = true
  await Promise.all([loadTemplates(), loadMonitoring(), loadDaily(), loadAlerts()])
  loading.value = false
})
</script>

<template>
  <main class="page">
    <h1>运维监控</h1>
    <p class="lede">模板效果、接口监控、数据日报与安全告警（仅主管可见）。P2 行117/118/119/123。</p>
    <p v-if="error" class="err">{{ error }}</p>

    <el-tabs v-model="tab">
      <el-tab-pane label="模板效果" name="templates">
        <el-table :data="templates" style="width: 100%">
          <el-table-column prop="kind" label="模板" width="220" />
          <el-table-column label="发送">
            <template #default="s">{{ templateStats[s.row.id]?.sent ?? '—' }}</template>
          </el-table-column>
          <el-table-column label="送达">
            <template #default="s">{{ templateStats[s.row.id]?.delivered ?? '—' }}</template>
          </el-table-column>
          <el-table-column label="点击">
            <template #default="s">{{ templateStats[s.row.id]?.clicked ?? '—' }}</template>
          </el-table-column>
          <el-table-column label="失败">
            <template #default="s">{{ templateStats[s.row.id]?.failed ?? '—' }}</template>
          </el-table-column>
        </el-table>
        <p class="hint">点击数靠消息内链接带模板标记回统计；失败口径与通知失败分类一致。</p>
      </el-tab-pane>

      <el-tab-pane label="接口监控" name="routes">
        <div class="row">
          <span>慢请求阈值：当前 {{ thresholdMs }} ms</span>
          <el-input v-model="newThreshold" style="width: 120px" placeholder="毫秒" />
          <el-button type="primary" @click="saveThreshold">更新阈值</el-button>
        </div>
        <h3>按路由统计（今日）</h3>
        <el-table :data="routeStats" style="width: 100%" max-height="320">
          <el-table-column prop="route" label="路由" min-width="220" />
          <el-table-column prop="method" label="方法" width="80" />
          <el-table-column prop="total" label="调用量" width="90" />
          <el-table-column prop="err4xx" label="4xx" width="70" />
          <el-table-column prop="err5xx" label="5xx" width="70" />
          <el-table-column label="平均耗时(ms)" width="120">
            <template #default="s">{{ avgMs(s.row) }}</template>
          </el-table-column>
          <el-table-column prop="maxMs" label="最大耗时(ms)" width="120" />
        </el-table>
        <h3>慢请求告警</h3>
        <el-table :data="slowAlerts" style="width: 100%" max-height="240">
          <el-table-column prop="route" label="路由" min-width="200" />
          <el-table-column prop="method" label="方法" width="80" />
          <el-table-column prop="status" label="状态码" width="80" />
          <el-table-column prop="durationMs" label="耗时(ms)" width="100" />
          <el-table-column prop="thresholdMs" label="阈值(ms)" width="100" />
          <el-table-column prop="createdAt" label="时间" width="180" />
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="数据日报" name="daily">
        <div class="row">
          <el-date-picker v-model="dailyDate" type="date" value-format="YYYY-MM-DD" placeholder="选择日期" @change="loadDaily" />
          <el-button type="primary" @click="generateDaily" :loading="loading">生成/刷新当日日报</el-button>
        </div>
        <el-table :data="dailyRows" style="width: 100%">
          <el-table-column prop="tableName" label="表" width="240" />
          <el-table-column prop="rowCount" label="行数" width="140" />
          <el-table-column label="较昨日增量">
            <template #default="s">{{ s.row.delta === undefined ? '—' : (s.row.delta > 0 ? '+' : '') + s.row.delta }}</template>
          </el-table-column>
        </el-table>
        <p class="hint">只报计数不含患者明细；日报任务只读统计，不改业务数据。</p>
      </el-tab-pane>

      <el-tab-pane label="安全告警" name="alerts">
        <el-table :data="alerts" style="width: 100%">
          <el-table-column prop="kind" label="类型" width="160" />
          <el-table-column prop="actorType" label="账号类型" width="100" />
          <el-table-column prop="actorId" label="账号" min-width="180" />
          <el-table-column prop="detail" label="详情" min-width="220" />
          <el-table-column prop="alertedAt" label="告警时间" width="180" />
          <el-table-column label="状态" width="100">
            <template #default="s">{{ s.row.readAt ? '已读' : '未读' }}</template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="s">
              <el-button v-if="!s.row.readAt" size="small" @click="markAlertRead(s.row.id)">标为已读</el-button>
            </template>
          </el-table-column>
        </el-table>
        <p class="hint">10 分钟内同一账号越权访问照片被拒 ≥5 次即告警；只记行为计数与账号，不记照片内容。</p>
      </el-tab-pane>
    </el-tabs>
  </main>
</template>

<style scoped>
.page { padding: 16px 20px; }
.lede { color: #666; margin: 4px 0 12px; }
.err { color: #c00; }
.hint { color: #888; font-size: 12px; margin-top: 8px; }
.row { display: flex; gap: 8px; align-items: center; margin-bottom: 12px; }
h3 { margin: 16px 0 8px; }
</style>
