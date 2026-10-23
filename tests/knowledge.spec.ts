import { test, expect } from '@playwright/test'

for (const role of ['manager', 'doctor']) {
 test(`知识库 ${role} 审核与发布权限`, async ({ page }) => {
  const employee = { id: 'employee-knowledge', username: 'knowledge', displayName: '测试员工', role, clinicIds: ['clinic-demo'], active: true, version: 1, capabilities: ['/knowledge', '/account'] }
  let article = { id: 'article-1', categoryId: 'cat-1', title: '就诊准备文章', summary: '摘要', body: '<script>纯文本正文</script>', sourceText: '参考来源第一版', sortOrder: 0, version: 1, reviewStatus: 'pending', reviewNote: '', reviewerName: '', reviewerId: '', reviewedAt: null, publishedAt: null as string | null }
  const writes: { action: string; body: Record<string, unknown> }[] = []
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'knowledge-test', employee: value })), employee)
  await page.route('**/api/v1/**', async route => {
   const path = new URL(route.request().url()).pathname
   if (path.endsWith('/admin/session')) return route.fulfill({ json: { data: employee } })
   if (path.endsWith('/knowledge/categories')) return route.fulfill({ json: { data: [{ id: 'cat-1', clinicId: 'clinic-demo', name: '就诊准备', active: true, version: 1, sortOrder: 0 }] } })
   if (path.endsWith('/clinics')) return route.fulfill({ json: { data: [{ id: 'clinic-demo', name: '演示院区' }] } })
   if (path.endsWith('/history')) return route.fulfill({ json: { data: [] } })
   if (route.request().method() === 'POST' && /\/(review|publish|unpublish)$/.test(path)) {
    const action = path.split('/').pop()!, body = route.request().postDataJSON(); writes.push({ action, body })
    article = { ...article, version: article.version + 1, ...(action === 'review' ? { reviewStatus: body.decision, reviewNote: body.note } : { publishedAt: action === 'publish' ? '2026-10-07T10:00:00+08:00' : null }) }
    return route.fulfill({ json: { data: article } })
   }
   if (path.endsWith('/knowledge/articles')) return route.fulfill({ json: { data: [article], meta: { total: 1 } } })
   if (path.endsWith('/articles/article-1')) return route.fulfill({ json: { data: article } })
   return route.fulfill({ json: { data: [] } })
  })
  await page.goto('/knowledge')
  await expect(page.getByRole('heading', { name: '知识库', exact: true })).toBeVisible()
  await page.getByRole('cell', { name: '就诊准备文章' }).click()
  await expect(page.locator('.body').first()).toHaveText('<script>纯文本正文</script>')
  await expect(page.locator('.body').nth(1)).toHaveText('参考来源第一版')
  if (role === 'doctor') {
   article.sourceText = ''
   await page.getByRole('button', { name: '刷新', exact: true }).click()
   await expect(page.getByRole('button', { name: '审核通过', exact: true })).toBeDisabled()
   article.sourceText = '参考来源第一版'
   await page.getByRole('button', { name: '刷新', exact: true }).click()
   await expect(page.getByRole('button', { name: '审核通过', exact: true })).toBeEnabled()
   await expect(page.getByRole('button', { name: '新增文章' })).toHaveCount(0)
   await expect(page.getByRole('button', { name: '退回修改', exact: true })).toBeDisabled()
   await page.getByPlaceholder('审核意见（退回时必填）').fill('需要补充')
   await page.getByRole('button', { name: '退回修改', exact: true }).click()
   await expect(page.getByText('审核意见：需要补充')).toBeVisible()
   expect(writes[0]).toEqual({ action: 'review', body: { version: 1, decision: 'rejected', note: '需要补充' } })
  } else {
   const publish = page.getByRole('button', { name: '审核后发布' })
   await expect(publish).toBeDisabled()
   article.reviewStatus = 'approved'
   await page.getByRole('button', { name: '刷新', exact: true }).click()
   await expect(publish).toBeEnabled(); await publish.click()
   await page.getByRole('button', { name: '确定', exact: true }).click()
   await expect(page.getByRole('button', { name: '下架文章' })).toBeEnabled()
   await page.getByRole('button', { name: '下架文章' }).click()
   await page.getByRole('button', { name: '确定', exact: true }).click()
   await expect(page.getByRole('button', { name: '下架文章' })).toBeDisabled()
   expect(writes.map(w => w.action)).toEqual(['publish', 'unpublish'])
  }
 })
}

