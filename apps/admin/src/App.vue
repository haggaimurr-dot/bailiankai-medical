<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElConfigProvider } from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import AppIcon from './components/AppIcon.vue'
import HandoffNoticeEntry from './components/HandoffNoticeEntry.vue'
import { useScheduleStore } from './stores/schedule'
import { displayDate, shanghaiDate } from './dates'
import { api } from './api'
import { auth, clearAuth, canAccess, employeeHome } from './auth'

const route = useRoute()
const store = useScheduleStore()
const todayLabel = displayDate(shanghaiDate())
onMounted(() => { if (auth.token && route.path !== '/overview' && auth.employee?.role !== 'operations' && !store.initialized) void store.refresh() })
async function logout() { try { await api.logoutEmployee() } finally { clearAuth(); window.location.assign('/login') } }
</script>

<template>
  <ElConfigProvider :locale="zhCn">
  <RouterView v-if="route.path === '/login'" />
  <div v-else class="app-shell">
    <aside class="sidebar">
      <RouterLink :to="employeeHome()" class="brand" aria-label="百莲凯管理后台首页">
        <span class="brand-mark"><span></span></span>
        <span class="brand-copy"><strong>百莲凯</strong><small>医疗美容</small></span>
      </RouterLink>
      <div class="nav-label">诊所工作空间</div>
      <nav class="side-nav" aria-label="主导航">
        <RouterLink v-if="canAccess('/assessments')" to="/assessments"><AppIcon name="check" /><span>AI 辅助评估</span></RouterLink>
        <RouterLink v-if="canAccess('/support')" to="/support"><AppIcon name="check" /><span>客服工作台</span></RouterLink>
        <RouterLink v-if="canAccess('/arrivals')" to="/arrivals"><AppIcon name="check" /><span>到诊工作台</span></RouterLink>
        <RouterLink v-if="canAccess('/knowledge')" to="/knowledge"><AppIcon name="doctors" /><span>知识库</span></RouterLink>
        <RouterLink v-if="canAccess('/workbench')" to="/workbench"><AppIcon name="check" /><span>待办工作台</span></RouterLink>
        <RouterLink v-if="canAccess('/handoffs')" to="/handoffs"><AppIcon name="layers" /><span>人员交接</span></RouterLink>
        <RouterLink v-if="canAccess('/cases')" to="/cases"><AppIcon name="doctors" /><span>案例库</span></RouterLink>
        <RouterLink v-if="canAccess('/messages')" to="/messages"><AppIcon name="calendar" /><span>消息与复诊提醒</span></RouterLink>
        <RouterLink v-if="canAccess('/overview')" to="/overview"><AppIcon name="check" /><span>医生统计与运营分析</span></RouterLink>
        <RouterLink v-if="canAccess('/appointments')" to="/appointments"><AppIcon name="check" /><span>预约管理</span><span class="nav-dot"></span></RouterLink>
        <RouterLink v-if="canAccess('/waitlist')" to="/waitlist"><AppIcon name="calendar" /><span>候补队列</span></RouterLink>
        <RouterLink v-if="canAccess('/reviews')" to="/reviews"><AppIcon name="check" /><span>患者评价审核</span></RouterLink>
        <RouterLink v-if="canAccess('/patients')" to="/patients"><AppIcon name="doctors" /><span>患者档案</span><span class="nav-dot"></span></RouterLink>
        <RouterLink v-if="canAccess('/care')" to="/care"><AppIcon name="doctors" /><span>方案与照片</span><span class="nav-dot"></span></RouterLink>
        <RouterLink v-if="canAccess('/followups')" to="/followups"><AppIcon name="check" /><span>治疗与随访</span><span class="nav-dot"></span></RouterLink>
        <RouterLink v-if="canAccess('/schedule')" to="/schedule"><AppIcon name="calendar" /><span>排班工作台</span><span class="nav-dot"></span></RouterLink>
        <RouterLink v-if="canAccess('/doctors')" to="/doctors"><AppIcon name="doctors" /><span>{{ auth.employee?.role === 'manager' ? '医生资料维护' : '医生信息' }}</span><span class="nav-dot"></span></RouterLink>
        <RouterLink v-if="canAccess('/clinics')" to="/clinics"><AppIcon name="location" /><span>院区资料维护</span></RouterLink>
        <RouterLink v-if="canAccess('/employees')" to="/employees"><AppIcon name="doctors" /><span>员工与权限</span><span class="nav-dot"></span></RouterLink>
        <RouterLink v-if="canAccess('/ops')" to="/ops"><AppIcon name="check" /><span>运维监控</span></RouterLink>
      </nav>
      <div class="sidebar-bottom">
        <div class="demo-card"><span class="demo-dot"></span><strong>开发演示环境</strong><p>当前使用虚构演示数据<br />请勿录入真实患者信息</p></div>
        <span class="sidebar-footnote">BAILIAN KAI · CARE WITH HEART</span>
      </div>
    </aside>
    <div class="workspace">
      <header class="topbar">
        <div class="breadcrumb">管理后台 <span>/</span> <strong>{{ route.meta.title }}</strong></div>
        <div class="topbar-right"><span class="topbar-date">{{ todayLabel }}</span><span class="environment-badge">演示版</span><HandoffNoticeEntry v-if="canAccess('/workbench')" /><span>{{ auth.employee?.displayName }}</span><RouterLink to="/account">修改密码</RouterLink><el-button size="small" @click="logout">退出</el-button></div>
      </header>
      <main class="main-content"><RouterView :key="route.fullPath" /></main>
      <footer class="page-footer"><span>百莲凯医疗美容 · 管理工作台</span><span>时间均为北京时间 UTC+8</span></footer>
    </div>
  </div>
  </ElConfigProvider>
</template>
