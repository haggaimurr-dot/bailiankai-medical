import { test, expect, type Page } from '@playwright/test'

test.use({ timezoneId: 'America/Los_Angeles' })

const now = new Date('2026-10-04T11:20:30+08:00')
const employee = { id: 'arrival-employee', username: 'arrival-manager', displayName: '到诊登记员', role: 'manager', clinicIds: ['clinic-demo'], staffId: null, active: true, version: 1, capabilities: ['/appointments', '/arrivals', '/account'] }
type MockAppointment = {
  id: string; patientId: string; slotId: string; doctorId: string; doctorName: string; clinicId: string; clinicName: string
  startsAt: string; endsAt: string; status: 'pending' | 'confirmed' | 'completed'; createdAt: string; updatedAt: string
  confirmationDeadline: string; version: number; contactName: string; contactPhone: string; note: string
  canReview: boolean; canCancel: boolean; canRegisterArrival: boolean; arrivedAt: string | null
}

function appointment(overrides: Partial<MockAppointment> = {}): MockAppointment {
  return { id: 'arrival-appointment', patientId: 'arrival-patient', slotId: 'arrival-slot', doctorId: 'doctor-demo-1', doctorName: '演示医生', clinicId: 'clinic-demo', clinicName: '演示院区', startsAt: '2026-10-04T12:00:00+08:00', endsAt: '2026-10-04T12:30:00+08:00', status: 'confirmed', createdAt: '2026-10-04T09:00:00+08:00', updatedAt: '2026-10-04T09:10:00+08:00', confirmationDeadline: '2026-10-04T11:00:00+08:00', version: 2, contactName: '虚构患者', contactPhone: '13800000000', note: '', canReview: false, canCancel: false, canRegisterArrival: true, arrivedAt: null, ...overrides }
}

async function setup(page: Page, options: { role?: string; initial?: Partial<MockAppointment>; mode?: 'normal' | 'lost-response' | 'conflict' | 'slow' } = {}) {
  await page.clock.install({ time: now })
  await page.clock.setFixedTime(now)
  const role = options.role || employee.role
  const account = { ...employee, role, capabilities: ['/appointments', '/account', ...(['manager', 'doctor', 'clinician', 'customer_service'].includes(role) ? ['appointment.review'] : [])] }
  let current = appointment(options.initial)
  const submissions: { version: number; arrivedAt: string }[] = []
  let releaseArrival = () => {}
  const arrivalDelay = options.mode === 'slow' ? new Promise<void>(resolve => { releaseArrival = resolve }) : Promise.resolve()
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'arrival-test-token', employee: value })), account)
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname.endsWith('/admin/session')) return route.fulfill({ json: { data: account } })
    if (url.pathname.endsWith('/arrival')) {
      const submitted = request.postDataJSON() as { version: number; arrivedAt: string }
      submissions.push(submitted)
      const sameInstant = current.arrivedAt && new Date(current.arrivedAt).getTime() === new Date(submitted.arrivedAt).getTime()
      if (options.mode === 'conflict') {
        current = { ...current, version: 3, arrivedAt: '2026-10-04T11:10:00+08:00', canRegisterArrival: false }
        return route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '预约记录已变化。' } } })
      }
      if (!sameInstant) current = { ...current, version: current.version + 1, arrivedAt: submitted.arrivedAt, canRegisterArrival: false }
      if (options.mode === 'lost-response' && submissions.length === 1) return route.abort('failed')
      await arrivalDelay
      return route.fulfill({ json: { data: current } })
    }
    if (url.pathname.endsWith('/review')) {
      current = { ...current, version: 2, status: 'confirmed', canReview: false, canRegisterArrival: true }
      return route.fulfill({ json: { data: current } })
    }
    if (url.pathname.endsWith('/admin/appointments/' + current.id)) return route.fulfill({ json: { data: current } })
    if (url.pathname.endsWith('/admin/appointments')) {
      const rows = !url.searchParams.has('status') || url.searchParams.get('status') === current.status ? [current] : []
      return route.fulfill({ json: { data: rows, meta: { total: rows.length, page: 1, pageSize: Number(url.searchParams.get('pageSize') || 20) } } })
    }
    if (url.pathname.endsWith('/clinics')) return route.fulfill({ json: { data: [{ id: 'clinic-demo', name: '演示院区' }] } })
    if (url.pathname.endsWith('/doctors')) return route.fulfill({ json: { data: [{ id: 'doctor-demo-1', clinicId: 'clinic-demo', name: '演示医生' }] } })
    return route.fulfill({ json: { data: [] } })
  })
  await page.goto('/appointments')
  await page.getByTestId('appointment-detail-arrival-appointment').click()
  const dialog = page.getByRole('dialog', { name: '预约详情' })
  await expect(dialog).toBeVisible()
  return { dialog, submissions, current: () => current, releaseArrival }
}

