import { test, expect } from '@playwright/test';
test.use({ baseURL: process.env.FOLLOWUP_TEST_BASE_URL || 'http://127.0.0.1:5173' });

test('随访日期筛选、模板天数校验与独立护理反馈', async ({ page }) => {
  const employee = { id: 'manager-test', username: 'test', displayName: '测试主管', role: 'manager', clinicIds: ['clinic-demo'], staffId: null, active: true, version: 1, capabilities: ['/followups', '/account'] };
  await page.addInitScript(employee => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-session', employee })), employee);
  let template: any = null;
  let feedback: any[] = [];
  const nursingBodies: any[] = [];
  let treatmentBody: any = null;
  const queueQueries: string[] = [];
  const appointment = { id: 'appt-test', contactName: '测试患者', doctorName: '测试医生', clinicId: 'clinic-demo', doctorId: 'doctor-test', status: 'completed', startsAt: '2026-10-07T01:00:00Z', version: 1 };
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname, method = route.request().method();
    let data: any = [], meta: any = undefined;
    if (path.endsWith('/admin/session')) data = employee;
    else if (path.endsWith('/clinics')) data = [{ id: 'clinic-demo', name: '测试院区' }];
    else if (path.endsWith('/admin/followup-templates')) {
      if (method === 'POST') template = { ...route.request().postDataJSON(), id: 'template-test', version: 1, updatedAt: '2026-10-07T02:00:00Z' };
      data = method === 'POST' ? template : template ? [template] : [];
    } else if (path.endsWith('/nursing-feedback')) {
      if (method === 'POST') {
        const body = route.request().postDataJSON(); nursingBodies.push(body);
        if (nursingBodies.length === 1) { await route.fulfill({ status: 503, json: { error: { code: 'SERVICE_UNAVAILABLE', message: '测试网络中断，请重试' } } }); return; }
        feedback = [{ ...body, id: 'feedback-test', actorName: '测试主管', createdAt: body.observedAt }]; data = feedback[0];
      } else data = feedback;
    } else if (path.endsWith('/admin/appointments')) { data = [appointment]; meta = { total: 1, page: 1, pageSize: 20 }; }
    else if (path.endsWith('/admin/followups')) { queueQueries.push(url.search); meta = { total: 0, page: 1, pageSize: 20 }; }
    else if (path.endsWith('/admin/followup-staff')) data = [{ id: 'doctor-test', displayName: '测试医生', role: 'doctor', status: 'on_duty' }];
    else if (path.endsWith('/care-plan')) data = { versions: [{ id: 'plan-test', version: 1 }] };
    else if (path.endsWith('/appointments/appt-test/followups')) data = { treatment: null, treatmentRevisions: [], tasks: [] };
    else if (path.endsWith('/treatment')) {
      treatmentBody = route.request().postDataJSON();
      data = { treatment: { ...treatmentBody, id: 'tr-test', version: 1 }, treatmentRevisions: [], tasks: treatmentBody.offsets.map((offset: number) => ({ id: `task-${offset}`, title: `第 ${offset} 天随访`, dueAt: '2026-10-08T01:00:00Z', status: 'pending' })) };
    }
    await route.fulfill({ json: { data, ...(meta ? { meta } : {}) } });
  });
  await page.goto('/followups');
  await expect(page.getByText('随访到期日期（北京时间）')).toBeVisible();
  const dateInputs = page.locator('.date-filter input');
  await dateInputs.nth(0).fill('2026-10-01'); await dateInputs.nth(1).fill('2026-10-07'); await dateInputs.nth(1).press('Enter');
  await expect.poll(() => queueQueries.some(q => q.includes('dateFrom=2026-10-01') && q.includes('dateTo=2026-10-07'))).toBeTruthy();
  await page.getByRole('button', { name: '清空日期' }).click();
  await expect.poll(() => queueQueries.at(-1)).not.toContain('dateFrom');
  await page.getByText('护理与随访规范', { exact: true }).first().click();
  await page.getByRole('button', { name: '新增模板' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('模板名称', { exact: true }).fill('自定义随访');
  await dialog.getByRole('spinbutton', { name: '新增随访天数' }).fill('3');
  await dialog.getByRole('button', { name: '添加天数' }).click();
  await expect(dialog.getByRole('alert').filter({ hasText: '该天数已添加' })).toBeVisible();
  await dialog.getByRole('spinbutton', { name: '新增随访天数' }).fill('14');
  await dialog.getByRole('button', { name: '添加天数' }).click();
  await expect(dialog.getByText('第 14 天', { exact: true })).toBeVisible();
  await dialog.getByText('正式规范', { exact: true }).click();
  await dialog.getByRole('button', { name: '保存模板', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('正式规范须填写');
  expect(template).toBeNull();
  await dialog.getByLabel('适用治疗', { exact: true }).fill('测试治疗范围');
  await dialog.getByLabel('护理要求', { exact: true }).fill('测试护理规范，仅用于功能验收');
  await dialog.getByLabel('随访检查要点', { exact: true }).fill('测试随访检查事项');
  await dialog.getByLabel('回评文本', { exact: true }).fill('测试回评参考');
  await dialog.getByLabel('异常处理参考文本', { exact: true }).fill('测试异常上报流程');
  await dialog.locator('.el-switch').click();
  await dialog.getByRole('button', { name: '保存模板', exact: true }).click();
  await expect(page.getByText('治疗后 1、3、7、14 天', { exact: true })).toBeVisible();
  expect(template.offsets).toEqual([1, 3, 7, 14]);
  expect(template.standardStatus).toBe('formal');
  expect(template.active).toBe(true);
  expect(template.nursingInstructions).toBe('测试护理规范，仅用于功能验收');
  await page.getByText('护理反馈', { exact: true }).click();
  await page.getByRole('button', { name: /测试患者/ }).click();
  await page.getByText('选择本院区已启用正式规范', { exact: true }).click();
  await page.getByRole('option', { name: '自定义随访', exact: true }).click();
  await expect(page.locator('.standard-reference')).toContainText('测试护理规范，仅用于功能验收');
  await expect(page.getByLabel('实际护理措施 / 跟进安排', { exact: true })).toHaveValue('');
  await page.getByLabel('护理情况 / 患者反馈', { exact: true }).fill('记录实际护理观察');
  await page.getByLabel('实际护理措施 / 跟进安排', { exact: true }).fill('记录已执行护理措施');
  await page.getByRole('button', { name: '保存护理反馈' }).click();
  await expect(page.getByText('测试网络中断，请重试')).toBeVisible();
  await expect(page.getByLabel('护理情况 / 患者反馈', { exact: true })).toHaveValue('记录实际护理观察');
  await page.getByRole('button', { name: '保存护理反馈' }).click();
  await expect(page.locator('.entry').filter({ hasText: '记录实际护理观察' })).toBeVisible();
  expect(nursingBodies[0].requestKey).toBe(nursingBodies[1].requestKey);
  await page.getByText('治疗记录与更正', { exact: true }).click();
  await page.locator('.list-item').filter({ hasText: '测试患者' }).click();
  await page.getByText('选择本院区启用模板', { exact: true }).click();
  await page.getByRole('option', { name: '自定义随访', exact: true }).click();
  await page.getByRole('button', { name: '应用随访天数' }).click();
  await expect(page.locator('.days-tags')).toContainText('第 14 天');
  await page.getByLabel('实际治疗摘要', { exact: true }).fill('实际治疗模板应用测试');
  await page.getByRole('button', { name: '记录治疗并生成随访', exact: true }).click();
  await page.getByRole('button', { name: '确认并生成任务', exact: true }).click();
  await expect(page.locator('.task-line')).toHaveCount(4);
  expect(treatmentBody.offsets).toEqual([1, 3, 7, 14]);
});


test('草稿保存、版本冲突保留编辑内容及停用规范', async ({ page }) => {
  const employee = { id: 'manager-test', displayName: '测试主管', role: 'manager', clinicIds: ['clinic-demo'], capabilities: ['/followups'] };
  await page.addInitScript(employee => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-session', employee })), employee);
  let standard: any = null, conflict = true;
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname, method = route.request().method();
    let data: any = [], meta: any;
    if (path.endsWith('/admin/session')) data = employee;
    else if (path.endsWith('/clinics')) data = [{ id: 'clinic-demo', name: '测试院区' }];
    else if (path.endsWith('/admin/followup-templates')) {
      if (method === 'POST') { standard = { ...route.request().postDataJSON(), id: 'standard-test', version: 1, updatedAt: '2026-10-08T01:00:00Z' }; data = standard; }
      else data = standard ? [standard] : [];
    } else if (path.endsWith('/followup-templates/standard-test')) {
      if (conflict) { conflict = false; await route.fulfill({ status: 409, json: { error: { code: 'CONFLICT', message: '规范已被其他主管修改，请刷新核对' } } }); return; }
      const body = route.request().postDataJSON(); expect(body.version).toBe(1);
      standard = { ...standard, ...body, version: 2 }; data = standard;
    } else if (path.endsWith('/admin/followups')) meta = { total: 0 };
    await route.fulfill({ json: { data, ...(meta ? { meta } : {}) } });
  });
  await page.goto('/followups');
  await page.getByText('护理与随访规范', { exact: true }).first().click();
  await page.getByRole('button', { name: '新增模板' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('模板名称', { exact: true }).fill('待完善规范');
  await expect(dialog.getByRole('switch')).toBeDisabled();
  await dialog.getByRole('button', { name: '保存模板', exact: true }).click();
  await expect(page.getByText('草稿待完善', { exact: true })).toBeVisible();
  expect(standard.standardStatus).toBe('draft'); expect(standard.active).toBe(false);
  await page.getByRole('button', { name: '编辑模板' }).click();
  await dialog.getByLabel('适用治疗', { exact: true }).fill('测试适用范围');
  await dialog.getByLabel('护理要求', { exact: true }).fill('测试护理要求');
  await dialog.getByLabel('随访检查要点', { exact: true }).fill('测试检查要点');
  await dialog.getByLabel('回评文本', { exact: true }).fill('测试回评');
  await dialog.getByLabel('异常处理参考文本', { exact: true }).fill('测试异常流程');
  await dialog.getByText('正式规范', { exact: true }).click();
  await dialog.getByRole('button', { name: '保存模板', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('规范已被其他主管修改');
  await expect(dialog.getByLabel('护理要求', { exact: true })).toHaveValue('测试护理要求');
  await dialog.getByRole('button', { name: '保存模板', exact: true }).click();
  await expect(page.locator('.entry')).toContainText('正式规范');
  await expect(page.locator('.entry')).toContainText('停用');
  expect(standard.active).toBe(false);
});

test('护理人员只读规范，护理参考排除草稿、停用和其他院区', async ({ page }) => {
  const employee = { id: 'nurse-test', displayName: '测试护理', role: 'nurse', clinicIds: ['clinic-demo'], capabilities: ['/followups'] };
  await page.addInitScript(employee => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'test-session', employee })), employee);
  const base = { clinicId: 'clinic-demo', offsets: [1, 7], response: '测试回评', disposition: '测试异常流程', applicableTreatment: '测试范围', nursingInstructions: '测试护理参考', followupChecklist: '测试检查参考', version: 1, updatedAt: '2026-10-08T01:00:00Z' };
  const standards = [
    { ...base, id: 'formal', name: '正式启用规范', active: true, standardStatus: 'formal' },
    { ...base, id: 'draft', name: '草稿规范', active: true, standardStatus: 'draft' },
    { ...base, id: 'disabled', name: '停用规范', active: false, standardStatus: 'formal' },
    { ...base, id: 'foreign', name: '其他院区规范', clinicId: 'clinic-other', active: true, standardStatus: 'formal' },
  ];
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname;
    let data: any = [], meta: any;
    if (path.endsWith('/admin/session')) data = employee;
    else if (path.endsWith('/clinics')) data = [{ id: 'clinic-demo', name: '测试院区' }];
    else if (path.endsWith('/admin/followup-templates')) data = standards;
    else if (path.endsWith('/admin/appointments')) { data = [{ id: 'appt-test', clinicId: 'clinic-demo', contactName: '测试患者', doctorName: '测试医生', startsAt: '2026-10-07T01:00:00Z', status: 'completed' }]; meta = { total: 1 }; }
    else if (path.endsWith('/admin/followups')) meta = { total: 0 };
    await route.fulfill({ json: { data, ...(meta ? { meta } : {}) } });
  });
  await page.goto('/followups');
  await page.getByText('护理与随访规范', { exact: true }).first().click();
  await expect(page.locator('.entry')).toHaveCount(4);
  await expect(page.getByRole('button', { name: '新增模板' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: '编辑模板' })).toHaveCount(0);
  await page.getByText('护理反馈', { exact: true }).click();
  await page.getByRole('button', { name: /测试患者/ }).click();
  await page.getByText('选择本院区已启用正式规范', { exact: true }).click();
  await expect(page.getByRole('option')).toHaveCount(1);
  await page.getByRole('option', { name: '正式启用规范', exact: true }).click();
  await expect(page.locator('.standard-reference')).toContainText('测试护理参考');
});
