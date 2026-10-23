import { errorMessage } from '../../services/api';
import { confirmDialog, confirmNewSession, isSessionExpired, patientApi, requirePatientIdentity, type CarePhoto } from '../../services/patient';
import { inspectPhoto, type PhotoInspection } from '../../services/photo-quality';

type Angle = CarePhoto['angle'];
type Source = 'camera' | 'album';
interface PhotoItem {
  angle: Angle; label: string; instruction: string; pose: string;
  id: string; path: string; pendingPath: string;
  status: 'empty' | 'ready' | 'uploading' | 'failed'; message: string;
}
interface PhotoDraft {
  angle: Angle; label: string; path: string; source: Source;
  inspection: PhotoInspection | null; clarityConfirmed: boolean; angleConfirmed: boolean; error: string;
}
interface PhotoData {
  uploadAllowed: boolean; loading: boolean; consentLoading: boolean; uploading: boolean;
  selecting: boolean; checking: boolean; granted: boolean; expired: boolean;
  error: string; actionError: string; patientId: string; items: PhotoItem[];
  draft: PhotoDraft | null; savedCount: number;
}
interface PhotoMethods {
  appointmentId: string; generation: number; disposed: boolean;
  load(): Promise<void>; grant(): Promise<void>; revoke(): Promise<void>;
  choose(event: WechatMiniprogram.TouchEvent): Promise<void>;
  retry(event: WechatMiniprogram.TouchEvent): void;
  toggleReview(event: WechatMiniprogram.TouchEvent): void;
  confirmDraft(): Promise<void>; discardDraft(): void;
  preview(event: WechatMiniprogram.TouchEvent): void; previewDraft(): void;
  newIdentity(): Promise<void>;
}

function blankItems(): PhotoItem[] {
  const base = { id: '', path: '', pendingPath: '', status: 'empty' as const, message: '' };
  return [
    { ...base, angle: 'front', label: '正面', pose: '正对镜头', instruction: '头部摆正，双眼看镜头；额头、双颊与下巴完整入镜。' },
    { ...base, angle: 'left', label: '左侧 45°', pose: '露出本人左脸', instruction: '鼻尖朝自己的右肩转约 45°，露出左侧面颊；保持头部水平。' },
    { ...base, angle: 'right', label: '右侧 45°', pose: '露出本人右脸', instruction: '鼻尖朝自己的左肩转约 45°，露出右侧面颊；保持头部水平。' },
  ];
}

function selectPhoto(source: Source): Promise<string> {
  return new Promise((resolve, reject) => {
    if (typeof wx.chooseMedia === 'function') {
      wx.chooseMedia({
        count: 1, mediaType: ['image'], sizeType: ['original'], sourceType: [source], camera: 'back',
        success: (result) => resolve(result.tempFiles[0]?.tempFilePath || ''), fail: reject,
      });
    } else {
      wx.chooseImage({
        count: 1, sizeType: ['original'], sourceType: [source],
        success: (result) => resolve(result.tempFilePaths[0] || ''), fail: reject,
      });
    }
  });
}

function pickerMessage(error: unknown, source: Source): string {
  const message = String((error as { errMsg?: string })?.errMsg || '');
  if (/cancel/i.test(message)) return '';
  if (/auth|deny|denied|permission|privacy/i.test(message)) {
    return source === 'camera' ? '未取得相机权限。请在微信或系统设置中允许使用相机后重试，也可从相册选择。' : '未取得相册权限。请在微信或系统设置中允许访问照片后重试，也可使用相机拍摄。';
  }
  return source === 'camera' ? '相机未能打开，请重试或从相册选择照片。' : '照片选择失败，请重试或使用相机拍摄。';
}

