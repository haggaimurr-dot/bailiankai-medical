import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { recordEntrySource } from './entrySource'

const routes: RouteRecordRaw[] = [
  { path: '/', name: 'home', component: () => import('./views/HomeView.vue'), meta: { tab: 'home', title: '百莲凯医疗美容' } },
  { path: '/doctors', name: 'doctors', component: () => import('./views/DoctorsView.vue'), meta: { tab: 'doctors', title: '医生团队' } },
  { path: '/doctors/:id', name: 'doctor-detail', component: () => import('./views/DoctorDetailView.vue'), meta: { title: '医生详情' } },
  { path: '/clinic', name: 'clinic', component: () => import('./views/ClinicView.vue'), meta: { title: '院区介绍' } },
  { path: '/schedule', name: 'schedule', component: () => import('./views/ScheduleView.vue'), meta: { title: '排班与预约' } },
  { path: '/booking', name: 'booking', component: () => import('./views/BookingView.vue'), meta: { title: '确认预约' } },
  { path: '/appointments', name: 'appointments', component: () => import('./views/AppointmentsView.vue'), meta: { tab: 'appointments', title: '我的预约' } },
  { path: '/appointments/:id', name: 'appointment-detail', component: () => import('./views/AppointmentDetailView.vue'), meta: { title: '预约详情' } },
  { path: '/appointments/:id/reschedule', name: 'reschedule', component: () => import('./views/RescheduleView.vue'), meta: { title: '改期' } },
  { path: '/assessment', name: 'assessment', component: () => import('./views/AssessmentView.vue'), meta: { title: 'AI 辅助评估' } },
  { path: '/assessment/tasks', name: 'assessment-tasks', component: () => import('./views/AssessmentTasksView.vue'), meta: { title: '我的评估' } },
  { path: '/assessment/result', name: 'assessment-result', component: () => import('./views/AssessmentResultView.vue'), meta: { title: '评估结果' } },
  { path: '/support', name: 'support', component: () => import('./views/SupportView.vue'), meta: { title: '咨询客服' } },
  { path: '/messages', name: 'messages', component: () => import('./views/MessagesView.vue'), meta: { tab: 'messages', title: '消息设置' } },
  { path: '/profile', name: 'profile', component: () => import('./views/ProfileView.vue'), meta: { tab: 'profile', title: '我的' } },
  { path: '/records', name: 'records', component: () => import('./views/RecordsView.vue'), meta: { title: '健康档案' } },
  { path: '/care/plan', name: 'care-plan', component: () => import('./views/CarePlanView.vue'), meta: { title: '关怀计划' } },
  { path: '/care/photos', name: 'care-photos', component: () => import('./views/CarePhotosView.vue'), meta: { title: '关怀照片' } },
  { path: '/followups', name: 'followups', component: () => import('./views/FollowupsView.vue'), meta: { title: '随访任务' } },
  { path: '/recovery', name: 'recovery', component: () => import('./views/RecoveryCalendarView.vue'), meta: { title: '恢复日历' } },
  { path: '/cases', name: 'cases', component: () => import('./views/CasesView.vue'), meta: { title: '真实案例' } },
  { path: '/knowledge', name: 'knowledge', component: () => import('./views/KnowledgeView.vue'), meta: { title: '知识库' } },
  { path: '/knowledge/:id', name: 'knowledge-detail', component: () => import('./views/KnowledgeDetailView.vue'), meta: { title: '文章详情' } },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export const router = createRouter({ history: createWebHistory(), routes, scrollBehavior: () => ({ top: 0 }) })

router.afterEach((to) => {
  document.title = `${String(to.meta.title || '百莲凯')} · 百莲凯患者端`
  recordEntrySource(to)
})
