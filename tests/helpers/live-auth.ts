import { expect, type APIRequestContext, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import type { components } from '../../packages/contracts/src/schema';

const base = 'http://127.0.0.1:8080/api/v1';
type Session = components['schemas']['EmployeeLoginResult']['data'];

export async function loginEmployee(request: APIRequestContext, page: Page): Promise<Session> {
  const credentials = Object.fromEntries(readFileSync('.local/employee-credentials.txt', 'utf8').trim().split(/\r?\n/).map(line => {
    const split = line.indexOf('=');
    return [line.slice(0, split), line.slice(split + 1)];
  }));
  const response = await request.post(`${base}/admin/session`, { data: credentials });
  expect(response.status()).toBe(200);
  const session: Session = (await response.json()).data;
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify(value)), session);
  return session;
}

export async function acceptPrivacy(request: APIRequestContext, token: string) {
  const policy = await request.get(`${base}/privacy-policy`);
  expect(policy.status()).toBe(200);
  const response = await request.post(`${base}/me/privacy-consent`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { policyVersion: (await policy.json()).data.version, action: 'accepted' },
  });
  expect(response.status()).toBe(200);
}
