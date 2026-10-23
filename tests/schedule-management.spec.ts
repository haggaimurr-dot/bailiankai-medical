import { test, expect, type Page } from '@playwright/test'
import type { components } from '../packages/contracts/src/schema'
import { slotCsvHeader } from '../apps/admin/src/schedule-import'

type Slot = components['schemas']['Slot']
const date = '2099-01-05'
const employee = { id: 'manager-test', username: 'manager', displayName: '测试主管', role: 'manager', capabilities: ['/schedule', '/account'], clinicIds: ['clinic-demo'], staffId: null, active: true, version: 1 }

async function setup(page: Page) {
  await page.addInitScript(employee => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-session', employee })), employee)
  let slots: Slot[] = [{ id: 'slot-test', clinicId: 'clinic-demo', doctorId: 'doctor-demo', startsAt: `${date}T14:00:00+08:00`, endsAt: `${date}T14:30:00+08:00`, capacity: 3, reserved: 1, status: 'open', version: 2 }]
  let setting: components['schemas']['ScheduleSetting'] = { clinicId: 'clinic-demo', templates: [], holidays: [], version: 0 }
  const writes: { path: string; body: any }[] = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url())
    let data: unknown = []
    let status = 200
    if (url.pathname.endsWith('/admin/session')) data = employee
    else if (url.pathname.includes('/schedule/settings/')) {
      if (request.method() === 'PUT') { const body = request.postDataJSON(); writes.push({ path: url.pathname, body }); setting = { ...body, clinicId: 'clinic-demo', version: setting.version + 1 } }
      data = setting
    }
    else if (url.pathname.endsWith('/clinics')) data = [{ id: 'clinic-demo', name: '测试院区', address: '测试地址', phone: '021-12345678' }]
    else if (url.pathname.endsWith('/doctors')) data = [{ id: 'doctor-demo', clinicId: 'clinic-demo', name: '测试医生', title: '医生', summary: '', focus: [], initials: '医' }]
    else if (request.method() === 'POST') {
      const body = request.postDataJSON()
      writes.push({ path: url.pathname, body })
      if (url.pathname.endsWith('/suspend')) slots = slots.map(s => body.slots.some((ref: { id: string }) => ref.id === s.id) ? { ...s, status: 'closed', version: s.version + 1 } : s)
      else {
        const created = body.slots.map((s: Slot, i: number) => ({ ...s, id: `new-${i}`, reserved: 0, version: 1 }))
        slots = [...slots, ...created]
        data = created
        status = 201
      }
    } else if (url.pathname.endsWith('/slots')) data = slots.filter(s => !url.searchParams.get('date') || s.startsAt.slice(0, 10) === url.searchParams.get('date'))
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
  await page.goto('/schedule')
  await expect(page.getByRole('button', { name: '编辑排班' }).first()).toBeVisible()
  return writes
}

test('单个创建及按星期批量生成使用北京时间，显示创建预览', async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole('button', { name: '创建排班', exact: true }).click()
  let dialog = page.getByRole('dialog', { name: '创建排班', exact: true })
  await expect(dialog.getByText('预览：将创建 1 个时段', { exact: false })).toBeVisible()
  await dialog.getByRole('button', { name: '确认创建' }).click()
  await expect(dialog).not.toBeVisible()
  expect(writes[0]!.body.slots).toHaveLength(1)
  expect(writes[0]!.body.slots[0].startsAt).toBe(`${date}T01:00:00.000Z`)
  await page.getByRole('button', { name: '批量生成', exact: true }).click()
  dialog = page.getByRole('dialog', { name: '批量生成排班' })
  await dialog.getByPlaceholder('结束日期').fill('2099-01-07')
  await dialog.getByPlaceholder('结束日期').press('Enter')
  await dialog.getByText('重复星期', { exact: true }).click()
  // Jan 5–7, 2099 are Monday–Wednesday: six half-hour slots per day.
  await expect(dialog.getByText('预览：将创建 18 个时段', { exact: false })).toBeVisible()
  await dialog.getByRole('button', { name: '确认创建' }).click()
  await expect(dialog).not.toBeVisible()
  expect(writes[1]!.body.slots).toHaveLength(18)
  expect(writes[1]!.body.slots[0].endsAt).toBe(`${date}T01:30:00.000Z`)
})

