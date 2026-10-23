import { test, expect, type Page } from '@playwright/test'
const manager = { id: 'manager-test', username: 'manager', displayName: '测试主管', role: 'manager', clinicIds: ['clinic-demo'], staffId: null, active: true, version: 1, capabilities: ['/employees', '/account', 'employee.access'] }
const worker = { ...manager, id: 'worker-test', username: 'worker', displayName: '测试员工', role: 'operations', capabilities: ['/account'], staffId: null }
async function setup(page: Page, employee = manager) {
  const mutations: { path: string; body: Record<string, unknown> }[] = []
  await page.addInitScript(value => { if (!sessionStorage.getItem('employees-test-initialized')) { sessionStorage.setItem('employees-test-initialized', 'true'); sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-token', employee: value })) } }, employee)
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (route.request().method() === 'POST') { mutations.push({ path, body: route.request().postDataJSON() }); return route.fulfill({ json: { data: worker } }) }
    let data: unknown = []
    if (path === '/api/v1/admin/session') data = employee
    if (path === '/api/v1/admin/employees') data = [manager, worker]
    if (path === '/api/v1/clinics') data = [{ id: 'clinic-demo', name: '测试院区' }]
    if (path === '/api/v1/admin/followup-staff') data = [{ id: 'doctor-test', displayName: '测试医生', role: 'doctor', status: 'on_duty' }, { id: 'nurse-test', displayName: '测试护理', role: 'nurse', status: 'on_duty' }]
    if (path === '/api/v1/admin/permissions') data = [{ key: 'employee.access', label: '调整员工角色与关联人员', roles: { manager: true, doctor: false, nurse: false, customer_service: false, operations: false, clinician: false } }]
    return route.fulfill({ json: { data } })
  })
  return mutations
}
test('权限菜单与直接访问使用后端授权', async ({ page }) => {
  await setup(page, worker)
  await page.goto('/employees')
  await expect(page).toHaveURL(/\/account$/)
  await expect(page.getByRole('heading', { name: '个人修改密码' })).toBeVisible()
  await expect(page.locator('.side-nav').getByRole('link', { name: '员工与权限' })).toHaveCount(0)
})
test('个人改密校验两次输入并提交原密码，成功退出', async ({ page }) => {
  const mutations = await setup(page, worker)
  await page.goto('/account')
  const fields = page.locator('.account-panel input')
  await fields.nth(0).fill('OldPassword123!')
  await fields.nth(1).fill('NewPassword123!')
  await fields.nth(2).fill('Mismatch123!')
  await page.getByRole('button', { name: '保存新密码' }).click()
  await expect(page.getByText('请填写原密码；新密码须为 12–72 字节，与原密码不同，且两次输入一致。')).toBeVisible()
  expect(mutations).toHaveLength(0)
  await fields.nth(2).fill('NewPassword123!')
  await page.getByRole('button', { name: '保存新密码' }).click()
  await expect(page).toHaveURL(/\/login$/)
  expect(mutations).toEqual([{ path: '/api/v1/admin/session/password', body: { currentPassword: 'OldPassword123!', password: 'NewPassword123!' } }])
  expect(await page.evaluate(() => sessionStorage.getItem('bailiankai.employee-session'))).toBeNull()
})
test('主管查看权限矩阵并调整既有角色和关联人员', async ({ page }) => {
  const mutations = await setup(page)
  await page.goto('/employees')
  await expect(page.getByRole('heading', { name: '角色权限矩阵' })).toBeVisible()
  await page.locator('.account').filter({ hasText: '测试员工' }).getByRole('button', { name: '角色与人员' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('.el-select').nth(0).click()
  await page.getByRole('option', { name: '医生', exact: true }).last().click()
  await dialog.locator('.el-select').nth(1).click()
  await expect(page.getByRole('option', { name: '测试护理', exact: true })).toHaveCount(0)
  await page.getByRole('option', { name: '测试医生', exact: true }).click()
  await dialog.getByRole('button', { name: '保存调整' }).click()
  await expect(dialog).not.toBeVisible()
  expect(mutations).toEqual([{ path: '/api/v1/admin/employees/worker-test/access', body: { version: 1, role: 'doctor', staffId: 'doctor-test' } }])
})
