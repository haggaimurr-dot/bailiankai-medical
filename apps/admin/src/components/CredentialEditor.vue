<script setup lang="ts">
import type { Credential } from '../api'
const rows = defineModel<Credential[]>({ required: true })
defineProps<{ disabled?: boolean }>()
function add() { rows.value.push({ name: '', number: '', issuer: '', imageUrl: '', expiresAt: '' }) }
</script>
<template>
  <section class="credentials"><h3>资质与附件</h3><p>最多 20 项。附件填写公开的 HTTPS 资质图片地址；发布后向患者展示。留空仅展示文字。</p>
    <div v-for="(row, index) in rows" :key="index" class="credential-row">
      <el-form-item :label="`资质名称 ${index + 1}`"><el-input v-model="row.name" :disabled="disabled" maxlength="120" /></el-form-item>
      <el-form-item :label="`证书编号 ${index + 1}`"><el-input v-model="row.number" :disabled="disabled" maxlength="120" /></el-form-item>
      <el-form-item :label="`颁发机构 ${index + 1}`"><el-input v-model="row.issuer" :disabled="disabled" maxlength="120" /></el-form-item>
      <el-form-item :label="`附件图片地址 ${index + 1}`"><el-input v-model="row.imageUrl" :disabled="disabled" maxlength="2048" placeholder="https://" /></el-form-item>
      <el-form-item :label="`到期日期 ${index + 1}`"><el-date-picker v-model="row.expiresAt" :disabled="disabled" type="date" value-format="YYYY-MM-DD" placeholder="长期有效（可留空）" clearable /></el-form-item>
      <el-button :disabled="disabled" type="danger" plain @click="rows.splice(index, 1)">移除资质 {{ index + 1 }}</el-button>
    </div>
    <el-button :disabled="disabled || rows.length >= 20" @click="add">添加资质附件</el-button>
  </section>
</template>
<style scoped>
.credentials h3{margin:12px 0}.credentials p{color:#718096;font-size:13px;line-height:1.7;margin-bottom:16px}.credential-row{border:1px solid #e3eaf2;padding:16px;border-radius:10px;margin-bottom:12px}
</style>
