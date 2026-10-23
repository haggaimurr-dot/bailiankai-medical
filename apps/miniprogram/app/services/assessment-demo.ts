// Local demonstration only. No patient photos, API requests or model calls.
export type AssessmentStatus = 'draft' | 'queued' | 'analyzing' | 'failed' | 'awaiting_review' | 'completed' | 'rejected' | 'cancelled';
export type Decision = 'approved' | 'amended' | 'rejected';
export interface Content { summary: string; observations: string[]; limitations: string[]; advice: string }
export interface Review { decision: Decision; note: string; content: Content | null; doctor: string; at: string; version: number }
export interface Assessment { id: string; clinic: string; status: AssessmentStatus; version: number; createdAt: string; updatedAt: string; photos: string[]; consent: boolean; attempts: number; result: Content | null; reviews: Review[] }
export const statusLabels: Record<AssessmentStatus, string> = { draft: '待补充照片', queued: '等待分析', analyzing: '分析中', failed: '分析失败', awaiting_review: '待医生复核', completed: '复核完成', rejected: '需补拍或面诊', cancelled: '已取消' };
export const angles = ['正面', '左侧', '右侧'];
export function createAssessment(clinic: string): Assessment {
  const now = new Date().toISOString();
  return { id: `demo-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`, clinic, status: 'draft', version: 1, createdAt: now, updatedAt: now, photos: [], consent: false, attempts: 0, result: null, reviews: [] };
}
export function exampleContent(): Content { return { summary: '示例照片中可见局部表面不均与颜色差异。', observations: ['面颊局部表面呈现不均匀的光影变化。', '部分区域可见颜色差异，需结合面诊进一步观察。'], limitations: ['示例内容不来自真实照片或模型分析。', '照片光照与角度会影响观察，无法据此确定诊断。'], advice: '可预约面诊，并准备既往治疗、用药及过敏史信息。' }; }
export function transition(task: Assessment, action: 'submit' | 'start' | 'finish' | 'fail' | 'retry' | 'cancel', expectedVersion = task.version): Assessment {
  if (task.version !== expectedVersion) throw new Error('任务已更新，请刷新后重试。');
  const next = JSON.parse(JSON.stringify(task)) as Assessment;
  const allowed: Record<typeof action, AssessmentStatus[]> = { submit: ['draft'], start: ['queued'], finish: ['analyzing'], fail: ['analyzing'], retry: ['failed'], cancel: ['draft', 'queued', 'analyzing', 'failed', 'awaiting_review', 'completed', 'rejected'] };
  if (!allowed[action].includes(task.status)) throw new Error('当前任务状态不支持此操作。');
  if (action === 'submit' && (!task.consent || !angles.every(a => task.photos.includes(a)))) throw new Error('请补齐三角度示例照片并同意演示用途说明。');
  if (action === 'retry' && task.attempts >= 3) throw new Error('已达到三次尝试上限，请新建任务。');
  if (action === 'submit' || action === 'retry') next.status = 'queued';
  if (action === 'start') { next.status = 'analyzing'; next.attempts++; }
  if (action === 'finish') { next.status = 'awaiting_review'; next.result = exampleContent(); }
  if (action === 'fail') next.status = 'failed';
  if (action === 'cancel') next.status = 'cancelled';
  next.version++; next.updatedAt = new Date().toISOString(); return next;
}
export function reviewAssessment(task: Assessment, decision: Decision, note: string, doctor: string, content: Content | null, version: number): Assessment {
  if (version !== task.version) throw new Error('任务已更新，请刷新后复核。');
  if (!['awaiting_review', 'completed', 'rejected'].includes(task.status) || !task.result) throw new Error('任务尚未形成可复核结果。');
  if (!note.trim() || note.trim().length > 2000) throw new Error('请填写 1–2000 字的复核依据或补拍说明。');
  if (!doctor.trim()) throw new Error('缺少复核医生身份。');
  const raw = decision === 'rejected' ? null : decision === 'approved' ? task.result : content;
  const final = raw ? { ...raw, observations: raw.observations.map(s => s.trim()).filter(Boolean), limitations: raw.limitations.map(s => s.trim()).filter(Boolean) } : null;
  if (decision !== 'rejected' && (!final?.summary.trim() || final.summary.length > 1000 || !final.advice.trim() || final.advice.length > 1000 || !final.limitations.length)) throw new Error('请填写完整的观察概要、局限与面诊建议。');
  const next = JSON.parse(JSON.stringify(task)) as Assessment;
  const at = new Date().toISOString();
  next.reviews.push({ decision, note: note.trim(), doctor, content: final ? JSON.parse(JSON.stringify(final)) : null, at, version: next.reviews.length + 1 });
  next.status = decision === 'rejected' ? 'rejected' : 'completed'; next.version++; next.updatedAt = at; return next;
}
export function patientResult(task: Assessment): Review | null {
  if (!['completed', 'rejected'].includes(task.status)) return null;
  return task.reviews[task.reviews.length - 1] || null;
}
export function demoTasks(): Assessment[] {
  return (['awaiting_review', 'completed', 'failed', 'rejected'] as const).map((status, i) => {
    let task = createAssessment('演示院区'); task.id = `demo-example-${i + 1}`; task.photos = [...angles]; task.consent = true;
    task = transition(transition(task, 'submit'), 'start');
    task = transition(task, status === 'failed' ? 'fail' : 'finish');
    if (status === 'completed' || status === 'rejected') task = reviewAssessment(task, status === 'completed' ? 'approved' : 'rejected', status === 'completed' ? '演示复核：保留观察局限，建议面诊核实。' : '演示补拍说明：请在自然光下重新拍摄，避免遮挡面颊。', '示例医生', null, task.version);
    return task;
  });
}
