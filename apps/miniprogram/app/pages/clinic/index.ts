import { api, errorMessage, type Clinic } from '../../services/api';
import { clinicPresentation, validLocation, previewCredential, type ClinicPresentation } from '../../services/presentation';

interface Data {
  loading: boolean; error: string; clinic: Clinic | null; presentation: ClinicPresentation | null;
  hasLocation: boolean; markers: { id: number; latitude: number; longitude: number; title: string; callout: { content: string; display: string; padding: number; borderRadius: number } }[]; canCall: boolean;
}
interface Methods {
  clinicId: string; generation: number; load(): Promise<void>; retry(): void;
  openLocation(): void; call(): void; copyAddress(): void; preview(e: WechatMiniprogram.TouchEvent): void; openAppointments(): void;
}
Page<Data, Methods>({
  data: { loading: true, error: '', clinic: null, presentation: null, hasLocation: false, markers: [], canCall: false },
  clinicId: '', generation: 0,
  onLoad(options) { this.clinicId = options.clinicId || ''; },
  onShow() { void this.load(); },
  onUnload() { this.generation++; },
  onPullDownRefresh() { void this.load(); },
  async load() {
    const generation = ++this.generation;
    this.setData({ loading: true, error: '', clinic: null, presentation: null, hasLocation: false, markers: [], canCall: false });
    try {
      if (!this.clinicId) throw new Error('院区入口信息不完整，请返回首页重新选择。');
      const clinic = (await api.clinics()).find(row => row.id === this.clinicId);
      if (generation !== this.generation) return;
      if (!clinic) throw new Error('院区资料暂不可用，请返回首页重新选择。');
      const presentation = clinicPresentation(clinic);
      const location = presentation.location;
      const hasLocation = validLocation(location);
      this.setData({ clinic, presentation, hasLocation,
        canCall: !presentation.isDemo && /^[+\d][\d\s()-]{4,31}$/.test(clinic.phone),
        markers: hasLocation ? [{ id: 1, latitude: location.latitude, longitude: location.longitude, title: clinic.name,
          callout: { content: presentation.isDemo ? '演示位置 · 非真实院址' : clinic.name, display: 'ALWAYS', padding: 8, borderRadius: 6 } }] : [],
      });
    } catch (e) { if (generation === this.generation) this.setData({ error: errorMessage(e) }); }
    finally { if (generation === this.generation) { this.setData({ loading: false }); wx.stopPullDownRefresh(); } }
  },
  retry() { void this.load(); },
  openLocation() {
    const { clinic, presentation, loading } = this.data;
    if (loading || !clinic || !presentation || !validLocation(presentation.location)) return;
    const open = () => wx.openLocation({ ...presentation.location!, name: presentation.isDemo ? '演示位置（非真实院址）' : clinic.name,
      address: presentation.isDemo ? '仅用于地图功能验证，请勿按此位置就诊' : clinic.address, scale: 16,
      fail: () => wx.showToast({ title: '地图无法打开，可复制地址', icon: 'none' }) });
    if (presentation.isDemo) wx.showModal({ title: '打开演示位置', content: '此坐标仅用于地图功能验证，并非真实院址。', confirmText: '查看示例', success: r => { if (r.confirm) open(); } });
    else open();
  },
  call() { if (this.data.canCall && this.data.clinic) wx.makePhoneCall({ phoneNumber: this.data.clinic.phone, fail: () => wx.showToast({ title: '拨号未完成', icon: 'none' }) }); },
  copyAddress() { if (this.data.clinic) wx.setClipboardData({ data: this.data.clinic.address }); },
  preview(e) { previewCredential(this.data.presentation?.credentials || [], Number(e.currentTarget.dataset.index)); },
  openAppointments() { wx.navigateTo({ url: '/pages/appointments/index' }); },
});
