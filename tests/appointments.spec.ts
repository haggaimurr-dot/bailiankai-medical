import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { acceptPrivacy, loginEmployee } from './helpers/live-auth';
import type { components } from '../packages/contracts/src/schema';

type Appointment = components['schemas']['Appointment'];
type PatientSession = components['schemas']['PatientSession'];
type Slot = components['schemas']['Slot'];
const base = 'http://127.0.0.1:8080/api/v1';
const cleanup: { token: string; id: string }[] = [];
let adminToken: string;
test.beforeEach(async ({ page, request }) => { adminToken = (await loginEmployee(request, page)).token; });
test.afterEach(async ({ request }) => { await request.delete(`${base}/admin/session`, { headers: auth(adminToken) }); });
const auth = (token: string) => ({ Authorization: `Bearer ${token}` });

async function patient(request: APIRequestContext): Promise<PatientSession> {
  const response = await request.post(`${base}/dev/patient-sessions`, { data: {} });
  expect(response.status()).toBe(201);
  const session: PatientSession = (await response.json()).data;
  await acceptPrivacy(request, session.token);
  return session;
}

async function slots(request: APIRequestContext): Promise<Slot[]> {
  const response = await request.get(`${base}/slots`);
  expect(response.status()).toBe(200);
  return (await response.json()).data;
}

async function create(request: APIRequestContext, session: PatientSession, label: string) {
  const slot = (await slots(request)).find(item => item.status === 'open' && item.capacity > item.reserved && new Date(item.startsAt).getTime() > Date.now() + 3600_000);
  expect(slot, 'Seed a future open slot with capacity before running browser tests.').toBeTruthy();
  const key = `e2e-${randomUUID()}`;
  const body = { slotId: slot!.id, contactName: `演示${label}`, contactPhone: '13800000000', note: '自动化联调，虚构资料' };
  const response = await request.post(`${base}/me/appointments`, {
    headers: { ...auth(session.token), 'Idempotency-Key': key }, data: body,
  });
  expect(response.status()).toBe(201);
  const appointment: Appointment = (await response.json()).data;
  cleanup.push({ token: session.token, id: appointment.id });
  return { appointment, slot: slot!, key, body };
}

async function myAppointment(request: APIRequestContext, token: string, id: string): Promise<Appointment> {
  const response = await request.get(`${base}/me/appointments/${id}`, { headers: auth(token) });
  expect(response.status()).toBe(200);
  return (await response.json()).data;
}

async function openDetail(page: Page, id: string) {
  await page.goto('/appointments');
  await page.getByTestId(`appointment-detail-${id}`).click();
  const detail = page.getByRole('dialog', { name: '预约详情' });
  await expect(detail).toBeVisible();
  await expect(detail.getByRole('button', { name: '确认预约', exact: true })).toBeVisible();
  return detail;
}

test.afterEach(async ({ request }) => {
  // Every record here was created by this test, using its own patient session.
  // Keep the audit history and release any reservation left by a failed assertion.
  for (const item of cleanup.splice(0)) {
    const current = await myAppointment(request, item.token, item.id);
    if (current.canCancel) {
      const response = await request.post(`${base}/me/appointments/${item.id}/cancel`, {
        headers: auth(item.token), data: { version: current.version },
      });
      expect(response.status()).toBe(200);
    }
  }
});

