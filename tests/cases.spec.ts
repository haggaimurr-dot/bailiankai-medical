import { test, expect } from '@playwright/test'

for (const role of ['manager', 'doctor']) {
 test(`案例库：${role} 的发布或审核权限及图片预览`, async ({ page }) => {
  const employee = { id: 'employee-case', username: 'case', displayName: '案例测试员工', role, capabilities: ['/cases', '/appointments'], clinicIds: ['clinic-demo'], staffId: role === 'doctor' ? 'doctor-demo-1' : null, active: true, version: 1 }
  let record = { id: 'case-1', appointmentId: 'appointment-1', patientId: 'patient-1', clinicId: 'clinic-demo', version: 1, revision: 1, consentRevision: 0, reviewStatus: 'pending', reviewNote: '', reviewerId: '', reviewerName: '', reviewedAt: null, publishedAt: null as string | null, policyVersion: 'case-display-v1', policyContent: '独立授权正文', content: { title: '治疗对比案例', summary: '本次摘要', body: '本次展示正文', pairs: [{ beforeImageId: 'before-case', afterImageId: 'after-case', beforeSourceId: 'before-source', afterSourceId: 'after-source', angle: 'front', beforeAt: '2026-10-01T10:00:00+08:00', afterAt: '2026-10-03T10:00:00+08:00' }] } }
  const actions: { action: string; body: unknown }[] = []
  let imageDenied = false
  await page.clock.install()
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'case-test-session', employee: value })), employee)
  await page.route('**/api/v1/**', async route => {
   const path = new URL(route.request().url()).pathname
   if (path.includes('/images/') && imageDenied) await route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND', message: '案例图片不可用' } } })
   else if (path.includes('/images/')) await route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64') })
   else if (path.endsWith('/admin/session')) await route.fulfill({ json: { data: employee } })
   else if (path.endsWith('/history')) await route.fulfill({ json: { data: [] } })
   else if (route.request().method() === 'POST' && /\/(review|publish|unpublish)$/.test(path)) {
    const action = path.split('/').pop()!; const body = route.request().postDataJSON(); actions.push({ action, body })
    record = { ...record, version: record.version + 1, ...(action === 'publish' ? { publishedAt: '2026-10-04T11:00:00+08:00' } : action === 'unpublish' ? { publishedAt: null } : { reviewStatus: body.decision, reviewNote: body.note }) }
    await route.fulfill({ json: { data: record } })
   } else if (path.endsWith('/admin/cases')) await route.fulfill({ json: { data: [record], meta: { page: 1, pageSize: 20, total: 1 } } })
   else if (path.endsWith('/cases/case-1')) await route.fulfill({ json: { data: record } })
   else await route.fulfill({ json: { data: [] } })
  })
  await page.goto('/cases')
  await expect(page.getByRole('heading', { name: '案例库', exact: true })).toBeVisible()
  await page.getByRole('cell', { name: '治疗对比案例' }).click()
  await expect(page.locator('.private-photo img')).toHaveCount(4)
  const slider = page.getByRole('slider', { name: '治疗前后图片对比' }); await expect(slider).toBeEnabled()
  await slider.evaluate((input: HTMLInputElement) => { input.value = '80'; input.dispatchEvent(new Event('input', { bubbles: true })) })
  await expect(page.locator('.comparison-stage .divider')).toHaveAttribute('style', /left: 80%/)
  if (role === 'manager') {
   const publish = page.getByRole('button', { name: '审核后发布' }); await expect(publish).toBeDisabled()
   record = { ...record, consentRevision: 1, reviewStatus: 'approved' }
   await page.getByRole('button', { name: '刷新', exact: true }).click()
   await page.getByRole('cell', { name: '治疗对比案例' }).click()
   await expect(publish).toBeEnabled(); await publish.click()
   await expect(page.getByRole('button', { name: '撤回展示' })).toBeEnabled()
   await page.getByRole('button', { name: '撤回展示' }).click()
   await page.getByRole('button', { name: '确定', exact: true }).click()
   await expect(publish).toBeEnabled()
   expect(actions.map(a => a.action)).toEqual(['publish', 'unpublish'])
  } else {
   await expect(page.getByRole('button', { name: '新增案例' })).toHaveCount(0)
   await expect(page.getByRole('button', { name: '拒绝', exact: true })).toBeDisabled()
   await page.getByPlaceholder('审核意见（拒绝时必填）').fill('需修订描述')
   await page.getByRole('button', { name: '拒绝', exact: true }).click()
   await expect(page.getByText('审核意见：需修订描述')).toBeVisible()
   expect(actions[0]).toEqual({ action: 'review', body: { version: 1, decision: 'rejected', note: '需修订描述' } })
  }
  imageDenied = true; await page.clock.fastForward(10_001)
  await expect(slider).toBeDisabled()
  await expect(page.locator('.comparison-stage')).toHaveClass(/unavailable/)
  await expect(page.locator('.comparison-stage .divider')).toHaveCount(0)
 })
}

test('护理、客服及运营没有案例后台入口', async ({ page }) => {
 const employee = { id: 'employee-nurse', role: 'nurse', capabilities: ['/appointments'], clinicIds: ['clinic-demo'], displayName: '护理', staffId: 'nurse-demo', active: true, version: 1 }
 await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'case-test', employee: value })), employee)
 await page.route('**/api/v1/**', route => route.fulfill({ json: { data: new URL(route.request().url()).pathname.endsWith('/admin/session') ? employee : [] } }))
 await page.goto('/cases'); await expect(page).toHaveURL(/\/appointments/)
 await expect(page.locator('nav').getByRole('link', { name: '案例库' })).toHaveCount(0)
})