test('停诊需要原因，提交版本并显示受影响名额', async ({ page }) => {
  const writes = await setup(page)
  await page.locator('.el-table__body .el-checkbox').first().click()
  await page.getByRole('button', { name: '停诊处理（1）' }).click()
  const dialog = page.getByRole('dialog', { name: '停诊处理', exact: true })
  await expect(dialog.getByText('将关闭 1 个时段，涉及 1 个已占用名额。', { exact: false })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '确认停诊' })).toBeDisabled()
  await dialog.getByRole('textbox', { name: '停诊原因' }).fill('医生临时请假')
  await dialog.getByRole('button', { name: '确认停诊' }).click()
  await expect(dialog).not.toBeVisible()
  expect(writes[0]!.body).toEqual({ slots: [{ id: 'slot-test', version: 2 }], reason: '医生临时请假' })
  await expect(page.locator('.schedule-table .el-table__body tbody tr').first()).toContainText('已关闭')
  await expect(page.locator('.schedule-table .el-table__body tbody tr').first()).toContainText('1 / 3')
})

test('重叠响应保留输入并提示整批未创建', async ({ page }) => {
  await setup(page)
  await page.route('**/api/v1/admin/slots', route => route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'SLOT_OVERLAP', message: '时段重复或重叠，整批未创建，请调整后重试' } }) }))
  await page.getByRole('button', { name: '创建排班', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '创建排班', exact: true })
  await dialog.getByRole('button', { name: '确认创建' }).click()
  await expect(dialog.getByText('时段重复或重叠，整批未创建，请调整后重试')).toBeVisible()
  await expect(dialog.getByRole('spinbutton', { name: '每时段容量' })).toHaveValue('3')
})

test('排班模板保存并应用，休诊和补班规则联动生成预览', async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole('button', { name: '新增模板', exact: true }).click()
  let dialog = page.getByRole('dialog', { name: '排班模板', exact: true })
  await dialog.getByRole('textbox', { name: '模板名称' }).fill('医生上午班')
  await dialog.getByRole('button', { name: '保存规则' }).click()
  await expect(dialog).not.toBeVisible()
  expect(writes[0]!.body.templates[0].name).toBe('医生上午班')
  await page.getByRole('tab', { name: '节假日规则' }).click()
  await page.getByRole('button', { name: '新增日期规则' }).click()
  dialog = page.getByRole('dialog', { name: '节假日规则', exact: true })
  await dialog.getByRole('textbox', { name: '规则名称' }).fill('院区休诊')
  await dialog.getByRole('button', { name: '保存规则' }).click()
  await expect(dialog).not.toBeVisible()
  await page.getByRole('button', { name: '新增日期规则' }).click()
  dialog = page.getByRole('dialog', { name: '节假日规则', exact: true })
  await dialog.getByRole('textbox', { name: '规则名称' }).fill('周末补班')
  const dateInput = dialog.getByRole('combobox', { name: '规则日期' })
  await dateInput.fill('2099-01-10'); await dateInput.press('Enter')
  await dialog.getByText('补班日', { exact: true }).click()
  await dialog.getByRole('button', { name: '保存规则' }).click()
  await expect(dialog).not.toBeVisible()
  await page.getByRole('button', { name: '批量生成', exact: true }).click()
  dialog = page.getByRole('dialog', { name: '批量生成排班' })
  await dialog.getByRole('combobox', { name: '使用排班模板' }).press('Enter')
  await page.getByRole('option', { name: '医生上午班' }).click()
  await dialog.getByPlaceholder('结束日期').fill('2099-01-10'); await dialog.getByPlaceholder('结束日期').press('Enter')
  await dialog.getByText('重复星期', { exact: true }).click()
  // Mon rest skipped, Tue–Fri plus Saturday work: five days * six slots.
  await expect(dialog.getByText('预览：将创建 30 个时段', { exact: false })).toBeVisible()
  await dialog.getByRole('button', { name: '确认创建' }).click()
  await expect(dialog).not.toBeVisible()
  const slots = writes.at(-1)!.body.slots
  expect(slots).toHaveLength(30)
  expect(slots.some((s: Slot) => s.startsAt.startsWith('2099-01-05'))).toBe(false)
  expect(slots.some((s: Slot) => s.startsAt.startsWith('2099-01-10'))).toBe(true)
})

