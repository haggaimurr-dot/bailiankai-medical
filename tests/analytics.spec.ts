import { expect, test, type Page, type Route } from '@playwright/test'
import type { components } from '../packages/contracts/src/schema'

type Report = components['schemas']['OperationsAnalyticsReport']
type Metrics = components['schemas']['OperationsAnalyticsMetrics']
const employee = { id: 'operations-test', username: 'operations', displayName: '测试运营', role: 'operations', clinicIds: ['clinic-east', 'clinic-west'], staffId: null, active: true, version: 1, capabilities: ['/overview', '/account'] }
const metrics: Metrics = { appointmentCount: 7, pendingAppointmentCount: 2, confirmedAppointmentCount: 4, arrivedAppointmentCount: 1, treatmentCount: 3, followupCount: 5, followupCompletedCount: 2, followupOverdueCount: 2, alertCount: 2, alertHandledCount: 1, pendingAlertCount: 1, arrivalRate: 25, followupCompletionRate: 40, alertHandlingRate: 50 }
const emptyMetrics: Metrics = { appointmentCount: 0, pendingAppointmentCount: 0, confirmedAppointmentCount: 0, arrivedAppointmentCount: 0, treatmentCount: 0, followupCount: 0, followupCompletedCount: 0, followupOverdueCount: 0, alertCount: 0, alertHandledCount: 0, pendingAlertCount: 0, arrivalRate: null, followupCompletionRate: null, alertHandlingRate: null }
const clinics = [{ id: 'clinic-east', name: '东院区' }, { id: 'clinic-west', name: '西院区' }]
const doctors = [{ id: 'doctor-east', name: '东院医生', clinicId: 'clinic-east' }, { id: 'doctor-west', name: '西院医生', clinicId: 'clinic-west' }]

