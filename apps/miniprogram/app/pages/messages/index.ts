import { patientApi, cachedSession, requirePatientIdentity } from '../../services/patient';
import { errorMessage } from '../../services/api';
import type { components } from '../../../../../packages/contracts/src/schema';
type Template = components['schemas']['MessageTemplate'] & { statusLabel: string };
type Change = components['schemas']['MessageConsentChange'];
const labels: Record<string, string> = { accepted: '已授权一次提醒', consumed: '本次授权已使用', rejected: '未同意', revoked: '已撤回' };
const pendingKey = (id: string) => `message-consent-pending:${id}`;
Page<{ templates: Template[]; patientId: string; busy: boolean; error: string; notice: string }, {
 generation: number; load(): Promise<void>; subscribe(e: WechatMiniprogram.CustomEvent): void; revoke(e: WechatMiniprogram.CustomEvent): Promise<void>; persist(change: Change): Promise<void>;
}>({
 data: { templates: [], patientId: '', busy: false, error: '', notice: '' }, generation: 0,
 onShow() { void this.load(); }, onUnload() { this.generation++; },
 async load() {
  const generation = ++this.generation, session = cachedSession();
  if (!session) { this.setData({ templates: [], patientId: '', error: '请先到“我的资料”登录。' }); return; }
  this.setData({ patientId: session.patient.id, busy: true, error: '' });
  try {
   const pending = wx.getStorageSync(pendingKey(session.patient.id)) as Change | undefined;
   if (pending) { await patientApi.setMessageConsent(pending, session.patient.id); wx.removeStorageSync(pendingKey(session.patient.id)); }
   const [templates, consents] = await Promise.all([patientApi.messageTemplates(session.patient.id), patientApi.messageConsents(session.patient.id)]);
   if (generation === this.generation) this.setData({ templates: templates.map(t => ({ ...t, statusLabel: labels[consents.find(c => c.templateId === t.templateId)?.status || ''] || '尚未授权' })) });
  } catch (e) { if (generation === this.generation) this.setData({ error: errorMessage(e) }); }
  finally { if (generation === this.generation) this.setData({ busy: false }); }
 },
 async persist(change) {
  const patientId = this.data.patientId;
  requirePatientIdentity(patientId);
  wx.setStorageSync(pendingKey(patientId), change);
  await patientApi.setMessageConsent(change, patientId);
  wx.removeStorageSync(pendingKey(patientId));
 },
 subscribe(e) {
  if (this.data.busy) return;
  const t = this.data.templates.find(t => t.id === e.currentTarget.dataset.id); if (!t) return;
  const patientId = this.data.patientId;
  this.setData({ busy: true, error: '', notice: '' });
  // Invoke directly from the tap event so WeChat can show the subscription prompt.
  wx.requestSubscribeMessage({ tmplIds: [t.templateId], success: async result => {
   try {
    requirePatientIdentity(patientId);
    const status = result[t.templateId] === 'accept' ? 'accepted' : 'rejected';
    await this.persist({ templateId: t.templateId, status, requestKey: `subscribe_${Date.now()}_${Math.random().toString(36).slice(2)}` });
    this.setData({ notice: status === 'accepted' ? '已记录本次订阅，可接收一次提醒。' : '未开启提醒，不影响预约与复诊。' }); await this.load();
   } catch (error) { this.setData({ error: errorMessage(error), busy: false }); }
  }, fail: () => { this.setData({ error: '未完成微信订阅，请重试。', busy: false }); } });
 },
 async revoke(e) {
  if (this.data.busy) return;
  const t = this.data.templates.find(t => t.id === e.currentTarget.dataset.id); if (!t) return;
  this.setData({ busy: true, error: '', notice: '' });
  try { await this.persist({ templateId: t.templateId, status: 'revoked', requestKey: `revoke_${Date.now()}_${Math.random().toString(36).slice(2)}` }); this.setData({ notice: '已停止后续提醒。' }); await this.load(); }
  catch (error) { this.setData({ error: errorMessage(error), busy: false }); }
 }
});
