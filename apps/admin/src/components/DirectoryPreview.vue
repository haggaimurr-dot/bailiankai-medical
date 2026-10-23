<script setup lang="ts">
import { computed } from 'vue'
import { ElImage } from 'element-plus'
import type { ClinicInput, DoctorInput } from '../api'
const props = defineProps<{ profile: ClinicInput | DoctorInput; clinicName?: string }>()
const doctor = computed(() => 'clinicId' in props.profile ? props.profile as DoctorInput : null)
const clinic = computed(() => !doctor.value ? props.profile as ClinicInput : null)
function imageSource(value?: string) {
  try { const url = new URL(value || ''); return url.protocol === 'https:' && !url.username && !url.password ? url.href : '' } catch { return '' }
}
</script>
<template>
  <article class="publication-preview">
    <el-tag v-if="profile.presentation?.isDemo" type="warning">演示资料</el-tag>
    <div class="preview-heading"><el-image v-if="imageSource(doctor?.avatarUrl || clinic?.presentation?.logoUrl)" :src="imageSource(doctor?.avatarUrl || clinic?.presentation?.logoUrl)" fit="cover"><template #error><span>图片不可用</span></template></el-image><span v-else-if="doctor" class="initials">{{ doctor.initials }}</span><div><h2>{{ profile.name }}</h2><p v-if="doctor">{{ doctor.title }} · {{ clinicName }}</p><p v-else>{{ clinic?.presentation?.brandName }}</p></div></div>
    <template v-if="doctor"><p class="multiline">{{ doctor.summary }}</p><div class="tags"><el-tag v-for="focus in doctor.focus" :key="focus">{{ focus }}</el-tag></div><h3>服务介绍</h3><p class="multiline">{{ doctor.serviceIntroduction || '面诊咨询、制定个性化方案、治疗与术后随访。' }}</p></template>
    <template v-if="clinic"><p>{{ clinic.presentation?.tagline }}</p><p class="multiline">{{ clinic.presentation?.introduction }}</p><h3>到院信息</h3><p>地址：{{ clinic.address }}</p><p>电话：{{ clinic.phone }}</p><p v-if="clinic.presentation?.hours">营业时间：{{ clinic.presentation.hours }}</p><p class="multiline" v-if="clinic.presentation?.transport">交通：{{ clinic.presentation.transport }}</p><p class="multiline" v-if="clinic.presentation?.arrivalGuide">到院指引：{{ clinic.presentation.arrivalGuide }}</p><p v-if="clinic.presentation?.location">坐标：{{ clinic.presentation.location.latitude }}, {{ clinic.presentation.location.longitude }}</p></template>
    <h3>资质信息</h3><p v-if="!profile.presentation?.credentials?.length">暂无资质附件</p>
    <section v-for="(credential, index) in profile.presentation?.credentials || []" :key="index" class="credential"><strong>{{ credential.name }}</strong><p v-if="credential.number">编号：{{ credential.number }}</p><p v-if="credential.issuer">颁发机构：{{ credential.issuer }}</p><el-image v-if="imageSource(credential.imageUrl)" :src="imageSource(credential.imageUrl)" :preview-src-list="[imageSource(credential.imageUrl)]" preview-teleported fit="contain"><template #error><span>附件图片不可用，请检查地址</span></template></el-image></section>
  </article>
</template>
<style scoped>
.publication-preview{padding:22px;background:#f8fafc;border:1px solid #e3eaf2;border-radius:12px;overflow-wrap:anywhere}.preview-heading{display:flex;align-items:center;gap:18px;margin:16px 0}.preview-heading .el-image,.initials{width:72px;height:72px;border-radius:16px;background:#e9edf5;display:grid;place-items:center;flex-shrink:0}.publication-preview h2{font-size:23px}.publication-preview h3{margin:22px 0 12px}.publication-preview p{line-height:1.8;color:#65748a;margin:8px 0}.multiline{white-space:pre-wrap}.tags{display:flex;flex-wrap:wrap;gap:8px}.credential{margin:12px 0;padding:14px;background:white;border-radius:8px}.credential .el-image{display:block;width:100%;height:190px;margin-top:12px}
</style>
