import { test, expect } from '@playwright/test'

test('manual chart offers one-time WeChat binding, revocation and status refresh', async ({ page }) => {
  const employee = { id: 'binding-manager', username: 'manager', displayName: '档案管理员', role: 'manager', capabilities: ['/patients', '/account'], clinicIds: ['clinic-demo'], active: true, version: 1 }
  const patient = { id: 'patient-manual-binding', displayName: '人工建档患者', createdAt: '2026-10-08T09:00:00+08:00', appointmentCount: 0, wechatBound: false, profile: { patientId: 'patient-manual-binding', clinicId: 'clinic-demo', gender: 'unknown', birthDate: '', contactPhone: '13800000000', tags: [], version: 1 } }
  let generation = 0, revoked = 0, conflict = false
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'binding-token', employee: value })), employee)
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/wechat-binding')) {
      if (route.request().method() === 'DELETE') { revoked++; await route.fulfill({ status: 204 }) }
      else if (conflict) await route.fulfill({ status: 409, json: { error: { code: 'PATIENT_BINDING_CONFLICT', message: '档案已绑定或资料已变化，请刷新后核对' } } })
      else { generation++; await route.fulfill({ status: 201, json: { data: { code: String(generation).repeat(32), expiresAt: '2026-10-08T09:15:00+08:00' } } }) }
    } else if (path.endsWith('/admin/patients/' + patient.id)) await route.fulfill({ json: { data: { patient, consultations: [], encounters: [] } } })
    else if (path.endsWith('/admin/patients')) await route.fulfill({ json: { data: [patient], total: 1, page: 1, pageSize: 20 } })
    else if (path.endsWith('/admin/session')) await route.fulfill({ json: { data: employee } })
    else if (path.endsWith('/clinics')) await route.fulfill({ json: { data: [{ id: 'clinic-demo', name: '演示院区' }] } })
    else await route.fulfill({ json: { data: [] } })
  })
  await page.goto('/patients')
  await page.getByRole('button', { name: /人工建档患者/ }).click()
  await page.getByRole('button', { name: '微信身份绑定' }).click()
  const dialog = page.getByRole('dialog', { name: '绑定患者微信身份' })
  await expect(dialog.getByText('尚未绑定微信患者身份')).toBeVisible()
  await dialog.getByRole('button', { name: '生成绑定码', exact: true }).click()
  await expect(dialog.getByRole('textbox', { name: '患者微信绑定码' })).toHaveValue('1'.repeat(32))
  await dialog.getByRole('button', { name: '重新生成绑定码' }).click()
  await expect(dialog.getByRole('textbox', { name: '患者微信绑定码' })).toHaveValue('2'.repeat(32))
  await dialog.getByRole('button', { name: '撤销未使用的绑定码' }).click()
  await expect(dialog.getByRole('textbox', { name: '患者微信绑定码' })).toHaveCount(0)
  expect(revoked).toBe(1)
  conflict = true
  await dialog.getByRole('button', { name: '生成绑定码', exact: true }).click()
  await expect(dialog.getByText('档案已绑定或资料已变化，请刷新后核对')).toBeVisible()
  patient.wechatBound = true
  await dialog.getByRole('button', { name: '刷新绑定状态' }).click()
  await expect(dialog.getByText('已绑定微信患者身份')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '生成绑定码', exact: true })).toHaveCount(0)
});

