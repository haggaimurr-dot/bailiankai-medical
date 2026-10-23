import { ApiError, errorMessage } from '../../services/api';
import { cachedSession, confirmDialog, confirmNewSession, patientApi, type PatientIdentity, type PrivacyPolicy, type PrivacyConsent } from '../../services/patient';
import { PATIENT_LOGIN_MODE } from '../../config/api';

interface ProfileData {
  patient: PatientIdentity | null; name: string; loading: boolean; busy: boolean;
  error: string; notice: string; phoneStatus: string; demoMode: boolean; policy: PrivacyPolicy | null; privacy: PrivacyConsent | null; bindingCode: string;
}
interface ProfileMethods {
  generation: number;
  load(): Promise<void>; login(): Promise<void>; accept(patient: PatientIdentity): void;
  nameChanged(event: WechatMiniprogram.CustomEvent<{ value: string }>): void;
  save(): Promise<void>;
  bindingCodeChanged(event: WechatMiniprogram.CustomEvent<{ value: string }>): void;
  bindChart(): Promise<void>;
  authorizePhone(event: WechatMiniprogram.CustomEvent<{ code?: string }>): Promise<void>;
  acceptPrivacy(): Promise<void>; revokePrivacy(): Promise<void>;
  revokePhone(): Promise<void>; logout(): Promise<void>; handleError(error: unknown): Promise<void>;
}
Page<ProfileData, ProfileMethods>({
  data: { patient: null, name: '', loading: false, busy: false, error: '', notice: '', phoneStatus: '', policy: null, privacy: null, demoMode: PATIENT_LOGIN_MODE === 'demo', bindingCode: '' },
  generation: 0,
  onShow() { void this.load(); },
  onUnload() { this.generation += 1; },
  accept(patient) {
    this.setData({ patient, name: patient.displayName, phoneStatus: patient.phoneAuthorization === 'granted' ? '已授权' : patient.phoneAuthorization === 'revoked' ? '已撤回' : '未授权' });
  },
  async load() {
    const generation = ++this.generation;
    if (!cachedSession()) {
      this.setData({ patient: null, privacy: null, loading: true, error: '' });
      try { const policy = await patientApi.privacyPolicy(); if (generation === this.generation) this.setData({ policy }); }
      catch (error) { if (generation === this.generation) this.setData({ error: errorMessage(error) }); }
      finally { if (generation === this.generation) this.setData({ loading: false }); }
      return;
    }
    this.setData({ loading: true, error: '' });
    try {
      const patient = await patientApi.me();
      const [policy, privacy] = await Promise.all([patientApi.privacyPolicy(), patientApi.privacyConsent(patient.id)]);
      if (generation === this.generation) { this.accept(patient); this.setData({ policy, privacy }); }
    }
    catch (error) { if (generation === this.generation) this.setData({ error: errorMessage(error), ...(!cachedSession() ? { patient: null } : {}) }); }
    finally { if (generation === this.generation) this.setData({ loading: false }); }
  },
  async login() {
    if (this.data.busy) return;
    this.setData({ busy: true, error: '', notice: '' });
    try { if (await confirmNewSession()) await this.load(); }
    catch (error) { this.setData({ error: errorMessage(error) }); }
    finally { this.setData({ busy: false }); }
  },
  nameChanged(event) { this.setData({ name: event.detail.value, notice: '' }); },
  bindingCodeChanged(event) { this.setData({ bindingCode: event.detail.value.trim().toLowerCase(), notice: '' }); },
  async bindChart() {
    const patient = this.data.patient, code = this.data.bindingCode;
    if (this.data.busy || !patient || patient.loginKind !== 'wechat') return;
    if (!/^[a-f0-9]{32}$/.test(code)) { this.setData({ error: '请粘贴医护提供的完整32位绑定码。' }); return; }
    this.setData({ busy: true, error: '', notice: '' });
    try {
      const preview = await patientApi.previewBinding(code, patient.id);
      if (!await confirmDialog('确认绑定院内档案', '姓名：' + preview.displayName + '\n院区：' + preview.clinicName + '\n档案编号：' + preview.patientId + '\n请确认这是您本人的档案。绑定后将使用此档案查看问诊与诊疗记录。', '确认绑定')) return;
      this.accept(await patientApi.bindChart(code, preview.version, patient.id));
      this.setData({ bindingCode: '' });
      await this.load();
      this.setData({ notice: '院内档案已绑定。重新登录同一微信账号可恢复此档案。' });
    } catch (error) { await this.handleError(error); }
    finally { this.setData({ busy: false }); }
  },
  async handleError(error) {
    if (error instanceof ApiError && error.code === 'VERSION_CONFLICT') {
      await this.load();
      this.setData({ error: '资料已在其他设备修改，已加载最新资料，请核对后重新操作。' });
    } else this.setData({ error: errorMessage(error), ...(!cachedSession() ? { patient: null } : {}) });
  },
  async save() {
    const patient = this.data.patient;
    if (this.data.busy || !patient) return;
    const name = this.data.name.trim();
    if (!name || Array.from(name).length > 40) { this.setData({ error: '请填写1–40字姓名。' }); return; }
    this.setData({ busy: true, error: '', notice: '' });
    try { this.accept(await patientApi.updateProfile(name, patient.version || 1, patient.id)); this.setData({ notice: '资料已保存。' }); }
    catch (error) { await this.handleError(error); }
    finally { this.setData({ busy: false }); }
  },
  async authorizePhone(event) {
    const patient = this.data.patient;
    if (this.data.busy || !patient) return;
    if (!event.detail.code) { this.setData({ notice: '未完成授权，账号手机号保持原状态。' }); return; }
    this.setData({ busy: true, error: '', notice: '' });
    try { this.accept(await patientApi.authorizePhone(event.detail.code, patient.version || 1, patient.id)); this.setData({ notice: '账号手机号已更新。' }); }
    catch (error) { await this.handleError(error); }
    finally { this.setData({ busy: false }); }
  },
  async acceptPrivacy() {
    const { patient, policy } = this.data;
    if (this.data.busy || !patient || !policy) return;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      if (!await confirmDialog('确认隐私说明', '请阅读页面所示隐私说明。确认同意版本 ' + policy.version + '？手机号及照片授权仍独立选择。', '同意')) return;
      await patientApi.setPrivacyConsent(policy.version, 'accepted', patient.id);
      await this.load(); this.setData({ notice: '已保存隐私同意记录。' });
    } catch (error) { await this.handleError(error); }
    finally { this.setData({ busy: false }); }
  },
  async revokePrivacy() {
    const { patient, policy } = this.data;
    if (this.data.busy || !patient || !policy) return;
    this.setData({ busy: true, error: '', notice: '' });
    try {
      if (!await confirmDialog('撤回隐私同意', '撤回将记录为不同意，不自动删除既有诊疗记录，也不代替手机号或照片的独立撤回。', '撤回')) return;
      await patientApi.setPrivacyConsent(policy.version, 'revoked', patient.id);
      await this.load(); this.setData({ notice: '已记录撤回隐私同意。' });
    } catch (error) { await this.handleError(error); }
    finally { this.setData({ busy: false }); }
  },
  async revokePhone() {
    const patient = this.data.patient;
    if (this.data.busy || !patient) return;
    this.setData({ busy: true });
    try {
      if (!await confirmDialog('撤回账号手机号授权', '撤回后将清除账号手机号。已提交预约的联系人手机号继续用于该次预约联系。', '撤回授权')) return;
      this.setData({ error: '', notice: '' });
      this.accept(await patientApi.revokePhone(patient.version || 1, patient.id));
      this.setData({ notice: '已撤回授权并清除账号手机号。' });
    } catch (error) { await this.handleError(error); }
    finally { this.setData({ busy: false }); }
  },
  async logout() {
    if (this.data.busy) return;
    this.setData({ busy: true, error: '', notice: '' });
    try { await patientApi.logout(); this.generation += 1; this.setData({ patient: null, privacy: null, name: '', notice: '已退出当前设备。重新登录同一微信账号可恢复原资料。' }); }
    catch (error) { await this.handleError(error); }
    finally { this.setData({ busy: false }); }
  },
});
