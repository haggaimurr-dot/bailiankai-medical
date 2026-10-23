import { test, expect, type Page } from '@playwright/test'
import type { DoctorAdmin } from '../apps/admin/src/api'

const clinic = { id: 'clinic-demo', name: '演示院区', address: '演示地址', phone: '021-12345678', active: true, version: 3, publishedAt: '2026-10-01T10:00:00+08:00', presentation: { isDemo: true, brandName: '百莲凯', tagline: '用心服务', introduction: '院区介绍', logoUrl: '', hours: '09:00–18:00', transport: '地铁到院', arrivalGuide: '前台登记', credentials: [] } }
const doctor = { id: 'doctor-demo', clinicId: clinic.id, name: '草稿医生', title: '主治医师', summary: '演示医生简介', focus: ['面诊'], initials: '医', avatarUrl: '', serviceIntroduction: '专属服务介绍', presentation: { isDemo: true, credentials: [{ name: '执业资质', number: 'DEMO-001', issuer: '演示机构', imageUrl: 'https://example.com/credential.png' }] }, active: true, version: 5, publishedAt: '2026-10-01T10:00:00+08:00' }
const manager = { id: 'manager-demo', username: 'manager', displayName: '主管', role: 'manager', staffId: null, clinicIds: [clinic.id], active: true, version: 1, capabilities: ['/doctors', '/clinics', '/employees', '/appointments', '/account'] }

test('mock 资料审核通过后可发布，修改后重新审核', async ({ page }) => {
 const writes = await setup(page, { formal: true })
 await page.goto('/doctors')
 await page.getByRole('button', { name: '预览与发布' }).click()
 const preview = page.getByRole('dialog', { name: '资料发布预览' })
 await expect(preview.getByRole('button', { name: '确认发布' })).toBeDisabled()
 await page.getByRole('button', { name: '关闭预览' }).click()
 await page.getByRole('button', { name: '资质审核', exact: true }).click()
 const review = page.getByRole('dialog', { name: '医生资质审核' })
 await expect(review.getByRole('button', { name: '提交审核' })).toBeDisabled()
 await review.getByLabel('核验依据 / 退回原因', { exact: true }).fill('MOCK：已核对模拟身份和证照')
 await review.getByText('已核对医生身份、职称及证照与所属院区的对应关系', { exact: true }).click()
 await expect(review.getByRole('checkbox')).toBeChecked()
 await review.getByRole('button', { name: '提交审核' }).click()
 await expect(review).not.toBeVisible()
 expect(writes[0].body).toEqual({ version: 5, status: 'approved', note: 'MOCK：已核对模拟身份和证照' })
 await page.getByRole('button', { name: '预览与发布' }).click()
 await expect(preview.getByRole('button', { name: '确认发布' })).toBeEnabled()
 await preview.getByRole('button', { name: '确认发布' }).click()
 await expect(preview).not.toBeVisible()
 expect(writes[1].body.version).toBe(6)
 await page.getByRole('button', { name: '编辑', exact: true }).click()
 await page.getByRole('dialog', { name: '编辑医生资料' }).getByRole('button', { name: '保存草稿' }).click()
 await expect(page.getByText('待审核', { exact: true })).toBeVisible()
})