test('患者重复提交只建一单，后台确认后患者查看并取消，名额恢复', async ({ page, request }) => {
  const session = await patient(request);
  const { appointment, slot, key, body } = await create(request, session, '确认流程');
  expect(appointment.status).toBe('pending');
  expect(appointment.canCancel).toBe(true);
  const replay = await request.post(`${base}/me/appointments`, {
    headers: { ...auth(session.token), 'Idempotency-Key': key }, data: body,
  });
  expect(replay.status()).toBe(200);
  expect((await replay.json()).data.id).toBe(appointment.id);
  expect((await slots(request)).find(item => item.id === slot.id)?.reserved).toBe(slot.reserved + 1);

  const detail = await openDetail(page, appointment.id);
  await expect(detail.getByTestId('appointment-contact-name')).toHaveText(body.contactName);
  const review = page.waitForResponse(response => response.url().endsWith(`/appointments/${appointment.id}/review`) && response.request().method() === 'POST');
  await detail.getByRole('button', { name: '确认预约', exact: true }).click();
  expect((await review).status()).toBe(200);
  await expect(detail.getByText('已确认', { exact: true })).toBeVisible();
  const confirmed = await myAppointment(request, session.token, appointment.id);
  expect(confirmed.status).toBe('confirmed');
  expect((await slots(request)).find(item => item.id === slot.id)?.reserved).toBe(slot.reserved + 1);

  const cancel = await request.post(`${base}/me/appointments/${appointment.id}/cancel`, {
    headers: auth(session.token), data: { version: confirmed.version },
  });
  expect(cancel.status()).toBe(200);
  expect((await myAppointment(request, session.token, appointment.id)).status).toBe('cancelled');
  expect((await slots(request)).find(item => item.id === slot.id)?.reserved).toBe(slot.reserved);
});

test('后台拒绝必须填写原因，患者读取到原因，其他患者无法访问', async ({ page, request }) => {
  const session = await patient(request);
  const stranger = await patient(request);
  const { appointment, slot } = await create(request, session, '拒绝流程');
  expect((await request.get(`${base}/me/appointments/${appointment.id}`, { headers: auth(stranger.token) })).status()).toBe(404);
  expect((await request.get(`${base}/admin/appointments`, { headers: auth(session.token) })).status()).toBe(401);
  const detail = await openDetail(page, appointment.id);
  await detail.getByRole('button', { name: '拒绝预约', exact: true }).click();
  await detail.getByRole('button', { name: '提交拒绝', exact: true }).click();
  await expect(detail.getByText('请填写拒绝原因，便于患者了解处理结果。')).toBeVisible();
  const reason = '演示：请重新选择其他时间';
  await detail.getByLabel('拒绝原因', { exact: true }).fill(reason);
  const review = page.waitForResponse(response => response.url().endsWith(`/appointments/${appointment.id}/review`) && response.request().method() === 'POST');
  await detail.getByRole('button', { name: '提交拒绝', exact: true }).click();
  expect((await review).status()).toBe(200);
  await expect(detail.getByText('已拒绝', { exact: true })).toBeVisible();
  const rejected = await myAppointment(request, session.token, appointment.id);
  expect(rejected.status).toBe('rejected');
  expect(rejected.rejectionReason).toBe(reason);
  expect(rejected.canCancel).toBe(false);
  expect((await slots(request)).find(item => item.id === slot.id)?.reserved).toBe(slot.reserved);
});

test('患者先取消后，后台旧详情不能再确认，冲突会刷新结果', async ({ page, request }) => {
  const session = await patient(request);
  const { appointment, slot } = await create(request, session, '并发流程');
  const detail = await openDetail(page, appointment.id);
  const cancelled = await request.post(`${base}/me/appointments/${appointment.id}/cancel`, {
    headers: auth(session.token), data: { version: appointment.version },
  });
  expect(cancelled.status()).toBe(200);
  const review = page.waitForResponse(response => response.url().endsWith(`/appointments/${appointment.id}/review`) && response.request().method() === 'POST');
  await detail.getByRole('button', { name: '确认预约', exact: true }).click();
  expect((await review).status()).toBe(409);
  await expect(detail.getByText('已取消', { exact: true })).toBeVisible();
  await expect(detail.getByText('预约状态已变化，已载入最新详情。请核对当前状态后再处理。')).toBeVisible();
  expect((await slots(request)).find(item => item.id === slot.id)?.reserved).toBe(slot.reserved);
  // The operator may inspect it, but a terminal appointment cannot be reviewed.
  const current = await request.get(`${base}/admin/appointments/${appointment.id}`, { headers: auth(adminToken!) });
  expect((await current.json()).data.canReview).toBe(false);
});
