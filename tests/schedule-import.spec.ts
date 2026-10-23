import { test, expect } from '@playwright/test'
import { slotCsvHeader, validateSlotCsv } from '../apps/admin/src/schedule-import'

const setting = { clinicId: 'clinic', templates: [], holidays: [{ date: '2099-01-06', name: '院区休诊', kind: 'rest' as const }], version: 1 }
const validate = (rows: string) => validateSlotCsv(`\uFEFF${slotCsvHeader}\r\n${rows}`, 'clinic', ['doctor'], setting, Date.parse('2099-01-01T00:00:00+08:00'))

test('院方 CSV 支持表格引号、相邻时段和关闭的休诊号源，转换北京时间', () => {
  const result = validate('"clinic","doctor",2099-01-05,09:00,09:30,3,开放\r\nclinic,doctor,2099-01-05,09:30,10:00,0,关闭\r\nclinic,doctor,2099-01-06,09:00,09:30,2,closed\r\n')
  expect(result.errors).toEqual([])
  expect(result.slots).toHaveLength(3)
  expect(result.slots[0]!.startsAt).toBe('2099-01-05T01:00:00.000Z')
  expect(result.slots[2]!.status).toBe('closed')
})

test('院方 CSV 拒绝跨院区、错医生、无效日期、已开始时段和休诊冲突并定位数据行', () => {
  const result = validate('other,doctor,2099-01-05,09:00,09:30,3,开放\nclinic,other,2099-01-05,09:00,09:30,3,开放\nclinic,doctor,2099-02-30,09:00,09:30,3,开放\nclinic,doctor,2098-01-05,09:00,09:30,3,开放\nclinic,doctor,2099-01-06,09:00,09:30,3,开放')
  expect(result.slots).toHaveLength(0)
  expect(result.errors.map(e => e.row)).toEqual([2, 3, 4, 5, 6])
  expect(result.errors[4]!.message).toContain('院区休诊')
})

test('院方 CSV 拒绝重复或重叠、无效容量和时间、损坏的表头或引号', () => {
  const result = validate('clinic,doctor,2099-01-05,09:00,10:00,3,开放\nclinic,doctor,2099-01-05,09:30,10:30,3,关闭\nclinic,doctor,2099-01-05,11:00,12:00,1.5,开放\nclinic,doctor,2099-01-05,25:00,26:00,3,开放')
  expect(result.errors).toHaveLength(3)
  expect(result.errors[0]!.message).toContain('重叠')
  expect(() => validate('"clinic,doctor')).toThrow('引号未闭合')
  expect(() => validateSlotCsv('错误表头\nabc', 'clinic', ['doctor'], setting)).toThrow('表头')
  expect(() => validate(Array(501).fill('clinic,doctor,2099-01-05,09:00,09:30,3,开放').join('\n'))).toThrow('1–500')
})