test('正式医学来源录入、修改与待审核筛选', async ({ page }) => {
 const employee = { id: 'manager-1', role: 'manager', displayName: '主管', clinicIds: ['clinic-demo'], capabilities: ['/knowledge', '/account'] }
 let article: Record<string, any> | undefined
 const saves: Record<string, unknown>[] = [], queries: string[] = []
 await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test', employee: value })), employee)
 await page.route('**/api/v1/**', async route => {
  const url = new URL(route.request().url()), path = url.pathname, method = route.request().method()
  if (path.endsWith('/admin/session')) return route.fulfill({ json: { data: employee } })
  if (path.endsWith('/knowledge/categories')) return route.fulfill({ json: { data: [{ id: 'cat-1', clinicId: 'clinic-demo', name: '正式科普', active: true }] } })
  if (path.endsWith('/clinics')) return route.fulfill({ json: { data: [{ id: 'clinic-demo', name: '院区' }] } })
  if (path.endsWith('/history')) return route.fulfill({ json: { data: [] } })
  if (method === 'POST' || method === 'PUT') {
   const body = route.request().postDataJSON(); saves.push(body)
   article = { ...article, ...body, id: 'article-1', version: (article?.version || 0) + 1, reviewStatus: 'pending', reviewNote: '', reviewerName: '', reviewedAt: null, publishedAt: null }
   return route.fulfill({ json: { data: article } })
  }
  if (path.endsWith('/knowledge/articles')) { queries.push(url.search); return route.fulfill({ json: { data: article ? [article] : [], meta: { total: article ? 1 : 0 } } }) }
  return route.fulfill({ json: { data: article } })
 })
 await page.goto('/knowledge')
 await page.getByRole('button', { name: '新增文章' }).click()
 await page.locator('.editor .el-select').click()
 await page.getByRole('option', { name: '正式科普' }).click()
 await page.locator('.editor .el-form-item').filter({ hasText: /^标题/ }).locator('input').fill('院方待审正文')
 await page.locator('.editor .el-form-item').filter({ hasText: /^正文/ }).locator('textarea').fill('院方录入的正文')
 await page.getByPlaceholder('每行一条，填写来源名称、发布机构、年份或版本及链接，供医生核对').fill('  来源第一版  ')
 await page.getByRole('button', { name: '保存草稿并提交审核' }).click()
 await expect(page.getByText('草稿已保存，等待医生审核。')).toBeVisible()
 expect(saves[0]?.sourceText).toBe('来源第一版')
 await page.getByRole('button', { name: '编辑草稿' }).click()
 await expect(page.getByPlaceholder('每行一条，填写来源名称、发布机构、年份或版本及链接，供医生核对')).toHaveValue('来源第一版')
 await page.getByPlaceholder('每行一条，填写来源名称、发布机构、年份或版本及链接，供医生核对').fill('来源第二版')
 await page.getByRole('button', { name: '保存草稿并提交审核' }).click()
 await expect(page.locator('.detail .body').nth(1)).toHaveText('来源第二版')
 expect(saves[1]).toMatchObject({ version: 1, sourceText: '来源第二版' })
 await page.locator('.filters .el-select').nth(3).click()
 await page.getByRole('option', { name: '待审核', exact: true }).click()
 await expect.poll(() => queries.at(-1)).toContain('reviewStatus=pending')
 await expect(page.getByRole('button', { name: '审核后发布' })).toBeDisabled()
})