Page<PhotoData, PhotoMethods>({
  data: {
    uploadAllowed: false, loading: true, consentLoading: false, uploading: false,
    selecting: false, checking: false, granted: false, expired: false,
    error: '', actionError: '', patientId: '', items: blankItems(), draft: null, savedCount: 0,
  },
  appointmentId: '', generation: 0, disposed: false,
  onLoad(options) { this.appointmentId = options.id || ''; void this.load(); },
  onShow() {
    if (this.appointmentId && !this.data.loading && !this.data.selecting && !this.data.checking && !this.data.uploading && !this.data.consentLoading && !this.data.draft && !this.disposed) void this.load();
  },
  onUnload() { this.generation += 1; this.disposed = true; },
  onPullDownRefresh() { void this.load(); },
  async load() {
    if (this.disposed || this.data.selecting || this.data.checking || this.data.uploading || this.data.consentLoading || this.data.draft) { wx.stopPullDownRefresh(); return; }
    const generation = ++this.generation;
    this.setData({ loading: true, error: '', actionError: '', expired: false, uploadAllowed: false });
    try {
      if (!this.appointmentId) throw new Error('缺少预约编号，请从预约详情重新进入。');
      const identity = await patientApi.me();
      if (generation !== this.generation || this.disposed) return;
      const [consent, photos, appointment] = await Promise.all([
        patientApi.photoConsent(this.appointmentId, identity.id),
        patientApi.photos(this.appointmentId, identity.id),
        patientApi.appointment(this.appointmentId),
      ]);
      if (generation !== this.generation || this.disposed) return;
      requirePatientIdentity(identity.id);
      const items = blankItems();
      for (const photo of photos) {
        const item = items.find((entry) => entry.angle === photo.angle);
        if (item) { item.id = photo.id; item.status = 'ready'; }
      }
      this.setData({ patientId: identity.id, granted: consent.granted, items, savedCount: items.filter((item) => item.id).length, uploadAllowed: appointment.status === 'pending' || appointment.status === 'confirmed' });
      await Promise.all(items.filter((item) => item.id).map(async (item) => {
        try { item.path = await patientApi.photoFile(item.id, identity.id); }
        catch (error) { item.status = 'failed'; item.message = errorMessage(error); }
      }));
      if (generation === this.generation && !this.disposed) {
        requirePatientIdentity(identity.id);
        this.setData({ items });
      }
    } catch (error) {
      if (generation === this.generation && !this.disposed) this.setData({ error: errorMessage(error), expired: isSessionExpired(error), uploadAllowed: false, patientId: '', granted: false, items: blankItems(), savedCount: 0 });
    } finally {
      if (generation === this.generation && !this.disposed) { this.setData({ loading: false }); wx.stopPullDownRefresh(); }
    }
  },
  async grant() {
    if (this.disposed || this.data.loading || this.data.error || !this.data.patientId || this.data.expired || this.data.consentLoading || this.data.granted || this.data.selecting || this.data.checking || this.data.uploading) return;
    const generation = this.generation, patientId = this.data.patientId;
    this.setData({ consentLoading: true, actionError: '' });
    try {
      const accepted = await confirmDialog('照片诊疗用途授权', '照片仅用于本次诊疗及治疗后随访资料查看。授权记录会保留在本次预约资料中，你可以随时撤回；撤回后医护端将不能继续查看。', '同意并继续');
      if (!accepted || this.disposed || generation !== this.generation) return;
      requirePatientIdentity(patientId);
      await patientApi.setPhotoConsent(this.appointmentId, true, patientId);
      if (!this.disposed && generation === this.generation) this.setData({ granted: true });
    } catch (error) {
      if (!this.disposed && generation === this.generation) this.setData({ actionError: errorMessage(error), expired: isSessionExpired(error) });
    } finally { if (!this.disposed && generation === this.generation) this.setData({ consentLoading: false }); }
  },
  async revoke() {
    if (this.disposed || this.data.loading || this.data.expired || this.data.consentLoading || !this.data.granted || this.data.selecting || this.data.checking || this.data.uploading) return;
    const generation = this.generation, patientId = this.data.patientId;
    this.setData({ consentLoading: true, actionError: '' });
    try {
      const accepted = await confirmDialog('撤回照片授权', '撤回后，医护端不能查看本次预约的面诊及随访照片。你仍可在自己的账号中查看已上传照片。', '撤回授权');
      if (!accepted || this.disposed || generation !== this.generation) return;
      requirePatientIdentity(patientId);
      this.setData({ draft: null });
      await patientApi.setPhotoConsent(this.appointmentId, false, patientId);
      if (!this.disposed && generation === this.generation) this.setData({ granted: false });
    } catch (error) {
      if (!this.disposed && generation === this.generation) this.setData({ actionError: errorMessage(error), expired: isSessionExpired(error) });
    } finally { if (!this.disposed && generation === this.generation) this.setData({ consentLoading: false }); }
  },
  async choose(event) {
    if (this.disposed || !this.data.uploadAllowed || !this.data.granted || this.data.expired || this.data.error || this.data.uploading || this.data.loading || this.data.consentLoading || this.data.selecting || this.data.checking || this.data.draft) return;
    const angle = String(event.currentTarget.dataset.angle) as Angle;
    const source = (event.currentTarget.dataset.source || 'album') as Source;
    const item = this.data.items.find((entry) => entry.angle === angle);
    if (!item || (source !== 'camera' && source !== 'album')) return;
    const generation = this.generation, patientId = this.data.patientId;
    this.setData({ selecting: true, actionError: '' });
    try {
      requirePatientIdentity(patientId);
      const path = await selectPhoto(source);
      if (this.disposed || generation !== this.generation || !path) return;
      requirePatientIdentity(patientId);
      const draft: PhotoDraft = { angle, label: item.label, path, source, inspection: null, clarityConfirmed: false, angleConfirmed: false, error: '' };
      this.setData({ selecting: false, checking: true, draft });
      wx.pageScrollTo({ selector: '#photo-review', duration: 200 });
      const inspection = await inspectPhoto(path, angle, this);
      if (this.disposed || generation !== this.generation) return;
      requirePatientIdentity(patientId);
      this.setData({ draft: { ...draft, inspection } });
    } catch (error) {
      if (!this.disposed && generation === this.generation) {
        const message = this.data.draft ? errorMessage(error) : pickerMessage(error, source);
        this.setData({ actionError: message, draft: null, expired: isSessionExpired(error) });
      }
    } finally {
      if (!this.disposed && generation === this.generation) this.setData({ selecting: false, checking: false });
    }
  },
  toggleReview(event) {
    const draft = this.data.draft, field = String(event.currentTarget.dataset.field);
    if (!draft || this.data.checking || this.data.uploading || this.data.consentLoading || draft.inspection?.blocked) return;
    if (field === 'clarity') this.setData({ draft: { ...draft, clarityConfirmed: !draft.clarityConfirmed, error: '' } });
    if (field === 'angle') this.setData({ draft: { ...draft, angleConfirmed: !draft.angleConfirmed, error: '' } });
  },
  discardDraft() {
    if (this.data.checking || this.data.uploading || this.data.consentLoading) return;
    this.setData({ draft: null, actionError: '' });
  },
  async confirmDraft() {
    const draft = this.data.draft;
    if (this.disposed || !draft || !draft.inspection || draft.inspection.blocked || !draft.clarityConfirmed || !draft.angleConfirmed || this.data.loading || this.data.selecting || this.data.checking || this.data.uploading || this.data.consentLoading || !this.data.granted || !this.data.uploadAllowed || this.data.expired || this.data.error) return;
    const generation = this.generation, patientId = this.data.patientId;
    this.setData({ uploading: true, draft: { ...draft, error: '' }, actionError: '' });
    try {
      requirePatientIdentity(patientId);
      // Recheck permissions immediately before replacing the current photo.
      const [consent, appointment] = await Promise.all([patientApi.photoConsent(this.appointmentId, patientId), patientApi.appointment(this.appointmentId)]);
      if (this.disposed || generation !== this.generation) return;
      requirePatientIdentity(patientId);
      if (!consent.granted) {
        this.setData({ granted: false, draft: null });
        throw new Error('照片授权已撤回，请重新授权后选择照片。');
      }
      if (appointment.status !== 'pending' && appointment.status !== 'confirmed') {
        this.setData({ uploadAllowed: false, draft: null });
        throw new Error('预约状态已更新，面诊照片当前仅供查看。');
      }
      const photo = await patientApi.uploadPhoto(this.appointmentId, draft.angle, draft.path, patientId);
      if (this.disposed || generation !== this.generation) return;
      const items = this.data.items.map((item) => item.angle === draft.angle ? { ...item, id: photo.id, path: '', pendingPath: '', status: 'ready' as const, message: '' } : item);
      this.setData({ items, savedCount: items.filter((item) => item.id).length, draft: null });
      try {
        const path = await patientApi.photoFile(photo.id, patientId);
        if (!this.disposed && generation === this.generation) this.setData({ items: this.data.items.map((item) => item.angle === draft.angle ? { ...item, path } : item) });
      } catch (error) {
        if (!this.disposed && generation === this.generation) this.setData({ items: this.data.items.map((item) => item.angle === draft.angle ? { ...item, status: 'failed' as const, message: '照片已保存，预览读取失败。请重试读取。' } : item), expired: isSessionExpired(error) });
      }
      if (!this.disposed && generation === this.generation) wx.showToast({ title: '照片已保存', icon: 'success' });
    } catch (error) {
      if (!this.disposed && generation === this.generation) {
        const message = errorMessage(error);
        this.setData({ ...(this.data.draft ? { draft: { ...draft, error: message } } : { actionError: message }), expired: isSessionExpired(error) });
      }
    } finally { if (!this.disposed && generation === this.generation) this.setData({ uploading: false }); }
  },
  retry(event) {
    if (this.data.uploading || this.data.checking || this.data.selecting || this.data.consentLoading) return;
    const angle = String(event.currentTarget.dataset.angle) as Angle;
    if (this.data.draft?.angle === angle) void this.confirmDraft();
    else if (this.data.items.some((item) => item.angle === angle && item.id)) void this.load();
  },
  preview(event) {
    const path = this.data.items.find((item) => item.angle === String(event.currentTarget.dataset.angle))?.path;
    if (path) wx.previewImage({ current: path, urls: [path] });
  },
  previewDraft() {
    if (this.data.draft?.path) wx.previewImage({ current: this.data.draft.path, urls: [this.data.draft.path] });
  },
  async newIdentity() {
    if (await confirmNewSession()) { this.setData({ draft: null, loading: true }); void this.load(); }
  },
});