function reportFor(url: URL, overrides: Partial<Report> = {}): Report {
  const groupBy = (url.searchParams.get('groupBy') || 'doctor') as Report['groupBy']
  const doctor = doctors.find(doctor => doctor.id === url.searchParams.get('doctorId')) || doctors[0]!
  const clinic = clinics.find(clinic => clinic.id === url.searchParams.get('clinicId')) || clinics[0]!
  return {
    metricVersion: 'v1', timezone: 'Asia/Shanghai', generatedAt: '2026-10-04T02:00:00.000Z',
    dateFrom: url.searchParams.get('dateFrom')!, dateTo: url.searchParams.get('dateTo')!, groupBy,
    summary: metrics, rows: [{ key: 'test-row', clinicId: groupBy === 'day' ? null : clinic.id, clinicName: groupBy === 'day' ? null : clinic.name, doctorId: groupBy === 'doctor' ? doctor.id : null, doctorName: groupBy === 'doctor' ? doctor.name : null, date: groupBy === 'day' ? url.searchParams.get('dateFrom')! : null, metrics }], clinics, doctors, ...overrides,
  }
}
async function fulfill(route: Route, data: Report) { await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ data }) }) }
async function setup(page: Page, initial: Partial<Report> = {}) {
  await page.clock.install({ time: new Date('2026-10-03T18:05:00.000Z') })
  await page.addInitScript(employee => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-session', employee })), employee)
  const requests: URL[] = [], publicRequests: string[] = []
  const behavior: { respond: (route: Route, url: URL) => Promise<void> } = { respond: (route, url) => fulfill(route, reportFor(url, initial)) }
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/admin/session')) return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ data: employee }) })
    if (url.pathname.endsWith('/admin/analytics')) { requests.push(url); return behavior.respond(route, url) }
    publicRequests.push(url.pathname)
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ data: [] }) })
  })
  await page.goto('/overview')
  await expect(page.getByRole('heading', { name: '医生统计与运营分析', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '正在查询统计', exact: true })).not.toBeVisible()
  return { requests, publicRequests, behavior }
}
async function choose(page: Page, filter: 'clinic' | 'doctor' | 'group', option: string) {
  const index = { clinic: 0, doctor: 1, group: 2 }[filter]
  await page.locator('.analytics-filter-bar .el-select').nth(index).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

test('北京时间本月默认范围；预约确认、实际到诊及治疗分别展示', async ({ page }) => {
  const { requests, publicRequests } = await setup(page)
  expect(requests[0]!.searchParams.get('dateFrom')).toBe('2026-10-01')
  expect(requests[0]!.searchParams.get('dateTo')).toBe('2026-10-04')
  expect(requests[0]!.searchParams.get('groupBy')).toBe('doctor')
  expect(publicRequests).toEqual([])
  await expect(page.getByTestId('analytics-card-appointments').locator('strong')).toHaveText('7次')
  await expect(page.getByTestId('analytics-card-confirmed').locator('strong')).toHaveText('4次')
  await expect(page.getByTestId('analytics-card-arrived').locator('strong')).toHaveText('1次')
  await expect(page.getByTestId('analytics-card-arrived')).toContainText('到诊率 25.0%')
  await expect(page.getByTestId('analytics-card-treatment').locator('strong')).toHaveText('3次')
  await expect(page.getByTestId('analytics-card-followups')).toContainText('共 5 项 · 完成率 40.0%')
  await expect(page.getByTestId('analytics-card-alerts')).toContainText('共 2 项 · 处理率 50.0%')
  await expect(page.locator('.analytics-queue')).toContainText('随访逾期未提交 2')
  await expect(page.locator('.analytics-definitions')).toContainText('后续取消仍保留确认计数')
  await expect(page.locator('.analytics-definitions')).toContainText('患者提交资料不算完成')
  await expect(page.locator('.analytics-definitions')).toContainText('随访负责人改派不会改变医生指标')
})

test('筛选按授权选项提交；改院区清除不兼容医生，并保留实际结果快照', async ({ page }) => {
  const { requests } = await setup(page)
  await choose(page, 'clinic', '东院区')
  await choose(page, 'doctor', '东院医生')
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByTestId('analytics-result-scope')).toContainText('东院区 · 东院医生')
  expect(requests.at(-1)!.searchParams.get('doctorId')).toBe('doctor-east')
  await choose(page, 'clinic', '西院区')
  await expect(page.locator('.analytics-filter-bar .el-select').nth(1)).toContainText('全部授权医生')
  await expect(page.getByTestId('analytics-result-scope')).toContainText('东院区 · 东院医生')
  await expect(page.getByText('筛选条件已修改，点击“查询统计”后更新结果。', { exact: true })).toBeVisible()
  await page.getByLabel('开始日期', { exact: true }).fill('2026-09-01')
  await page.getByLabel('结束日期', { exact: true }).fill('2026-09-30')
  await choose(page, 'group', '按每日')
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByTestId('analytics-result-scope')).toContainText('2026-09-01 至 2026-09-30 · 西院区 · 全部授权医生 · 按每日统计')
  const submitted = requests.at(-1)!.searchParams
  expect(submitted.get('clinicId')).toBe('clinic-west')
  expect(submitted.has('doctorId')).toBe(false)
  expect(submitted.get('groupBy')).toBe('day')
  await expect(page.locator('.analytics-table tbody')).toContainText('2026-09-01')
  await expect(page.locator('.analytics-table thead')).not.toContainText('医生')
  await choose(page, 'group', '按院区')
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByTestId('analytics-result-scope')).toContainText('按院区统计')
  await expect(page.locator('.analytics-table tbody')).toContainText('西院区')
})

test('零分母显示不适用符号，空结果和无授权有明确状态', async ({ page }) => {
  const sparse = { ...emptyMetrics, appointmentCount: 2, pendingAppointmentCount: 2 }
  const { behavior } = await setup(page, { summary: sparse })
  behavior.respond = (route, url) => {
    const report = reportFor(url, { summary: sparse })
    report.rows[0]!.metrics = sparse
    return fulfill(route, report)
  }
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByTestId('analytics-card-arrived')).toContainText('到诊率 —')
  await expect(page.locator('.analytics-table tbody').getByRole('cell', { name: '—', exact: true })).toHaveCount(3)
  await expect(page.getByText('比率以同范围合计计算；分母为 0 时显示“—”，表示不适用。', { exact: true })).toBeVisible()
  behavior.respond = (route, url) => fulfill(route, reportFor(url, { summary: emptyMetrics, rows: [] }))
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByRole('heading', { name: '该范围暂无统计数据', exact: true })).toBeVisible()
  await expect(page.getByTestId('analytics-card-appointments').locator('strong')).toHaveText('0次')
  behavior.respond = (route, url) => fulfill(route, reportFor(url, { summary: emptyMetrics, rows: [], clinics: [], doctors: [] }))
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByRole('heading', { name: '暂无授权院区', exact: true })).toBeVisible()
  await expect(page.getByTestId('analytics-card-appointments')).not.toBeVisible()
  await expect(page.getByRole('button', { name: '查询统计', exact: true })).toBeDisabled()
})

