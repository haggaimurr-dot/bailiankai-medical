<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, errorMessage } from '../api'
import type { components } from '../../../../packages/contracts/src/schema'

type DoctorReview = components['schemas']['DoctorReview']

const status = ref('')
const rows = ref<DoctorReview[]>([])
const loading = ref(false)
const error = ref('')
const deciding = ref('')
const note = ref('')

const statusText: Record<string, string> = { pending: '待审核', approved: '已通过', rejected: '已驳回' }

async function load() {
  loading.value = true
  error.value = ''
  try {
    rows.value = await api.doctorReviews(status.value)
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

async function decide(row: DoctorReview, decision: 'approved' | 'rejected') {
  deciding.value = row.id
  try {
    await api.decideDoctorReview(row.id, decision, note.value.trim() || undefined)
    ElMessage.success(decision === 'approved' ? '已通过，评价将向患者公开显示。' : '已驳回，该评价不会公开显示。')
    note.value = ''
    await load()
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    deciding.value = ''
  }
}

onMounted(load)
</script>

<template>
  <main class="page">
    <h1>患者评价审核</h1>
    <p class="lede">仅已治疗患者可评，每单一次；评价先待审核，通过后才向患者端公开显示，未通过的不显示。</p>

    <div class="filters">
      <label>状态
        <select v-model="status" @change="load">
          <option value="">全部</option>
          <option value="pending">待审核</option>
          <option value="approved">已通过</option>
          <option value="rejected">已驳回</option>
        </select>
      </label>
      <button :disabled="loading" @click="load">刷新</button>
    </div>
    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="loading">加载中……</p>
    <p v-else-if="!rows.length">暂无评价。</p>

    <el-table v-else :data="rows" stripe>
      <el-table-column label="医生" prop="doctorId" min-width="140" />
      <el-table-column label="患者" min-width="140">
        <template #default="{ row }">{{ row.displayName || row.patientId }}</template>
      </el-table-column>
      <el-table-column label="评分" width="80">
        <template #default="{ row }">{{ row.rating }} 分</template>
      </el-table-column>
      <el-table-column label="内容" prop="content" min-width="280" />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">{{ statusText[row.status] || row.status }}</template>
      </el-table-column>
      <el-table-column label="提交时间" min-width="150">
        <template #default="{ row }">{{ row.createdAt.slice(0, 16).replace('T', ' ') }}</template>
      </el-table-column>
      <el-table-column label="操作" min-width="260">
        <template #default="{ row }">
          <div v-if="row.status === 'pending'" class="row-actions">
            <el-input v-model="note" placeholder="审核备注（驳回时建议填写）" maxlength="500" style="width: 220px" />
            <el-button size="small" type="success" :disabled="deciding === row.id" @click="decide(row, 'approved')">通过</el-button>
            <el-button size="small" type="danger" :disabled="deciding === row.id" @click="decide(row, 'rejected')">驳回</el-button>
          </div>
          <span v-else-if="row.reviewNote">{{ row.reviewNote }}</span>
        </template>
      </el-table-column>
    </el-table>
  </main>
</template>

<style scoped>
.filters { display: flex; gap: 12px; align-items: flex-end; margin: 16px 0; }
.filters label { font-size: 13px; display: flex; flex-direction: column; gap: 4px; }
.filters select, .filters button { padding: 8px 10px; border: 1px solid #dbe2ea; border-radius: 8px; background: #fff; }
.error { color: #b91c1c; }
.row-actions { display: flex; gap: 8px; align-items: center; }
</style>
