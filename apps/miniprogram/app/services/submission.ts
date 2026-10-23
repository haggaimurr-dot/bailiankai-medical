import { SUBMISSION_STORAGE_KEY } from '../config/storage';
import { requirePatientIdentity, type AppointmentCreate } from './patient';

export interface Submission {
  key: string;
  patientId: string;
  createdAt: number;
  body: AppointmentCreate | null;
  selection: { slotId: string; clinicId: string; doctorId: string; date: string };
}

const CONTACT_TTL = 24 * 60 * 60 * 1000;

export function loadSubmission(): Submission | null {
  const draft = wx.getStorageSync(SUBMISSION_STORAGE_KEY) as Submission | undefined;
  if (!draft?.key || !draft.patientId || !draft.selection?.slotId) return null;
  // 联系资料只在本机短期保留。请求 key 继续保留用于查明在途请求的结果。
  if (Date.now() - draft.createdAt > CONTACT_TTL && draft.body) {
    draft.body = null;
    wx.setStorageSync(SUBMISSION_STORAGE_KEY, draft);
  }
  return draft;
}

export function clearSubmission(key?: string) {
  if (!key) return;
  const draft = wx.getStorageSync(SUBMISSION_STORAGE_KEY) as Submission | undefined;
  if (draft?.key === key) wx.removeStorageSync(SUBMISSION_STORAGE_KEY);
}

export async function saveSubmission(patientId: string, body: AppointmentCreate, selection: Submission['selection']): Promise<Submission> {
  const key = await new Promise<string>((resolve, reject) => {
    wx.getRandomValues({ length: 16, success: ({ randomValues }) => resolve(Array.from(new Uint8Array(randomValues), (byte) => byte.toString(16).padStart(2, '0')).join('')), fail: () => reject(new Error('无法创建安全的申请编号，请重试。')) });
  });
  const draft: Submission = { key, patientId, createdAt: Date.now(), body: { ...body }, selection: { ...selection } };
  // 必须在发起网络写入前保存，保存失败时不发送预约请求。
  requirePatientIdentity(patientId);
  if (loadSubmission()) throw new Error('已有一笔申请等待确认，请先查询原申请结果。');
  wx.setStorageSync(SUBMISSION_STORAGE_KEY, draft);
  return draft;
}
