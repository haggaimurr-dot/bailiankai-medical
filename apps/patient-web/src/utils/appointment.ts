import type { Appointment } from '../patient'
import { clinicTime } from './date'

export const statusInfo = {
  waiting: { label: '候补中', theme: 'warning', hint: '候补不占名额。有空位后会通知你，请在限时内确认，再等待工作人员确认。' },
  pending: { label: '待确认', theme: 'warning', hint: '名额已暂留，请等待工作人员确认。' },
  completed: { label: '已治疗', theme: 'success', hint: '实际治疗已记录，可查看方案并填写随访。' },
  confirmed: { label: '已确认', theme: 'success', hint: '预约已确认，请按约定时间到院。' },
  rejected: { label: '已拒绝', theme: 'danger', hint: '本次申请未获确认，暂留名额已释放。' },
  cancelled: { label: '已取消', theme: 'default', hint: '本次预约或候补已取消。' },
  expired: { label: '已过期', theme: 'default', hint: '申请已超过有效时间，可重新选择时段。' },
} as const

export function dateTime(value: string): string {
  if (!value) return '—'
  const shifted = new Date(Date.parse(value) + 8 * 60 * 60 * 1000)
  const month = String(shifted.getUTCMonth() + 1).padStart(2, '0')
  const day = String(shifted.getUTCDate()).padStart(2, '0')
  return `${shifted.getUTCFullYear()}-${month}-${day} ${clinicTime(value)}`
}

export const cancelReasonLabels: Record<string, string> = {
  schedule_conflict: '时间冲突',
  personal: '个人安排变化',
  found_elsewhere: '已在他处就诊',
  price: '价格原因',
  changed_mind: '改变主意',
  other: '其他',
}

export function cancelReasonLabel(code?: string) {
  if (!code) return '未填写'
  return cancelReasonLabels[code] ?? code
}

export function appointmentView(appointment: Appointment) {
  const info = statusInfo[appointment.status]
  return {
    ...appointment,
    statusLabel: info.label,
    statusTheme: info.theme,
    statusHint: info.hint,
    timeLabel: `${dateTime(appointment.startsAt)} — ${clinicTime(appointment.endsAt)}`,
    deadlineLabel: dateTime(appointment.confirmationDeadline),
    createdLabel: dateTime(appointment.createdAt),
    cancellable: appointment.canCancel && Date.parse(appointment.startsAt) > Date.now(),
  }
}

export type AppointmentView = ReturnType<typeof appointmentView>
