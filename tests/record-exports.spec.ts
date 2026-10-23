import { test, expect, type Page } from '@playwright/test'
import type { components } from '../packages/contracts/src/schema'

test.use({ launchOptions: { timeout: 15000 } })

type ExportJob = components['schemas']['RecordExportJob']
type ExportInput = components['schemas']['RecordExportInput']
const patients = [
  { id: 'patient-export-1', displayName: '档案患者甲', createdAt: '2026-10-01T09:00:00+08:00', appointmentCount: 1 },
  { id: 'patient-export-2', displayName: '档案患者乙', createdAt: '2026-10-01T09:00:00+08:00', appointmentCount: 1 },
]
const employee = (role: string) => ({ id: 'employee-export', username: 'export-user', displayName: '档案医护', role, clinicIds: ['clinic-export'], staffId: role === 'manager' ? null : 'staff-export', active: true, version: 1, capabilities: ['/patients', '/account'] })
const job = (overrides: Partial<ExportJob> = {}): ExportJob => ({
  id: 'export-test-1', employeeId: 'employee-export', patientCount: 2, sections: ['appointments', 'consultations', 'care_plans', 'treatments', 'followups'], dateFrom: '', dateTo: '', masked: true, reason: '月度档案核对', status: 'ready', createdAt: '2026-10-04T09:00:00+08:00', completedAt: '2026-10-04T09:00:05+08:00', expiresAt: new Date(Date.now() + 60000).toISOString(), sizeBytes: 200, errorCode: '', downloadCount: 0, ...overrides,
})
type MockState = { posted: ExportInput[]; listReads: number; downloads: number; auditReads: number; jobs: ExportJob[]; pollReady?: boolean; denyDownload?: boolean; authorization: string[] }

async function mockExports(page: Page, role = 'manager', jobs: ExportJob[] = []) {
  const state: MockState = { posted: [], listReads: 0, downloads: 0, auditReads: 0, jobs, authorization: [] }
  await page.addInitScript(value => {
    sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-export-token', employee: value }))
    const create = URL.createObjectURL.bind(URL), revoke = URL.revokeObjectURL.bind(URL)
    const tracking = { created: [] as string[], revoked: [] as string[] }
    Object.assign(window, { exportBlobTracking: tracking })
    URL.createObjectURL = blob => { const url = create(blob); tracking.created.push(url); return url }
    URL.revokeObjectURL = url => { tracking.revoked.push(url); revoke(url) }
  }, employee(role))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url())
    if (url.pathname.includes('/record-exports')) {
      state.authorization.push(request.headers()['authorization'] || '')
      if (url.pathname.endsWith('/download')) {
        state.downloads++
        if (state.denyDownload) {
          state.jobs = state.jobs.map(item => ({ ...item, status: 'expired' }))
          await route.fulfill({ status: 410, json: { error: { code: 'EXPORT_EXPIRED', message: '导出文件已过期，请重新提交导出。' } } })
        } else {
          state.jobs = state.jobs.map(item => ({ ...item, downloadCount: item.downloadCount + 1 }))
          await route.fulfill({ contentType: 'application/json', headers: { 'Content-Disposition': 'attachment; filename="patient-records.json"' }, body: '{"patients":[{"appointments":[]}]}' })
        }
      } else if (url.pathname.endsWith('/events')) {
        state.auditReads++
        await route.fulfill({ json: { data: [{ id: 'audit-1', jobId: 'export-test-1', actorId: 'employee-export', action: 'downloaded', detailCode: 'OK', createdAt: '2026-10-04T09:00:10+08:00' }] } })
      } else if (request.method() === 'POST') {
        const body = request.postDataJSON() as ExportInput
        state.posted.push(body)
        state.jobs = [job({ patientCount: body.patientIds.length, sections: body.sections, masked: body.masked ?? true, reason: body.reason, dateFrom: body.dateFrom || '', dateTo: body.dateTo || '', status: 'queued', completedAt: null, expiresAt: null })]
        await route.fulfill({ status: 202, json: { data: state.jobs[0] } })
      } else {
        state.listReads++
        if (state.pollReady && state.listReads > 1) state.jobs = state.jobs.map(item => ({ ...item, status: 'ready', expiresAt: new Date(Date.now() + 60000).toISOString(), completedAt: new Date().toISOString() }))
        await route.fulfill({ json: { data: state.jobs, total: state.jobs.length, page: 1, pageSize: 20 } })
      }
    } else if (url.pathname.endsWith('/admin/session')) await route.fulfill({ json: { data: employee(role) } })
    else if (url.pathname.endsWith('/admin/patients')) await route.fulfill({ json: { data: patients, total: patients.length, page: 1, pageSize: 20 } })
    else if (url.pathname.includes('/admin/patients/')) {
      const patient = patients.find(item => url.pathname.endsWith('/' + item.id)) || patients[0]
      await route.fulfill({ json: { data: { patient, consultations: [], encounters: [] } } })
    } else await route.fulfill({ json: { data: [] } })
  })
  await page.goto('/patients')
  await expect(page.getByRole('heading', { name: '统一患者档案' })).toBeVisible()
  return state
}