test('受影响患者联系记录持久化，改期后自动归入已处理', async ({ page }) => {
  await setup(page)
  let task: components['schemas']['SchedulePatientTask'] = { id: 'task-test', appointmentId: 'appointment-test', slotId: 'slot-test', reason: '临时停诊', slotStatus: 'closed', contactStatus: 'todo', note: '', version: 1, updatedBy: 'manager-test', createdAt: `${date}T00:00:00+08:00`, updatedAt: `${date}T00:00:00+08:00`, contactName: '测试患者', contactPhone: '13800000000', appointmentStatus: 'confirmed', appointmentVersion: 2, currentSlotId: 'slot-test', doctorId: 'doctor-demo', startsAt: `${date}T14:00:00+08:00`, endsAt: `${date}T14:30:00+08:00` }
  let moved: any
  await page.route('**/api/v1/admin/schedule/patients**', async route => {
    const req = route.request()
    if (req.method() === 'PUT') { task = { ...task, ...req.postDataJSON(), version: task.version + 1 } }
    if (req.method() === 'POST') { moved = req.postDataJSON(); expect(req.headers()['idempotency-key']).toBeTruthy(); task = { ...task, currentSlotId: moved.slotId, appointmentStatus: 'pending', appointmentVersion: 3 } }
    await route.fulfill({ json: { data: req.method() === 'GET' ? [task] : { saved: true } } })
  })
  await page.route('**/api/v1/slots?**', route => route.fulfill({ json: { data: [{ id: 'slot-target', clinicId: 'clinic-demo', doctorId: 'doctor-demo', startsAt: `${date}T15:00:00+08:00`, endsAt: `${date}T15:30:00+08:00`, capacity: 3, reserved: 1, version: 1, status: 'open' }] } }))
  await page.getByRole('button', { name: '刷新清单' }).click()
  await page.getByRole('button', { name: '联系记录', exact: true }).click()
  let dialog = page.getByRole('dialog', { name: '联系记录', exact: true })
  await dialog.getByText('已联系', { exact: true }).click()
  await dialog.getByRole('textbox', { name: '联系备注' }).fill('患者同意下午三点就诊')
  await dialog.getByRole('button', { name: '保存联系记录' }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.locator('.patients-panel')).toContainText('患者同意下午三点就诊')
  await page.getByRole('button', { name: '安排改期', exact: true }).click()
  dialog = page.getByRole('dialog', { name: '安排改期', exact: true })
  await dialog.getByRole('combobox', { name: '可改期时段' }).press('Enter')
  await page.getByRole('option', { name: '15:00–15:30 · 剩余 2 个' }).click()
  await dialog.getByRole('button', { name: '确认改期' }).click()
  await expect(dialog).not.toBeVisible()
  expect(moved).toEqual({ slotId: 'slot-target', version: 2 })
  await page.locator('.patients-panel').getByText('已处理', { exact: true }).click()
  await expect(page.locator('.patients-panel')).toContainText('已改期')
})

