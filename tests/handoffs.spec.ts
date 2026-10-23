import { expect, test, type Page } from '@playwright/test'

async function setup(page: Page, role = 'manager', failQueue = false) {
  const employee = { id: 'handoff-test', username: 'test', displayName: '测试账号', role, clinicIds: ['clinic-test'], staffId: role === 'nurse' ? 'nurse-test' : null, active: true, version: 1, capabilities: ['/workbench', '/handoffs', '/followups', '/appointments', '/employees', '/account'] }
  await page.addInitScript(employee => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-token', employee })), employee)
  let read = false
  const seen: string[] = []
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); seen.push(url.pathname + url.search)
    if (url.pathname.endsWith('/admin/session')) return route.fulfill({ json: { data: employee } })
    if (url.pathname.endsWith('/handoff-notifications/notice-test/read')) { read = true; return route.fulfill({ json: { data: { read: true } } }) }
    if (url.pathname.endsWith('/handoff-notifications')) return route.fulfill({ json: { data: [{ id: 'notice-test', receiptId: 'receipt-test', kind: role === 'manager' ? 'escalated' : 'assigned', createdAt: '2026-10-07T01:00:00Z', readAt: read ? '2026-10-07T02:00:00Z' : null, taskTitle: '术后随访', fromName: '交出人', toName: '接手人', receiptStatus: 'pending' }], meta: { total: 1, unreadTotal: read ? 0 : 1, page: 1, pageSize: 20 } } })
    if (url.pathname.endsWith('/handoff-receipts')) return route.fulfill({ json: { data: [], meta: { total: url.searchParams.has('receiptId') ? 0 : 3, overdueTotal: 2, todoTotal: 2, overdueHours: 24, page: 1, pageSize: 20 } } })
    if (url.pathname.endsWith('/followups')) {
      if (failQueue) return route.fulfill({ status: 503, json: { error: { message: '随访服务暂不可用' } } })
      return route.fulfill({ json: { data: [], meta: { total: 4, page: 1, pageSize: 20 } } })
    }
    if (url.pathname.endsWith('/handoff-settings')) return route.fulfill({ json: { data: { overdueHours: 24, version: 1, updatedBy: null, updatedAt: '2026-10-07T01:00:00Z' } } })
    return route.fulfill({ json: { data: [] } })
  })
  return seen
}

test('工作台按事项分流，已读独立于交接确认，通知定位对应交接', async ({ page }) => {
  const seen = await setup(page)
  await page.goto('/workbench')
  await expect(page.locator('.todo-card')).toHaveCount(5)
  await expect(page.locator('.todo-card').filter({ hasText: '超时升级' })).toContainText('2项')
  await expect(page.locator('.notification-panel')).toContainText('1 条未读')
  await page.getByRole('button', { name: '标为已读' }).click()
  await expect(page.locator('.notification-panel')).toContainText('0 条未读')
  expect(seen.some(url => url.includes('/confirm'))).toBe(false)
  await page.getByRole('button', { name: '查看交接', exact: true }).click()
  await expect(page).toHaveURL(/\/handoffs\?status=all&receiptId=receipt-test/)
  await expect(page.getByRole('heading', { name: '人员交接', exact: true })).toBeVisible()
  await expect(page.getByText('当前显示通知关联的交接记录')).toBeVisible()
  expect(seen.some(url => url.includes('receiptId=receipt-test'))).toBe(true)
  await page.getByText('人员离岗交接', { exact: true }).click()
  await expect(page.getByRole('heading', { name: '随访负责人及离岗交接' })).toBeVisible()
  await expect(page.getByRole('heading', { name: /任务交接确认/ })).not.toBeVisible()
  await page.getByText('提醒设置', { exact: true }).click()
  await expect(page.getByRole('heading', { name: '交接通知与超时升级' })).toBeVisible()
})

test('护理账号只见本人确认和临床待办', async ({ page }) => {
  await setup(page, 'nurse')
  await page.goto('/workbench')
  await expect(page.locator('.todo-card')).toHaveCount(3)
  await expect(page.locator('.notification-panel')).toContainText('任务交接通知')
  await page.locator('.todo-card').filter({ hasText: '交接待确认' }).click()
  await expect(page.getByText('人员离岗交接', { exact: true })).not.toBeVisible()
  await expect(page.getByText('提醒设置', { exact: true })).not.toBeVisible()
})

test('队列读取失败保留交接和通知，入口携带状态筛选', async ({ page }) => {
  await setup(page, 'manager', true)
  await page.goto('/workbench')
  await expect(page.locator('.todo-card').filter({ hasText: '随访待回评' })).toContainText('—项')
  await expect(page.locator('.todo-card').filter({ hasText: '交接待确认' })).toContainText('3项')
  await expect(page.locator('.notification-panel')).toContainText('超时升级提醒')
  await page.locator('.todo-card').filter({ hasText: '异常待处理' }).click()
  await expect(page).toHaveURL('/followups?status=alert')
  await expect(page.locator('.receipt-panel')).not.toBeVisible()
  await expect(page.locator('.staff-panel')).not.toBeVisible()
})