test('patient directory aggregates visits and saves independent consultations', async ({ page }) => {
  const patient = { id: 'patient-unified', displayName: '档案演示患者', createdAt: '2026-10-01T09:00:00+08:00', appointmentCount: 2 }
  const consultations: { id: string; recordedAt: string; [key: string]: unknown }[] = []
  const employee = { id: 'employee-manager', username: 'manager', displayName: '演示管理员', role: 'manager', capabilities: ['/patients', '/account'], staffId: null, active: true, version: 1 }
  const encounter = (index: number) => ({
    appointment: { id: 'appointment-' + index, patientId: patient.id, clinicId: 'clinic-demo', startsAt: '2026-10-0' + index + 'T10:00:00+08:00', clinicName: '演示诊所', doctorName: '演示医生' + index, status: 'completed', note: '第' + index + '次预约' },
    carePlan: { draftVersion: 0, draft: null, versions: [{ id: 'plan-' + index, version: 1, publishedAt: '2026-10-01T09:00:00+08:00', changeReason: '初次发布', content: { summary: '方案' + index, stages: [], instructions: '护理说明' } }] },
    photos: index === 1 ? [{ id: 'photo-1', appointmentId: 'appointment-1', angle: 'front', uploadedAt: '2026-10-01T09:00:00+08:00' }] : [],
    followup: { treatment: { id: 'treatment-' + index, summary: '治疗' + index, notes: '治疗备注', treatedAt: '2026-10-01T10:00:00+08:00', version: 1 }, treatmentRevisions: [], tasks: [{ id: 'task-' + index, appointmentId: 'appointment-' + index, title: '随访' + index, dueAt: '2026-10-02T10:00:00+08:00', status: 'pending', assigneeName: '演示医护', symptoms: '', response: '', photos: [] }] },
  })
  let posted: Record<string, string> | undefined
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-patient-record-token', employee: value })), employee)
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/consultations')) {
      posted = route.request().postDataJSON()
      const item = { ...posted, id: 'consultation-' + (consultations.length + 1), patientId: patient.id, recordedBy: employee.id, recordedAt: '2026-10-02T10:00:00+08:00' }
      consultations.unshift(item)
      await route.fulfill({ status: 201, json: { data: item } })
    } else if (url.pathname.endsWith('/admin/patients/' + patient.id)) {
      const filtered = url.searchParams.has('dateFrom') ? consultations.filter(item => item.recordedAt.slice(0, 10) >= url.searchParams.get('dateFrom')! && item.recordedAt.slice(0, 10) <= url.searchParams.get('dateTo')!) : consultations
      await route.fulfill({ json: { data: { patient, consultations: filtered, encounters: [encounter(2), encounter(1)] } } })
    } else if (url.pathname.endsWith('/admin/patients')) {
      await route.fulfill({ json: { data: [patient], total: 1, page: 1, pageSize: 20 } })
    } else if (url.pathname.endsWith('/admin/session')) {
      await route.fulfill({ json: { data: employee } })
    } else { await route.fulfill({ json: { data: [] } }) }
  })
  await page.goto('/patients')
  await page.getByRole('button', { name: /档案演示患者/ }).click()
  await expect(page.getByRole('heading', { name: '档案演示患者' })).toBeVisible()
  await expect(page).toHaveURL(/patientId=patient-unified/)
  await page.getByLabel('主诉（必填）').fill('痘坑与痘印问诊')
  await page.getByLabel('病史 / 过敏史').fill('无已知过敏史')
  await page.getByRole('button', { name: '保存问诊记录' }).click()
  await expect(page.getByRole('heading', { name: '初诊 · 痘坑与痘印问诊' })).toBeVisible()
  expect(posted).toMatchObject({ clinicId: 'clinic-demo', visitType: 'initial', stageIndex: 0, stageStatus: 'not_started', chiefComplaint: '痘坑与痘印问诊', history: '无已知过敏史', assessment: '', advice: '' })
  await page.locator('.el-form-item').filter({ hasText: '就诊类型' }).locator('.el-select').click()
  await page.getByRole('option', { name: '复诊', exact: true }).click()
  await page.locator('.el-form-item').filter({ hasText: '关联既往问诊（必填）' }).locator('.el-select').click()
  await page.getByRole('option', { name: /痘坑与痘印问诊/ }).click()
  await page.getByLabel('主诉（必填）').fill('恢复情况复诊')
  await page.getByRole('button', { name: '保存问诊记录' }).click()
  await expect(page.getByRole('heading', { name: '复诊 · 恢复情况复诊' })).toBeVisible()
  expect(posted).toMatchObject({ visitType: 'revisit', parentConsultationId: 'consultation-1' })
  const filteredResponse = page.waitForResponse(response => response.url().includes('dateFrom=2026-10-03') && response.url().includes('dateTo=2026-10-04'))
  await page.getByPlaceholder('问诊起始日期', { exact: true }).fill('2026-10-03')
  await page.getByPlaceholder('问诊结束日期', { exact: true }).fill('2026-10-04')
  await page.getByPlaceholder('问诊结束日期', { exact: true }).press('Enter')
  await filteredResponse
  await expect(page.getByRole('heading', { name: '复诊 · 恢复情况复诊' })).toHaveCount(0)
  await page.getByRole('tab', { name: '预约 (2)' }).click()
  await expect(page.getByText('第1次预约')).toBeVisible()
  await expect(page.getByText('第2次预约')).toBeVisible()
  await page.getByRole('tab', { name: '方案 (2)' }).click()
  await expect(page.getByRole('heading', { name: /方案1/ })).toBeVisible()
  await expect(page.getByRole('heading', { name: /方案2/ })).toBeVisible()
  await page.getByRole('tab', { name: '治疗 (2)' }).click()
  await expect(page.getByRole('heading', { name: '治疗1' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '治疗2' })).toBeVisible()
  await page.getByRole('tab', { name: '随访 (2)' }).click()
  await expect(page.getByRole('heading', { name: /随访1/ })).toBeVisible()
  await page.getByRole('tab', { name: '照片 (1)' }).click()
  await expect(page.getByRole('button', { name: '查看授权照片' })).toBeVisible()
  await expect(page.getByText('面诊照片 · 正面')).toBeVisible()
})