test('创建案例保存文案与两组独立图片关联', async ({ page }) => {
 const employee = { id: 'manager-case', role: 'manager', capabilities: ['/cases', '/appointments'], displayName: '主管', clinicIds: ['clinic-demo'], staffId: null, active: true, version: 1 }
 const sources = ['front', 'left'].map(angle => ({ id: `before-${angle}`, angle, uploadedAt: '2026-10-01T10:00:00+08:00' }))
 const after = ['front', 'left'].map(angle => ({ id: `after-${angle}`, angle, uploadedAt: '2026-10-03T10:00:00+08:00' }))
 const candidateQueries: URLSearchParams[] = []
 let posted: Record<string, unknown> | undefined
 let record: Record<string, unknown> | undefined
 await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'case-create', employee: value })), employee)
 await page.route('**/api/v1/**', async route => {
  const path = new URL(route.request().url()).pathname
  if (path.endsWith('/admin/session')) await route.fulfill({ json: { data: employee } })
  else if (path.endsWith('/admin/cases') && route.request().method() === 'POST') {
   posted = route.request().postDataJSON()
   record = { ...posted, id: 'case-created', version: 1, revision: 1, consentRevision: 0, reviewStatus: 'pending', publishedAt: null, content: { ...posted, pairs: [] } }
   await route.fulfill({ status: 201, json: { data: record } })
  } else if (path.endsWith('/admin/cases')) await route.fulfill({ json: { data: record ? [record] : [], meta: { page: 1, pageSize: 20, total: record ? 1 : 0 } } })
  else if (path.endsWith('/admin/appointments')) { candidateQueries.push(new URL(route.request().url()).searchParams); await route.fulfill({ json: { data: [{ id: 'appointment-1', contactName: '演示患者', startsAt: '2026-10-01T10:00:00+08:00' }], meta: { total: 41 } } }) }
  else if (path.endsWith('/appointment-1/photos')) await route.fulfill({ json: { data: sources } })
  else if (path.endsWith('/appointment-1/followups')) await route.fulfill({ json: { data: { tasks: [{ submittedAt: '2026-10-03T10:00:00+08:00', photos: after }] } } })
  else if (path.endsWith('/content')) await route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64') })
  else await route.fulfill({ json: { data: [] } })
 })
 await page.goto('/cases'); await page.getByRole('button', { name: '新增案例' }).click()
 await page.getByPlaceholder('搜索已治疗预约：姓名、手机号或编号').fill('演示患者')
 await page.getByRole('button', { name: '搜索预约' }).click()
 await page.locator('.candidate-search .el-pager').getByText('2', { exact: true }).click()
 await expect.poll(() => candidateQueries.at(-1)?.get('page')).toBe('2')
 expect(candidateQueries.at(-1)?.get('q')).toBe('演示患者'); expect(candidateQueries.at(-1)?.get('status')).toBe('completed'); expect(candidateQueries.at(-1)?.get('pageSize')).toBe('20')
 await page.getByPlaceholder('搜索已治疗预约：姓名、手机号或编号').fill('appointment-1')
 await page.getByRole('button', { name: '搜索预约' }).click()
 await expect.poll(() => candidateQueries.at(-1)?.get('page')).toBe('1')
 await page.getByPlaceholder('也可输入其他已治疗预约编号').fill('appointment-1')
 await page.getByRole('button', { name: '加载照片' }).click()
 await page.getByLabel('标题', { exact: true }).fill('两组对比案例')
 await page.getByLabel('摘要', { exact: true }).fill('案例摘要')
 await page.getByLabel('案例分类', { exact: true }).fill('瘢痕修复')
 const tags = page.locator('.el-form-item').filter({ hasText: '案例标签（最多10个，每个30字）' }).locator('input'); await tags.fill('正面'); await page.getByRole('option', { name: '正面', exact: true }).click()
 await page.getByLabel('展示正文（请移除姓名、联系方式及其他身份信息）').fill('展示正文')
 const pick = async (placeholder: string, index: number, label: string) => {
  await page.locator('.editor .el-form-item').filter({ hasText: placeholder === '选择面诊照片' ? '治疗前照片' : '治疗后照片' }).nth(index).locator('.el-select').click()
  await page.getByRole('option', { name: label, exact: true }).click()
 }
 await pick('选择面诊照片', 0, 'front · 2026-10-01T10:00:00+08:00')
 await pick('选择随访照片', 0, 'front · 2026-10-03T10:00:00+08:00')
 await page.getByRole('button', { name: '添加对比角度' }).click()
 await pick('选择面诊照片', 1, 'left · 2026-10-01T10:00:00+08:00')
 await pick('选择随访照片', 1, 'left · 2026-10-03T10:00:00+08:00')
 await page.getByRole('button', { name: '保存并等待患者授权' }).click()
 await expect(page.getByRole('heading', { name: '两组对比案例' })).toBeVisible()
 expect(posted).toEqual({ appointmentId: 'appointment-1', title: '两组对比案例', summary: '案例摘要', body: '展示正文', category: '瘢痕修复', tags: ['正面'], pairs: [{ beforeSourceId: 'before-front', afterSourceId: 'after-front' }, { beforeSourceId: 'before-left', afterSourceId: 'after-left' }] })
 await expect(page.getByRole('button', { name: '审核后发布' })).toBeDisabled()
})
