import { test, expect } from '@playwright/test';
import { loginEmployee } from './helpers/live-auth';

test('真实后台入口、客服默认队列与每日运营统计', async ({ page, request }) => {
  test.setTimeout(120_000);
  const session = await loginEmployee(request, page);
  const failures: string[] = [];
  page.on('response', response => {
    if (response.url().includes('/api/v1/') && response.status() >= 400) failures.push(`${response.status()} ${new URL(response.url()).pathname}`);
  });
  try {
    for (const path of ['/appointments', '/arrivals', '/workbench', '/handoffs', '/followups', '/care', '/patients', '/schedule', '/doctors', '/clinics', '/employees', '/messages', '/cases', '/knowledge', '/overview', '/support', '/account']) {
      await page.goto(path);
      await expect(page).toHaveURL(new RegExp(`${path}$`));
      await expect(page.locator('h1')).toBeVisible();
      await page.waitForLoadState('networkidle');
      expect(failures, `业务请求失败：${path}`).toEqual([]);
    }
    const headers = { Authorization: `Bearer ${session.token}` };
    const queue = await request.get('http://127.0.0.1:8080/api/v1/admin/support?page=1&pageSize=20&overdue=false', { headers });
    expect(queue.status()).toBe(200);
    expect(Array.isArray((await queue.json()).data)).toBe(true);
    const dateTo = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date());
    const dateFrom = `${dateTo.slice(0, 7)}-01`;
    const query = new URLSearchParams({ dateFrom, dateTo, groupBy: 'day' });
    const report = await request.get(`http://127.0.0.1:8080/api/v1/admin/analytics?${query}`, { headers });
    expect(report.status()).toBe(200);
    const data = (await report.json()).data;
    expect(data.timezone).toBe('Asia/Shanghai');
    expect(data.rows).toHaveLength(Number(dateTo.slice(-2)));
    expect(data.rows[0].date).toBe(dateFrom);
    expect(data.rows.at(-1).date).toBe(dateTo);
  } finally {
    await request.delete('http://127.0.0.1:8080/api/v1/admin/session', { headers: { Authorization: `Bearer ${session.token}` } });
  }
});
