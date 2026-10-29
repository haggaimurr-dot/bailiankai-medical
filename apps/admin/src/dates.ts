export function shanghaiDate(value = new Date()): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(value)
}

export function slotTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value))
}

export interface ExpiringCredential { name: string; expiresAt: string; daysLeft: number }

// 新增-P1–P2：资质到期提醒。返回 windowDays 天内到期（含已过期）的资质，
// 按到期先后排序；未填有效期的资质视为长期有效，不提醒。
export function expiringCredentials(credentials: { name?: string; expiresAt?: string | null }[] | undefined, windowDays = 30): ExpiringCredential[] {
  if (!credentials) return []
  const today = new Date(`${shanghaiDate()}T12:00:00+08:00`).getTime()
  const out: ExpiringCredential[] = []
  for (const row of credentials) {
    if (!row.expiresAt) continue
    const expiry = new Date(`${row.expiresAt}T12:00:00+08:00`).getTime()
    if (Number.isNaN(expiry)) continue
    const daysLeft = Math.round((expiry - today) / 86400000)
    if (daysLeft <= windowDays) out.push({ name: row.name || '资质', expiresAt: row.expiresAt, daysLeft })
  }
  return out.sort((a, b) => a.daysLeft - b.daysLeft)
}

export function expiryText(item: ExpiringCredential): string {
  return item.daysLeft < 0 ? `已过期 ${-item.daysLeft} 天` : item.daysLeft === 0 ? '今天到期' : `${item.daysLeft} 天后到期`
}

export function displayDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', month: 'long', day: 'numeric', weekday: 'long' }).format(new Date(`${value}T12:00:00+08:00`))
}
