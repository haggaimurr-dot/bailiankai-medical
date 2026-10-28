import type { components } from '../../../../../packages/contracts/src/schema';
import { ApiError, errorMessage } from '../../services/api';
import { confirmChoice, confirmDialog, confirmNewSession, isSessionExpired, patientApi } from '../../services/patient';
import { appointmentView, dateTime, type AppointmentView } from '../../utils/appointment';
import { countdownLabel } from '../../utils/date';

interface DetailData {
  checkInImage: string; checkInExpiry: string; checkInCountdown: string; checkInLoading: boolean; checkInError: string;
  loading: boolean; error: string; expired: boolean; creatingIdentity: boolean;
  timeline: (components['schemas']['AppointmentTimelineEvent'] & { label: string; timeLabel: string })[]; timelineError: string;
  appointment: AppointmentView | null; cancelling: boolean; notice: string;
}
interface DetailMethods {
  showCheckInCode(): Promise<void>; checkInTimer: ReturnType<typeof setTimeout> | null; checkInInterval: ReturnType<typeof setInterval> | null;
  startCheckInCountdown(expiresAt: string): void; clearCheckInCountdown(): void;
  id: string; generation: number; timelineGeneration: number; initialized: boolean; disposed: boolean;
  load(): Promise<void>; refresh(): void; cancelAppointment(): Promise<void>;
  newIdentity(): Promise<void>; openAppointments(): void; openSchedule(): void;
  loadTimeline(): Promise<void>; openReschedule(): void; openRebook(): void;
  openPhotos(): void; openCarePlan(): void; openFollowups(): void;
}
Page<DetailData, DetailMethods>({
  data: { checkInImage: '', checkInExpiry: '', checkInCountdown: '', checkInLoading: false, checkInError: '', loading: true, error: '', expired: false, creatingIdentity: false, appointment: null, timeline: [], timelineError: '', cancelling: false, notice: '' },
  checkInTimer: null, checkInInterval: null, id: '', generation: 0, timelineGeneration: 0, initialized: false, disposed: false,
  onLoad(options) { this.id = options.id || ''; void this.load(); },
  onShow() { if (this.initialized && !this.data.loading && !this.data.cancelling) void this.load(); },
  onUnload() { this.timelineGeneration += 1; this.generation += 1; this.disposed = true; this.clearCheckInCountdown(); },
  clearCheckInCountdown() {
    if (this.checkInTimer) { clearTimeout(this.checkInTimer); this.checkInTimer = null; }
    if (this.checkInInterval) { clearInterval(this.checkInInterval); this.checkInInterval = null; }
    this.setData({ checkInCountdown: '' });
  },
  startCheckInCountdown(expiresAt: string) {
    this.clearCheckInCountdown();
    const update = () => {
      const left = Math.ceil((Date.parse(expiresAt) - Date.now()) / 1000);
      this.setData({ checkInCountdown: countdownLabel(left) });
      if (left <= 0 && this.checkInInterval) { clearInterval(this.checkInInterval); this.checkInInterval = null; }
    };
    update();
    this.checkInInterval = setInterval(update, 1000);
  },
  onPullDownRefresh() { if (this.data.cancelling) wx.stopPullDownRefresh(); else void this.load(); },
  async load() {
    if (this.disposed) return;
    this.timelineGeneration++;
    const generation = ++this.generation;
    this.clearCheckInCountdown();
    this.setData({ checkInImage: '', checkInExpiry: '', checkInError: '', loading: true, error: '', expired: false });
    try {
      if (!this.id) throw new Error('缺少预约编号，请从“我的预约”重新进入。');
      const appointment = await patientApi.appointment(this.id);
      if (generation !== this.generation) return;
      this.setData({ appointment: appointmentView(appointment) });
      await this.loadTimeline();
    } catch (error) {
      if (generation === this.generation) this.setData({ error: errorMessage(error), expired: isSessionExpired(error) });
    } finally {
      if (generation === this.generation) {
        this.initialized = true;
        this.setData({ loading: false });
        wx.stopPullDownRefresh();
      }
    }
  },
  async showCheckInCode() {
    if (this.data.loading || this.data.checkInLoading || this.data.cancelling) return;
    const generation = this.generation;
    const timelineGeneration = ++this.timelineGeneration;
    this.setData({ checkInLoading: true, checkInError: '', checkInImage: '' });
    try {
      const code = await patientApi.checkInCode(this.id);
      // WeChat image requires a file path; write the private PNG to its local sandbox.
      const filePath = `${wx.env.USER_DATA_PATH}/appointment-checkin-${Date.now()}.png`;
      await new Promise<void>((resolve, reject) => wx.getFileSystemManager().writeFile({filePath, data:code.imageDataUrl.split(',')[1]!, encoding:'base64', success:() => resolve(), fail:reject}));
      if (!this.disposed && generation === this.generation) {
        this.setData({checkInImage:filePath,checkInExpiry:dateTime(code.expiresAt)});
        this.startCheckInCountdown(code.expiresAt);
        this.checkInTimer = setTimeout(() => {
          wx.getFileSystemManager().unlink({filePath, fail:() => {}});
          if (!this.disposed && this.data.checkInImage === filePath) this.setData({checkInImage:'',checkInError:'签到码已到期，请重新领取。'});
        }, Math.max(0, Date.parse(code.expiresAt) - Date.now()));
      } else wx.getFileSystemManager().unlink({filePath, fail:() => {}});
    } catch (cause) { if (!this.disposed && generation === this.generation) this.setData({checkInError:errorMessage(cause)}); }
    finally { if (!this.disposed) this.setData({checkInLoading:false}); }
  },
  refresh() { if (!this.data.cancelling) void this.load(); },
  async cancelAppointment() {
    const appointment = this.data.appointment;
    if (!appointment?.cancellable || this.data.loading || this.data.cancelling) return;
    this.setData({ cancelling: true, notice: '' });
    try {
      if (appointment.canReschedule) {
        const choice = await confirmChoice('确认取消预约', '取消后终止本次预约或候补，已占用的名额会释放。只是想换个时间？点「改期」直接改期，不用取消重约。', '取消预约', '改期');
        if (choice === 'cancel') { this.setData({ cancelling: false }); this.openReschedule(); return; }
        if (choice !== 'confirm') return;
      } else if (!(await confirmDialog('确认取消预约', '取消后终止本次预约或候补，已占用的名额会释放。如需再次到院，请重新提交申请。', '取消预约'))) return;
      const cancelled = await patientApi.cancel(appointment.id, appointment.version);
      if (this.disposed) return;
      this.setData({ appointment: appointmentView(cancelled), notice: '预约或候补已取消。' });
      await this.loadTimeline();
    } catch (error) {
      if (this.disposed) return;
      if (isSessionExpired(error)) this.setData({ error: errorMessage(error), expired: true });
      else {
        await this.load();
        if (this.disposed) return;
        const cancelled = this.data.appointment?.status === 'cancelled';
        this.setData({ notice: cancelled ? '预约或候补已取消。' : error instanceof ApiError && error.status === 409 ? '预约状态已发生变化，已刷新最新结果。请核对后再操作。' : '暂时未能确认取消结果，请刷新后查看；如仍可取消，可再次操作。' });
      }
    } finally { if (!this.disposed) this.setData({ cancelling: false }); }
  },
  async loadTimeline() {
    const generation = this.generation;
    const timelineGeneration = ++this.timelineGeneration;
    this.setData({ timelineError: '' });
    try {
      const events = await patientApi.appointmentTimeline(this.id);
      if (generation !== this.generation || timelineGeneration !== this.timelineGeneration || this.disposed) return;
      const labels: Record<string, string> = { 'suspend-move': '停诊改期', submit: '提交预约', waitlist: '登记候补', claim: '候补转为待确认', reschedule: '改期申请', confirm: '工作人员确认', reject: '工作人员拒绝', cancel: '患者取消', expire: '申请过期', arrival: '登记到诊', arrive: '登记到诊', 'suspension-reschedule': '工作人员协调改期', treatment: '记录治疗', complete: '记录治疗' };
      this.setData({ timeline: events.map(event => ({ ...event, label: labels[event.action] || '预约状态更新', timeLabel: dateTime(event.createdAt) })) });
    } catch (error) {
      if (generation !== this.generation || timelineGeneration !== this.timelineGeneration || this.disposed) return;
      this.setData({ timelineError: errorMessage(error), timeline: [] });
    }
  },
  openReschedule() {
    if (!this.data.loading && !this.data.cancelling && this.data.appointment?.canReschedule) wx.navigateTo({ url: `/pages/reschedule/index?id=${encodeURIComponent(this.id)}` });
  },
  openRebook() {
    const appointment = this.data.appointment;
    if (!appointment) return;
    wx.navigateTo({ url: `/pages/schedule/index?clinicId=${encodeURIComponent(appointment.clinicId)}&doctorId=${encodeURIComponent(appointment.doctorId)}` });
  },
  async newIdentity() {
    if (this.data.creatingIdentity) return;
    this.setData({ creatingIdentity: true });
    try { if (await confirmNewSession() && !this.disposed) wx.redirectTo({ url: '/pages/appointments/index' }); }
    catch (error) { if (!this.disposed) this.setData({ error: errorMessage(error) }); }
    finally { if (!this.disposed) this.setData({ creatingIdentity: false }); }
  },
  openAppointments() { wx.redirectTo({ url: '/pages/appointments/index' }); },
  openSchedule() { wx.navigateTo({ url: '/pages/schedule/index' }); },
  openFollowups() { if (this.id) wx.navigateTo({ url: `/pages/followups/index?id=${encodeURIComponent(this.id)}` }); },
  openPhotos() { if (this.id) wx.navigateTo({ url: `/pages/care-photos/index?id=${encodeURIComponent(this.id)}` }); },
  openCarePlan() { if (this.id) wx.navigateTo({ url: `/pages/care-plan/index?id=${encodeURIComponent(this.id)}` }); },
});
