import { api, errorMessage, type Clinic, type KnowledgeCategory, type KnowledgeArticle } from '../../services/api';
import { knowledge } from '../../services/knowledge';
import { clinicPresentation } from '../../services/presentation';
import { cachedSession, patientApi } from '../../services/patient';
interface Data { loading: boolean; busy: boolean; error: string; clinics: Clinic[]; clinicIndex: number; categories: KnowledgeCategory[]; categoryId: string; rows: KnowledgeArticle[]; page: number; total: number; query: string; keyword: string; favorites: boolean; needsLogin: boolean; }
interface Methods { generation: number; requestedClinicId: string; initialize(): Promise<void>; load(more?: boolean): Promise<void>; retry(): void; more(): void; search(): void; input(e: WechatMiniprogram.CustomEvent<{value: string}>): void; toggleFavorites(): void; changeClinic(e: WechatMiniprogram.CustomEvent<{ value: string }>): void; changeCategory(e: WechatMiniprogram.TouchEvent): void; open(e: WechatMiniprogram.TouchEvent): void; }
Page<Data, Methods>({
  data: { loading: true, busy: false, error: '', clinics: [], clinicIndex: 0, categories: [], categoryId: '', rows: [], page: 0, total: 0, query: '', keyword: '', favorites: false, needsLogin: false },
  generation: 0, requestedClinicId: '',
  onLoad(options) { this.requestedClinicId = options.clinicId || ''; this.setData({ favorites: options.favorites === '1' }); },
  onShow() { void this.initialize(); },
  onUnload() { this.generation++; },
  onHide() { this.generation++; this.setData({ busy: false, loading: false, rows: [] }); },
  onPullDownRefresh() { void this.initialize(); },
  onReachBottom() { this.more(); },
  async initialize() {
    const generation = ++this.generation;
    this.setData({ loading: true, busy: false, error: '', rows: [], categories: [], total: 0, page: 0 });
    try {
      const clinics = await api.clinics();
      if (generation !== this.generation) return;
      const clinicIndex = Math.max(0, clinics.findIndex(row => row.id === this.requestedClinicId));
      this.setData({ clinics, clinicIndex });
      await this.load();
    } catch (e) { if (generation === this.generation) this.setData({ loading: false, error: errorMessage(e) }); }
    finally { wx.stopPullDownRefresh(); }
  },
  async load(more = false) {
    const generation = ++this.generation;
    const clinic = this.data.clinics[this.data.clinicIndex];
    if (!clinic) { this.setData({ loading: false, busy: false }); return; }
    const page = more ? this.data.page + 1 : 1;
    this.setData({ busy: true, loading: !more, error: '', ...(more ? {} : { rows: [], total: 0, page: 0 }) });
    try {
      const session = cachedSession();
      if (this.data.favorites && !session) { this.setData({ needsLogin: true, rows: [], total: 0 }); return; }
      this.setData({ needsLogin: false });
      const allowMock = clinicPresentation(clinic).isDemo;
      const [categories, result] = await Promise.all([more ? Promise.resolve(this.data.categories) : knowledge.categories(clinic.id, allowMock && !this.data.favorites), this.data.favorites ? patientApi.knowledgeFavorites(session!.patient.id, { clinicId: clinic.id, ...(this.data.categoryId ? { categoryId: this.data.categoryId } : {}), ...(this.data.keyword ? { q: this.data.keyword } : {}), page }) : knowledge.articles(clinic.id, this.data.categoryId, page, 20, allowMock, this.data.keyword)]);
      if (generation !== this.generation) return;
      this.setData({ categories, rows: more ? [...this.data.rows, ...(result.data || [])] : result.data || [], total: result.meta?.total || 0, page });
    } catch (e) { if (generation === this.generation) this.setData({ error: errorMessage(e) }); }
    finally { if (generation === this.generation) this.setData({ loading: false, busy: false }); }
  },
  retry() { if (!this.data.clinics.length) void this.initialize(); else void this.load(); },
  input(e) { this.setData({ query: e.detail.value }); },
  search() { if (this.data.busy) return; this.setData({ keyword: this.data.query.trim() }); void this.load(); },
  toggleFavorites() { if (this.data.busy) return; this.setData({ favorites: !this.data.favorites, categoryId: '' }); void this.load(); },
  more() { if (!this.data.busy && !this.data.loading && this.data.rows.length < this.data.total) void this.load(true); },
  changeClinic(e) { if (this.data.busy) return; const clinicIndex = Number(e.detail.value); const clinic = this.data.clinics[clinicIndex]; if (!clinic) return; this.requestedClinicId = clinic.id; this.setData({ clinicIndex, categoryId: '' }); void this.load(); },
  changeCategory(e) { if (this.data.busy) return; this.setData({ categoryId: String(e.currentTarget.dataset.id || '') }); void this.load(); },
  open(e) { const row = this.data.rows.find(item => item.id === e.currentTarget.dataset.id); if (row) wx.navigateTo({ url: `/pages/knowledge-detail/index?id=${encodeURIComponent(row.id)}` }); },
});
