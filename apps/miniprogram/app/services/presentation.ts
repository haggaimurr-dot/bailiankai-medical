import type { components } from '../../../../packages/contracts/src/schema';
import type { Clinic, Doctor } from './api';
import { ENABLE_HOME_MOCK } from '../config/presentation';

export type ClinicPresentation = components['schemas']['ClinicPresentation'];
export type Credential = components['schemas']['Credential'];

// Only the existing demo IDs receive mock material. Published presentation wins.
export function clinicPresentation(clinic: Clinic): ClinicPresentation {
  if (clinic.presentation) return { ...clinic.presentation, credentials: clinic.presentation.credentials || [] };
  if (ENABLE_HOME_MOCK && clinic.id === 'clinic-demo') return {
    isDemo: true, brandName: '百莲凯医疗美容', tagline: '用心沟通 · 陪伴每一步', logoUrl: '',
    introduction: '专注皮肤面诊、个体方案沟通与恢复随访。本段为品牌介绍示例，正式服务范围和机构信息以院方核验资料为准。',
    hours: '09:00–18:00（演示）',
    transport: '地图使用上海市中心示例坐标，演示公共交通与停车说明；此处并非真实院址。',
    arrivalGuide: '请先在“我的预约”确认预约结果和时段。到院后向前台出示预约信息，再按工作人员指引候诊。',
    location: { latitude: 31.2304, longitude: 121.4737 },
    credentials: [{ name: '医疗机构执业许可证（样例）', number: 'MOCK-CLINIC-001', issuer: '演示发证机构', imageUrl: '' }],
  };
  return { isDemo: true, brandName: clinic.name, tagline: '', introduction: '', logoUrl: '', hours: '', transport: '', arrivalGuide: '', credentials: [] };
}

export function doctorPresentation(doctor: Doctor) {
  return doctor.presentation ? { ...doctor.presentation, credentials: doctor.presentation.credentials || [] } : {
    isDemo: true,
    credentials: ENABLE_HOME_MOCK && /^doctor-demo-[12]$/.test(doctor.id) ? [{ name: '医师执业证书（样例）', number: `MOCK-${doctor.id}`, issuer: '演示发证机构', imageUrl: '' }] : [],
  };
}

export function validLocation(location?: ClinicPresentation['location']): location is NonNullable<ClinicPresentation['location']> {
  return !!location && Number.isFinite(location.latitude) && Number.isFinite(location.longitude) &&
    Math.abs(location.latitude) <= 90 && Math.abs(location.longitude) <= 180;
}

export function navigateClinic(clinicId: string) {
  wx.navigateTo({ url: `/pages/clinic/index?clinicId=${encodeURIComponent(clinicId)}` });
}

export function previewCredential(credentials: Credential[], index: number) {
  const current = credentials[index]?.imageUrl;
  if (!current) return;
  wx.previewImage({ current, urls: credentials.map(row => row.imageUrl).filter(Boolean), fail: () => wx.showToast({ title: '证照图片暂时无法打开', icon: 'none' }) });
}
