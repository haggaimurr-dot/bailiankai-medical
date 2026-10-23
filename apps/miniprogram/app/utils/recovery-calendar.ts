import type { FollowupTask } from '../services/patient';

export interface CalendarEvent {
  id: string; taskId: string; appointmentId: string; date: string; time: string;
  kind: 'task' | 'feedback' | 'review'; kindLabel: string; title: string;
  status: FollowupTask['status']; statusLabel: string; assigneeName: string;
  content: string; disposition: string; pending: boolean; overdue: boolean; timestamp: number;
}
export interface CalendarDay {
  date: string; day: number; inMonth: boolean; today: boolean; selected: boolean;
  hasTask: boolean; hasPending: boolean; hasReview: boolean;
}
export interface RecoveryPoint {
  id: string; appointmentId: string; title: string; date: string; severity: number; severityLabel: string;
  symptoms: string; concern: boolean; response: string; disposition: string;
}
export interface RecoverySeries { appointmentId: string; label: string; points: RecoveryPoint[]; }
export function recoverySeries(tasks: FollowupTask[]): RecoverySeries[] {
  const groups = new Map<string, RecoveryPoint[]>();
  const severityLabels = ['没有不适', '轻微不适', '明显不适', '严重不适'];
  for (const task of tasks) {
    if (!task.submittedAt || !Number.isFinite(Date.parse(task.submittedAt)) || task.status === 'pending') continue;
    const points = groups.get(task.appointmentId) || [];
    points.push({ id: task.id, appointmentId: task.appointmentId, title: task.title,
      date: new Date(Date.parse(task.submittedAt) + 8 * 3600000).toISOString().slice(0, 16).replace('T', ' '),
      severity: task.severity, severityLabel: severityLabels[task.severity] || '未记录', symptoms: task.symptoms,
      concern: task.concern, response: task.response, disposition: task.disposition });
    groups.set(task.appointmentId, points);
  }
  return [...groups].map(([appointmentId, points]) => {
    points.sort((a, b) => a.date.localeCompare(b.date) || a.id.localeCompare(b.id));
    return { appointmentId, label: `${points[0].title} · ${points[0].date.slice(0, 10)}起 · ${points.length}次反馈`, points };
  }).sort((a, b) => a.appointmentId.localeCompare(b.appointmentId));
}
const labels: Record<FollowupTask['status'], string> = {
  pending: '待填写反馈', submitted: '等待医护回评', alert: '等待医护处理', reviewed: '医护已回评', handled: '异常已处理',
};
export function beijingDate(value: string | number = Date.now()): string {
  const date = new Date(new Date(value).getTime() + 8 * 3600000);
  return date.toISOString().slice(0, 10);
}
export function calendarEvents(tasks: FollowupTask[]): CalendarEvent[] {
  const events: CalendarEvent[] = [];
  for (const task of tasks) {
    const add = (kind: CalendarEvent['kind'], value: string | null, kindLabel: string, content = '') => {
      if (!value || !Number.isFinite(Date.parse(value))) return;
      const local = new Date(Date.parse(value) + 8 * 3600000).toISOString();
      events.push({ id: `${task.id}-${kind}`, taskId: task.id, appointmentId: task.appointmentId,
        date: local.slice(0, 10), time: local.slice(11, 16), kind, kindLabel, title: task.title,
        status: task.status, statusLabel: labels[task.status], assigneeName: task.assigneeName,
        content, disposition: kind === 'review' ? task.disposition : '',
        pending: kind === 'task' && task.status === 'pending', overdue: kind === 'task' && task.status === 'pending' && task.overdue,
        timestamp: Date.parse(value) });
    };
    add('task', task.dueAt, '随访任务');
    add('feedback', task.submittedAt, '已提交反馈', task.symptoms);
    add('review', task.reviewedAt, '医护回评', task.response);
  }
  return events.sort((a, b) => a.timestamp - b.timestamp || a.id.localeCompare(b.id));
}
export function shiftMonth(month: string, offset: number): string {
  const [year, number] = month.split('-').map(Number);
  return new Date(Date.UTC(year, number - 1 + offset, 1)).toISOString().slice(0, 7);
}
export function calendarDays(month: string, selected: string, events: CalendarEvent[], today = beijingDate()): CalendarDay[] {
  const [year, number] = month.split('-').map(Number);
  const first = new Date(Date.UTC(year, number - 1, 1));
  const start = first.getTime() - ((first.getUTCDay() + 6) % 7) * 86400000;
  const byDate = new Map<string, CalendarEvent[]>();
  for (const event of events) byDate.set(event.date, [...(byDate.get(event.date) || []), event]);
  return Array.from({ length: 42 }, (_, index) => {
    const day = new Date(start + index * 86400000), date = day.toISOString().slice(0, 10), items = byDate.get(date) || [];
    return { date, day: day.getUTCDate(), inMonth: date.slice(0, 7) === month, today: date === today, selected: date === selected,
      hasTask: items.some(item => item.kind === 'task'), hasPending: items.some(item => item.pending), hasReview: items.some(item => item.kind === 'review') };
  });
}
