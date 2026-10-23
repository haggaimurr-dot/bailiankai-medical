import { errorMessage } from '../../services/api';
import { confirmNewSession, isSessionExpired, patientApi, type AppointmentStatus } from '../../services/patient';
import { loadSubmission } from '../../services/submission';
import { appointmentView, type AppointmentView } from '../../utils/appointment';

interface ListData {
  loading: boolean; loadingMore: boolean; error: string; expired: boolean;
  creatingIdentity: boolean; patientName: string; records: AppointmentView[];
  status: string; filters: { key: string; label: string }[]; page: number; total: number;
  hasMore: boolean; hasSubmission: boolean;
}
interface ListMethods {
  generation: number; initialized: boolean;
  load(append?: boolean): Promise<void>;
  retry(): void;
  loadMore(): void;
  onStatusChange(event: WechatMiniprogram.TouchEvent): void;
  openDetail(event: WechatMiniprogram.TouchEvent): void;
  openRecovery(): void;
  openFollowups(): void;
  openRecords(): void;
  openSchedule(): void;
  newIdentity(): Promise<void>;
}

Page<ListData, ListMethods>({
  data: {
    loading: true, loadingMore: false, error: '', expired: false, creatingIdentity: false,
    patientName: '', records: [], status: '', page: 0, total: 0, hasMore: false, hasSubmission: false,
    filters: [{ key: '', label: '全部' }, { key: 'waiting', label: '候补中' }, { key: 'pending', label: '待确认' }, { key: 'confirmed', label: '已确认' }, { key: 'completed', label: '已治疗' }, { key: 'rejected', label: '已拒绝' }, { key: 'cancelled', label: '已取消' }, { key: 'expired', label: '已过期' }],
  },
  generation: 0,
  initialized: false,
  onLoad() { void this.load(); },
  onShow() { if (this.initialized && !this.data.loading) void this.load(); },
  onUnload() { this.generation += 1; },
  onPullDownRefresh() { void this.load(); },
  onReachBottom() { this.loadMore(); },

  async load(append = false) {
    if (append && (this.data.loadingMore || this.data.loading || !this.data.hasMore)) return;
    const generation = ++this.generation;
    const page = append ? this.data.page + 1 : 1;
    const status = this.data.status as AppointmentStatus | '';
    this.setData({ loading: !append, loadingMore: append, error: '', expired: false, ...(append ? {} : { records: [] }) });
    try {
      const identity = await patientApi.me();
      const result = await patientApi.list(status, page);
      if (generation !== this.generation) return;
      const records = [...(append ? this.data.records : []), ...result.data.map(appointmentView)];
      this.setData({ patientName: identity.displayName, records, page: result.meta.page, total: result.meta.total, hasMore: records.length < result.meta.total, hasSubmission: Boolean(loadSubmission()) });
    } catch (error) {
      if (generation === this.generation) this.setData({ error: errorMessage(error), expired: isSessionExpired(error) });
    } finally {
      if (generation === this.generation) {
        this.initialized = true;
        this.setData({ loading: false, loadingMore: false });
        wx.stopPullDownRefresh();
      }
    }
  },
  retry() { void this.load(); },
  loadMore() { void this.load(true); },
  onStatusChange(event) {
    const status = String(event.currentTarget.dataset.status || '');
    if (status === this.data.status || !this.data.filters.some((item) => item.key === status)) return;
    this.setData({ status });
    void this.load();
  },
  openDetail(event) {
    const id = String(event.currentTarget.dataset.id || '');
    if (id) wx.navigateTo({ url: `/pages/appointment-detail/index?id=${encodeURIComponent(id)}` });
  },
  openRecovery() { wx.navigateTo({ url: '/pages/booking/index' }); },
  openFollowups() { wx.navigateTo({ url: '/pages/followups/index' }); },
  openRecords() { wx.navigateTo({ url: '/pages/records/index' }); },
  openSchedule() { wx.navigateTo({ url: '/pages/schedule/index' }); },
  async newIdentity() {
    if (this.data.creatingIdentity) return;
    this.setData({ creatingIdentity: true });
    try { if (await confirmNewSession()) await this.load(); } catch (error) { this.setData({ error: errorMessage(error) }); }
    finally { this.setData({ creatingIdentity: false }); }
  },
});