test('direct chart creation supports structured consultations, reusable templates and persistent tags', async ({ page }) => {
 const employee = { id: 'employee-manager', role: 'manager', capabilities: ['/patients', '/account'], displayName: '管理员', clinicIds: ['clinic-demo'], staffId: null, active: true, version: 1 }
 type Chart = { patientId: string; clinicId: string; gender: string; birthDate: string; contactPhone: string; tags: string[]; version: number }
 let patient: { id: string; displayName: string; createdAt: string; appointmentCount: number; profile: Chart } | undefined
 const consultations: Record<string, unknown>[] = []
 const templates: Record<string, unknown>[] = []
 let consultationPost: Record<string, unknown> = {}
 let templatePost: Record<string, unknown> = {}
 await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'chart-test', employee: value })), employee)
 await page.route('**/api/v1/**', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname, body = req.method() === 'POST' || req.method() === 'PUT' ? req.postDataJSON() : null
  if (path.endsWith('/admin/session')) return route.fulfill({ json: { data: employee } })
  if (path.endsWith('/clinics')) return route.fulfill({ json: { data: [{ id: 'clinic-demo', name: '演示院区' }] } })
  if (path.endsWith('/consultation-templates')) {
   if (body) { templatePost = body; const item = { ...body, id: 'template-1', recordedBy: employee.id, createdAt: '2026-10-02T10:00:00+08:00' }; templates.push(item); return route.fulfill({ status: 201, json: { data: item } }) }
   return route.fulfill({ json: { data: templates } })
  }
  if (path.endsWith('/consultation-templates/template-1')) { templates.length = 0; return route.fulfill({ status: 204 }) }
  if (path.endsWith('/admin/patients')) {
   if (body) { patient = { id: 'patient-direct', displayName: body.displayName, createdAt: '2026-10-02T09:00:00+08:00', appointmentCount: 0, profile: { ...body, patientId: 'patient-direct', version: 1 } }; return route.fulfill({ status: 201, json: { data: patient } }) }
   const matched = patient && (!url.searchParams.get('tag') || patient.profile.tags.includes(url.searchParams.get('tag')!))
   return route.fulfill({ json: { data: matched ? [patient] : [], total: matched ? 1 : 0, page: 1, pageSize: 20 } })
  }
  if (path.endsWith('/patient-direct/profile')) { patient!.displayName = body.displayName; patient!.profile = { ...body, patientId: 'patient-direct', version: body.version + 1 }; return route.fulfill({ json: { data: patient!.profile } }) }
  if (path.endsWith('/patient-direct/consultations')) { consultationPost = body; const item = { ...body, id: 'consultation-' + consultations.length, recordedAt: '2026-10-02T10:00:00+08:00', recordedBy: employee.id }; consultations.unshift(item); return route.fulfill({ status: 201, json: { data: item } }) }
  if (path.endsWith('/admin/patients/patient-direct')) return route.fulfill({ json: { data: { patient, consultations, encounters: [] } } })
  return route.fulfill({ json: { data: [] } })
 })
 await page.goto('/patients')
 await page.getByRole('button', { name: '新建患者档案' }).click()
 const dialog = page.getByRole('dialog', { name: '直接新建患者档案' })
 await dialog.getByLabel('患者姓名（必填）').fill('直接建档患者')
 await dialog.getByLabel('联系手机号').fill('123')
 await dialog.getByRole('button', { name: '创建档案', exact: true }).click()
 await expect(page.getByText('请填写有效的11位手机号')).toBeVisible()
 await dialog.getByLabel('联系手机号').fill('13800138000')
 await dialog.locator('.el-form-item').filter({ hasText: '患者标签' }).locator('input').fill('敏感肌')
 await page.getByRole('option', { name: '敏感肌', exact: true }).click()
 await dialog.getByLabel('联系手机号').click()
 await dialog.getByRole('button', { name: '创建档案', exact: true }).click()
 await expect(page.getByRole('heading', { name: '直接建档患者' })).toBeVisible()
 await expect(page.getByText('联系手机号：13800138000')).toBeVisible()
 await page.getByLabel('主诉（必填）').fill('皮肤状态评估')
 await page.getByLabel('既往病史', { exact: true }).fill('既往病史已核对')
 await page.getByLabel('过敏史', { exact: true }).fill('患者自述对某成分过敏')
 await page.getByLabel('既往治疗史').fill('既往治疗已核对')
 await page.getByLabel('患者诉求').fill('改善肤质')
 await page.getByRole('button', { name: '存为模板' }).click()
 const prompt = page.getByRole('dialog', { name: '保存问诊模板' })
 await prompt.getByPlaceholder('模板名称').fill('皮肤初诊模板')
 await prompt.getByRole('button', { name: '确定', exact: true }).click()
 await expect(page.getByText('问诊模板已保存')).toBeVisible()
 expect(templatePost).toMatchObject({ name: '皮肤初诊模板', medicalHistory: '既往病史已核对', goals: '改善肤质' })
 expect(templatePost).not.toHaveProperty('patientId')
 expect(templatePost).not.toHaveProperty('appointmentId')
 await page.getByRole('button', { name: '保存问诊记录' }).click()
 await expect(page.getByRole('heading', { name: '初诊 · 皮肤状态评估' })).toBeVisible()
 expect(consultationPost).toMatchObject({ clinicId: 'clinic-demo', medicalHistory: '既往病史已核对', allergyHistory: '患者自述对某成分过敏', treatmentHistory: '既往治疗已核对', goals: '改善肤质' })
 await page.reload()
 await expect(page.getByRole('heading', { name: '直接建档患者' })).toBeVisible()
 await page.getByLabel('主诉（必填）').fill('新的患者主诉')
 await page.locator('.template-toolbar .el-select').click()
 await page.getByRole('option', { name: '皮肤初诊模板 · 初诊' }).click()
 await page.getByRole('button', { name: '应用模板', exact: true }).click()
 await expect(page.getByLabel('主诉（必填）')).toHaveValue('新的患者主诉')
 await expect(page.getByLabel('过敏史', { exact: true })).toHaveValue('患者自述对某成分过敏')
 await page.getByRole('button', { name: '编辑资料 / 标签' }).click()
 const edit = page.getByRole('dialog', { name: '编辑患者资料与标签' })
 await edit.getByLabel('患者姓名（必填）').fill('更新档案患者')
 await edit.locator('.el-form-item').filter({ hasText: '患者标签' }).locator('input').fill('复诊跟进')
 await page.getByRole('option', { name: '复诊跟进', exact: true }).click()
 await edit.getByLabel('患者姓名（必填）').click()
 await edit.getByRole('button', { name: '保存患者资料', exact: true }).click()
 await expect(page.getByRole('heading', { name: '更新档案患者' })).toBeVisible()
 await page.reload()
 await expect(page.locator('.patient-header')).toContainText('更新档案患者')
 await expect(page.locator('.patient-detail > .patient-tags')).toContainText('复诊跟进')
 await page.locator('.template-toolbar .el-select').click()
 await page.getByRole('option', { name: '皮肤初诊模板 · 初诊' }).click()
 const tagResponse = page.waitForResponse(response => response.url().includes('/admin/patients?') && response.url().includes('tag='))
 await page.locator('.tag-filter').click()
 await page.getByRole('option', { name: '复诊跟进', exact: true }).click()
 await tagResponse
 await expect(page.getByRole('button', { name: /更新档案患者/ })).toBeVisible()
 await page.getByRole('button', { name: '删除模板', exact: true }).click()
 await expect(page.getByText('模板已删除')).toBeVisible()
})