test('院方正式号源导入校验后提交，接口冲突保留文件和预览', async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole('button', { name: '导入院方号源', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '导入院方正式号源' })
  await expect(dialog.getByRole('button', { name: '确认导入' })).toBeDisabled()
  const file = dialog.getByLabel('院方号源文件')
  await file.setInputFiles({ name: '院方.csv', mimeType: 'text/csv', buffer: Buffer.from(`${slotCsvHeader}\nclinic-other,doctor-demo,${date},09:00,09:30,3,开放`) })
  await expect(dialog.getByText('院区ID与当前院区不一致。')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '确认导入' })).toBeDisabled()
  await file.setInputFiles({ name: '院方.csv', mimeType: 'text/csv', buffer: Buffer.from(`${slotCsvHeader}\nclinic-demo,doctor-demo,${date},09:00,09:30,3,开放\nclinic-demo,doctor-demo,${date},09:30,10:00,2,关闭`) })
  await expect(dialog.getByText('校验通过：2 个时段', { exact: false })).toBeVisible()
  await page.route('**/api/v1/admin/slots', route => route.fulfill({ status: 409, json: { error: { code: 'SLOT_OVERLAP', message: '时段重叠，整批未创建' } } }))
  await dialog.getByRole('button', { name: '确认导入' }).click()
  await expect(dialog.getByText('时段重叠，整批未创建')).toBeVisible()
  await expect(dialog.getByText('校验通过：2 个时段', { exact: false })).toBeVisible()
  await page.unroute('**/api/v1/admin/slots')
  await dialog.getByRole('button', { name: '确认导入' }).click()
  await expect(dialog).not.toBeVisible()
  expect(writes.at(-1)!.body.slots).toEqual([
    { clinicId: 'clinic-demo', doctorId: 'doctor-demo', startsAt: `${date}T01:00:00.000Z`, endsAt: `${date}T01:30:00.000Z`, capacity: 3, status: 'open' },
    { clinicId: 'clinic-demo', doctorId: 'doctor-demo', startsAt: `${date}T01:30:00.000Z`, endsAt: `${date}T02:00:00.000Z`, capacity: 2, status: 'closed' },
  ])
})

test('连续节假日整批保存，已有日期冲突时禁止覆盖', async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole('tab', { name: '节假日规则' }).click()
  await page.getByRole('button', { name: '新增日期规则' }).click()
  let dialog = page.getByRole('dialog', { name: '节假日规则', exact: true })
  await dialog.getByRole('textbox', { name: '规则名称' }).fill('院方假期')
  await dialog.getByText('连续日期', { exact: true }).click()
  await dialog.getByPlaceholder('规则结束日期').fill('2099-01-07')
  await dialog.getByPlaceholder('规则结束日期').press('Enter')
  await dialog.getByRole('textbox', { name: '规则名称' }).click()
  await expect(dialog.getByText('将新增 3 条日期规则', { exact: false })).toBeVisible()
  await dialog.getByRole('button', { name: '保存规则' }).click()
  await expect(dialog).not.toBeVisible()
  expect(writes[0]!.body.holidays.map((h: any) => h.date)).toEqual(['2099-01-05', '2099-01-06', '2099-01-07'])
  await page.getByRole('button', { name: '新增日期规则' }).click()
  dialog = page.getByRole('dialog', { name: '节假日规则', exact: true })
  await dialog.getByRole('textbox', { name: '规则名称' }).fill('重复假期')
  await expect(dialog.getByText('所选日期已有规则，请调整范围或编辑已有规则。')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '保存规则' })).toBeDisabled()
})

test('院方号源提交前读取最新休诊规则，规则变化时阻止写入', async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole('button', { name: '导入院方号源', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '导入院方正式号源' })
  await dialog.getByLabel('院方号源文件').setInputFiles({ name: '号源.csv', mimeType: 'text/csv', buffer: Buffer.from(`${slotCsvHeader}\nclinic-demo,doctor-demo,${date},09:00,09:30,3,开放`) })
  await expect(dialog.getByText('校验通过：1 个时段', { exact: false })).toBeVisible()
  await page.route('**/api/v1/admin/schedule/settings/clinic-demo', route => route.fulfill({ json: { data: { clinicId: 'clinic-demo', templates: [], holidays: [{ date, name: '新增休诊', kind: 'rest' }], version: 1 } } }))
  await dialog.getByRole('button', { name: '确认导入' }).click()
  await expect(dialog.getByText('与休诊规则“新增休诊”冲突，请调整日期或设为关闭。')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '确认导入' })).toBeDisabled()
  expect(writes).toHaveLength(0)
})
