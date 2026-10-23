import { test, expect, request as apiRequest } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { acceptPrivacy, loginEmployee } from './helpers/live-auth';
import type { components } from '../packages/contracts/src/schema';
const base = 'http://127.0.0.1:8080/api/v1';
let adminToken: string;
test.beforeEach(async ({ page, request }) => { adminToken = (await loginEmployee(request, page)).token; });
test.afterEach(async ({ request }) => { await request.delete(`${base}/admin/session`, { headers: auth(adminToken) }); });
const auth = (token: string) => ({ Authorization: `Bearer ${token}` });
test('实际治疗生成任务，普通回评与异常处理在医护页面完成，患者可读结果', async ({ page, request }) => {
  test.setTimeout(60000);
  const session = (await (await request.post(`${base}/dev/patient-sessions`, { data: {} })).json()).data;
  await acceptPrivacy(request, session.token);
  const slots: components['schemas']['Slot'][] = (await (await request.get(`${base}/slots`)).json()).data;
  const slot = slots.find(item => item.status === 'open' && item.reserved < item.capacity && Date.parse(item.startsAt) > Date.now() + 3600000)!;
  expect(slot).toBeTruthy();
  const name = `随访验收${randomUUID().slice(0, 6)}`;
  const created = await request.post(`${base}/me/appointments`, { headers: { ...auth(session.token), 'Idempotency-Key': randomUUID() }, data: { slotId: slot.id, contactName: name, contactPhone: '13800000000' } });
  expect(created.status()).toBe(201);
  let appointment = (await created.json()).data;
  try {
    const confirmed = await request.post(`${base}/admin/appointments/${appointment.id}/review`, { headers: auth(adminToken!), data: { version: appointment.version, action: 'confirm' } });
    expect(confirmed.status()).toBe(200); appointment = (await confirmed.json()).data;
    const content = { summary: '虚构随访方案', stages: [{ title: '演示阶段', body: '虚构内容' }], instructions: '演示说明' };
    const draft = await request.put(`${base}/admin/appointments/${appointment.id}/care-plan/draft`, { headers: auth(adminToken!), data: { version: 0, content } });
    expect(draft.status()).toBe(200);
    const published = await request.post(`${base}/admin/appointments/${appointment.id}/care-plan/publish`, { headers: auth(adminToken!), data: { version: (await draft.json()).data.draftVersion, changeReason: '验收发布' } });
    expect(published.status()).toBe(200);
    await page.goto('/followups');
    await page.getByText('治疗记录与更正', { exact: true }).click();
    await expect(page.getByRole('heading', { name: '预约', exact: true })).toBeVisible();
    await expect(page.locator('.list-item').first()).toBeVisible();
    // The task may be beyond page one after repeated local acceptance runs.
    let found = false;
    for (let index = 0; index < 10; index++) {
      const item = page.locator('.list-item').filter({ hasText: name });
      if (await item.count()) { await item.click(); found = true; break; }
      const next = page.getByRole('button', { name: '下一页', exact: true });
      if (!(await next.isEnabled())) break;
      const refreshed = page.waitForResponse(response => response.url().includes('/admin/appointments?'));
      await next.click(); await refreshed;
    }
    expect(found).toBe(true);
    await expect(page.getByRole('heading', { name: `${name} · 实际治疗记录` })).toBeVisible();
    const actualTime = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(Date.now() - 3600000)).replace(' ', 'T');
    await page.locator('input[type="datetime-local"]').fill(actualTime);
    await page.getByLabel('实际治疗摘要', { exact: true }).fill('虚构实际治疗验收');
    await page.getByRole('button', { name: '记录治疗并生成随访', exact: true }).click();
    const recorded = page.waitForResponse(response => response.url().endsWith(`/appointments/${appointment.id}/treatment`));
    await page.getByRole('button', { name: '确认并生成任务', exact: true }).click();
    expect((await recorded).status()).toBe(200);
    await expect(page.locator('.task-line')).toHaveCount(3);
    await page.getByRole('button', { name: '更正治疗记录', exact: true }).click();
    await page.getByLabel('实际治疗摘要', { exact: true }).fill('更正后的虚构治疗摘要');
    await page.getByLabel('更正原因', { exact: true }).fill('核对演示录入内容');
    await page.getByRole('button', { name: '保存更正', exact: true }).click();
    const correction = page.waitForResponse(response => response.url().endsWith(`/appointments/${appointment.id}/treatment/corrections`));
    await page.getByRole('button', { name: '确认更正', exact: true }).click();
    expect((await correction).status()).toBe(200);
    await expect(page.getByRole('heading', { name: '当前治疗记录 · V2' })).toBeVisible();
    await expect(page.locator('.revision')).toHaveCount(2);
    const workspace = (await (await request.get(`${base}/me/appointments/${appointment.id}/followups`, { headers: auth(session.token) })).json()).data;
    expect(workspace.tasks).toHaveLength(3);
    expect(workspace.treatment.summary).toBe('更正后的虚构治疗摘要');
    expect(workspace.treatmentRevisions.map((item: { summary: string }) => item.summary)).toEqual(['更正后的虚构治疗摘要', '虚构实际治疗验收']);
    const [normal, alert] = workspace.tasks;
    const submit = async (id: string, severity: number) => {
      const result = await request.post(`${base}/me/followups/${id}/submit`, { headers: auth(session.token), data: { version: 1, symptoms: severity ? '虚构明显不适' : '虚构无不适', severity, concern: false } });
      expect(result.status()).toBe(200);
    };
    await submit(normal.id, 0); await submit(alert.id, 2);
    await page.getByText('随访任务', { exact: true }).click();
    await expect(page.locator('.list-item').filter({ hasText: name })).toHaveCount(3);
    await page.locator('.list-item').filter({ hasText: name }).filter({ hasText: normal.title }).click();
    await page.getByLabel('给患者的回评', { exact: true }).fill('普通随访回评验收');
    await page.getByRole('button', { name: '提交医护回评', exact: true }).click();
    await expect(page.locator('.review-result')).toContainText('普通随访回评验收');
    await page.locator('.list-item').filter({ hasText: name }).filter({ hasText: alert.title }).click();
    await page.getByLabel('给患者的回评', { exact: true }).fill('异常随访回评验收');
    await page.getByRole('button', { name: '提交回评并完成异常处理', exact: true }).click();
    await expect(page.getByRole('alert').filter({ hasText: '异常任务需要处理记录' })).toBeVisible();
    await page.getByLabel('异常处理记录（患者可见）', { exact: true }).fill('已核实并安排演示复诊');
    await page.getByRole('button', { name: '提交回评并完成异常处理', exact: true }).click();
    await expect(page.locator('.review-result')).toContainText('已核实并安排演示复诊');
    const patient = (await (await request.get(`${base}/me/appointments/${appointment.id}/followups`, { headers: auth(session.token) })).json()).data;
    expect(patient.tasks.find((task: { id: string }) => task.id === normal.id).response).toBe('普通随访回评验收');
    expect(patient.tasks.find((task: { id: string }) => task.id === alert.id).status).toBe('handled');
    const latest = (await (await request.get(`${base}/me/appointments/${appointment.id}`, { headers: auth(session.token) })).json()).data;
    expect(latest.status).toBe('completed'); expect(latest.canCancel).toBe(false);
    await page.getByRole('link', { name: '查看关联方案' }).click();
    await expect(page.getByText('虚构随访方案', { exact: true }).first()).toBeVisible();
  } finally {
    const cleanupRequest = await apiRequest.newContext();
    const current = (await (await cleanupRequest.get(`${base}/me/appointments/${appointment.id}`, { headers: auth(session.token) })).json()).data;
    if (current.canCancel) await cleanupRequest.post(`${base}/me/appointments/${appointment.id}/cancel`, { headers: auth(session.token), data: { version: current.version } });
    await cleanupRequest.dispose();
    // Completed test appointments have already released their reservation atomically.
  }
});