test('selected scope configures a masked export, polls completion, downloads with auth and shows audit', async ({ page }) => {
  const state = await mockExports(page)
  state.pollReady = true
  await expect(page.getByRole('button', { name: '导出已选（0）' })).toBeDisabled()
  await page.getByRole('checkbox', { name: '选择本页患者' }).locator('xpath=ancestor::label[1]').click()
  await page.getByRole('button', { name: '导出已选（2）' }).click()
  const dialog = page.getByRole('dialog', { name: '导出患者档案' })
  await expect(dialog.getByRole('radio', { name: '严格脱敏', exact: true })).toBeChecked()
  await expect(dialog.getByText('档案患者甲、档案患者乙', { exact: true })).toBeVisible()
  await expect(dialog.getByText('不包含照片和照片链接', { exact: false })).toBeVisible()
  await dialog.getByRole('checkbox', { name: '方案', exact: true }).locator('xpath=ancestor::label[1]').click()
  await dialog.getByRole('checkbox', { name: '治疗', exact: true }).locator('xpath=ancestor::label[1]').click()
  await dialog.getByLabel('导出起始日期').fill('2026-10-01')
  await dialog.getByLabel('导出结束日期').fill('2026-10-04')
  await dialog.getByRole('button', { name: '提交导出任务' }).click()
  await expect(dialog.getByText('请填写导出用途。')).toBeVisible()
  expect(state.posted).toHaveLength(0)
  await dialog.getByLabel('导出用途（必填）').fill('月度档案核对')
  await dialog.getByRole('button', { name: '提交导出任务' }).click()
  const drawer = page.getByRole('dialog', { name: '档案导出任务' })
  await expect(drawer.getByText('排队中', { exact: true })).toBeVisible()
  await expect(drawer.getByText('可下载', { exact: true })).toBeVisible({ timeout: 6000 })
  expect(state.posted).toEqual([{ patientIds: ['patient-export-1', 'patient-export-2'], sections: ['appointments', 'consultations', 'followups'], dateFrom: '2026-10-01', dateTo: '2026-10-04', masked: true, reason: '月度档案核对' }])
  const downloaded = page.waitForEvent('download')
  await drawer.getByRole('button', { name: '下载档案' }).click()
  expect((await downloaded).suggestedFilename()).toBe('patient-records-export-test-1.json')
  await expect(drawer.getByText('已下载 1 次', { exact: false })).toBeVisible()
  await drawer.getByRole('button', { name: '查看审计' }).click()
  const audit = page.getByRole('dialog', { name: '导出审计', exact: true })
  await expect(audit.getByRole('cell', { name: '下载档案' })).toBeVisible()
  await expect(audit.getByRole('cell', { name: 'employee-export' })).toBeVisible()
  expect(state.auditReads).toBe(1)
  expect(state.authorization.every(value => value === 'Bearer test-export-token')).toBe(true)
  const blobs = await page.evaluate(() => (window as unknown as { exportBlobTracking: { created: string[]; revoked: string[] } }).exportBlobTracking)
  expect(blobs.created).toHaveLength(1)
  expect(blobs.revoked).toEqual(blobs.created)
  await expect(page.locator('a[href*="token="]')).toHaveCount(0)
})

