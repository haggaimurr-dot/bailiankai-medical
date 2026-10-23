import { api, ApiError, errorMessage, type Slot } from '../../services/api';
import { confirmDialog, confirmNewSession, ensureSession, isSessionExpired, patientApi, type Appointment, type AppointmentCreate } from '../../services/patient';
import { clearSubmission, loadSubmission, saveSubmission, type Submission } from '../../services/submission';
import { clinicTime } from '../../utils/date';
import { dateTime } from '../../utils/appointment';
import { PATIENT_LOGIN_MODE } from '../../config/api';

interface BookingData {
  waitlist: boolean;
  loading: boolean;
  error: string;
  expired: boolean;
  creatingIdentity: boolean;
  patientName: string;
  accountPhone: string;
  officialIdentity: boolean;
  clinicName: string;
  doctorName: string;
  timeLabel: string;
  slot: Slot | null;
  slotAvailable: boolean;
  contactName: string;
  contactPhone: string;
  note: string;
  validationError: string;
  submitError: string;
  submitting: boolean;
  recovering: boolean;
  recoveryMode: boolean;
  recoveryMessage: string;
  canRetry: boolean;
  canAbandon: boolean;
  completedId: string;
}

interface BookingMethods {
  generation: number;
  patientId: string;
  selection: Submission['selection'];
  draft: Submission | null;
  disposed: boolean;
  visible: boolean;
  routeReady: boolean;
  initialize(): Promise<void>;
  onFieldChange(event: WechatMiniprogram.CustomEvent<{ value: string }>): void;
  submit(): Promise<void>;
  sendDraft(draft: Submission): Promise<void>;
  querySubmission(): Promise<void>;
  retrySubmission(): Promise<void>;
  abandonRecovery(): Promise<void>;
  complete(appointment: Appointment, requestKey: string): void;
  openCompleted(): void;
  newIdentity(): Promise<void>;
  openAppointments(): void;
  useAccountPhone(): void;
  backToSchedule(): void;
}

