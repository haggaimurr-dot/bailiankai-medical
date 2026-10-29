import { errorMessage, type KnowledgeArticle } from '../../services/api';
import { API_BASE_URL } from '../../config/api';
import { knowledge } from '../../services/knowledge';
import { cachedSession, patientApi } from '../../services/patient';

// 阅读打点：fire-and-forget，失败不影响阅读；演示文章不打点。
function sendOpenBeacon(id: string) {
  try {
    const session = cachedSession();
    wx.request({
      url: `${API_BASE_URL}/knowledge/articles/${encodeURIComponent(id)}/open`,
      method: 'POST',
      data: { clinicId: '' },
      header: { 'Content-Type': 'application/json', ...(session ? { Authorization: `Bearer ${session.token}` } : {}) },
    });
  } catch { /* 阅读不受打点影响 */ }
}
interface QuizState { answers: Record<number, number>; submitted: boolean; score: number; }
interface Data { loading: boolean; error: string; article: KnowledgeArticle | null; reviewedDate: string; publishedDate: string; saved: boolean; saving: boolean; favoriteError: string; related: KnowledgeArticle[]; relatedError: string; demo: boolean; quiz: QuizState; }
interface Methods { id: string; generation: number; load(): Promise<void>; retry(): void; toggleFavorite(): Promise<void>; openRelated(e: WechatMiniprogram.TouchEvent): void; answer(e: WechatMiniprogram.TouchEvent): void; submitQuiz(): void; resetQuiz(): void; }
Page<Data, Methods>({
  data: { loading: true, error: '', article: null, reviewedDate: '', publishedDate: '', saved: false, saving: false, favoriteError: '', related: [], relatedError: '', demo: false, quiz: { answers: {}, submitted: false, score: 0 } }, id: '', generation: 0,
  onLoad(options) { this.id = options.id || ''; },
  onShow() { void this.load(); },
  onUnload() { this.generation++; },
  onHide() { this.generation++; this.setData({ quiz: { answers: {}, submitted: false, score: 0 } });
   this.setData({ article: null, related: [], saved: false, saving: false }); },
  onPullDownRefresh() { if (this.data.saving) wx.stopPullDownRefresh(); else void this.load(); },
  async load() {
    const generation = ++this.generation;
    this.setData({ loading: true, error: '', article: null, related: [], relatedError: '', favoriteError: '', saved: false, quiz: { answers: {}, submitted: false, score: 0 } });
    try {
      if (!this.id) throw new Error('文章入口信息不完整，请返回科普列表。');
      const article = await knowledge.article(this.id);
      if (generation !== this.generation) return;
      const date = (value: string) => { const d = new Date(value); return Number.isFinite(d.getTime()) ? new Date(d.getTime() + 8 * 3600000).toISOString().slice(0, 10) : ''; };
      const demo = article.id.startsWith('mock-');
      this.setData({ article, demo, reviewedDate: date(article.reviewedAt), publishedDate: date(article.publishedAt) });
      if (!demo) sendOpenBeacon(article.id);
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
  answer(e) { const qi = Number(e.currentTarget.dataset.qi); const oi = Number(e.currentTarget.dataset.oi); if (this.data.quiz.submitted || !Number.isInteger(qi) || !Number.isInteger(oi)) return; this.setData({ quiz: { answers: { ...this.data.quiz.answers, [qi]: oi }, submitted: false, score: 0 } }); },
  submitQuiz() { const quiz = this.data.article?.quiz || []; if (quiz.some((_, qi) => this.data.quiz.answers[qi] === undefined)) { wx.showToast({ title: '还有题目没作答', icon: 'none' }); return; } const score = quiz.filter((q, qi) => this.data.quiz.answers[qi] === q.answerIndex).length; this.setData({ quiz: { ...this.data.quiz, submitted: true, score } }); },
  resetQuiz() { this.setData({ quiz: { answers: {}, submitted: false, score: 0 } }); },
  openRelated(e) { const id = String(e.currentTarget.dataset.id || ''); if (this.data.related.some(row => row.id === id)) wx.redirectTo({ url: `/pages/knowledge-detail/index?id=${encodeURIComponent(id)}` }); },
});