test('确认预约不会自动登记到诊，独立登记使用北京时间并更新列表', async ({ page }) => {
  const { dialog, submissions } = await setup(page, { initial: { status: 'pending', canReview: true, canRegisterArrival: false, confirmationDeadline: '2026-10-04T12:00:00+08:00', version: 1 } })
  await expect(dialog.getByTestId('register-arrival')).toHaveCount(0)
  await dialog.getByRole('button', { name: '确认预约', exact: true }).click()
  await expect(dialog.getByTestId('arrival-not-recorded')).toHaveText('尚未登记到诊')
  expect(submissions).toHaveLength(0)
  await expect(dialog.getByPlaceholder('选择实际到诊时间')).toHaveValue('2026-10-04 11:20:30')
  const timeInput = dialog.getByPlaceholder('选择实际到诊时间')
  await timeInput.fill('2026-10-04 10:45:00')
  await timeInput.press('Enter')
  await dialog.getByTestId('register-arrival').click()
  await expect(dialog.getByTestId('arrival-recorded-time')).toContainText('2026-10-04 10:45')
  expect(submissions).toEqual([{ version: 2, arrivedAt: '2026-10-04T02:45:00.000Z' }])
  await expect(dialog.getByTestId('register-arrival')).toHaveCount(0)
  await dialog.getByRole('button', { name: '关闭详情', exact: true }).click()
  await expect(page.getByTestId('arrival-status-arrival-appointment')).toContainText('已登记到诊')
})

test('治疗记录仍需独立补录到诊，服务端能力和角色共同限制按钮', async ({ page }) => {
  const { dialog } = await setup(page, { role: 'nurse', initial: { status: 'completed', canRegisterArrival: true } })
  await expect(dialog.getByTestId('arrival-not-recorded')).toBeVisible()
  await expect(dialog.getByText('预约确认、治疗记录不会自动计为到诊', { exact: false })).toBeVisible()
  await expect(dialog.getByTestId('register-arrival')).toHaveCount(0)
})

test('服务端未授予到诊能力时管理员无法提交', async ({ page }) => {
  const { dialog } = await setup(page, { initial: { canRegisterArrival: false } })
  await expect(dialog.getByTestId('register-arrival')).toHaveCount(0)
})

test('保存成功但响应丢失后，重试复用同一时间与版本', async ({ page }) => {
  const { dialog, submissions, current } = await setup(page, { mode: 'lost-response' })
  await dialog.getByTestId('register-arrival').click()
  await expect(dialog.getByTestId('register-arrival')).toHaveText('重试到诊登记')
  await expect(dialog.getByPlaceholder('选择实际到诊时间')).toBeDisabled()
  await page.clock.setFixedTime(new Date(now.getTime() + 5 * 60_000))
  await dialog.getByTestId('register-arrival').click()
  await expect(dialog.getByTestId('arrival-recorded-time')).toBeVisible()
  expect(submissions).toHaveLength(2)
  expect(submissions[1]).toEqual(submissions[0])
  expect(current().version).toBe(3)
})

test('并发冲突自动刷新最新到诊记录', async ({ page }) => {
  const { dialog, submissions } = await setup(page, { mode: 'conflict' })
  await dialog.getByTestId('register-arrival').click()
  await expect(dialog.getByText('预约记录已变化，已载入最新详情。请核对到诊记录后再处理。')).toBeVisible()
  await expect(dialog.getByTestId('arrival-recorded-time')).toContainText('11:10')
  await expect(dialog.getByTestId('register-arrival')).toHaveCount(0)
  expect(submissions).toHaveLength(1)
})

test('连续点击期间禁用提交并只发送一次到诊登记', async ({ page }) => {
  const { dialog, submissions, releaseArrival } = await setup(page, { mode: 'slow' })
  try {
    await dialog.getByTestId('register-arrival').evaluate(element => {
      const button = element as HTMLButtonElement
      button.click()
      button.click()
    })
    await expect(dialog.getByTestId('register-arrival')).toBeDisabled()
    await expect.poll(() => submissions.length).toBe(1)
  } finally {
    // Always release the intercepted request so a failed assertion can close
    // its browser context without leaving a route handler awaiting forever.
    releaseArrival()
  }
  await expect(dialog.getByTestId('arrival-recorded-time')).toBeVisible()
  expect(submissions).toHaveLength(1)
})
