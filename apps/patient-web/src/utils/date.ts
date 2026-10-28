const SHANGHAI_OFFSET_MS = 8 * 60 * 60 * 1000
const WEEKDAYS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']
const pad = (value: number) => String(value).padStart(2, '0')

export interface DayOption {
  date: string
  label: string
  shortDate: string
}

// 院区排班统一按北京时间展示，不受设备时区影响。
export function upcomingDays(count = 7): DayOption[] {
  const today = new Date(Date.now() + SHANGHAI_OFFSET_MS)
  const start = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate())
  return Array.from({ length: count }, (_, index) => {
    const day = new Date(start + index * 24 * 60 * 60 * 1000)
    return {
      date: `${day.getUTCFullYear()}-${pad(day.getUTCMonth() + 1)}-${pad(day.getUTCDate())}`,
      label: index === 0 ? '今天' : index === 1 ? '明天' : WEEKDAYS[day.getUTCDay()],
      shortDate: `${pad(day.getUTCMonth() + 1)}/${pad(day.getUTCDate())}`,
    }
  })
}

export function clinicTime(isoTime: string): string {
  const time = new Date(new Date(isoTime).getTime() + SHANGHAI_OFFSET_MS)
  return `${pad(time.getUTCHours())}:${pad(time.getUTCMinutes())}`
}

export function shanghaiDate(offsetDays = 0): string {
  return upcomingDays(offsetDays + 1)[offsetDays].date
}

/** 倒计时文案：秒数转 m:ss，负数按 0 计。 */
export function countdownLabel(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds))
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
}
