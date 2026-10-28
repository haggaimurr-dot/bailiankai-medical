import { api, ApiError, errorMessage, type Slot } from '../../services/api';
import { confirmDialog, ensureSession, isSessionExpired, patientApi, requirePatientIdentity } from '../../services/patient';
import { appointmentView, dateTime, type AppointmentView } from '../../utils/appointment';
import { clinicTime, upcomingDays, type DayOption } from '../../utils/date';

interface Draft { patientId: string; key: string; body: { slotId: string; version: number } }
interface Data {
  loading: boolean; loadingSlots: boolean; submitting: boolean; error: string; slotError: string; notice: string;
  appointment: AppointmentView | null; days: DayOption[]; date: string;
  slots: (Slot & { label: string; available: boolean })[]; recommendations: (Slot & { label: string; available: boolean })[]; selected: string; recovering: boolean;
}
interface Methods {
  id: string; patientId: string; generation: number; slotGeneration: number; disposed: boolean; draft: Draft | null;
  load(): Promise<void>; loadSlots(): Promise<void>; loadRecommendations(after: string): Promise<void>; chooseRecommendation(e: WechatMiniprogram.TouchEvent): Promise<void>; changeDate(e: WechatMiniprogram.TouchEvent): void;
  selectSlot(e: WechatMiniprogram.TouchEvent): void; submit(): Promise<void>; openDetail(): void;
}
Page<Data, Methods>({
  data: { loading: true, loadingSlots: false, submitting: false, error: '', slotError: '', notice: '', appointment: null, days: [], date: '', slots: [], recommendations: [], selected: '', recovering: false },
  id: '', patientId: '', generation: 0, slotGeneration: 0, disposed: false, draft: null,
  onLoad(options) { this.id = options.id || ''; void this.load(); },
  onUnload() { this.disposed = true; this.generation++; this.slotGeneration++; },
  onPullDownRefresh() { if (!this.data.submitting) void this.load(); else wx.stopPullDownRefresh(); },
  async load() {
    const generation = ++this.generation;
    this.slotGeneration++;
    this.setData({ loading: true, error: '', slotError: '', slots: [] });
    try {
      if (!this.id) throw new Error('请从预约详情进入。');
      const session = await ensureSession();
      const appointment = await patientApi.appointment(this.id);
      if (generation !== this.generation) return;
      this.patientId = session.patient.id;
      const saved = wx.getStorageSync(`booking-change:${this.id}`) as Draft | undefined;
      if (saved && saved.patientId !== this.patientId) throw new Error('当前患者身份已变化，请使用原账号查询改期结果。');
      this.draft = saved || null;
      const days = upcomingDays();
      const preferred = this.data.date || dateTime(appointment.startsAt).slice(0, 10);
      const date = days.some(day => day.date === preferred) ? preferred : days[0].date;
      this.setData({ appointment: appointmentView(appointment), days, date, recovering: Boolean(this.draft), selected: this.draft?.body.slotId || '' });
      await this.loadSlots();
      await this.loadRecommendations(appointment.startsAt);
    } catch (error) {
      if (generation === this.generation) this.setData({ error: errorMessage(error) });
    } finally { if (generation === this.generation) { this.setData({ loading: false }); wx.stopPullDownRefresh(); } }
  },
  async loadSlots() {
    const appointment = this.data.appointment;
    if (!appointment) return;
    const generation = ++this.slotGeneration;
    this.setData({ loadingSlots: true, slotError: '', slots: [] });
    try {
      const slots = await api.slots({ clinicId: appointment.clinicId, doctorId: appointment.doctorId, date: this.data.date });
      if (generation !== this.slotGeneration) return;
      this.setData({ slots: slots.filter(slot => slot.id !== appointment.slotId || appointment.status === 'waiting').map(slot => ({ ...slot, label: `${dateTime(slot.startsAt)} — ${clinicTime(slot.endsAt)}`, available: slot.status === 'open' && (slot.remaining ?? slot.capacity - slot.reserved) > 0 && Date.parse(slot.startsAt) > Date.now() })) });
    } catch (error) { if (generation === this.slotGeneration) this.setData({ slotError: errorMessage(error) }); }
    finally { if (generation === this.slotGeneration) this.setData({ loadingSlots: false }); }
  },
  async loadRecommendations(after: string) {
    const appointment = this.data.appointment;
    if (!appointment) return;
    try {
      const rows = await api.slotRecommendations({ clinicId: appointment.clinicId, doctorId: appointment.doctorId, after, limit: 3 });
      this.setData({ recommendations: rows.filter(slot => slot.id !== appointment.slotId).map(slot => ({ ...slot, label: `${dateTime(slot.startsAt)} — ${clinicTime(slot.endsAt)}`, available: slot.status === 'open' && (slot.remaining ?? slot.capacity - slot.reserved) > 0 && Date.parse(slot.startsAt) > Date.now() })) });
    } catch { this.setData({ recommendations: [] }); }
  },
  async chooseRecommendation(event) {
    if (this.data.submitting || this.data.loadingSlots || this.draft) return;
    const id = String(event.currentTarget.dataset.id || '');
    const slot = this.data.recommendations.find(item => item.id === id);
    if (!slot || !slot.available) return;
    this.setData({ date: slot.startsAt.slice(0, 10), selected: '' });
    await this.loadSlots();
    if (this.data.slots.some(item => item.id === id && item.available)) this.setData({ selected: id, notice: '' });
  },
  changeDate(event) {
    if (this.data.submitting || this.draft) return;
    const date = String(event.currentTarget.dataset.date || '');
    if (!this.data.days.some(day => day.date === date)) return;
    this.setData({ date, selected: '' }); void this.loadSlots();
  },
  selectSlot(event) {
    if (this.data.submitting || this.data.loadingSlots || this.draft) return;
    const id = String(event.currentTarget.dataset.id || '');
    if (this.data.slots.some(slot => slot.id === id && slot.available)) this.setData({ selected: id, notice: '' });
  },
  async submit() {
    const appointment = this.data.appointment;
    if (!appointment || this.data.loading || this.data.submitting) return;
    if (!this.draft && (!appointment.canReschedule || !this.data.slots.some(slot => slot.id === this.data.selected && slot.available))) return;
    this.setData({ submitting: true, notice: '' });
    try {
      if (!this.draft) {
        const selected = this.data.slots.find(slot => slot.id === this.data.selected)!;
        if (!await confirmDialog(appointment.status === 'waiting' ? '申请预约名额' : '确认改期', `${selected.label}。成功后需工作人员重新确认；失败时保留原申请。`, '确认申请')) return;
        if (this.disposed) return;
        const key = await new Promise<string>((resolve, reject) => wx.getRandomValues({ length: 16, success: ({ randomValues }) => resolve(Array.from(new Uint8Array(randomValues), b => b.toString(16).padStart(2, '0')).join('')), fail: () => reject(new Error('无法生成申请编号，请重试。')) }));
        if (this.disposed) return;
        requirePatientIdentity(this.patientId);
        const saved = wx.getStorageSync(`booking-change:${this.id}`) as Draft | undefined;
        if (saved) throw new Error('已有改期申请待确认，请刷新后重试。');
        const draft = { patientId: this.patientId, key, body: { slotId: this.data.selected, version: appointment.version } };
        wx.setStorageSync(`booking-change:${this.id}`, draft);
        this.draft = draft;
        this.setData({ recovering: true });
      }
      const draft = this.draft!;
      await patientApi.reschedule(this.id, draft.body, draft.key, draft.patientId);
      wx.removeStorageSync(`booking-change:${this.id}`); this.draft = null;
      if (!this.disposed) this.openDetail();
    } catch (error) {
      if (this.disposed) return;
      const definite = error instanceof ApiError && [400, 404, 409].includes(error.status) && error.code !== 'IDEMPOTENCY_CONFLICT';
      if (definite) { wx.removeStorageSync(`booking-change:${this.id}`); this.draft = null; await this.load(); }
      if (this.disposed) return;
      this.setData({ notice: isSessionExpired(error) ? '登录已失效，请重新登录原账号后再确认结果。' : this.draft ? '暂时未能确认结果。请使用原申请重试，不能另选时段重复提交。' : errorMessage(error), recovering: Boolean(this.draft) });
    } finally { if (!this.disposed) this.setData({ submitting: false }); }
  },
  openDetail() { wx.redirectTo({ url: `/pages/appointment-detail/index?id=${encodeURIComponent(this.id)}` }); },
});