for (const role of ['doctor', 'clinician', 'nurse']) {
  test(`${role} can export the current patient with strict masking only`, async ({ page }) => {
    const state = await mockExports(page, role)
    await page.getByRole('checkbox', { name: '选择患者 档案患者乙' }).locator('xpath=ancestor::label[1]').click()
    await page.getByRole('button', { name: /档案患者甲.*次预约/ }).click()
    await page.getByRole('button', { name: '导出此档案' }).click()
    const dialog = page.getByRole('dialog', { name: '导出患者档案' })
    await expect(dialog.getByRole('radio', { name: '当前患者', exact: true })).toBeChecked()
    await expect(dialog.getByRole('radio', { name: '完整档案（仅管理员）' })).toBeDisabled()
    await dialog.getByLabel('导出用途（必填）').fill('治疗记录核对')
    await dialog.getByRole('button', { name: '提交导出任务' }).click()
    await expect(page.getByRole('dialog', { name: '档案导出任务' }).getByText('排队中', { exact: true })).toBeVisible()
    expect(state.posted[0]).toMatchObject({ patientIds: ['patient-export-1'], masked: true, reason: '治疗记录核对' })
  })
}

test('manager full export validates content and bounded date range before submission', async ({ page }) => {
  const state = await mockExports(page)
  await page.getByRole('checkbox', { name: '选择患者 档案患者甲' }).locator('xpath=ancestor::label[1]').click()
  await page.getByRole('button', { name: '导出已选（1）' }).click()
  const dialog = page.getByRole('dialog', { name: '导出患者档案' })
  await dialog.getByLabel('导出用途（必填）').fill('患者本人申请完整副本')
  for (const label of ['预约', '问诊记录', '方案', '治疗', '随访']) await dialog.getByRole('checkbox', { name: label, exact: true }).locator('xpath=ancestor::label[1]').click()
  await dialog.getByRole('button', { name: '提交导出任务' }).click()
  await expect(dialog.getByText('请至少选择一项档案内容。')).toBeVisible()
  await dialog.getByRole('checkbox', { name: '问诊记录', exact: true }).locator('xpath=ancestor::label[1]').click()
  await dialog.getByRole('radio', { name: '完整档案（仅管理员）' }).locator('xpath=ancestor::label[1]').click()
  await dialog.getByLabel('导出起始日期').fill('2025-01-01')
  await dialog.getByRole('button', { name: '提交导出任务' }).click()
  await expect(dialog.getByText('请同时填写起始日期和结束日期。')).toBeVisible()
  await dialog.getByLabel('导出结束日期').fill('2026-10-04')
  await dialog.getByRole('button', { name: '提交导出任务' }).click()
  await expect(dialog.getByText('日期范围须按先后顺序填写，且最多 366 天。')).toBeVisible()
  expect(state.posted).toHaveLength(0)
  await dialog.getByLabel('导出起始日期').fill('2026-10-01')
  await dialog.getByRole('button', { name: '提交导出任务' }).click()
  await expect(page.getByRole('dialog', { name: '档案导出任务' })).toBeVisible()
  expect(state.posted[0]).toMatchObject({ patientIds: ['patient-export-1'], masked: false, sections: ['consultations'] })
})

test('download expiry is enforced locally without pending polling', async ({ page }) => {
  const state = await mockExports(page, 'manager', [job()])
  await page.clock.install()
  await page.getByRole('button', { name: '导出任务', exact: true }).click()
  const drawer = page.getByRole('dialog', { name: '档案导出任务' })
  await expect(drawer.getByRole('button', { name: '下载档案' })).toBeEnabled()
  await page.clock.fastForward(61000)
  await expect(drawer.getByRole('button', { name: '下载档案' })).toBeDisabled()
  await expect(drawer.getByText('已过期', { exact: true })).toBeVisible()
  expect(state.listReads).toBe(1)
  expect(state.downloads).toBe(0)
})

test('server download rejection refreshes expired status and polling stops when closed', async ({ page }) => {
  const state = await mockExports(page, 'manager', [job()])
  await page.clock.install()
  state.denyDownload = true
  await page.getByRole('button', { name: '导出任务', exact: true }).click()
  const drawer = page.getByRole('dialog', { name: '档案导出任务' })
  await drawer.getByRole('button', { name: '下载档案' }).click()
  await expect(page.getByText('导出文件已过期，请重新提交导出。')).toBeVisible()
  await expect(drawer.getByRole('button', { name: '下载档案' })).toBeDisabled()
  await expect(drawer.getByText('已过期', { exact: true })).toBeVisible()
  state.jobs = [job({ status: 'processing', expiresAt: null, completedAt: null })]
  await drawer.getByRole('button', { name: '刷新任务' }).click()
  await expect(drawer.getByText('生成中', { exact: true })).toBeVisible()
  await drawer.locator('.el-drawer__close-btn').click()
  await expect(drawer).not.toBeVisible()
  const reads = state.listReads
  await page.clock.fastForward(2300)
  expect(state.listReads).toBe(reads)
})
