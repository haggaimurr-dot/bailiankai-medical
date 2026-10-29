import { createRouter, createWebHistory } from 'vue-router'
import WorkbenchView from './views/WorkbenchView.vue'
import ScheduleView from './views/ScheduleView.vue'
import DirectoryView from './views/DirectoryView.vue'
import DoctorsView from './views/DoctorsView.vue'
import ArrivalsView from './views/ArrivalsView.vue'
import AppointmentsView from './views/AppointmentsView.vue'
import FollowupView from './views/FollowupView.vue'
import CareView from './views/CareView.vue'
import PatientsView from './views/PatientsView.vue'
import LoginView from './views/LoginView.vue'
import EmployeesView from './views/EmployeesView.vue'
import OverviewView from './views/OverviewView.vue'
import MessagesView from './views/MessagesView.vue'
import CasesView from './views/CasesView.vue'
import SupportView from './views/SupportView.vue'
import KnowledgeView from './views/KnowledgeView.vue'
import { auth, setAuth, clearAuth, canAccess, employeeHome } from './auth'
import { api } from './api'
import AccountView from './views/AccountView.vue'
import AssessmentsView from './views/AssessmentsView.vue'
import WaitlistView from './views/WaitlistView.vue'
import ReviewsView from './views/ReviewsView.vue'
import OpsMonitorView from './views/OpsMonitorView.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/assessments', component: AssessmentsView, meta: { title: 'AI 辅助评估', assessmentMode: 'list' } },
    { path: '/assessments/new', component: AssessmentsView, meta: { title: '评估入口', assessmentMode: 'entry' } },
    { path: '/assessments/:id/task', component: AssessmentsView, meta: { title: '评估任务', assessmentMode: 'task' } },
    { path: '/assessments/:id/result', component: AssessmentsView, meta: { title: '评估结果', assessmentMode: 'result' } },
    { path: '/assessments/:id/review', component: AssessmentsView, meta: { title: '医生复核', assessmentMode: 'review' } },
    { path: '/assessments/quality', component: () => import('./views/AssessmentQualityView.vue'), meta: { title: '模型效果抽检' } },
    { path: '/support', component: SupportView, meta: { title: '客服工作台' } },
    { path: '/', redirect: '/appointments' },
    { path: '/account', component: AccountView, meta: { title: '个人修改密码' } },
    { path: '/login', component: LoginView, meta: { title: '员工登录' } },
    { path: '/overview', component: OverviewView, meta: { title: '医生统计与运营分析' } },
    { path: '/arrivals', component: ArrivalsView, meta: { title: '到诊工作台' } },
    { path: '/appointments', component: AppointmentsView, meta: { title: '预约管理' } },
    { path: '/waitlist', component: WaitlistView, meta: { title: '候补队列' } },
    { path: '/reviews', component: ReviewsView, meta: { title: '患者评价审核' } },
    { path: '/workbench', component: WorkbenchView, meta: { title: '待办工作台' } },
    { path: '/handoffs', component: FollowupView, props: { handoffOnly: true }, meta: { title: '人员交接' } },
    { path: '/followups', component: FollowupView, meta: { title: '治疗与随访' } },
    { path: '/care', component: CareView, meta: { title: '方案与照片' } },
    { path: '/patients', component: PatientsView, meta: { title: '统一患者档案' } },
    { path: '/schedule', component: ScheduleView, meta: { title: '排班工作台' } },
    { path: '/doctors', component: DoctorsView, meta: { title: '医生信息' } },
    { path: '/clinics', component: DirectoryView, meta: { title: '院区资料维护' } },
    { path: '/employees', component: EmployeesView, meta: { title: '员工与权限' } },
    { path: '/messages', component: MessagesView, meta: { title: '消息与复诊提醒' } },
    { path: '/cases', component: CasesView, meta: { title: '案例库' } },
    { path: '/knowledge', component: KnowledgeView, meta: { title: '知识库' } },
    { path: '/ops', component: OpsMonitorView, meta: { title: '运维监控' } },
    { path: '/:pathMatch(.*)*', redirect: '/appointments' },
  ],
})

router.beforeEach(async (to) => {
  if (!auth.token) return to.path === '/login' ? true : '/login'
  try { setAuth(auth.token, await api.employeeSession()) }
  catch { clearAuth(); return to.path === '/login' ? true : '/login' }
  if (to.path === '/login') return employeeHome()
  if (!canAccess(to.path)) return employeeHome()
  return true
})

router.afterEach((to) => { document.title = `百莲凯 · ${String(to.meta.title || '管理后台')}` })