test('mock 演示审核退回原因与版本冲突', async ({ page }) => {
 const writes = await setup(page, { reviewConflict: true })
 await page.goto('/doctors'); await page.getByRole('button', { name: '资质审核', exact: true }).click()
 const review = page.getByRole('dialog', { name: '医生资质审核' })
 await expect(review.getByText('当前为 mock 资料，审核仅用于演示流程。')).toBeVisible()
 await review.getByText('退回', { exact: true }).click()
 await review.getByRole('button', { name: '提交审核' }).click()
 await expect(review.getByText('请填写核验依据或退回原因。')).toBeVisible()
 expect(writes).toHaveLength(0)
 await review.getByLabel('核验依据 / 退回原因', { exact: true }).fill('MOCK：附件需补充')
 await review.getByRole('button', { name: '提交审核' }).click()
 await expect(review.getByText(/列表已尝试刷新/)).toBeVisible()
 await expect(review.getByRole('button', { name: '提交审核' })).toBeDisabled()
 expect(writes[0].body.status).toBe('rejected')
})
async function setup(page: Page, options: { conflict?: boolean; role?: string; inactiveClinic?: boolean; seeded?: boolean; formal?: boolean; reviewConflict?: boolean } = {}) {
 const employee = { ...manager, role: options.role || 'manager', capabilities: options.role ? ['/doctors', '/appointments', '/account'] : manager.capabilities }
 let currentDoctor: DoctorAdmin = structuredClone(doctor), currentClinic = { ...clinic, active: !options.inactiveClinic }
 if (options.formal) currentDoctor.presentation!.isDemo = false
 let account = { id: 'account-1', username: 'doctor1', displayName: '待关联医生', role: 'doctor', clinicIds: [clinic.id], staffId: null as string | null, active: true, version: 7 }
 const writes: { path: string; body: any }[] = []
 await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'directory-session', employee: value })), employee)
 await page.route('https://example.com/**', route => route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64') }))
 await page.route('**/api/v1/**', async route => {
  const path = new URL(route.request().url()).pathname, method = route.request().method()
  if (method !== 'GET') {
   const body = route.request().postDataJSON(); writes.push({ path, body })
   if (path.endsWith('/review')) {
    if (options.reviewConflict) { currentDoctor.version++; await route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '资料已变化，请刷新后重试' } } }); return }
    currentDoctor = { ...currentDoctor, version: currentDoctor.version + 1, qualificationReview: { status: body.status, note: body.note, reviewerId: manager.id, reviewedAt: '2026-10-08T09:00:00+08:00' } }
    await route.fulfill({ json: { data: currentDoctor } }); return
   }
   if (options.conflict && method === 'PUT') { currentDoctor.version++; await route.fulfill({ status: 409, json: { error: { code: 'VERSION_CONFLICT', message: '资料已变化，请刷新后重试' } } }); return }
   if (path.endsWith('/employee')) { account = { ...account, staffId: doctor.id, version: 8 }; currentDoctor.version++; await route.fulfill({ json: { data: account } }); return }
   if (path.includes('/admin/doctors')) { currentDoctor = { ...currentDoctor, ...(method === 'PUT' || path.endsWith('/doctors') ? body : {}), ...(method === 'PUT' ? { qualificationReview: undefined } : {}), ...(path.endsWith('/status') ? { active: body.active } : {}), version: path.endsWith('/doctors') ? 1 : currentDoctor.version + 1 }; await route.fulfill({ json: { data: currentDoctor } }); return }
   if (path.includes('/admin/clinics')) { currentClinic = { ...currentClinic, ...body, version: path.endsWith('/clinics') ? 1 : currentClinic.version + 1 }; await route.fulfill({ json: { data: currentClinic } }); return }
  }
  let data: any = []
  if (path.endsWith('/admin/session')) data = employee
  else if (path.endsWith('/admin/clinics')) data = [currentClinic]
  else if (path.endsWith('/admin/doctors')) data = [currentDoctor]
  else if (path.endsWith('/admin/employees')) data = [account, { ...account, id: 'wrong-role', role: 'nurse' }, { ...account, id: 'wrong-clinic', clinicIds: ['other'] }, { ...account, id: 'bound', staffId: 'other' }]
  else if (path.endsWith('/history')) data = [{ id: 'revision-3', kind: path.includes('/clinics/') ? 'clinics' : 'doctors', resourceId: doctor.id, version: 3, action: options.seeded ? 'seeded' : 'published', contentJson: JSON.stringify(options.seeded ? { profile: { ...doctor, name: '初始化发布医生' }, active: true } : path.includes('/clinics/') ? clinic : { ...doctor, name: '上次发布医生' }), actorId: manager.id, createdAt: doctor.publishedAt }]
  else if (path.endsWith('/clinics')) data = [clinic]
  else if (path.endsWith('/doctors')) data = [doctor]
  await route.fulfill({ json: { data } })
 })
 return writes
}

