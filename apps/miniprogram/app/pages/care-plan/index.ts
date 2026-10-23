import { errorMessage } from '../../services/api';
import { confirmDialog, confirmNewSession, isSessionExpired, patientApi, type CarePlanVersion } from '../../services/patient';

interface PlanVersionView extends CarePlanVersion { publishedAtLabel: string; linked: boolean; adviceVersion: string; readAt: string | null; readLabel: string; }
interface PlanData { loading: boolean; error: string; readError: string; expired: boolean; creatingIdentity: boolean; versions: PlanVersionView[]; confirmingId: string; }
interface PlanMethods { patientId: string; confirmRead(event: WechatMiniprogram.TouchEvent): Promise<void>; linkedVersionId: string; appointmentId: string; generation: number; disposed: boolean; load(): Promise<void>; retry(): void; newIdentity(): Promise<void>; }

Page<PlanData, PlanMethods>({
  data: { loading: true, error: '', readError: '', expired: false, creatingIdentity: false, versions: [], confirmingId: '' },
  patientId: '', linkedVersionId: '', appointmentId: '', generation: 0, disposed: false,
  onLoad(options) { this.appointmentId = options.id || ''; this.linkedVersionId = options.versionId || ''; void this.load(); },
  onShow() { if (this.appointmentId && !this.data.loading && !this.disposed) void this.load(); },
  onUnload() { this.generation += 1; this.disposed = true; },
  onPullDownRefresh() { void this.load(); },
  async load() {
    if (this.disposed) return;
    const generation = ++this.generation;
    this.setData({ loading: true, error: '', readError: '', expired: false, versions: [] });
    try {
      if (!this.appointmentId) throw new Error('缺少预约编号，请从预约详情重新进入。');
      const identity = await patientApi.me();
      const versions = await patientApi.planVersions(this.appointmentId, identity.id);
      const rendered = await Promise.all(versions.map(async (item) => {
        const state = item.content.instructions?.trim() ? await patientApi.advice('plan', item.id, identity.id) : null;
        return {
          ...item, adviceVersion: state?.adviceVersion || '', readAt: state?.readAt || null,
        readLabel: state?.readAt ? new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'short', timeStyle: 'short' }).format(new Date(state.readAt)) : '', linked: item.id === this.linkedVersionId,
        publishedAtLabel: new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(item.publishedAt)),
        };
      }));
      if (generation === this.generation) { this.patientId = identity.id; this.setData({ versions: rendered }); }
    } catch (error) { if (generation === this.generation) this.setData({ error: errorMessage(error), expired: isSessionExpired(error) }); }
    finally { if (generation === this.generation) { this.setData({ loading: false }); wx.stopPullDownRefresh(); } }
  },
  async confirmRead(event) {
    if (this.data.confirmingId || this.data.loading) return;
    const item = this.data.versions.find(version => version.id === String(event.currentTarget.dataset.id || ''));
    if (!item || !item.adviceVersion || item.readAt) return;
    const generation = this.generation, patientId = this.patientId;
    this.setData({ confirmingId: item.id, readError: '' });
    try {
      if (!await confirmDialog('确认已阅读医嘱', item.content.instructions || '', '已阅读')) return;
      if (this.disposed || generation !== this.generation) return;
      const receipt = await patientApi.confirmAdviceRead('plan', item.id, item.adviceVersion, patientId);
      if (!this.disposed && generation === this.generation) this.setData({ versions: this.data.versions.map(version => version.id === item.id ? {
        ...version, readAt: receipt.readAt,
        readLabel: new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'short', timeStyle: 'short' }).format(new Date(receipt.readAt)),
      } : version) });
    } catch (error) { if (!this.disposed && generation === this.generation) this.setData({ readError: errorMessage(error), expired: isSessionExpired(error), ...(isSessionExpired(error) ? { error: errorMessage(error), versions: [] } : {}) }); }
    finally { if (!this.disposed) this.setData({ confirmingId: '' }); }
  },
  retry() { void this.load(); },
  async newIdentity() {
    if (this.data.creatingIdentity) return;
    this.setData({ creatingIdentity: true });
    try { if (await confirmNewSession()) void this.load(); }
    catch (error) { this.setData({ error: errorMessage(error) }); }
    finally { if (!this.disposed) this.setData({ creatingIdentity: false }); }
  },
});
