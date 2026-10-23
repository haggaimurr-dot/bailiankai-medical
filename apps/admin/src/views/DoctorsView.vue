<script setup lang="ts">
import { computed } from 'vue'
import { auth } from '../auth'
import DirectoryView from './DirectoryView.vue'
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
import { useScheduleStore } from '../stores/schedule'

const store = useScheduleStore()
const router = useRouter()
const count = computed(() => store.clinicDoctors.length)
async function viewSchedule(doctorId: string) {
  store.doctorId = doctorId
  await router.push('/schedule')
  await store.refresh()
}
</script>

<template>
  <DirectoryView v-if="auth.employee?.role === 'manager'" />
  <template v-else>
  <section class="page-heading"><div><div class="eyebrow">OUR MEDICAL TEAM</div><h1>医生信息</h1><p>查看院区医生信息，快速进入对应排班。</p></div><el-button class="refresh-button" :loading="store.loading" @click="store.refresh()"><AppIcon v-if="!store.loading" name="refresh" />刷新数据</el-button></section>
  <section class="surface doctor-directory">
    <div class="panel-heading"><div><h2>院区医生 <span class="count-chip">{{ store.loading || store.error ? '—' : count }}</span></h2><p>以下为虚构演示资料，仅用于开发验证。</p></div><el-select :model-value="store.clinicId" placeholder="选择院区" aria-label="医生所属院区" class="directory-clinic" @change="store.selectClinic"><el-option v-for="clinic in store.clinics" :key="clinic.id" :label="clinic.name" :value="clinic.id" /></el-select></div>
    <div v-if="store.error" class="state-block error-state" role="alert"><span class="state-symbol">!</span><h3>医生信息暂未加载</h3><p>{{ store.error }}</p><el-button type="primary" :loading="store.loading" @click="store.refresh()">重新加载</el-button></div>
    <div v-else v-loading="store.loading" class="doctor-grid" :aria-busy="store.loading">
      <article v-for="doctor in store.clinicDoctors" :key="doctor.id" class="doctor-profile">
        <div class="profile-header"><span class="profile-avatar">{{ doctor.initials }}</span><div><h3>{{ doctor.name }}</h3><p>{{ doctor.title }}</p></div><span class="profile-demo">演示</span></div>
        <div class="profile-clinic"><AppIcon name="location" />{{ store.selectedClinic?.name }}</div>
        <p class="profile-summary">{{ doctor.summary }}</p>
        <div class="focus-tags"><span v-for="focus in doctor.focus" :key="focus">{{ focus }}</span></div>
        <el-button class="profile-action" @click="viewSchedule(doctor.id)">查看医生排班<AppIcon name="arrow" /></el-button>
      </article>
      <el-empty v-if="!store.loading && !count" description="当前院区暂无医生信息" />
    </div>
  </section>
  </template>
</template>
