import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';

// 先通过微信 CLI auto 打开本项目。Node 22+ 提供 WebSocket。
// --write-check 仅用于本地演示库，会临时调整一个未来排班并恢复容量/状态。
const endpoint = process.env.MINIPROGRAM_WS || 'ws://127.0.0.1:9420';
const apiBase = 'http://127.0.0.1:8080/api/v1';
const writeCheck = process.argv.includes('--write-check');
const socket = new WebSocket(endpoint);
const pending = new Map();
const exceptions = [];
let sequence = 0;

socket.addEventListener('message', ({ data }) => {
  const message = JSON.parse(data);
  if (message.method === 'App.exceptionThrown') exceptions.push(message.params);
  const callback = pending.get(message.id);
  if (!callback) return;
  pending.delete(message.id);
  clearTimeout(callback.timer);
  if (message.error) callback.reject(new Error(message.error.message));
  else callback.resolve(message.result);
});

function send(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = String(++sequence);
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`${method} timed out after 20 seconds`));
    }, 20000);
    pending.set(id, { resolve, reject, timer });
    socket.send(JSON.stringify({ id, method, params }));
  });
}

async function pageData(pageId) {
  return (await send('Page.getData', { pageId })).data;
}

async function waitUntil(check, label) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const result = await check();
    if (result) return result;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`Timed out: ${label}`);
}

async function currentPage(path) {
  return waitUntil(async () => {
    const page = await send('App.getCurrentPage');
    return page.path === path ? page : null;
  }, path);
}

async function readyData(pageId) {
  return waitUntil(async () => {
    const data = await pageData(pageId);
    if (data.error || data.initialError) throw new Error(data.error || data.initialError);
    return !data.loading && !data.initializing ? data : null;
  }, 'page data loaded from API');
}

async function api(path, options) {
  const response = await fetch(`${apiBase}${path}`, options);
  const body = await response.json();
  if (!response.ok) throw new Error(body.error?.message || `API HTTP ${response.status}`);
  return body.data;
}

let originalSlot;
let changedSlot;
let token;
let schedule;
try {
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true });
    socket.addEventListener('error', () => reject(new Error(`Cannot connect to ${endpoint}`)), { once: true });
  });
  await send('App.callWxMethod', { method: 'reLaunch', args: [{ url: '/pages/doctors/index' }] });
  const doctorsPage = await currentPage('pages/doctors/index');
  const doctorsData = await readyData(doctorsPage.pageId);
  const doctors = await api(`/doctors?clinicId=${encodeURIComponent(doctorsData.clinic.id)}`);
  assert.ok(doctors.length, 'Demo API must have doctors');
  assert.deepEqual(doctorsData.doctors.map((doctor) => doctor.id), doctors.map((doctor) => doctor.id));
  await waitUntil(async () => {
    const cards = await send('Page.getElements', { pageId: doctorsPage.pageId, selector: '.doctor-card' });
    return cards.elements.length === doctors.length;
  }, 'doctor cards rendered');
  console.log(`PASS: simulator displays ${doctors.length} doctors from the live API.`);

  const doctor = doctors[doctors.length - 1];
  await send('Page.callMethod', {
    pageId: doctorsPage.pageId,
    method: 'openSchedule',
    args: [{ currentTarget: { dataset: { doctorId: doctor.id } } }],
  });
  schedule = await currentPage('pages/schedule/index');
  let data = await readyData(schedule.pageId);
  assert.equal(data.doctorId, doctor.id);
  const date = data.days[2].date;
  await send('Page.callMethod', {
    pageId: schedule.pageId,
    method: 'onDateChange',
    args: [{ currentTarget: { dataset: { date } } }],
  });
  data = await readyData(schedule.pageId);
  const slotsPath = `/slots?clinicId=${encodeURIComponent(data.clinic.id)}&doctorId=${encodeURIComponent(doctor.id)}&date=${date}`;
  const slots = await api(slotsPath);
  assert.ok(slots.length, 'Selected future date must have seeded slots');
  assert.deepEqual(data.slots.map((slot) => slot.id), slots.map((slot) => slot.id));
  assert.ok(data.slots.every((slot) => slot.doctorId === doctor.id));
  await waitUntil(async () => {
    const cards = await send('Page.getElements', { pageId: schedule.pageId, selector: '.slot' });
    return cards.elements.length === slots.length;
  }, 'slot cards rendered');
  console.log(`PASS: doctor/date filters display ${slots.length} matching live slots on ${date}.`);

  if (writeCheck) {
    process.loadEnvFile(fileURLToPath(new URL('../../../.env', import.meta.url)));
    token = process.env.DEMO_ADMIN_TOKEN;
    assert.ok(token, 'DEMO_ADMIN_TOKEN must be configured in the root .env');
    originalSlot = slots[0];
    changedSlot = await api(`/admin/slots/${encodeURIComponent(originalSlot.id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: JSON.stringify({ capacity: originalSlot.capacity, status: originalSlot.status === 'open' ? 'closed' : 'open', version: originalSlot.version }),
    });
    await send('Page.callMethod', { pageId: schedule.pageId, method: 'onPullDownRefresh', args: [] });
    data = await readyData(schedule.pageId);
    const updated = data.slots.find((slot) => slot.id === originalSlot.id);
    assert.equal(updated.status, changedSlot.status);
    assert.equal(updated.version, changedSlot.version);
    console.log('PASS: admin API update is visible after simulator pull-to-refresh.');
  }
} finally {
  try {
    if (changedSlot) {
      await api(`/admin/slots/${encodeURIComponent(originalSlot.id)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ capacity: originalSlot.capacity, status: originalSlot.status, version: changedSlot.version }),
      });
      await send('Page.callMethod', { pageId: schedule.pageId, method: 'onPullDownRefresh', args: [] });
      await readyData(schedule.pageId);
      console.log('Restored original slot capacity and status; version increased normally.');
    }
  } finally {
    socket.close();
    for (const callback of pending.values()) clearTimeout(callback.timer);
  }
}
assert.deepEqual(exceptions, [], 'Simulator must not report JavaScript exceptions');
console.log('PASS: no simulator JavaScript exceptions.');
