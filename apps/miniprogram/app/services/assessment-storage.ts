import { cachedSession } from './patient';
import { demoTasks, type Assessment } from './assessment-demo';
export function assessmentScope(): string { return cachedSession()?.patient.id || 'demo-visitor'; }
const key = () => `assessment-demo-v1:${assessmentScope()}`;
export function readAssessments(): Assessment[] {
  const value: unknown = wx.getStorageSync(key());
  if (!value) { const tasks = demoTasks(); wx.setStorageSync(key(), tasks); return tasks; }
  if (!Array.isArray(value)) throw new Error('演示记录读取失败。');
  return value as Assessment[];
}
export function saveAssessment(task: Assessment): void { wx.setStorageSync(key(), [task, ...readAssessments().filter(t => t.id !== task.id)]); }
export function resetAssessments(): void { wx.setStorageSync(key(), demoTasks()); }
