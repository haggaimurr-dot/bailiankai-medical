import { api, errorMessage, type Clinic, type Doctor } from '../../services/api';
import { doctorPresentation, navigateClinic, previewCredential, type Credential } from '../../services/presentation';

interface DetailData {
  loading: boolean;
  error: string;
  doctor: Doctor | null;
  clinic: Clinic | null;
  services: { number: string; title: string; description: string }[];
  notes: string[];
  isDemo: boolean; credentials: Credential[]; reviewedDate: string;
}

interface DetailMethods {
  clinicId: string;
  doctorId: string;
  requestId: number;
  load(): Promise<void>;
  retry(): void;
  openSchedule(): void;
  openDoctors(): void;
  openClinic(): void; preview(e: WechatMiniprogram.TouchEvent): void;
}

Page<DetailData, DetailMethods>({
  data: {
    loading: true, error: '', doctor: null, clinic: null,
    isDemo: true, credentials: [], reviewedDate: '',
    services: [
      { number: '01', title: '面诊沟通', description: '围绕你的皮肤困扰与个人需求，沟通既往就诊、治疗及用药信息。' },
      { number: '02', title: '个人方案', description: '面诊后，可在预约详情查看医生已发布的个人方案与医嘱。' },
      { number: '03', title: '治疗与随访', description: '治疗后，按随访任务记录恢复感受与选填照片，查看医护回评。' },
    ],
    notes: [
      '选择出诊日期和可用时段，填写联系人、手机号及希望沟通的问题。',
      '提交预约后需要工作人员确认，请在“我的预约”查看确认结果与到院时间。',
      '到院前整理既往就诊、治疗及用药信息；预约后的面诊照片为选填项。',
      '具体治疗安排以医生面诊和发布的个人方案为准，预约面诊不等于确认治疗。',
    ],
  },
  clinicId: '', doctorId: '', requestId: 0,
  onLoad(options) {
    this.clinicId = options.clinicId || '';
    this.doctorId = options.doctorId || '';
  },
  onShow() { void this.load(); },
  onUnload() { this.requestId += 1; },
  onPullDownRefresh() { void this.load(); },
  async load() {
    const requestId = ++this.requestId;
    this.setData({ loading: true, error: '', doctor: null, clinic: null, credentials: [], reviewedDate: '' });
    try {
      if (!this.clinicId || !this.doctorId) throw new Error('医生入口信息不完整，请返回医生列表重新选择。');
      const [clinics, doctors] = await Promise.all([api.clinics(), api.doctors(this.clinicId)]);
      if (requestId !== this.requestId) return;
      const clinic = clinics.find((item) => item.id === this.clinicId);
      const doctor = doctors.find((item) => item.id === this.doctorId && item.clinicId === this.clinicId);
      if (!clinic || !doctor) throw new Error('这位医生的资料暂不可用，请返回医生列表重新选择。');
      const presentation = doctorPresentation(doctor);
      const reviewedAt = Date.parse(doctor.qualificationReviewedAt || '');
      const reviewedDate = Number.isFinite(reviewedAt) ? new Date(reviewedAt + 8 * 3600000).toISOString().slice(0, 10) : '';
      this.setData({ clinic, doctor, isDemo: presentation.isDemo, credentials: presentation.credentials, reviewedDate });
    } catch (error) {
      if (requestId === this.requestId) this.setData({ error: errorMessage(error) });
    } finally {
      if (requestId === this.requestId) {
        this.setData({ loading: false });
        wx.stopPullDownRefresh();
      }
    }
  },
  retry() { void this.load(); },
  openSchedule() {
    const { clinic, doctor, loading, error } = this.data;
    if (loading || error || !clinic || !doctor) return;
    wx.navigateTo({ url: `/pages/schedule/index?clinicId=${encodeURIComponent(clinic.id)}&doctorId=${encodeURIComponent(doctor.id)}` });
  },
  openDoctors() { wx.navigateTo({ url: '/pages/doctors/index' }); },
  openClinic() { if (this.data.clinic && !this.data.loading && !this.data.error) navigateClinic(this.data.clinic.id); },
  preview(e) { previewCredential(this.data.credentials, Number(e.currentTarget.dataset.index)); },
});