test('医生草稿编辑、资质附件、发布快照和启停携带当前版本', async ({ page }) => {
 const writes = await setup(page)
 await page.goto('/doctors')
 await page.getByRole('button', { name: '编辑', exact: true }).click()
 const editor = page.getByRole('dialog', { name: '编辑医生资料' })
 await editor.getByLabel('医生姓名', { exact: true }).fill('更新医生')
 await editor.getByRole('button', { name: '添加资质附件' }).click()
 await editor.getByLabel('资质名称 2', { exact: true }).fill('新增资质')
 await editor.getByLabel('附件图片地址 2', { exact: true }).fill('http://invalid.com/file')
 await editor.getByRole('button', { name: '保存草稿' }).click()
 await expect(editor.getByText('每项资质须填写名称，附件地址须为有效 HTTPS 地址。')).toBeVisible()
 expect(writes).toHaveLength(0)
 await editor.getByLabel('附件图片地址 2', { exact: true }).fill('https://example.com/second.png')
 await editor.getByRole('button', { name: '预览草稿' }).click()
 await expect(page.getByRole('dialog', { name: '资料发布预览' }).getByText('新增资质', { exact: true })).toBeVisible()
 await expect(page.getByRole('button', { name: '确认发布' })).toHaveCount(0)
 await page.getByRole('button', { name: '关闭预览' }).click()
 await editor.getByRole('button', { name: '保存草稿' }).click()
 await expect(editor).not.toBeVisible()
 expect(writes[0].body.version).toBe(5)
 expect(writes[0].body.presentation.credentials[1].imageUrl).toBe('https://example.com/second.png')
 expect(writes[0].body).not.toHaveProperty('active')
 await page.getByRole('button', { name: '预览与发布' }).click()
 const preview = page.getByRole('dialog', { name: '资料发布预览' })
 await expect(preview.getByRole('heading', { name: '更新医生', exact: true })).toBeVisible()
 await expect(preview.getByRole('heading', { name: '上次发布医生', exact: true })).toBeVisible()
 await preview.getByRole('button', { name: '确认发布' }).click()
 await expect(preview).not.toBeVisible()
 expect(writes[1].body).toEqual({ version: 6 })
 await page.getByRole('button', { name: '停用', exact: true }).click()
 await page.getByRole('button', { name: '确认', exact: true }).click()
 await expect(page.getByRole('button', { name: '启用', exact: true })).toBeVisible()
 expect(writes[2].body).toEqual({ version: 7, active: false })
 await page.getByRole('button', { name: '历史', exact: true }).click()
 await expect(page.getByText('v3 · 发布资料', { exact: true })).toBeVisible()
})

test('版本冲突保留草稿，重开编辑读取最新版本', async ({ page }) => {
 const writes = await setup(page, { conflict: true })
 await page.goto('/doctors'); await page.getByRole('button', { name: '编辑', exact: true }).click()
 const editor = page.getByRole('dialog', { name: '编辑医生资料' })
 await editor.getByLabel('医生姓名', { exact: true }).fill('未保存修改')
 await editor.getByRole('button', { name: '保存草稿' }).click()
 await expect(editor.getByText(/列表已尝试刷新/)).toBeVisible()
 await expect(editor.getByLabel('医生姓名', { exact: true })).toHaveValue('未保存修改')
 await expect(editor.getByRole('button', { name: '保存草稿' })).toBeDisabled()
 expect(writes).toHaveLength(1)
 await editor.getByRole('button', { name: '取消', exact: true }).click()
 await page.getByRole('button', { name: '编辑', exact: true }).click()
 await expect(editor.getByLabel('医生姓名', { exact: true })).toHaveValue('草稿医生')
 await expect(editor.getByRole('button', { name: '保存草稿' })).toBeEnabled()
})

test('账号关联只允许符合条件的账号，并发送双方版本', async ({ page }) => {
 const writes = await setup(page)
 await page.goto('/doctors'); await page.getByRole('button', { name: '账号关联' }).click()
 await page.getByRole('dialog', { name: '医生账号关联' }).locator('.el-select').click()
 await expect(page.getByRole('option')).toHaveCount(1)
 await page.getByRole('option', { name: '待关联医生（doctor1）' }).click()
 await page.getByRole('button', { name: '确认关联' }).click()
 await expect(page.getByRole('dialog', { name: '医生账号关联' })).not.toBeVisible()
 expect(writes[0].body).toEqual({ employeeId: 'account-1', version: 5, employeeVersion: 7 })
 await page.getByRole('button', { name: '账号关联' }).click()
 await expect(page.getByText('已关联：待关联医生（doctor1） · 账号启用')).toBeVisible()
 await expect(page.getByRole('button', { name: '确认关联' })).toBeDisabled()
})

