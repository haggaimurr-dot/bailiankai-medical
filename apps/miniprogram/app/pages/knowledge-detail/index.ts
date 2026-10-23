import { errorMessage, type KnowledgeArticle } from '../../services/api';
import { knowledge } from '../../services/knowledge';
import { cachedSession, patientApi } from '../../services/patient';
interface Data { loading: boolean; error: string; article: KnowledgeArticle | null; reviewedDate: string; publishedDate: string; saved: boolean; saving: boolean; favoriteError: string; related: KnowledgeArticle[]; relatedError: string; demo: boolean; }
interface Methods { id: string; generation: number; load(): Promise<void>; retry(): void; toggleFavorite(): Promise<void>; openRelated(e: WechatMiniprogram.TouchEvent): void; }
Page<Data, Methods>({
  data: { loading: true, error: '', article: null, reviewedDate: '', publishedDate: '', saved: false, saving: false, favoriteError: '', related: [], relatedError: '', demo: false }, id: '', generation: 0,
  onLoad(options) { this.id = options.id || ''; },
  onShow() { void this.load(); },
  onUnload() { this.generation++; },
  onHide() { this.generation++; this.setData({ article: null, related: [], saved: false, saving: false }); },
  onPullDownRefresh() { if (this.data.saving) wx.stopPullDownRefresh(); else void this.load(); },
  async load() {
    const generation = ++this.generation;
    this.setData({ loading: true, error: '', article: null, related: [], relatedError: '', favoriteError: '', saved: false });
    try {
      if (!this.id) throw new Error('文章入口信息不完整，请返回科普列表。');
      const article = await knowledge.article(this.id);
      if (generation !== this.generation) return;
      const date = (value: string) => { const d = new Date(value); return Number.isFinite(d.getTime()) ? new Date(d.getTime() + 8 * 3600000).toISOString().slice(0, 10) : ''; };
      const demo = article.id.startsWith('mock-');
      this.setData({ article, demo, reviewedDate: date(article.reviewedAt), publishedDate: date(article.publishedAt) });
      const session = cachedSession();
      const [related, favorite] = await Promise.allSettled([knowledge.related(article.id), session && !demo ? patientApi.knowledgeFavorite(article.id, session.patient.id) : Promise.resolve(false)]);
      if (generation !== this.generation) return;
      this.setData({ related: related.status === 'fulfilled' ? related.value : [], relatedError: related.status === 'rejected' ? errorMessage(related.reason) : '', saved: favorite.status === 'fulfilled' ? favorite.value : false, favoriteError: favorite.status === 'rejected' ? errorMessage(favorite.reason) : '' });
    } catch (e) { if (generation === this.generation) this.setData({ error: errorMessage(e) }); }
    finally { if (generation === this.generation) { this.setData({ loading: false }); wx.stopPullDownRefresh(); } }
  },
  retry() { void this.load(); },
  async toggleFavorite() {
    if (!this.data.article || this.data.saving || this.data.loading || this.data.demo) return;
    const session = cachedSession();
    if (!session) { wx.navigateTo({ url: '/pages/profile/index' }); return; }
    const generation = this.generation, id = this.id;
    this.setData({ saving: true, favoriteError: '' });
    try {
      const saved = await patientApi.setKnowledgeFavorite(id, !this.data.saved, session.patient.id);
      if (generation === this.generation) { this.setData({ saved }); wx.showToast({ title: saved ? '已收藏' : '已取消收藏', icon: 'success' }); }
    } catch (e) { if (generation === this.generation) this.setData({ favoriteError: errorMessage(e) }); }
    finally { if (generation === this.generation) this.setData({ saving: false }); }
  },
  openRelated(e) { const id = String(e.currentTarget.dataset.id || ''); if (this.data.related.some(row => row.id === id)) wx.redirectTo({ url: `/pages/knowledge-detail/index?id=${encodeURIComponent(id)}` }); },
});
