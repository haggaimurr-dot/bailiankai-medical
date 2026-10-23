import { errorMessage } from '../../services/api';
import { confirmDialog, confirmNewSession, isSessionExpired, patientApi, type TimelineEvent } from '../../services/patient';

interface RecordView extends TimelineEvent { dateLabel: string; kindLabel: string; summaryLabel: string; readLabel: string; nextVisitLabel: string; stageLabel: string; }
interface RecordsData {
  loading: boolean; loadingMore: boolean; creatingIdentity: boolean; expired: boolean;
  error: string; patientName: string; records: RecordView[]; page: number; total: number; hasMore: boolean; confirmingId: string;
}
interface RecordsMethods {
  generation: number; disposed: boolean; patientId: string;
  load(append?: boolean): Promise<void>; loadMore(): void; retry(): void; newIdentity(): Promise<void>;
  confirmRead(event: WechatMiniprogram.TouchEvent): Promise<void>;
  openDetail(event: WechatMiniprogram.TouchEvent): void; openCalendar(): void;
}
const kinds: Record<TimelineEvent['kind'], string> = { initial: '问诊', revisit: '复诊', plan: '方案发布', treatment: '实际治疗', followup: '随访任务', submitted: '随访反馈', reviewed: '医护回评', appointment: '预约' };
const statuses: Record<string, string> = { waiting: '候补中', pending: '待处理', confirmed: '已确认', completed: '已治疗', rejected: '未确认', cancelled: '已取消', expired: '已过期', submitted: '待回评', alert: '待医护关注', reviewed: '已回评', handled: '已处理' };
const stages: Record<string, string> = { not_started: '未开始', in_progress: '进行中', completed: '已完成' };
function dateLabel(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value));
}
function render(item: TimelineEvent): RecordView {
  return { ...item, dateLabel: dateLabel(item.occurredAt), kindLabel: kinds[item.kind],
    summaryLabel: item.kind === 'appointment' && item.summary === 'pending' ? '待确认' : item.kind === 'followup' && item.summary === 'pending' ? '待填写' : item.kind === 'appointment' || item.kind === 'followup' ? statuses[item.summary] || item.summary : item.summary,
    readLabel: item.readAt ? dateLabel(item.readAt) : '', nextVisitLabel: item.nextVisitAt ? dateLabel(item.nextVisitAt) : '',
    stageLabel: item.stageIndex ? `第 ${item.stageIndex} 阶段${item.stageStatus ? ' · ' + (stages[item.stageStatus] || item.stageStatus) : ''}` : '' };
}
Page<RecordsData, RecordsMethods>({
  data: { loading: true, loadingMore: false, creatingIdentity: false, expired: false, error: '', patientName: '', records: [], page: 0, total: 0, hasMore: false, confirmingId: '' },
  generation: 0, disposed: false, patientId: '',
  onLoad() { this.disposed = false; void this.load(); },
  onShow() { if (!this.data.loading && !this.disposed) void this.load(); },
  onUnload() { this.generation += 1; this.disposed = true; },
  onPullDownRefresh() { void this.load(); },
  onReachBottom() { this.loadMore(); },
  async load(append = false) {
    if (this.disposed || (append && (this.data.loading || this.data.loadingMore || !this.data.hasMore))) return;
    const generation = ++this.generation;
    const page = append ? this.data.page + 1 : 1;
    this.setData({ loading: !append, loadingMore: append, error: '', expired: false, ...(append ? {} : { records: [], patientName: '' }) });
    try {
      const identity = await patientApi.me();
      if (append && identity.id !== this.patientId) {
        if (generation === this.generation) this.setData({ records: [], patientName: '', hasMore: false });
        throw new Error('患者身份已变化，请刷新档案。');
      }
      const result = await patientApi.timeline(page, identity.id);
      if (generation !== this.generation) return;
      this.patientId = identity.id;
      const records = [...(append ? this.data.records : []), ...result.data!.map(render)];
      this.setData({ patientName: identity.displayName, records, page: result.meta!.page, total: result.meta!.total, hasMore: page * result.meta!.pageSize < result.meta!.total });
    } catch (error) {
      if (generation === this.generation) this.setData({ error: errorMessage(error), expired: isSessionExpired(error), ...(isSessionExpired(error) ? { records: [], patientName: '' } : {}) });
    } finally {
      if (generation === this.generation) { this.setData({ loading: false, loadingMore: false }); wx.stopPullDownRefresh(); }
    }
  },
  loadMore() { void this.load(true); }, retry() { void this.load(); },
  async newIdentity() {
    if (this.data.creatingIdentity) return;
    this.setData({ creatingIdentity: true });
    try { if (await confirmNewSession()) void this.load(); }
    catch (error) { this.setData({ error: errorMessage(error) }); }
    finally { if (!this.disposed) this.setData({ creatingIdentity: false }); }
  },
  async confirmRead(event) {
    if (this.data.confirmingId || this.data.loading) return;
    const item = this.data.records.find(row => row.id === String(event.currentTarget.dataset.id || ''));
    if (!item || !item.adviceVersion || item.readAt) return;
    const generation = this.generation, patientId = this.patientId;
    this.setData({ confirmingId: item.id, error: '' });
    try {
      if (!await confirmDialog('确认已阅读医嘱', item.advice, '已阅读')) return;
      if (this.disposed || generation !== this.generation) return;
      const receipt = await patientApi.confirmAdviceRead(item.kind, item.resourceId, item.adviceVersion, patientId);
      if (this.disposed || generation !== this.generation) return;
      this.setData({ records: this.data.records.map(row => row.id === item.id ? render({ ...row, readAt: receipt.readAt }) : row) });
    } catch (error) {
      if (!this.disposed && generation === this.generation) this.setData({ error: errorMessage(error), expired: isSessionExpired(error), ...(isSessionExpired(error) ? { records: [], patientName: '' } : {}) });
    } finally { if (!this.disposed) this.setData({ confirmingId: '' }); }
  },
  openDetail(event) {
    const item = this.data.records.find(row => row.id === String(event.currentTarget.dataset.id || ''));
    if (!item?.appointmentId) return;
    const id = encodeURIComponent(item.appointmentId);
    const url = item.kind === 'plan' ? `/pages/care-plan/index?id=${id}&versionId=${encodeURIComponent(item.resourceId)}`
      : ['followup', 'submitted', 'reviewed'].includes(item.kind) ? `/pages/followups/index?id=${id}&taskId=${encodeURIComponent(item.resourceId)}` : `/pages/appointment-detail/index?id=${id}`;
    wx.navigateTo({ url });
  },
  openCalendar() { wx.navigateTo({ url: '/pages/recovery-calendar/index' }); },
});
