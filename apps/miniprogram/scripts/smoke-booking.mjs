import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';

// Node 22+, live local API, and the WeChat CLI `auto` WebSocket are required.
// Creates an isolated demo identity and two appointments. Its appointments are
// cancelled in cleanup; audit records remain. Existing storage is restored.
const endpoint = process.env.MINIPROGRAM_WS || 'ws://127.0.0.1:9420';
const apiBase = 'http://127.0.0.1:8080/api/v1';
const keys = {
  session: 'blk_dev_patient_session_v1',
  expired: 'blk_dev_patient_expired_v1',
  submission: 'blk_appointment_submission_v1',
};
const socket = new WebSocket(endpoint);
const pending = new Map();
const isolationMarker = randomUUID();
const exceptions = [];
let sequence = 0;
let connected = false;
let storageIsolated = false;
let requestMocked = false;
let modalMocked = false;
let session;
let adminToken;

socket.addEventListener('message', ({ data }) => {
  const message = JSON.parse(data);
  if (message.method === 'App.exceptionThrown') exceptions.push(message.params);
  const callback = pending.get(message.id);
  if (!callback) return;
  pending.delete(message.id);
  clearTimeout(callback.timer);
  if (message.error) callback.reject(new Error(`${callback.method} failed`));
  else callback.resolve(message.result);
});

function send(method, params = {}) {
  return new Promise((resolve, reject) => {
    if (socket.readyState !== WebSocket.OPEN) return reject(new Error('Simulator connection is not open'));
    const id = String(++sequence);
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`${method} timed out after 20 seconds`));
    }, 20000);
    pending.set(id, { method, resolve, reject, timer });
    socket.send(JSON.stringify({ id, method, params }));
  });
}

async function evaluate(fn, ...args) {
  return (await send('App.callFunction', { functionDeclaration: fn.toString(), args })).result;
}

async function waitUntil(check, label) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const result = await check();
    if (result) return result;
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  throw new Error(`Timed out: ${label}`);
}

async function currentPage(path) {
  await waitUntil(async () => {
    const page = await send('App.getCurrentPage');
    return page.path === path ? page : null;
  }, path).catch(async (error) => {
    const routes = await evaluate(function pageDiagnostics() {
      return getCurrentPages().map((page) => ({ route: page.route, loading: page.data.loading, hasError: Boolean(page.data.error), slotCount: page.data.slots?.length }));
    });
    throw new Error(`${error.message}; page stack: ${JSON.stringify(routes)}`);
  });
  // Match miniprogram-automator's changeRoute: the logic-layer route can be
  // visible before the native transition completes. Its SDK waits 3 seconds.
  await new Promise((resolve) => setTimeout(resolve, 3000));
  // reLaunch may recreate the same path with a different pageId. Re-read after
  // the transition; returning the earlier handle invokes a destroyed page.
  return waitUntil(async () => {
    const page = await send('App.getCurrentPage');
    return page.path === path ? page : null;
  }, `settled ${path}`);
}

async function pageData(pageId) {
  return (await send('Page.getData', { pageId })).data;
}

async function readyData(pageId) {
  return waitUntil(async () => {
    const data = await pageData(pageId);
    if (data.error || data.initialError || data.expired) throw new Error('Simulator page reported a load or session error');
    return !data.loading && !data.initializing ? data : null;
  }, 'page API data');
}

async function call(page, method, ...args) {
  return send('Page.callMethod', { pageId: page.pageId, method, args });
}

async function renderedElements(page, selector, count) {
  return waitUntil(async () => {
    const { elements } = await send('Page.getElements', { pageId: page.pageId, selector });
    if (!elements.length || (count !== undefined && elements.length !== count)) return null;
    for (const element of elements) {
      const { properties } = await send('Element.getDOMProperties', {
        pageId: page.pageId,
        elementId: element.elementId,
        names: ['offsetWidth', 'offsetHeight'],
      });
      if (!(Number(properties[0]) > 0 && Number(properties[1]) > 0)) return null;
    }
    return elements;
  }, `rendered ${selector}`);
}

async function renderedText(page, selector, expected) {
  return waitUntil(async () => {
    const elements = await renderedElements(page, selector, 1);
    const { properties } = await send('Element.getDOMProperties', {
      pageId: page.pageId,
      elementId: elements[0].elementId,
      names: ['innerText'],
    });
    return String(properties[0] || '').trim() === expected;
  }, `rendered ${selector} status ${expected}`);
}

