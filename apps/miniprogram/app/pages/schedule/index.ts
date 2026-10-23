import { api, errorMessage, type Clinic, type Doctor, type Slot } from '../../services/api';
import { clinicTime, upcomingDays, type DayOption } from '../../utils/date';

interface SlotView extends Slot {
  timeRange: string;
  doctorName: string;
  remaining: number;
  stateLabel: string;
  available: boolean;
  waitlistAvailable: boolean;
}

interface ScheduleData {
  initializing: boolean;
  loading: boolean;
  initialError: string;
  error: string;
  clinics: Clinic[];
  clinic: Clinic | null;
  clinicIndex: number;
  doctors: Doctor[];
  doctorId: string;
  days: DayOption[];
  date: string;
  slots: SlotView[];
  availableCount: number;
}

interface ScheduleMethods {
  allDoctors: Doctor[];
  requestId: number;
  initializationId: number;
  initialSelection: { clinicId?: string; doctorId?: string };
  initialize(options?: { clinicId?: string; doctorId?: string }): Promise<void>;
  loadSlots(): Promise<void>;
  retry(): void;
  onClinicChange(event: WechatMiniprogram.CustomEvent<{ value: string }>): void;
  onDoctorChange(event: WechatMiniprogram.TouchEvent): void;
  onDateChange(event: WechatMiniprogram.TouchEvent): void;
  bookSlot(event: WechatMiniprogram.TouchEvent): void;
  openAppointments(): void;
}

Page<ScheduleData, ScheduleMethods>({
  data: {
    initializing: true,
    loading: true,
    initialError: '',
    error: '',
    clinics: [],
    clinic: null,
    clinicIndex: 0,
    doctors: [],
    doctorId: '',
    days: [],
    date: '',
    slots: [],
    availableCount: 0,
  },
  allDoctors: [],
  requestId: 0,
  initializationId: 0,
  initialSelection: {},

  onLoad(options) {
    void this.initialize(options);
  },
  onShow() { if (!this.data.initializing && this.data.clinic) void this.loadSlots(); },

  onUnload() {
    this.initializationId += 1;
    this.requestId += 1;
  },

  onPullDownRefresh() {
    void this.initialize(this.data.initialError ? this.initialSelection : { clinicId: this.data.clinic?.id, doctorId: this.data.doctorId });
  },

  async initialize(options = {}) {
    this.initialSelection = options;
    const initializationId = ++this.initializationId;
    this.requestId += 1;
    this.setData({ initializing: true, loading: true, initialError: '', error: '', slots: [], clinic: null, doctors: [], doctorId: '', availableCount: 0 });
    try {
      const [clinics, allDoctors] = await Promise.all([api.clinics(), api.doctors()]);
      if (initializationId !== this.initializationId) return;
      this.allDoctors = allDoctors;
      const clinicIndex = Math.max(0, clinics.findIndex((clinic) => clinic.id === options.clinicId));
      const clinic = clinics[clinicIndex] || null;
      const doctors = allDoctors.filter((doctor) => doctor.clinicId === clinic?.id);
      if (options.clinicId && clinic?.id !== options.clinicId) throw new Error('所选院区资料暂不可用，请返回重新选择。');
      if (options.doctorId && !doctors.some((doctor) => doctor.id === options.doctorId)) throw new Error('所选医生资料暂不可用，请返回医生列表重新选择。');
      const doctorId = doctors.some((doctor) => doctor.id === options.doctorId) ? options.doctorId! : '';
      const days = upcomingDays();
      const date = days.some((day) => day.date === this.data.date) ? this.data.date : days[0].date;
      this.setData({ clinics, clinic, clinicIndex, doctors, doctorId, days, date, initializing: false });
      await this.loadSlots();
    } catch (error) {
      if (initializationId === this.initializationId) {
        this.setData({ initialError: errorMessage(error), initializing: false, loading: false });
      }
    } finally {
      if (initializationId === this.initializationId) wx.stopPullDownRefresh();
    }
  },

  async loadSlots() {
    const requestId = ++this.requestId;
    const { clinic, doctorId, date } = this.data;
    this.setData({ loading: true, error: '', slots: [], availableCount: 0 });
    if (!clinic) {
      this.setData({ loading: false });
      return;
    }
    try {
      const rawSlots = await api.slots({ clinicId: clinic.id, doctorId, date });
      if (requestId !== this.requestId) return;
      const slots = rawSlots.map((slot): SlotView => {
        const doctor = this.allDoctors.find((item) => item.id === slot.doctorId);
        const remaining = Math.max(0, slot.capacity - slot.reserved);
        const started = new Date(slot.startsAt).getTime() <= Date.now();
        const available = slot.status === 'open' && remaining > 0 && !started;
        return {
          ...slot,
          timeRange: `${clinicTime(slot.startsAt)} — ${clinicTime(slot.endsAt)}`,
          doctorName: doctor?.name || '医生资料更新中',
          remaining,
          stateLabel: slot.status === 'closed' ? '已停诊' : started ? '已开始' : remaining === 0 ? '已满' : '可用',
          available,
          waitlistAvailable: slot.status === 'open' && slot.capacity > 0 && remaining === 0 && !started,
        };
      });
      slots.sort((a, b) => a.startsAt.localeCompare(b.startsAt) || a.doctorName.localeCompare(b.doctorName));
      this.setData({ slots, availableCount: slots.filter((slot) => slot.available).length });
    } catch (error) {
      if (requestId === this.requestId) this.setData({ error: errorMessage(error) });
    } finally {
      if (requestId === this.requestId) this.setData({ loading: false });
    }
  },

  retry() {
    if (this.data.initialError) {
      void this.initialize(this.initialSelection);
    } else {
      void this.loadSlots();
    }
  },

  onClinicChange(event) {
    const clinicIndex = Number(event.detail.value);
    const clinic = this.data.clinics[clinicIndex];
    if (!clinic || clinic.id === this.data.clinic?.id) return;
    this.setData({ clinicIndex, clinic, doctorId: '', doctors: this.allDoctors.filter((doctor) => doctor.clinicId === clinic.id) });
    void this.loadSlots();
  },

  onDoctorChange(event) {
    const doctorId = String(event.currentTarget.dataset.id || '');
    if (doctorId === this.data.doctorId) return;
    this.setData({ doctorId });
    void this.loadSlots();
  },

  onDateChange(event) {
    const date = String(event.currentTarget.dataset.date || '');
    if (!date || date === this.data.date) return;
    this.setData({ date });
    void this.loadSlots();
  },
  bookSlot(event) {
    const slot = this.data.slots.find((item) => item.id === event.currentTarget.dataset.id);
    if (!slot || (!slot.available && !slot.waitlistAvailable) || this.data.loading) return;
    const query = `slotId=${encodeURIComponent(slot.id)}&clinicId=${encodeURIComponent(slot.clinicId)}&doctorId=${encodeURIComponent(slot.doctorId)}&date=${this.data.date}${slot.waitlistAvailable ? '&waitlist=1' : ''}`;
    wx.navigateTo({ url: `/pages/booking/index?${query}` });
  },
  openAppointments() { wx.navigateTo({ url: '/pages/appointments/index' }); },
});
