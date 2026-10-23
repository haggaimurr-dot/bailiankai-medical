import { api, errorMessage, type Clinic, type Doctor, type KnowledgeArticle } from '../../services/api';
import { clinicPresentation, doctorPresentation, navigateClinic, type ClinicPresentation } from '../../services/presentation';
import { knowledge } from '../../services/knowledge';

interface HomeData {
  loading: boolean; error: string; clinics: Clinic[]; clinic: Clinic | null;
  clinicIndex: number; doctors: Doctor[];
  presentation: ClinicPresentation | null; articles: KnowledgeArticle[]; knowledgeError: string;
  steps: { number: string; title: string; description: string }[];
}
interface HomeMethods {
  requestId: number;
  load(clinicId?: string): Promise<void>;
  retry(): void;
  onClinicChange(event: WechatMiniprogram.CustomEvent<{ value: string }>): void;
  openDoctors(): void; openDoctorDetail(event: WechatMiniprogram.TouchEvent): void; openSchedule(event: WechatMiniprogram.TouchEvent): void;
  openAppointments(): void; openRecords(): void; openCalendar(): void; openFlow(): void; openProfile(): void;
  openClinic(): void; openKnowledge(): void; openArticle(e: WechatMiniprogram.TouchEvent): void;
}

Page<HomeData, HomeMethods>({
  data: {
    loading: true, error: '', clinics: [], clinic: null, clinicIndex: 0, doctors: [],
    presentation: null, articles: [], knowledgeError: '',
    steps: [
      { number: '01', title: '预约面诊', description: '选择医生与时段，提交预约后等待确认。' },
      { number: '02', title: '到院沟通', description: '与医生沟通皮肤困扰、既往经历和个人需求。' },
      { number: '03', title: '查看方案', description: '面诊后，在预约详情查看医生发布的个人方案。' },
      { number: '04', title: '治疗与随访', description: '治疗后按任务填写恢复反馈，查看医护回评。' },
    ],
  },
  requestId: 0,
  onShow() { void this.load(this.data.clinic?.id); },
  onUnload() { this.requestId += 1; },
  onPullDownRefresh() { void this.load(this.data.clinic?.id); },
  async load(clinicId) {
    const requestId = ++this.requestId;
    this.setData({ loading: true, error: '', doctors: [], clinics: [], clinic: null, presentation: null, articles: [], knowledgeError: '' });
    try {
      const clinics = await api.clinics();
      if (requestId !== this.requestId) return;
      const clinicIndex = Math.max(0, clinics.findIndex((item) => item.id === clinicId));
      const clinic = clinics[clinicIndex] || null;
      this.setData({ clinics, clinic, clinicIndex, presentation: clinic ? clinicPresentation(clinic) : null });
      if (!clinic) return;
      const [doctorResult, articleResult] = await Promise.allSettled([api.doctors(clinic.id), knowledge.articles(clinic.id, '', 1, 2, clinicPresentation(clinic).isDemo)]);
      if (requestId !== this.requestId) return;
      this.setData({ doctors: doctorResult.status === 'fulfilled' ? doctorResult.value.slice(0, 2).map(row => ({ ...row, presentation: doctorPresentation(row) })) : [],
        error: doctorResult.status === 'rejected' ? errorMessage(doctorResult.reason) : '',
        articles: articleResult.status === 'fulfilled' ? articleResult.value.data || [] : [],
        knowledgeError: articleResult.status === 'rejected' ? errorMessage(articleResult.reason) : '',
      });
    } catch (error) {
      if (requestId === this.requestId) this.setData({ error: errorMessage(error) });
    } finally {
      if (requestId === this.requestId) {
        this.setData({ loading: false });
        wx.stopPullDownRefresh();
      }
    }
  },
  retry() { void this.load(this.data.clinic?.id); },
  onClinicChange(event) {
    const clinic = this.data.clinics[Number(event.detail.value)];
    if (clinic) void this.load(clinic.id);
  },
  openDoctors() { wx.navigateTo({ url: '/pages/doctors/index' }); },
  openDoctorDetail(event) {
    const doctor = this.data.doctors.find((item) => item.id === event.currentTarget.dataset.doctorId);
    if (!doctor || this.data.loading) return;
    wx.navigateTo({ url: `/pages/doctor-detail/index?clinicId=${encodeURIComponent(doctor.clinicId)}&doctorId=${encodeURIComponent(doctor.id)}` });
  },
  openSchedule(event) {
    const clinicId = this.data.clinic?.id;
    if (!clinicId) { this.openDoctors(); return; }
    const doctorId = String(event.currentTarget.dataset.doctorId || '');
    wx.navigateTo({ url: `/pages/schedule/index?clinicId=${encodeURIComponent(clinicId)}&doctorId=${encodeURIComponent(doctorId)}` });
  },
  openAppointments() { wx.navigateTo({ url: '/pages/appointments/index' }); },
  openProfile() { wx.navigateTo({ url: '/pages/profile/index' }); },
  openRecords() { wx.navigateTo({ url: '/pages/records/index' }); },
  openCalendar() { wx.navigateTo({ url: '/pages/recovery-calendar/index' }); },
  openFlow() { wx.pageScrollTo({ selector: '#care-flow', duration: 300 }); },
  openClinic() { if (this.data.clinic && !this.data.loading) navigateClinic(this.data.clinic.id); },
  openKnowledge() { wx.navigateTo({ url: `/pages/knowledge/index?clinicId=${encodeURIComponent(this.data.clinic?.id || '')}` }); },
  openArticle(e) { const row = this.data.articles.find(item => item.id === e.currentTarget.dataset.id); if (row) wx.navigateTo({ url: `/pages/knowledge-detail/index?id=${encodeURIComponent(row.id)}` }); },
});