async function api(path, { token, method = 'GET', body } = {}) {
  const response = await fetch(`${apiBase}${path}`, {
    method,
    headers: { ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    signal: AbortSignal.timeout(15000),
  });
  const envelope = await response.json();
  if (!response.ok) {
    const error = new Error(`API HTTP ${response.status}: ${envelope.error?.code || 'REQUEST_FAILED'}`);
    error.status = response.status;
    throw error;
  }
  return envelope;
}

async function openBooking() {
  await send('App.callWxMethod', { method: 'reLaunch', args: [{ url: '/pages/doctors/index' }] });
  const doctorsPage = await currentPage('pages/doctors/index');
  const doctorsData = await readyData(doctorsPage.pageId);
  assert.ok(doctorsData.doctors.length, 'Demo doctors must exist');
  const doctor = doctorsData.doctors[doctorsData.doctors.length - 1];
  await call(doctorsPage, 'openSchedule', { currentTarget: { dataset: { doctorId: doctor.id } } });
  const schedule = await currentPage('pages/schedule/index');
  let scheduleData = await readyData(schedule.pageId);
  let selected;
  for (const day of scheduleData.days.slice(1)) {
    await call(schedule, 'onDateChange', { currentTarget: { dataset: { date: day.date } } });
    scheduleData = await readyData(schedule.pageId);
    selected = scheduleData.slots.find((slot) => slot.available && Date.parse(slot.startsAt) > Date.now() + 5 * 60000);
    if (selected) break;
  }
  assert.ok(selected, 'A future open demo slot with available capacity is required');
  // Native view updates follow setData asynchronously. Wait for the selected
  // date's slot cards before invoking the same handler as a user tap.
  await renderedElements(schedule, '.slot', scheduleData.slots.length);
  await waitUntil(async () => {
    const data = await pageData(schedule.pageId);
    return !data.loading && data.slots.some((slot) => slot.id === selected.id && slot.available);
  }, 'selected slot remains available after native rendering');
  const { elements: bookingButtons } = await send('Page.getElements', { pageId: schedule.pageId, selector: '.book-slot' });
  const buttonIndex = scheduleData.slots.filter((slot) => slot.available).findIndex((slot) => slot.id === selected.id);
  assert.ok(bookingButtons[buttonIndex], 'The selected slot must have a rendered booking button');
  await send('Element.tap', { pageId: schedule.pageId, elementId: bookingButtons[buttonIndex].elementId });
  const booking = await currentPage('pages/booking/index');
  const bookingData = await readyData(booking.pageId);
  assert.equal(bookingData.slot.id, selected.id, 'Native selection must reach the same booking slot');
  assert.equal(bookingData.slotAvailable, true, 'The selected slot must be available');
  assert.equal(bookingData.recoveryMode, false, 'The isolated identity must have no previous draft');
  await renderedElements(booking, '.contact-form', 1);
  await call(booking, 'onFieldChange', { currentTarget: { dataset: { field: 'contactName' } }, detail: { value: '演示测试访客' } });
  await call(booking, 'onFieldChange', { currentTarget: { dataset: { field: 'contactPhone' } }, detail: { value: '13800000000' } });
  await call(booking, 'onFieldChange', { currentTarget: { dataset: { field: 'note' } }, detail: { value: '自动化验证，全部为虚构资料' } });
  return booking;
}

async function appointmentData(page, status) {
  const result = await waitUntil(async () => {
    const data = await readyData(page.pageId);
    return data.appointment?.status === status && !data.cancelling ? data : null;
  }, `appointment status ${status}`);
  const labels = { pending: '待确认', confirmed: '已确认', cancelled: '已取消' };
  await renderedText(page, '.result-title', labels[status]);
  return result;
}

async function cancelOnPage(page) {
  if (!modalMocked) {
    await send('App.mockWxMethod', { method: 'showModal', result: { confirm: true, cancel: false, errMsg: 'showModal:ok' } });
    modalMocked = true;
  }
  await call(page, 'cancelAppointment');
  return appointmentData(page, 'cancelled');
}

async function cleanupAppointments() {
  if (!session) return;
  // This identity is created by this script and never shares the user's records.
  const result = await api('/me/appointments?page=1&pageSize=100', { token: session.token });
  for (const appointment of result.data) {
    if (!appointment.canCancel) continue;
    try {
      await api(`/me/appointments/${encodeURIComponent(appointment.id)}/cancel`, { token: session.token, method: 'POST', body: { version: appointment.version } });
    } catch (error) {
      if (error.status !== 409) throw error;
      const latest = (await api(`/me/appointments/${encodeURIComponent(appointment.id)}`, { token: session.token })).data;
      if (latest.canCancel) await api(`/me/appointments/${encodeURIComponent(latest.id)}/cancel`, { token: session.token, method: 'POST', body: { version: latest.version } });
    }
  }
  const remaining = await api('/me/appointments?page=1&pageSize=100', { token: session.token });
  assert.equal(remaining.data.filter((item) => item.status === 'pending' || item.status === 'confirmed').length, 0, 'Script-owned appointments must not retain capacity');
}

try {
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`Cannot connect to ${endpoint}`)), 10000);
    socket.addEventListener('open', () => { clearTimeout(timer); connected = true; resolve(); }, { once: true });
    socket.addEventListener('error', () => { clearTimeout(timer); reject(new Error(`Cannot connect to ${endpoint}`)); }, { once: true });
  });
  process.loadEnvFile(fileURLToPath(new URL('../../../.env', import.meta.url)));
  adminToken = process.env.DEMO_ADMIN_TOKEN;
  assert.ok(adminToken, 'Root .env must configure DEMO_ADMIN_TOKEN');
  await send('App.callWxMethod', { method: 'reLaunch', args: [{ url: '/pages/doctors/index' }] });
  const publicPage = await currentPage('pages/doctors/index');
  await readyData(publicPage.pageId);
  session = (await api('/dev/patient-sessions', { method: 'POST', body: {} })).data;
  // Mark the attempt before the remote call: even if its reply is lost, cleanup
  // can identify this run and restore the already-written backup safely.
  storageIsolated = true;
  await evaluate(function isolateStorage(storageKeys, testSession, marker) {
    const app = getApp();
    if (app.__blkM1Smoke) throw new Error('A prior booking smoke test has not restored its state');
    const present = wx.getStorageInfoSync().keys;
    app.__blkM1Smoke = {
      marker,
      backups: Object.keys(storageKeys).map((name) => ({ key: storageKeys[name], present: present.indexOf(storageKeys[name]) >= 0, value: wx.getStorageSync(storageKeys[name]) })),
      originalRequest: wx.request,
      dropped: false,
      committedAppointmentId: '',
    };
    wx.setStorageSync(storageKeys.session, testSession);
    wx.removeStorageSync(storageKeys.expired);
    wx.removeStorageSync(storageKeys.submission);
    return true;
  }, keys, session, isolationMarker);

  const booking = await openBooking();
  await call(booking, 'submit');
  const detail = await currentPage('pages/appointment-detail/index');
  const pendingData = await appointmentData(detail, 'pending');
  const firstId = pendingData.appointment.id;
  assert.equal(pendingData.appointment.canCancel, true);
  await api(`/admin/appointments/${encodeURIComponent(firstId)}/review`, { token: adminToken, method: 'POST', body: { action: 'confirm', version: pendingData.appointment.version } });
  await call(detail, 'refresh');
  const confirmedData = await appointmentData(detail, 'confirmed');
  assert.equal(confirmedData.appointment.id, firstId);
  await cancelOnPage(detail);
  await call(detail, 'openAppointments');
  const listPage = await currentPage('pages/appointments/index');
  const listData = await readyData(listPage.pageId);
  assert.equal(listData.records.find((item) => item.id === firstId)?.status, 'cancelled');
  await renderedElements(listPage, '.appointment-card', listData.records.length);
  console.log('PASS: native schedule → booking submit → pending → admin confirmation → refresh → native cancellation → appointment list.');

  const recoveryBooking = await openBooking();
  // Preserve the real request, but replace its successful callback with a network
  // failure exactly once. The server commits before the client loses its result.
  await send('App.mockWxMethod', {
    method: 'request',
    functionDeclaration: function dropCommittedResponse(options) {
      const state = getApp().__blkM1Smoke;
      const target = String(options.method || 'GET').toUpperCase() === 'POST' && /\/api\/v1\/me\/appointments$/.test(options.url);
      if (!target || state.dropped) return state.originalRequest.call(wx, options);
      const wrapped = Object.assign({}, options, {
        success(response) {
          if (response.statusCode >= 200 && response.statusCode < 300) {
            state.dropped = true;
            state.committedAppointmentId = response.data.data.id;
            if (options.fail) options.fail({ errMsg: 'request:fail simulated response loss after commit' });
          } else if (options.success) options.success(response);
        },
        complete(response) {
          if (options.complete) options.complete(state.dropped ? { errMsg: 'request:fail simulated response loss after commit' } : response);
        },
      });
      return state.originalRequest.call(wx, wrapped);
    }.toString(),
    args: [],
  });
  requestMocked = true;
  await call(recoveryBooking, 'submit');
  const recovering = await waitUntil(async () => {
    const data = await pageData(recoveryBooking.pageId);
    if (data.submitError || data.validationError || data.expired) throw new Error('Response-loss scenario produced an unexpected page error');
    return !data.submitting && data.recoveryMode && data.recoveryMessage ? data : null;
  }, 'response loss recovery mode');
  assert.equal(recovering.completedId, '');
  await renderedElements(recoveryBooking, '.recovery-card', 1);
  const recoveryState = await evaluate(function readRecoveryState(submissionKey) {
    const state = getApp().__blkM1Smoke;
    const draft = wx.getStorageSync(submissionKey);
    return { dropped: state.dropped, appointmentId: state.committedAppointmentId, key: draft && draft.key, hasBody: Boolean(draft && draft.body) };
  }, keys.submission);
  assert.equal(recoveryState.dropped, true, 'Only a committed successful response may be dropped');
  assert.ok(recoveryState.appointmentId, 'The dropped response must have created an appointment');
  assert.ok(recoveryState.key, 'Recovery must retain the original request key');
  assert.equal(recoveryState.hasBody, true, 'Recovery must retain the original request snapshot');
  await send('App.mockWxMethod', { method: 'request' });
  requestMocked = false;
  await call(recoveryBooking, 'querySubmission');
  const recoveredDetail = await currentPage('pages/appointment-detail/index');
  const recoveredData = await appointmentData(recoveredDetail, 'pending');
  assert.equal(recoveredData.appointment.id, recoveryState.appointmentId, 'By-request recovery must return the original appointment');
  const afterRecovery = await api('/me/appointments?page=1&pageSize=100', { token: session.token });
  assert.equal(afterRecovery.meta.total, 2, 'Recovering a lost response must not create a duplicate appointment');
  const draftCleared = await evaluate(function submissionCleared(submissionKey) { return !wx.getStorageSync(submissionKey); }, keys.submission);
  assert.equal(draftCleared, true);
  await cancelOnPage(recoveredDetail);
  console.log('PASS: a committed POST response can be lost; the original request key recovers exactly one appointment, and its capacity is released.');
  assert.equal(exceptions.length, 0, 'Simulator must not report JavaScript exceptions');
  console.log('PASS: no simulator JavaScript exceptions.');
} finally {
  const cleanupErrors = [];
  if (connected && requestMocked) {
    try { await send('App.mockWxMethod', { method: 'request' }); } catch { cleanupErrors.push('restore wx.request'); }
  }
  if (connected && modalMocked) {
    try { await send('App.mockWxMethod', { method: 'showModal' }); } catch { cleanupErrors.push('restore wx.showModal'); }
  }
  try { await cleanupAppointments(); } catch { cleanupErrors.push('release script-owned appointment capacity'); }
  if (connected && storageIsolated) {
    try {
      // Leave private pages before returning the original identity to storage.
      await send('App.callWxMethod', { method: 'reLaunch', args: [{ url: '/pages/doctors/index' }] });
      await currentPage('pages/doctors/index');
      const restored = await evaluate(function restoreStorage(marker) {
        const app = getApp();
        const state = app.__blkM1Smoke;
        if (!state || state.marker !== marker) return false;
        for (const backup of state.backups) {
          if (backup.present) wx.setStorageSync(backup.key, backup.value);
          else wx.removeStorageSync(backup.key);
        }
        delete app.__blkM1Smoke;
        return true;
      }, isolationMarker);
      if (restored) console.log('Restored the original three storage keys; unrelated storage and audit records were preserved.');
    } catch { cleanupErrors.push('restore original session and draft storage'); }
  }
  socket.close();
  for (const callback of pending.values()) clearTimeout(callback.timer);
  if (cleanupErrors.length) throw new Error(`Cleanup requires attention: ${cleanupErrors.join(', ')}`);
}