test('加载和错误隐藏旧统计；重试使用失败查询的快照', async ({ page }) => {
  const { behavior, requests } = await setup(page)
  let release: () => void = () => undefined
  const blocked = new Promise<void>(resolve => { release = resolve })
  behavior.respond = async route => {
    await blocked
    await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'REPORT_FAILED', message: '统计查询失败，请重试' } }) })
  }
  await choose(page, 'clinic', '东院区')
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByRole('heading', { name: '正在查询统计', exact: true })).toBeVisible()
  await expect(page.getByTestId('analytics-card-confirmed')).not.toBeVisible()
  await expect(page.locator('.analytics-table')).not.toBeVisible()
  release()
  await expect(page.getByRole('alert')).toContainText('统计查询失败，请重试')
  await expect(page.getByTestId('analytics-card-confirmed')).not.toBeVisible()
  await choose(page, 'clinic', '西院区')
  behavior.respond = (route, url) => fulfill(route, reportFor(url))
  await page.getByRole('button', { name: '重试本次查询', exact: true }).click()
  await expect(page.getByTestId('analytics-result-scope')).toContainText('东院区')
  expect(requests.at(-1)!.searchParams.get('clinicId')).toBe('clinic-east')
  await expect(page.getByText('筛选条件已修改，点击“查询统计”后更新结果。', { exact: true })).toBeVisible()
})

test('后发查询结果不会被迟到的响应或错误覆盖', async ({ page }) => {
  const { behavior, requests } = await setup(page)
  let releaseOld: () => void = () => undefined
  const oldGate = new Promise<void>(resolve => { releaseOld = resolve })
  behavior.respond = async (route, url) => {
    if (url.searchParams.get('groupBy') === 'clinic') { await oldGate; return fulfill(route, reportFor(url, { summary: { ...metrics, appointmentCount: 91 } })) }
    return fulfill(route, reportFor(url, { summary: { ...metrics, appointmentCount: 12 } }))
  }
  await choose(page, 'group', '按院区')
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect.poll(() => requests.length).toBe(2)
  await choose(page, 'group', '按每日')
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByTestId('analytics-card-appointments').locator('strong')).toHaveText('12次')
  const oldResponse = page.waitForResponse(response => new URL(response.url()).searchParams.get('groupBy') === 'clinic')
  releaseOld()
  await (await oldResponse).finished()
  await expect(page.getByTestId('analytics-card-appointments').locator('strong')).toHaveText('12次')
  await expect(page.getByTestId('analytics-result-scope')).toContainText('按每日统计')

  let releaseError: () => void = () => undefined
  const errorGate = new Promise<void>(resolve => { releaseError = resolve })
  behavior.respond = async (route, url) => {
    if (url.searchParams.get('groupBy') === 'doctor') { await errorGate; return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'OLD_ERROR', message: '旧请求错误' } }) }) }
    return fulfill(route, reportFor(url, { summary: { ...metrics, appointmentCount: 13 } }))
  }
  await choose(page, 'group', '按医生')
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect.poll(() => requests.length).toBe(4)
  await choose(page, 'group', '按院区')
  await page.getByRole('button', { name: '查询统计', exact: true }).click()
  await expect(page.getByTestId('analytics-card-appointments').locator('strong')).toHaveText('13次')
  const oldError = page.waitForResponse(response => new URL(response.url()).searchParams.get('groupBy') === 'doctor')
  releaseError()
  await (await oldError).finished()
  await expect(page.getByTestId('analytics-card-appointments').locator('strong')).toHaveText('13次')
  await expect(page.getByRole('alert')).not.toBeVisible()
})