Page<BookingData, BookingMethods>({
  data: {
    waitlist: false, loading: true, error: '', expired: false, creatingIdentity: false, patientName: '', accountPhone: '', officialIdentity: false,
    clinicName: '', doctorName: '', timeLabel: '', slot: null, slotAvailable: false,
    contactName: PATIENT_LOGIN_MODE === 'demo' ? '演示访客' : '', contactPhone: PATIENT_LOGIN_MODE === 'demo' ? '13800000000' : '', note: '', validationError: '',
    submitError: '', submitting: false, recovering: false, recoveryMode: false,
    recoveryMessage: '', canRetry: false, canAbandon: false, completedId: '',
  },
  generation: 0,
  patientId: '',
  selection: { slotId: '', clinicId: '', doctorId: '', date: '' },
  draft: null,
  disposed: false,
  visible: true,
  routeReady: false,

  onLoad(options) {
    this.setData({ waitlist: options.waitlist === '1' });
    this.selection = { slotId: options.slotId || '', clinicId: options.clinicId || '', doctorId: options.doctorId || '', date: options.date || '' };
    void this.initialize();
  },
  onShow() { this.visible = true; },
  onRouteDone() {
    this.routeReady = true;
    if (!this.disposed && this.visible && this.data.completedId) this.openCompleted();
  },
  onHide() { this.visible = false; this.routeReady = false; },
  onUnload() { this.generation += 1; this.disposed = true; this.visible = false; this.routeReady = false; },

  async initialize() {
    if (this.disposed) return;
    const generation = ++this.generation;
    this.setData({ loading: true, error: '', expired: false, submitError: '', validationError: '' });
    try {
      const session = await ensureSession();
      const patient = await patientApi.me();
      if (generation !== this.generation) return;
      this.patientId = session.patient.id;
      this.draft = loadSubmission();
      if (this.draft && this.draft.patientId !== this.patientId) throw new Error('本机还有原患者账号的申请恢复信息，请先查看“我的预约”确认当前身份。');
      if (this.draft) { this.selection = this.draft.selection; this.setData({ waitlist: Boolean(this.draft.body?.waitlist) }); }
      const selection = this.selection;
      if (!selection.slotId || !selection.clinicId || !selection.date) throw new Error('请先选择医生和可用时段。');
      this.setData({
        patientName: patient.displayName, accountPhone: patient.phoneAuthorization === 'granted' ? patient.phone || '' : '', officialIdentity: patient.loginKind === 'wechat',
        recoveryMode: Boolean(this.draft), canRetry: Boolean(this.draft?.body), canAbandon: false,
        ...(this.draft?.body ? { contactName: this.draft.body.contactName, contactPhone: this.draft.body.contactPhone, note: this.draft.body.note || '' } : {}),
      });
      const [clinics, doctors, slots] = await Promise.all([api.clinics(), api.doctors(selection.clinicId), api.slots(selection)]);
      if (generation !== this.generation) return;
      const slot = slots.find((item) => item.id === selection.slotId) || null;
      this.setData({
        clinicName: clinics.find((clinic) => clinic.id === selection.clinicId)?.name || '原申请院区',
        doctorName: doctors.find((doctor) => doctor.id === selection.doctorId)?.name || '原申请医生',
        timeLabel: slot ? `${dateTime(slot.startsAt)} — ${clinicTime(slot.endsAt)}` : `${selection.date}（时段已结束或更新）`,
        slot,
        slotAvailable: Boolean(slot && slot.status === 'open' && (this.data.waitlist ? slot.capacity > 0 && slot.reserved >= slot.capacity : slot.capacity > slot.reserved) && Date.parse(slot.startsAt) > Date.now()),
        loading: false,
      });
      if (this.draft) await this.querySubmission();
    } catch (error) {
      if (generation === this.generation) this.setData({ error: errorMessage(error), expired: isSessionExpired(error), loading: false });
    }
  },

  onFieldChange(event) {
    if (this.data.submitting || this.data.recoveryMode || this.data.completedId) return;
    const field = String(event.currentTarget.dataset.field);
    const value = event.detail.value;
    if (field === 'contactName') this.setData({ contactName: value, validationError: '' });
    if (field === 'contactPhone') this.setData({ contactPhone: value, validationError: '' });
    if (field === 'note') this.setData({ note: value, validationError: '' });
  },

  async submit() {
    if (this.data.submitting || this.data.recovering || this.data.recoveryMode || this.data.completedId || !this.data.slotAvailable) return;
    const body: AppointmentCreate = { ...(this.data.waitlist ? { waitlist: true } : {}), slotId: this.selection.slotId, contactName: this.data.contactName.trim(), contactPhone: this.data.contactPhone.trim(), note: this.data.note.trim() };
    const length = Array.from(body.contactName).length;
    const validationError = length < 1 || length > 40 ? '请填写 1–40 字的联系人姓名。' : !/^1[3-9]\d{9}$/.test(body.contactPhone) ? '请填写 11 位大陆手机号码。' : Array.from(body.note || '').length > 300 ? '备注不能超过 300 字。' : '';
    if (validationError) { this.setData({ validationError }); return; }
    this.setData({ submitting: true, submitError: '', validationError: '' });
    try {
      const existing = loadSubmission();
      if (existing) {
        this.setData({ submitting: false });
        await this.initialize();
        return;
      }
      const session = await ensureSession();
      if (session.patient.id !== this.patientId) throw new Error('患者身份已变化，请重新加载页面后提交。');
      this.draft = await saveSubmission(this.patientId, body, this.selection);
      if (!this.disposed) this.setData({ recoveryMode: true, canRetry: true });
      await this.sendDraft(this.draft);
    } catch (error) {
      if (!this.disposed) this.setData({ submitError: errorMessage(error), expired: isSessionExpired(error) });
    } finally {
      if (!this.disposed) this.setData({ submitting: false });
    }
  },

  async sendDraft(draft) {
    if (!draft.body) return;
    try {
      const appointment = await patientApi.create(draft.body, draft.key, draft.patientId);
      this.complete(appointment, draft.key);
    } catch (error) {
      if (isSessionExpired(error)) {
        if (!this.disposed) this.setData({ expired: true, submitError: errorMessage(error) });
        return;
      }
      if (error instanceof ApiError && error.code === 'PATIENT_IDENTITY_CHANGED') {
        if (!this.disposed) this.setData({ error: errorMessage(error), canRetry: false });
        return;
      }
      const definiteFailure = error instanceof ApiError && [400, 404, 409].includes(error.status) && !['IDEMPOTENCY_CONFLICT', 'PATIENT_IDENTITY_CHANGED'].includes(error.code);
      if (definiteFailure) {
        clearSubmission(draft.key);
        this.draft = null;
        if (!this.disposed) this.setData({ recoveryMode: false, submitError: errorMessage(error), canRetry: false, slotAvailable: !error.code.startsWith('SLOT_') && this.data.slotAvailable });
      } else {
        if (!this.disposed) this.setData({ recoveryMode: true, canRetry: true, submitError: '', recoveryMessage: '暂时未能确认提交结果。原申请可能已被接收，请先查询结果；重试会使用同一申请编号和原资料。' });
      }
    }
  },

  async querySubmission() {
    if (this.data.submitting || this.data.recovering || !this.draft) return;
    const draft = this.draft;
    this.setData({ recovering: true, submitError: '', canAbandon: false });
    try {
      const appointment = await patientApi.byRequest(draft.key, draft.patientId);
      this.complete(appointment, draft.key);
    } catch (error) {
      if (this.disposed) return;
      if (error instanceof ApiError && error.status === 404) {
        this.setData({ recoveryMessage: draft.body ? '暂未查询到结果，原请求可能仍在处理中。可再次查询，或用原申请编号和原资料重试。' : '本机联系资料保存已超过 24 小时，已清除。尚未查到原申请，可继续查询；放弃恢复前请先核对“我的预约”。', canRetry: Boolean(draft.body), canAbandon: !draft.body });
      } else if (error instanceof ApiError && error.code === 'PATIENT_IDENTITY_CHANGED') {
        this.setData({ error: errorMessage(error), canRetry: false });
      } else {
        this.setData({ submitError: errorMessage(error), expired: isSessionExpired(error) });
      }
    } finally { if (!this.disposed) this.setData({ recovering: false }); }
  },

  async retrySubmission() {
    if (this.data.submitting || this.data.recovering || !this.draft?.body) return;
    this.setData({ submitting: true, submitError: '' });
    try { await this.sendDraft(this.draft); } finally { if (!this.disposed) this.setData({ submitting: false }); }
  },

  async abandonRecovery() {
    if (!this.data.canAbandon || this.data.recovering || this.data.submitting || !this.draft) return;
    const confirmed = await confirmDialog('放弃原申请恢复', '原请求仍可能完成，放弃恢复不会取消已经创建的预约。请先核对“我的预约”。确认后将清除本机的原申请编号。', '确认放弃');
    if (!confirmed) return;
    clearSubmission(this.draft.key);
    this.backToSchedule();
  },

  complete(appointment, requestKey) {
    clearSubmission(requestKey);
    this.draft = null;
    if (this.disposed) return;
    this.setData({ completedId: appointment.id, recoveryMode: false, submitError: '' });
    // 恢复查询可能早于首次路由动画结束，等当前页就绪后再进入详情。
    if (this.visible && this.routeReady) this.openCompleted();
  },
  openCompleted() {
    if (!this.disposed && this.visible && this.data.completedId) wx.redirectTo({ url: `/pages/appointment-detail/index?id=${encodeURIComponent(this.data.completedId)}` });
  },
  async newIdentity() {
    if (this.data.creatingIdentity || this.data.submitting || this.data.recovering) return;
    this.setData({ creatingIdentity: true });
    try { if (await confirmNewSession()) await this.initialize(); } catch (error) { if (!this.disposed) this.setData({ error: errorMessage(error) }); }
    finally { if (!this.disposed) this.setData({ creatingIdentity: false }); }
  },
  openAppointments() { wx.navigateTo({ url: '/pages/appointments/index' }); },
  useAccountPhone() {
    if (this.data.submitting || this.data.recoveryMode || this.data.completedId || !this.data.accountPhone) return;
    this.setData({ contactPhone: this.data.accountPhone, validationError: '' });
  },
  backToSchedule() { wx.redirectTo({ url: '/pages/schedule/index' }); },
});
