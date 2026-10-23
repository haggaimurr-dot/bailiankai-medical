import { api, errorMessage, type Clinic, type Doctor } from '../../services/api';
import { clinicPresentation, doctorPresentation, navigateClinic } from '../../services/presentation';

interface DoctorsData {
  loading: boolean;
  error: string;
  clinics: Clinic[];
  doctors: Doctor[];
  clinic: Clinic | null;
  clinicIndex: number;
  isDemo: boolean;
}

interface DoctorsMethods {
  requestId: number;
  loadDoctors(clinicId?: string): Promise<void>;
  retry(): void;
  onClinicChange(event: WechatMiniprogram.CustomEvent<{ value: string }>): void;
  openSchedule(event: WechatMiniprogram.TouchEvent): void;
  openDoctorDetail(event: WechatMiniprogram.TouchEvent): void;
  openAppointments(): void;
  openClinic(): void;
}

Page<DoctorsData, DoctorsMethods>({
  data: {
    loading: true,
    error: '',
    clinics: [],
    doctors: [],
    clinic: null,
    clinicIndex: 0,
    isDemo: true,
  },
  requestId: 0,

  onShow() {
    void this.loadDoctors(this.data.clinic?.id);
  },

  onUnload() {
    this.requestId += 1;
  },

  onPullDownRefresh() {
    void this.loadDoctors(this.data.clinic?.id);
  },

  async loadDoctors(clinicId) {
    const requestId = ++this.requestId;
    this.setData({ loading: true, error: '', doctors: [], clinic: null, clinics: [] });
    try {
      const clinics = await api.clinics();
      if (requestId !== this.requestId) return;
      const clinicIndex = Math.max(0, clinics.findIndex((clinic) => clinic.id === clinicId));
      const clinic = clinics[clinicIndex] || null;
      this.setData({ clinics, clinic, clinicIndex, doctors: [], isDemo: clinic ? clinicPresentation(clinic).isDemo : true });
      const doctors = clinic ? await api.doctors(clinic.id) : [];
      if (requestId !== this.requestId) return;
      this.setData({ doctors: doctors.map(row => ({ ...row, presentation: doctorPresentation(row) })) });
    } catch (error) {
      if (requestId === this.requestId) this.setData({ error: errorMessage(error) });
    } finally {
      if (requestId === this.requestId) {
        this.setData({ loading: false });
        wx.stopPullDownRefresh();
      }
    }
  },

  retry() {
    void this.loadDoctors(this.data.clinic?.id);
  },

  onClinicChange(event) {
    const clinic = this.data.clinics[Number(event.detail.value)];
    if (clinic) void this.loadDoctors(clinic.id);
  },

  openDoctorDetail(event) {
    const doctor = this.data.doctors.find((item) => item.id === event.currentTarget.dataset.doctorId);
    if (!doctor || this.data.loading) return;
    wx.navigateTo({ url: `/pages/doctor-detail/index?clinicId=${encodeURIComponent(doctor.clinicId)}&doctorId=${encodeURIComponent(doctor.id)}` });
  },
  openSchedule(event) {
    const clinicId = this.data.clinic?.id;
    if (!clinicId) return;
    const doctorId = String(event.currentTarget.dataset.doctorId || '');
    wx.navigateTo({
      url: `/pages/schedule/index?clinicId=${encodeURIComponent(clinicId)}&doctorId=${encodeURIComponent(doctorId)}`,
    });
  },
  openAppointments() { wx.navigateTo({ url: '/pages/appointments/index' }); },
  openClinic() { if (this.data.clinic && !this.data.loading) navigateClinic(this.data.clinic.id); },
});
