import { errorMessage } from '../../services/api';
import { confirmNewSession, isSessionExpired, patientApi, requirePatientIdentity, type FollowupTask } from '../../services/patient';
import { beijingDate, calendarDays, calendarEvents, recoverySeries, shiftMonth, type CalendarDay, type CalendarEvent, type RecoverySeries, type RecoveryPoint } from '../../utils/recovery-calendar';

interface CalendarData {
  loading: boolean; expired: boolean; creatingIdentity: boolean; error: string;
  month: string; monthLabel: string; selected: string; days: CalendarDay[]; events: CalendarEvent[];
  selectedEvents: CalendarEvent[]; pendingEvents: CalendarEvent[]; pendingCount: number; reviewCount: number; taskCount: number; weekdays: string[];
  rangeLabel: string; pendingScope: string; allPendingEvents: CalendarEvent[]; visiblePendingEvents: (CalendarEvent & { crossMonth: boolean })[];
  series: RecoverySeries[]; seriesIndex: number; pointLabels: string[]; beforeIndex: number; afterIndex: number;
  before: RecoveryPoint | null; after: RecoveryPoint | null; trendLabel: string;
}
interface CalendarMethods {
  generation: number; disposed: boolean;
  load(): Promise<void>; render(month?: string, selected?: string): void; retry(): void;
  previousMonth(): void; nextMonth(): void; today(): void;
  changePendingScope(event: WechatMiniprogram.TouchEvent): void;
  changeSeries(event: WechatMiniprogram.PickerChange): void; changePoint(event: WechatMiniprogram.PickerChange): void;
  renderComparison(seriesIndex: number, beforeIndex?: number, afterIndex?: number): void;
  selectDate(event: WechatMiniprogram.TouchEvent): void; openTask(event: WechatMiniprogram.TouchEvent): void; newIdentity(): Promise<void>;
}
Page<CalendarData, CalendarMethods>({
  data: { loading: true, expired: false, creatingIdentity: false, error: '', month: '', monthLabel: '', selected: '', days: [], events: [],
    selectedEvents: [], pendingEvents: [], pendingCount: 0, reviewCount: 0, taskCount: 0, weekdays: ['一', '二', '三', '四', '五', '六', '日'],
    rangeLabel: '', pendingScope: 'range', allPendingEvents: [], visiblePendingEvents: [], series: [], seriesIndex: 0,
    pointLabels: [], beforeIndex: 0, afterIndex: 0, before: null, after: null, trendLabel: '' },
  generation: 0, disposed: false,
  onLoad() { this.render(beijingDate().slice(0, 7), beijingDate()); void this.load(); },
  onShow() { if (!this.data.loading && !this.disposed) void this.load(); },
  onUnload() { this.disposed = true; this.generation++; },
  onPullDownRefresh() { void this.load(); },
  async load() {
    if (this.disposed) return;
    const generation = ++this.generation;
    const days = calendarDays(this.data.month, this.data.selected, []);
    const dates = { dateFrom: days[0].date, dateTo: days[days.length - 1].date };
    this.setData({ loading: true, expired: false, error: '', events: [], selectedEvents: [], pendingEvents: [], pendingCount: 0, reviewCount: 0, taskCount: 0,
      allPendingEvents: [], visiblePendingEvents: [], series: [], pointLabels: [], before: null, after: null, trendLabel: '' });
    try {
      const identity = await patientApi.me();
      if (generation !== this.generation) return;
      const tasks = new Map<string, FollowupTask>();
      let page = 1;
      while (generation === this.generation) {
        const result = await patientApi.followups('', page, identity.id, dates);
        if (generation !== this.generation) return;
        const rows = result.data || [];
        for (const task of rows) tasks.set(task.id, task);
        if (!result.meta) throw new Error('随访分页信息缺失，请重新加载。');
        if (result.meta.page * result.meta.pageSize >= result.meta.total) break;
        if (!rows.length) throw new Error('随访数据读取不完整，请重新加载。');
        page++;
      }
      if (generation !== this.generation) return;
      // Unbounded history supplies cross-month pending tasks and same-appointment comparisons.
      const history = new Map<string, FollowupTask>();
      page = 1;
      while (generation === this.generation) {
        const result = await patientApi.followups('', page, identity.id);
        if (generation !== this.generation) return;
        const rows = result.data || [];
        for (const task of rows) history.set(task.id, task);
        if (!result.meta) throw new Error('随访分页信息缺失，请重新加载。');
        if (result.meta.page * result.meta.pageSize >= result.meta.total) break;
        if (!rows.length) throw new Error('随访数据读取不完整，请重新加载。');
        page++;
      }
      requirePatientIdentity(identity.id);
      const events = calendarEvents([...tasks.values()]).filter(event => event.date >= dates.dateFrom && event.date <= dates.dateTo);
      const pendingEvents = events.filter(item => item.pending);
      const allPendingEvents = calendarEvents([...history.values()]).filter(item => item.pending);
      this.setData({ events, pendingEvents, pendingCount: pendingEvents.length, taskCount: tasks.size,
        reviewCount: events.filter(item => item.kind === 'review').length, allPendingEvents,
        visiblePendingEvents: (this.data.pendingScope === 'all' ? allPendingEvents : pendingEvents).map(item => ({ ...item, crossMonth: item.date.slice(0, 7) !== this.data.month })),
        series: recoverySeries([...history.values()]) });
      this.renderComparison(0);
      this.render();
    } catch (error) {
      if (generation === this.generation) this.setData({ error: errorMessage(error), expired: isSessionExpired(error) });
    } finally {
      if (generation === this.generation) { this.setData({ loading: false }); wx.stopPullDownRefresh(); }
    }
  },
  render(month, selected) {
    month = month || this.data.month;
    selected = selected || this.data.selected;
    const days = calendarDays(month, selected, this.data.events);
    this.setData({ month, monthLabel: `${Number(month.slice(0, 4))}年${Number(month.slice(5))}月`, selected,
      rangeLabel: `${days[0].date} 至 ${days[41].date}`, days,
      visiblePendingEvents: this.data.visiblePendingEvents.map(item => ({ ...item, crossMonth: item.date.slice(0, 7) !== month })),
      selectedEvents: this.data.events.filter(item => item.date === selected) });
  },
  changePendingScope(event) {
    const scope = String(event.currentTarget.dataset.scope || '');
    if (scope !== 'range' && scope !== 'all') return;
    this.setData({ pendingScope: scope, visiblePendingEvents: (scope === 'all' ? this.data.allPendingEvents : this.data.pendingEvents)
      .map(item => ({ ...item, crossMonth: item.date.slice(0, 7) !== this.data.month })) });
  },
  renderComparison(seriesIndex, beforeIndex = 0, afterIndex) {
    const points = this.data.series[seriesIndex]?.points || [];
    afterIndex = afterIndex ?? Math.max(0, points.length - 1);
    const before = points[beforeIndex] || null, after = points[afterIndex] || null;
    let trendLabel = '至少需要两次已提交反馈才能对照';
    if (before && after && before.id !== after.id) {
      if (beforeIndex >= afterIndex) trendLabel = '请选择早于对照反馈的基准反馈';
      else if (![0, 1, 2, 3].includes(before.severity) || ![0, 1, 2, 3].includes(after.severity)) trendLabel = '不适程度未完整记录';
      else trendLabel = after.severity < before.severity ? '自述不适程度降低' : after.severity > before.severity ? '自述不适程度升高' : '自述不适程度持平';
    }
    this.setData({ seriesIndex, pointLabels: points.map(point => `${point.date} · ${point.title}`), beforeIndex, afterIndex, before, after, trendLabel });
  },
  changeSeries(event) {
    const index = Number(event.detail.value);
    if (this.data.series[index]) this.renderComparison(index);
  },
  changePoint(event) {
    const index = Number(event.detail.value);
    if (!this.data.series[this.data.seriesIndex]?.points[index]) return;
    this.renderComparison(this.data.seriesIndex, event.currentTarget.dataset.side === 'before' ? index : this.data.beforeIndex,
      event.currentTarget.dataset.side === 'after' ? index : this.data.afterIndex);
  },
  retry() { void this.load(); },
  previousMonth() { const month = shiftMonth(this.data.month, -1); this.render(month, `${month}-01`); void this.load(); },
  nextMonth() { const month = shiftMonth(this.data.month, 1); this.render(month, `${month}-01`); void this.load(); },
  today() { const today = beijingDate(); this.render(today.slice(0, 7), today); void this.load(); },
  selectDate(event) {
    const date = String(event.currentTarget.dataset.date || '');
    if (this.data.days.some(day => day.date === date)) {
      const changedMonth = date.slice(0, 7) !== this.data.month;
      this.render(date.slice(0, 7), date);
      if (changedMonth) void this.load();
    }
  },
  openTask(event) {
    if (this.data.loading || this.data.error || this.data.expired) return;
    const id = String(event.currentTarget.dataset.id || '');
    const item = [...this.data.events, ...this.data.allPendingEvents].find(item => item.id === id);
    if (item) wx.navigateTo({ url: `/pages/followups/index?id=${encodeURIComponent(item.appointmentId)}&taskId=${encodeURIComponent(item.taskId)}` });
  },
  async newIdentity() {
    if (this.data.creatingIdentity) return;
    this.setData({ creatingIdentity: true });
    try { if (await confirmNewSession() && !this.disposed) void this.load(); }
    catch (error) { if (!this.disposed) this.setData({ error: errorMessage(error) }); }
    finally { if (!this.disposed) this.setData({ creatingIdentity: false }); }
  },
});
