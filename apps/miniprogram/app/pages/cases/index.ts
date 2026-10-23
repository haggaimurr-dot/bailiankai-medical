import type { components } from '../../../../../packages/contracts/src/schema';
import { API_BASE_URL } from '../../config/api';
import { request, requestEnvelope, errorMessage } from '../../services/api';
import { patientApi, confirmDialog } from '../../services/patient';

type Case = components['schemas']['CaseView'];
interface ImagePair { beforeImageId: string; afterImageId: string; beforePath: string; afterPath: string; angle: string; beforeAt: string; afterAt: string; position: number; }
interface Data { category: string; tag: string; activeCategory: string; activeTag: string; mine: boolean; loading: boolean; busy: boolean; error: string; rows: Case[]; selected: Case | null; pairs: ImagePair[]; page: number; total: number; imagesReady: boolean; }
interface Methods {
 generation: number; visible: boolean; patientId: string; timer?: ReturnType<typeof setTimeout>; files: string[]; loadedImages: string[];
 filterInput(e: WechatMiniprogram.Input): void; search(): Promise<void>; compare(e: WechatMiniprogram.SliderChange): void; load(): Promise<void>; more(): Promise<void>; select(e: WechatMiniprogram.TouchEvent): Promise<void>;
 detail(id: string): Promise<void>; close(): void; consent(e: WechatMiniprogram.TouchEvent): Promise<void>;
 imageFailed(): void; imageLoaded(e: WechatMiniprogram.TouchEvent): void; release(): void; download(id: string, imageId: string): Promise<string>;
}
function caseFilters(category: string, tag: string) { return { ...(category ? { category } : {}), ...(tag ? { tag } : {}) }; }
function removeFile(path: string) { wx.getFileSystemManager().unlink({ filePath: path, fail: () => {} }); }
Page<Data, Methods>({
 data: { category: '', tag: '', activeCategory: '', activeTag: '', mine: false, loading: true, busy: false, error: '', rows: [], selected: null, pairs: [], page: 1, total: 0, imagesReady: false },
 generation: 0, visible: false, patientId: '', files: [], loadedImages: [],
 onLoad(options) { this.setData({ mine: options.mine === '1' }); wx.setNavigationBarTitle({ title: this.data.mine ? '我的案例展示授权' : '案例库' }); },
 onShow() { this.visible = true; void this.load(); },
 onHide() { this.visible = false; this.release(); this.setData({ selected: null, pairs: [], rows: [] }); },
 onUnload() { this.visible = false; this.release(); },
 onPullDownRefresh() { void this.load().finally(() => wx.stopPullDownRefresh()); },
 release() { this.generation++; if (this.timer) clearTimeout(this.timer); this.files.forEach(removeFile); this.files = []; this.loadedImages = []; this.setData({ pairs: [], imagesReady: false }); },
 filterInput(e) { const field = e.currentTarget.dataset.field; if (field === 'category') this.setData({ category: e.detail.value }); if (field === 'tag') this.setData({ tag: e.detail.value }); },
 async search() { if (!this.data.busy) { this.setData({ activeCategory: this.data.category.trim(), activeTag: this.data.tag.trim() }); await this.load(); } },
 compare(e) { const index = Number(e.currentTarget.dataset.index); if (!this.data.imagesReady || !Number.isInteger(index) || !this.data.pairs[index]) return; const pairs = this.data.pairs.map((pair, i) => i === index ? { ...pair, position: Math.max(0, Math.min(100, Number(e.detail.value))) } : pair); this.setData({ pairs }); },
 async load() {
  this.release(); const generation = this.generation; this.setData({ loading: true, selected: null, rows: [], error: '', page: 1 });
  try {
   if (this.data.mine) this.patientId = (await patientApi.me()).id;
   const result = this.data.mine ? await patientApi.cases(1, this.patientId, { ...caseFilters(this.data.activeCategory, this.data.activeTag) }) : await requestEnvelope<Case[]>('/cases', { data: { page: 1, pageSize: 20, ...caseFilters(this.data.activeCategory, this.data.activeTag) } });
   if (generation === this.generation && this.visible) this.setData({ rows: result.data || [], total: result.meta?.total || 0 });
   if (generation === this.generation && this.visible && !this.data.mine) this.timer = setTimeout(() => void this.load(), 10000);
  } catch(e) { if (generation === this.generation) this.setData({ error: errorMessage(e) }); }
  finally { if (generation === this.generation) this.setData({ loading: false }); }
 },
 async more() {
  if (this.data.loading || this.data.rows.length >= this.data.total) return;
  const generation = this.generation, page = this.data.page + 1; this.setData({ loading: true });
  try { const result = this.data.mine ? await patientApi.cases(page, this.patientId, { ...caseFilters(this.data.activeCategory, this.data.activeTag) }) : await requestEnvelope<Case[]>('/cases', { data: { page, pageSize: 20, ...caseFilters(this.data.activeCategory, this.data.activeTag) } }); if (generation === this.generation) this.setData({ rows: [...this.data.rows, ...(result.data || [])], total: result.meta?.total || 0, page }); }
  catch(e) { if (generation === this.generation) this.setData({ error: errorMessage(e) }); }
  finally { if (generation === this.generation) this.setData({ loading: false }); }
 },
 async download(id, imageId) {
  if (this.data.mine) return patientApi.casePhotoFile(id, imageId, this.patientId);
  return new Promise((resolve, reject) => wx.downloadFile({ url: `${API_BASE_URL}/cases/${encodeURIComponent(id)}/images/${encodeURIComponent(imageId)}/content`, timeout: 10000, success: r => { if (r.statusCode === 200) resolve(r.tempFilePath); else { if (r.tempFilePath) removeFile(r.tempFilePath); reject(new Error('案例已撤回或图片暂不可读取。')); } }, fail: () => reject(new Error('图片加载失败，请重试。')) }));
 },
 async select(e) { if (this.data.busy) return; await this.detail(e.currentTarget.dataset.id as string); },
 async detail(id) {
  const previous = this.data.selected, positions = this.data.pairs.map(pair => pair.position);
  this.release(); const generation = this.generation; this.setData({ selected: null, error: '' });
  const files: string[] = [];
  try {
   const selected = this.data.mine ? await patientApi.caseDetail(id, this.patientId) : await request<Case>(`/cases/${encodeURIComponent(id)}`);
   if (generation !== this.generation || !this.visible) return;
   if (this.data.mine) this.setData({ selected });
   const pairs: ImagePair[] = [];
   for (const pair of selected.content.pairs) {
    const beforePath = await this.download(id, pair.beforeImageId); files.push(beforePath);
    const afterPath = await this.download(id, pair.afterImageId); files.push(afterPath);
    if (generation !== this.generation || !this.visible) { files.forEach(removeFile); return; }
    pairs.push({ ...pair, beforePath, afterPath, position: previous?.id === selected.id && previous.revision === selected.revision ? (positions[pairs.length] ?? 50) : 50 });
   }
   if (generation !== this.generation || !this.visible) { files.forEach(removeFile); return; }
   const latest = this.data.mine ? await patientApi.caseDetail(id, this.patientId) : await request<Case>(`/cases/${encodeURIComponent(id)}`);
   if (latest.revision !== selected.revision || latest.version !== selected.version) throw new Error('案例已变化，请刷新后查看新版本。');
   if (generation !== this.generation || !this.visible) { files.forEach(removeFile); return; }
   this.files = files; this.setData({ selected, pairs, imagesReady: false });
   if (!this.data.mine) this.timer = setTimeout(() => { if (this.visible && !this.data.busy) void this.detail(id); }, 10000);
  } catch(e) { files.forEach(removeFile); if (generation === this.generation) { this.release(); this.setData({ ...(this.data.mine ? {} : { selected: null }), error: errorMessage(e), rows: [] }); } }
 },
 imageFailed() { this.setData({ imagesReady: false, error: '图片未完整显示，请刷新后再确认授权。' }); },
 imageLoaded(e) {
  const id = e.currentTarget.dataset.id as string;
  if (!id || !this.data.pairs.some(pair => pair.beforeImageId === id || pair.afterImageId === id)) return;
  if (!this.loadedImages.includes(id)) this.loadedImages.push(id);
  this.setData({ imagesReady: !this.data.error && this.loadedImages.length === this.data.pairs.length * 2 });
 },
 close() { this.release(); this.setData({ selected: null }); void this.load(); },
 async consent(e) {
  const selected = this.data.selected; if (!selected || this.data.busy) return;
  const granted = e.currentTarget.dataset.granted === 'true';
  if (granted && !this.data.imagesReady) return;
  const generation = this.generation;
  this.setData({ busy: true, error: '' }); if (this.timer) clearTimeout(this.timer);
  try {
   if (!await confirmDialog(granted ? '独立案例展示授权' : '撤回案例展示授权', granted ? selected.policyContent : '撤回后停止本案例在小程序中的后续展示，不影响诊疗服务和诊疗照片授权。', granted ? '同意展示' : '撤回展示')) return;
   if (generation !== this.generation || !this.visible) return;
   await patientApi.caseConsent(selected.id, { version: selected.version, granted, policyVersion: selected.policyVersion as 'case-display-v1' }, this.patientId);
   if (generation === this.generation) { wx.showToast({ title: granted ? '已记录独立授权' : '已撤回展示', icon: 'success' }); await this.load(); }
  } catch(e) { if (generation === this.generation) this.setData({ error: errorMessage(e) }); }
  finally { this.setData({ busy: false }); if (this.visible && this.data.selected) void this.detail(this.data.selected.id); }
 }
});
