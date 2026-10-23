import type { components } from '../../../packages/contracts/src/schema'

type SlotCreate = components['schemas']['SlotCreate']
type Setting = components['schemas']['ScheduleSetting']
export const slotCsvHeader = '院区ID,医生ID,日期,开始时间,结束时间,容量,开放状态'

// Accept CSV exported by spreadsheets, including BOM, CRLF and quoted fields.
function csvRows(text: string): string[][] {
  const rows: string[][] = [], row: string[] = []
  let field = '', quoted = false, closed = false
  const source = text.replace(/^\uFEFF/, '')
  const finish = () => { row.push(field.trim()); field = ''; closed = false }
  for (let i = 0; i < source.length; i++) {
    const c = source[i]!
    if (quoted) {
      if (c === '"') {
        if (source[i + 1] === '"') { field += '"'; i++ }
        else { quoted = false; closed = true }
      } else field += c
    } else if (c === ',') finish()
    else if (c === '\n' || c === '\r') {
      if (c === '\r' && source[i + 1] === '\n') i++
      finish(); if (row.some(v => v)) rows.push([...row]); row.length = 0
    } else if (c === '"' && !field && !closed) quoted = true
    else {
      if (closed || c === '"') throw new Error('CSV 引号格式不正确，请重新导出 UTF-8 CSV。')
      field += c
    }
  }
  if (quoted) throw new Error('CSV 引号未闭合，请重新导出文件。')
  finish(); if (row.some(v => v)) rows.push([...row])
  return rows
}

export function validateSlotCsv(text: string, clinicId: string, doctorIds: string[], setting: Setting, now = Date.now()) {
  const rows = csvRows(text)
  if (rows.shift()?.join(',') !== slotCsvHeader) throw new Error(`表头须为：${slotCsvHeader}。`)
  if (!rows.length || rows.length > 500) throw new Error('每次导入须包含 1–500 个时段。')
  const slots: SlotCreate[] = [], errors: { row: number; message: string }[] = []
  rows.forEach((row, index) => {
    const fail = (message: string) => errors.push({ row: index + 2, message })
    if (row.length !== 7) { fail('须包含完整的 7 列。'); return }
    const [clinic, doctor, date, start, end, count, state] = row as [string, string, string, string, string, string, string]
    if (clinic !== clinicId) { fail('院区ID与当前院区不一致。'); return }
    if (!doctorIds.includes(doctor)) { fail('医生ID不属于当前院区的可排班医生。'); return }
    const startsAt = new Date(`${date}T${start}:00+08:00`), endsAt = new Date(`${date}T${end}:00+08:00`)
    const day = new Date(`${date}T00:00:00+08:00`)
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(day.getTime()) || new Date(day.getTime() + 28800000).toISOString().slice(0, 10) !== date) { fail('日期须为有效的 YYYY-MM-DD。'); return }
    if (![start, end].every(v => /^(?:[01]\d|2[0-3]):[0-5]\d$/.test(v)) || endsAt <= startsAt) { fail('时间须为 HH:mm，结束时间须晚于开始时间。'); return }
    if (startsAt.getTime() <= now) { fail('时段已开始，请核对日期。'); return }
    if (!/^\d{1,3}$/.test(count) || Number(count) > 100) { fail('容量须为 0–100 的整数。'); return }
    const status = state === '开放' || state === 'open' ? 'open' : state === '关闭' || state === 'closed' ? 'closed' : null
    if (!status) { fail('开放状态须为开放或关闭。'); return }
    const rest = setting.holidays.find(h => h.date === date && h.kind === 'rest')
    if (status === 'open' && rest) { fail(`与休诊规则“${rest.name}”冲突，请调整日期或设为关闭。`); return }
    const conflict = slots.find(s => s.doctorId === doctor && Date.parse(s.startsAt) < endsAt.getTime() && Date.parse(s.endsAt) > startsAt.getTime())
    if (conflict) { fail('与文件中该医生的其他时段重复或重叠。'); return }
    slots.push({ clinicId: clinic, doctorId: doctor, startsAt: startsAt.toISOString(), endsAt: endsAt.toISOString(), capacity: Number(count), status })
  })
  return { slots, errors }
}
