import { test, expect } from '@playwright/test'

const employee = { id: 'care-manager', username: 'manager', displayName: '主管', role: 'manager', capabilities: ['/care', '/account', 'care.write'], clinicIds: ['clinic-demo'], staffId: null, active: true, version: 1 }
const oldContent = { summary: '旧版摘要', stages: [{ title: '第一阶段', body: '完整历史阶段内容' }, { title: '删除阶段', body: '旧阶段说明' }], instructions: '完整历史医嘱' }
const newContent = { summary: '新版摘要', stages: [{ title: '第一阶段', body: '更新后的阶段内容' }], instructions: '新版医嘱' }

test('跨页已治疗预约直接打开，分页保留详情并只读', async ({ page }) => {
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'care-test', employee: value })), employee)
  const linked = { id: 'appointment-completed', status: 'completed', doctorId: 'doctor-demo', doctorName: '演示医生', clinicName: '演示院区', contactName: '历史患者', startsAt: '2026-10-01T10:00:00+08:00' }
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/admin/session')) return route.fulfill({ json: { data: employee } })
    if (path.endsWith('/admin/appointments')) return route.fulfill({ json: { data: [{ ...linked, id: 'other-appointment', status: 'confirmed', contactName: '其他患者' }], meta: { page: 1, pageSize: 20, total: 101 } } })
    if (path.endsWith('/appointment-completed')) return route.fulfill({ json: { data: linked } })
    if (path.endsWith('/care-plan')) return route.fulfill({ json: { data: { draftVersion: 0, draft: null, versions: [{ id: 'plan-old', version: 1, content: oldContent, publishedAt: '2026-10-01T09:00:00+08:00', publishedBy: '负责医生', changeReason: '初次发布' }] } } })
    return route.fulfill({ json: { data: [] } })
  })
  await page.goto('/care?appointmentId=appointment-completed')
  await expect(page.getByText('演示医生 · 历史患者')).toBeVisible()
  await expect(page.getByRole('button', { name: '保存草稿' })).toBeDisabled()
  await expect(page.getByRole('textbox', { name: '方案摘要' })).toBeDisabled()
  await expect(page.locator('.care-version-card')).toContainText('完整历史医嘱')
  await page.locator('.care-pagination .number').filter({ hasText: /^6$/ }).click()
  await expect(page.getByText('演示医生 · 历史患者')).toBeVisible()
  await expect(page.getByRole('button', { name: '保存草稿' })).toBeDisabled()
})

test('预约服务端分页搜索、历史全文差异和模板保存应用', async ({ page }) => {
  test.setTimeout(60_000)
  const requests: URL[] = []
  let templates: object[] = []
  let savedContent: unknown
  let draftVersion = 0
  let draft: unknown = null
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'care-test', employee: value })), employee)
  const appointment = (id: string) => ({ id, status: 'confirmed', doctorId: 'doctor-demo', doctorName: '演示医生', clinicName: '演示院区', contactName: id === 'appointment-101' ? '第101位患者' : '测试患者', startsAt: '2026-10-01T10:00:00+08:00' })
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/admin/session')) return route.fulfill({ json: { data: employee } })
    if (path.endsWith('/admin/appointments')) {
      requests.push(url)
      const second = url.searchParams.get('page') === '6' || !!url.searchParams.get('q')
      return route.fulfill({ json: { data: [appointment(second ? 'appointment-101' : 'appointment-1')], meta: { page: Number(url.searchParams.get('page')), pageSize: 20, total: url.searchParams.get('q') ? 1 : 101 } } })
    }
    if (path.endsWith('/care-plan/draft')) {
      savedContent = route.request().postDataJSON().content
      draft = savedContent; draftVersion++
      return route.fulfill({ json: { data: { draftVersion, draft, versions: [] } } })
    }
    if (path.endsWith('/care-plan')) return route.fulfill({ json: { data: { draftVersion, draft, versions: [
      { id: 'plan-2', version: 2, content: newContent, publishedAt: '2026-10-02T09:00:00+08:00', publishedBy: '负责医生', changeReason: '修订说明' },
      { id: 'plan-1', version: 1, content: oldContent, publishedAt: '2026-10-01T09:00:00+08:00', publishedBy: '负责医生', changeReason: '初次发布' },
    ] } } })
    if (path.endsWith('/photos')) return route.fulfill({ json: { data: [] } })
    if (path.endsWith('/care-plan-templates')) {
      if (route.request().method() === 'POST') {
        const body = route.request().postDataJSON()
        const row = { id: 'template-1', ...body, createdAt: '2026-10-01T09:00:00+08:00' }
        templates = [row]
        return route.fulfill({ status: 201, json: { data: row } })
      }
      return route.fulfill({ json: { data: templates } })
    }
    return route.fulfill({ json: { data: [] } })
  })
  await page.goto('/care')
  await page.locator('.care-pagination .number').filter({ hasText: /^6$/ }).click()
  await expect(page.getByText('第101位患者')).toBeVisible()
  expect(requests.some(url => url.searchParams.get('page') === '6')).toBeTruthy()
  await page.getByRole('textbox', { name: '搜索预约' }).fill('第101位')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect.poll(() => requests.at(-1)?.searchParams.get('q')).toBe('第101位')
  expect(requests.at(-1)?.searchParams.get('page')).toBe('1')
  await page.getByRole('button', { name: /第101位患者/ }).click()
  await expect(page.locator('.care-version-card').last()).toContainText('完整历史阶段内容')
  await expect(page.locator('.care-version-card').last()).toContainText('完整历史医嘱')
  await expect(page.locator('.comparison-table')).toContainText('删除阶段')
  await expect(page.locator('.comparison-table .changed')).toHaveCount(5)
  await page.locator('.comparison-controls .el-checkbox').click()
  await expect(page.getByRole('checkbox', { name: '只看差异' })).toBeChecked()
  await expect(page.locator('.comparison-table tbody tr')).toHaveCount(5)
  await page.getByRole('textbox', { name: '模板名称' }).fill('测试模板')
  await page.getByRole('button', { name: '存为模板' }).click()
  await expect(page.getByText('个人模板已保存')).toBeVisible()
  await page.locator('.care-form textarea').first().fill('临时编辑')
  await expect(page.getByRole('button', { name: '发布新版本' })).toBeDisabled()
  await page.getByRole('button', { name: '应用模板' }).click()
  await page.getByRole('button', { name: '应用模板', exact: true }).last().click()
  await expect(page.locator('.care-form textarea').first()).toHaveValue('新版摘要')
  await page.getByRole('button', { name: '保存草稿' }).click()
  await expect.poll(() => savedContent).toEqual(newContent)
})
