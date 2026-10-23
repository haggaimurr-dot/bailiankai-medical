import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { loginEmployee } from './helpers/live-auth';
import type { components } from '../packages/contracts/src/schema';

type Slot = components['schemas']['Slot'];
const apiBase = 'http://127.0.0.1:8080/api/v1';
const date = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date(Date.now() + 86400_000));
let headers: { Authorization: string };
test.beforeEach(async ({ page, request }) => { headers = { Authorization: `Bearer ${(await loginEmployee(request, page)).token}` }; });
test.afterEach(async ({ request }) => { await request.delete(`${apiBase}/admin/session`, { headers }); });

async function readSlots(request: APIRequestContext): Promise<Slot[]> {
  const response = await request.get(`${apiBase}/slots?date=${date}`);
  expect(response.ok()).toBeTruthy();
  return (await response.json()).data;
}

async function restore(request: APIRequestContext, original: Slot) {
  const current = (await readSlots(request)).find(slot => slot.id === original.id)!;
  const response = await request.patch(`${apiBase}/admin/slots/${original.id}`, {
    headers, data: { capacity: original.capacity, status: original.status, version: current.version },
  });
  expect(response.status()).toBe(200);
}

async function openSchedule(page: Page) {
  await page.goto('/schedule');
  await expect(page.getByRole('heading', { name: '排班工作台', exact: true })).toBeVisible();
  await page.locator('#date-filter').fill(date.replaceAll('-', ' 年 ').replace(/ 年 (\d{2})$/, ' 月 $1 日'));
  await page.locator('#date-filter').press('Enter');
  await page.getByRole('heading', { name: '排班工作台', exact: true }).click();
  await expect(page.getByRole('button', { name: '编辑排班' }).first()).toBeVisible();
}

test('后台保存容量和状态后，公共接口读取到相同结果', async ({ page, request }) => {
  const original = (await readSlots(request))[0]!;
  try {
    await openSchedule(page);
    await page.getByRole('button', { name: '编辑排班' }).first().click();
    const dialog = page.getByRole('dialog', { name: '编辑排班' });
    await dialog.getByRole('spinbutton').fill(String(original.capacity + 1));
    await dialog.locator('.el-switch__core').click();
    const savedResponse = page.waitForResponse(response => response.request().method() === 'PATCH');
    await dialog.getByRole('button', { name: '保存修改' }).click();
    const response = await savedResponse;
    expect(response.status()).toBe(200);
    const saved: Slot = (await response.json()).data;
    expect(saved.id).toBe(original.id);
    expect(saved.capacity).toBe(original.capacity + 1);
    expect(saved.status).toBe(original.status === 'open' ? 'closed' : 'open');
    expect((await readSlots(request)).find(slot => slot.id === saved.id)).toEqual(saved);
    await expect(dialog).not.toBeVisible();
    await openSchedule(page);
    await expect(page.locator('.el-table__body tbody tr').first()).toContainText(` / ${saved.capacity}`);
    await page.screenshot({ path: '.local/admin-preview.png', fullPage: true });
  } finally {
    await restore(request, original);
  }
});

test('旧版本修改被拒绝，页面刷新后保留另一处已保存内容', async ({ page, request }) => {
  const original = (await readSlots(request))[0]!;
  try {
    await openSchedule(page);
    await page.getByRole('button', { name: '编辑排班' }).first().click();
    const dialog = page.getByRole('dialog', { name: '编辑排班' });
    await dialog.getByRole('spinbutton').fill('9');
    const update = await request.patch(`${apiBase}/admin/slots/${original.id}`, {
      headers, data: { capacity: 7, status: 'closed', version: original.version },
    });
    expect(update.status()).toBe(200);
    const saveResponse = page.waitForResponse(response => response.request().method() === 'PATCH');
    await dialog.getByRole('button', { name: '保存修改' }).click();
    expect((await saveResponse).status()).toBe(409);
    await expect(dialog).not.toBeVisible();
    await expect(page.getByText('该时段已被其他操作更新，列表已重新加载。请核对最新内容后再次编辑。')).toBeVisible();
    expect((await readSlots(request)).find(slot => slot.id === original.id)?.capacity).toBe(7);
  } finally {
    await restore(request, original);
  }
});

test('读取失败时可重试，医生页正常，1280宽度无横向溢出', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.route('**/api/v1/clinics', route => route.fulfill({
    status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'DB_UNAVAILABLE', message: '测试：服务暂不可用' } }),
  }));
  await page.goto('/schedule');
  await expect(page.getByRole('heading', { name: '排班数据暂未加载' })).toBeVisible();
  await page.unroute('**/api/v1/clinics');
  await page.getByRole('button', { name: '重新加载' }).click();
  await expect(page.getByText('测试：服务暂不可用')).not.toBeVisible();
  await expect(page.getByRole('button', { name: '编辑排班' }).first()).toBeVisible();
  await page.locator('#date-filter').fill('2020 年 01 月 01 日');
  await page.locator('#date-filter').press('Enter');
  await page.getByRole('heading', { name: '排班工作台', exact: true }).click();
  await expect(page.getByText('该日期暂无排班，可切换日期或医生查看。')).toBeVisible();
  await expect(page.locator('#date-filter')).toHaveValue('2020 年 01 月 01 日');
  await page.setViewportSize({ width: 1280, height: 600 });
  await page.getByRole('link', { name: '医生资料维护' }).click();
  await expect(page.getByRole('heading', { name: '医生与院区维护', exact: true })).toBeVisible();
  await expect(page.getByText('演示医生一', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.getByRole('link', { name: '员工与权限' }).click();
  await expect(page.getByRole('heading', { name: '员工与权限', exact: true })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath('sidebar-short-window.png'), fullPage: true });
});
