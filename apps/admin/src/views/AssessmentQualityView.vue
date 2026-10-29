<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage } from '../api'
import type { components } from '../../../../packages/contracts/src/schema'
import { useScheduleStore } from '../stores/schedule'

type SampleItem = components['schemas']['QualitySampleItem']

// 新增-P2 行 102：模型效果抽检与人工评分回流。
const store = useScheduleStore()
const count = ref(10)
const samples = ref<SampleItem[]>([])
const sampling = ref(false)
const sampleError = ref('')
const scores = ref<Record<string, { score: number; comment: string; modelVersion: string; saving: boolean }>>({})
const report = ref<components['schemas']['QualityReport'] | null>(null)
const reportLoading = ref(false)
const drillVersion = ref('')

async function sample() {
  sampling.value = true
  sampleError.value = ''
  try {
    samples.value = await api.sampleQualityAssessments({ clinicId: store.clinicId || undefined, count: count.value })
    for (const s of samples.value) {
      if (!scores.value[s.assessmentId]) scores.value[s.assessmentId] = { score: 5, comment: '', modelVersion: '', saving: false }
    }
  } catch (e) { sampleError.value = errorMessage(e) }
  finally { sampling.value = false }
}

async function saveScore(item: SampleItem) {
  const form = scores.value[item.assessmentId]
  if (!form || form.score < 1 || form.score > 5) { ElMessage.warning('评分须为 1–5 分'); return }
  form.saving = true
  try {
    await api.saveQualityScore(item.assessmentId, { score: form.score, comment: form.comment, modelVersion: form.modelVersion })
    ElMessage.success('评分已保存')
    item.hasScore = true
    await loadReport()
  } catch (e) { ElMessage.error(errorMessage(e)) }
  finally { form.saving = false }
}

async function loadReport() {
  reportLoading.value = true
  try {
    report.value = await api.qualityReport(drillVersion.value)
  } catch (e) { ElMessage.error(errorMessage(e)) }
  finally { reportLoading.value = false }
}
onMounted(loadReport)
</script>

<template>
  <section class="page-heading"><div><div class="eyebrow">QUALITY SAMPLING</div><h1>模型效果抽检</h1><p>抽检已复核发布的评估，人工打分回流，报表按模型版本汇总。</p></div></section>

  <section class="surface panel">
    <div class="panel-heading"><div><h2>抽检评分</h2><p>随机抽取已复核发布的评估，逐条打分（1–5 分）。</p></div></div>
    <div class="filter-bar">
      <div class="filter-field"><label for="sample-count">抽检数量</label><el-input-number id="sample-count" v-model="count" :min="1" :max="50" :step="1" /></div>
      <div class="filter-field filter-action"><el-button type="primary" :loading="sampling" @click="sample">开始抽检</el-button></div>
    </div>
    <el-alert v-if="sampleError" :title="sampleError" type="error" :closable="false" show-icon />
    <div v-if="samples.length" class="sample-list">
      <article v-for="item in samples" :key="item.assessmentId" class="sample-card">
        <div class="sample-meta"><strong>{{ item.assessmentId.slice(-8).toUpperCase() }}</strong><small>{{ item.hasScore ? '已评分' : '未评分' }}</small></div>
        <div class="score-form">
          <el-rate v-model="scores[item.assessmentId].score" :max="5" />
          <el-input v-model="scores[item.assessmentId].comment" placeholder="评分备注（可选）" maxlength="1000" />
          <el-input v-model="scores[item.assessmentId].modelVersion" placeholder="模型版本（可选）" maxlength="200" />
          <el-button type="primary" size="small" :loading="scores[item.assessmentId].saving" @click="saveScore(item)">保存评分</el-button>
        </div>
      </article>
    </div>
  </section>

  <section class="surface panel">
    <div class="panel-heading"><div><h2>评分报表</h2><p>按模型版本汇总，点击版本下钻到单条明细。</p></div><el-button :loading="reportLoading" @click="drillVersion = ''; loadReport()">刷新</el-button></div>
    <div v-loading="reportLoading">
      <el-table v-if="report?.summary?.length" :data="report.summary" style="width: 100%">
        <el-table-column label="模型版本" min-width="160"><template #default="{ row }"><el-button link type="primary" @click="drillVersion = row.modelVersion; loadReport()">{{ row.modelVersion || '（未标注）' }}</el-button></template></el-table-column>
        <el-table-column prop="count" label="评分条数" width="100" />
        <el-table-column prop="avgScore" label="平均分" width="100"><template #default="{ row }">{{ Number(row.avgScore).toFixed(2) }}</template></el-table-column>
        <el-table-column prop="minScore" label="最低分" width="90" />
        <el-table-column prop="maxScore" label="最高分" width="90" />
      </el-table>
      <p v-else class="batch-empty">暂无评分数据。</p>
      <div v-if="drillVersion">
        <h3>明细：{{ drillVersion || '（未标注）' }} <el-button link @click="drillVersion = ''; loadReport()">返回汇总</el-button></h3>
        <el-table :data="report?.items || []" style="width: 100%">
          <el-table-column label="评估" min-width="140"><template #default="{ row }">{{ row.assessmentId.slice(-8).toUpperCase() }}</template></el-table-column>
          <el-table-column prop="score" label="评分" width="80" />
          <el-table-column prop="comment" label="备注" min-width="200" />
          <el-table-column prop="scorer" label="评分人" width="140" />
        </el-table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.panel { padding: 20px; margin-top: 16px; }
.sample-list { display: grid; gap: 12px; margin-top: 12px; }
.sample-card { border: 1px solid var(--el-border-color-lighter); border-radius: 12px; padding: 14px; }
.sample-meta { display: flex; justify-content: space-between; margin-bottom: 10px; }
.sample-meta small { color: var(--el-text-color-secondary); }
.score-form { display: grid; gap: 8px; }
.filter-action { align-self: end; }
</style>