test('院区新增保存展示信息和资质，医生发布受院区状态限制', async ({ page }) => {
 const writes = await setup(page, { inactiveClinic: true })
 await page.goto('/doctors'); await page.getByRole('button', { name: '预览与发布' }).click()
 await expect(page.getByText('所属院区须先启用并发布。')).toBeVisible()
 await expect(page.getByRole('button', { name: '确认发布' })).toBeDisabled()
 await page.getByRole('button', { name: '关闭预览' }).click()
 await page.getByRole('button', { name: '院区资料', exact: true }).click()
 await page.getByRole('button', { name: '新增院区' }).click()
 const editor = page.getByRole('dialog', { name: '新增院区资料' })
 for (const [label, value] of [['院区名称', '新增院区'], ['院区地址', '新地址'], ['联系电话', '021-00000000'], ['品牌名称', '品牌名称']]) await editor.getByLabel(label, { exact: true }).fill(value)
 await editor.getByRole('button', { name: '添加资质附件' }).click()
 await editor.getByLabel('资质名称 1', { exact: true }).fill('机构资质')
 await editor.getByRole('button', { name: '保存草稿' }).click()
 await expect(editor).not.toBeVisible()
 expect(writes[0].path).toBe('/api/v1/admin/clinics')
 expect(writes[0].body).not.toHaveProperty('version')
 expect(writes[0].body.presentation.credentials).toEqual([{ name: '机构资质', number: '', issuer: '', imageUrl: '' }])
})

test('非主管保留医生查看页面，不能进入院区维护', async ({ page }) => {
 await setup(page, { role: 'doctor' }); await page.goto('/doctors')
 await expect(page.getByRole('button', { name: '新增医生' })).toHaveCount(0)
 await expect(page.getByRole('button', { name: '编辑', exact: true })).toHaveCount(0)
 await page.goto('/clinics'); await expect(page).not.toHaveURL(/\/clinics/)
})


test('新增医生、院区编辑和初始化发布快照', async ({ page }) => {
 const writes = await setup(page, { seeded: true })
 await page.goto('/doctors'); await page.getByRole('button', { name: '预览与发布' }).click()
 await expect(page.getByRole('dialog', { name: '资料发布预览' }).getByRole('heading', { name: '初始化发布医生' })).toBeVisible()
 await page.getByRole('button', { name: '关闭预览' }).click()
 await page.getByRole('button', { name: '新增医生' }).click()
 const doctorEditor = page.getByRole('dialog', { name: '新增医生资料' })
 for (const [label, value] of [['医生姓名', '新增医生'], ['职称', '主治医师'], ['医生简介', '新简介'], ['头像缩写', '新'], ['擅长项目', '项目一、项目二']]) await doctorEditor.getByLabel(label, { exact: true }).fill(value)
 await doctorEditor.getByRole('button', { name: '保存草稿' }).click()
 await expect(doctorEditor).not.toBeVisible()
 expect(writes[0].path).toBe('/api/v1/admin/doctors')
 expect(writes[0].body.focus).toEqual(['项目一', '项目二'])
 expect(writes[0].body).not.toHaveProperty('version')
 await page.getByRole('button', { name: '院区资料', exact: true }).click()
 await page.getByRole('button', { name: '编辑', exact: true }).click()
 const clinicEditor = page.getByRole('dialog', { name: '编辑院区资料' })
 await clinicEditor.getByLabel('到院指引', { exact: true }).fill('更新到院指引')
 await clinicEditor.getByRole('button', { name: '保存草稿' }).click()
 await expect(clinicEditor).not.toBeVisible()
 expect(writes[1].body.version).toBe(3)
 expect(writes[1].body.presentation.arrivalGuide).toBe('更新到院指引')
 expect(writes[1].body.presentation.brandName).toBe('百莲凯')
})
