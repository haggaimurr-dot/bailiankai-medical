import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { deflateSync } from 'node:zlib';

// Node 22+, live local API, and the WeChat CLI `auto` WebSocket are required.
// Uses an isolated demo identity and synthetic photos only. Restores existing
// storage and releases test capacity while retaining demo records and audit.
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
let employeeSessionCreated = false;

socket.addEventListener('message', ({ data }) => {
  const message = JSON.parse(data);
  if (message.method === 'App.exceptionThrown') exceptions.push(message.params);
  const callback = pending.get(message.id);
  if (!callback) return;
  pending.delete(message.id);
  clearTimeout(callback.timer);
  if (message.error) callback.reject(new Error(`${callback.method} failed: ${JSON.stringify(message.error)}`));
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
  assert.equal((await send('App.getCurrentPage')).pageId, pageId, 'Observed page must still be current');
  return evaluate(function currentData() { const pages = getCurrentPages(); return pages[pages.length - 1].data; });
}

async function readyData(pageId) {
  return waitUntil(async () => {
    const data = await pageData(pageId);
    if (data.error || data.initialError || data.expired) throw new Error('Simulator page reported a load or session error');
    return !data.loading && !data.initializing ? data : null;
  }, 'page API data');
}

async function call(page, method, ...args) {
  assert.equal((await send('App.getCurrentPage')).pageId, page.pageId, 'Action page must still be current');
  return evaluate(function invoke(route, name, argumentsList) {
    const pages = getCurrentPages(), current = pages[pages.length - 1];
    if (current.route !== route || typeof current[name] !== 'function') throw new Error('Action page changed');
    return current[name].apply(current, argumentsList);
  }, page.path, method, args);
}

async function renderedElements(page, selector, count) {
  return waitUntil(async () => {
    if (process.argv.includes('--photos-only')) {
      assert.equal((await send('App.getCurrentPage')).pageId, page.pageId, 'Rendered page must still be current');
      const results = await evaluate(function nativeBounds(query) {
        return new Promise(resolve => wx.createSelectorQuery().selectAll(query).boundingClientRect().exec(resolve));
      }, selector);
      const nodes = results?.[0] || [];
      return nodes.length && (count === undefined || nodes.length === count) && nodes.every(node => node.width > 0 && node.height > 0) ? nodes : null;
    }
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

// A large, sharp geometric fixture exercises the real image inspection and
// upload path; it contains no face and does not validate a person's pose.
function syntheticPng() {
  const width = 900, height = 1200;
  const pixels = Buffer.alloc((width * 3 + 1) * height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const value = ((Math.floor(x / 13) + Math.floor(y / 13)) % 2) ? 230 : 35;
      const offset = y * (width * 3 + 1) + 1 + x * 3;
      pixels[offset] = value; pixels[offset + 1] = value; pixels[offset + 2] = value;
    }
  }
  function chunk(type, data) {
    const name = Buffer.from(type), bytes = Buffer.concat([name, data]);
    let crc = 0xffffffff;
    for (const byte of bytes) {
      crc ^= byte;
      for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
    }
    const size = Buffer.alloc(4), checksum = Buffer.alloc(4);
    size.writeUInt32BE(data.length); checksum.writeUInt32BE((crc ^ 0xffffffff) >>> 0);
    return Buffer.concat([size, bytes, checksum]);
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width); header.writeUInt32BE(height, 4); header[8] = 8; header[9] = 2;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', header), chunk('IDAT', deflateSync(pixels)), chunk('IEND', Buffer.alloc(0))]);
}

async function reviewFixture(page, angle) {
  await call(page, 'choose', { currentTarget: { dataset: { angle, source: 'album' } } });
  await waitUntil(async () => {
    const data = await pageData(page.pageId);
    if (data.error) throw new Error('Synthetic image inspection failed');
    return !data.checking && data.draft?.angle === angle;
  }, `Local inspection and staged review ${angle}`);
  if (angle === 'front') await screenshot('review');
  const before = (await api(`/admin/appointments/${appointment.id}/photos`, { token: adminToken })).data;
  await call(page, 'confirmDraft');
  assert.deepEqual((await api(`/admin/appointments/${appointment.id}/photos`, { token: adminToken })).data, before, 'Unconfirmed review must not upload');
  await call(page, 'toggleReview', { currentTarget: { dataset: { field: 'clarity' } } });
  await call(page, 'confirmDraft');
  assert.deepEqual((await api(`/admin/appointments/${appointment.id}/photos`, { token: adminToken })).data, before, 'Angle must also be confirmed before upload');
  await call(page, 'toggleReview', { currentTarget: { dataset: { field: 'angle' } } });
  if (angle === 'front' && process.argv.includes('--screenshots')) {
    await evaluate(function scroll() { wx.pageScrollTo({ selector: '#review-actions', duration: 0 }); return true; });
    await screenshot('review-confirm');
  }
  await call(page, 'confirmDraft');
}

async function screenshot(name) {
  if (!process.argv.includes('--screenshots')) return;
  await new Promise(resolve => setTimeout(resolve, 200));
  const result = await send('App.captureScreenshot');
  assert.ok(result.data, 'Native screenshot required');
  const directory = fileURLToPath(new URL('../../../tmp', import.meta.url));
  mkdirSync(directory, { recursive: true });
  writeFileSync(`${directory}/photo-${name}.png`, Buffer.from(result.data, 'base64'));
}


let appointment;
let chooseMocked = false;
let mediaMocked = false;
try {
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Simulator connection failed')), 10000);
    socket.addEventListener('open', () => { clearTimeout(timer); connected = true; resolve(); }, { once: true });
    socket.addEventListener('error', reject, { once: true });
  });
  process.loadEnvFile(fileURLToPath(new URL('../../../.env', import.meta.url)));
  adminToken = process.env.ADMIN_SESSION_TOKEN;
  if (!adminToken) {
    const credentialsPath = process.env.EMPLOYEE_CREDENTIALS_FILE || fileURLToPath(new URL('../../../.local/employee-credentials.txt', import.meta.url));
    let credentials;
    try { credentials = Object.fromEntries(readFileSync(credentialsPath, 'utf8').trim().split(/\r?\n/).map(line => { const index = line.indexOf('='); return [line.slice(0, index), line.slice(index + 1)]; })); }
    catch { /* Older local setups may still explicitly allow the demo token. */ }
    if (credentials?.username && credentials?.password) {
      adminToken = (await api('/admin/session', { method: 'POST', body: { username: credentials.username, password: credentials.password } })).data.token;
      employeeSessionCreated = true;
    } else adminToken = process.env.DEMO_ADMIN_TOKEN;
  }
  assert.ok(adminToken, 'Local employee credentials or ADMIN_SESSION_TOKEN required');
  session = (await api('/dev/patient-sessions', { method: 'POST', body: {} })).data;
  const policy = (await api('/privacy-policy')).data;
  await api('/me/privacy-consent', { token: session.token, method: 'POST', body: { policyVersion: policy.version, action: 'accepted' } });
  console.log('Connected; isolating test session.');
  await evaluate(function isolate(storageKeys, testSession, marker) {
    const app = getApp();
    if (app.__blkM2Smoke) throw new Error('Previous M2 test requires cleanup');
    const present = wx.getStorageInfoSync().keys;
    app.__blkM2Smoke = { marker, backups: Object.values(storageKeys).map(key => ({ key, present: present.indexOf(key) >= 0, value: wx.getStorageSync(key) })) };
    wx.setStorageSync(storageKeys.session, testSession);
    wx.removeStorageSync(storageKeys.expired);
    wx.removeStorageSync(storageKeys.submission);
    return true;
  }, keys, session, isolationMarker);
  storageIsolated = true;
  console.log('Storage isolated.');
  const doctors = (await api('/doctors')).data;
  const date = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(Date.now() + 86400000));
  const slots = (await api(`/slots?doctorId=${doctors.at(-1).id}&date=${date}`)).data;
  const slot = slots.find(x => x.status === 'open' && x.reserved < x.capacity);
  assert.ok(slot, 'Future available slot required');
  const res = await fetch(`${apiBase}/me/appointments`, { method: 'POST', headers: { Authorization: `Bearer ${session.token}`, 'Content-Type': 'application/json', 'Idempotency-Key': randomUUID() }, body: JSON.stringify({ slotId: slot.id, contactName: 'M2模拟器访客', contactPhone: '13800000000', note: '虚构交互验收资料' }) });
  assert.ok(res.ok, `Create appointment: ${res.status}`);
  appointment = (await res.json()).data;
  appointment = (await api(`/admin/appointments/${appointment.id}/review`, { token: adminToken, method: 'POST', body: { action: 'confirm', version: appointment.version } })).data;
  let page, data;
  if (!process.argv.includes('--plan-only')) {
  if (process.argv.includes('--photos-only')) {
    // Keep module acceptance independent of the known cross-page automation timeout.
    await evaluate(function route(url) { wx.reLaunch({ url }); return true; }, `/pages/care-photos/index?id=${appointment.id}`);
  } else {
    await evaluate(function route(url) { wx.reLaunch({ url }); return true; }, `/pages/appointment-detail/index?id=${appointment.id}`);
    const detail = await currentPage('pages/appointment-detail/index');
    await readyData(detail.pageId);
    await call(detail, 'openPhotos');
  }
  page = await currentPage('pages/care-photos/index');
  data = await readyData(page.pageId);
  assert.equal(data.granted, false);
  await renderedElements(page, '.photo-card', 3);
  await screenshot('guide');
  await send('App.mockWxMethod', { method: 'showModal', result: { confirm: true, cancel: false } });
  modalMocked = true;
  await call(page, 'grant');
  await waitUntil(async () => (await pageData(page.pageId)).granted, 'Native photo consent');
  // Produce a synthetic non-patient image inside the simulator filesystem.
  // Only the system picker is stubbed; uploadFile/downloadFile and API are real.
  const fixtureBytes = syntheticPng();
  const fixture = await evaluate(function makeFixture(base64) {
    const path = wx.env.USER_DATA_PATH + '/m2-synthetic.png';
    wx.getFileSystemManager().writeFileSync(path, base64, 'base64');
    return path;
  }, fixtureBytes.toString('base64'));
  await send('App.mockWxMethod', { method: 'chooseImage', result: { tempFilePaths: [fixture], tempFiles: [{ path: fixture, size: fixtureBytes.length }] } });
  chooseMocked = true;
  await send('App.mockWxMethod', { method: 'chooseMedia', result: { tempFiles: [{ tempFilePath: fixture, fileType: 'image', size: fixtureBytes.length }] } });
  mediaMocked = true;
  for (const angle of ['front', 'left', 'right']) {
    await reviewFixture(page, angle);
    await waitUntil(async () => {
      const d = await pageData(page.pageId);
      const item = d.items.find(x => x.angle === angle);
      if (item.status === 'failed') throw new Error(`Upload ${angle}: ${item.message}`);
      return !d.uploading && item.status === 'ready' && item.path && item.id;
    }, `Real upload and authenticated download ${angle}`);
  }
  await renderedElements(page, '.photo-preview', 3);
  if (process.argv.includes('--screenshots')) await evaluate(function scroll() { wx.pageScrollTo({ selector: '.photo-card', duration: 0 }); return true; });
  await screenshot('saved');
  const before = (await api(`/admin/appointments/${appointment.id}/photos`, { token: adminToken })).data;
  assert.equal(before.length, 3);
  const photoId = before.find(x => x.angle === 'front').id;
  const allowed = await fetch(`${apiBase}/admin/photos/${photoId}/content`, { headers: { Authorization: `Bearer ${adminToken}` } });
  assert.equal(allowed.status, 200);
  assert.equal(allowed.headers.get('content-type'), 'image/jpeg');
  await reviewFixture(page, 'front');
  await waitUntil(async () => {
    const d = await pageData(page.pageId);
    return !d.uploading && d.items[0].id && d.items[0].id !== photoId;
  }, 'Replace front photo');
  const photos = (await api(`/admin/appointments/${appointment.id}/photos`, { token: adminToken })).data;
  assert.equal(photos.length, 3);
  const replacement = photos.find(x => x.angle === 'front').id;
  assert.equal((await fetch(`${apiBase}/admin/photos/${photoId}/content`, { headers: { Authorization: `Bearer ${adminToken}` } })).status, 404);
  await call(page, 'revoke');
  await waitUntil(async () => !(await pageData(page.pageId)).granted, 'Native revoke');
  assert.deepEqual((await api(`/admin/appointments/${appointment.id}/photos`, { token: adminToken })).data, [], 'Revocation hides staff photo list');
  assert.equal((await fetch(`${apiBase}/admin/photos/${replacement}/content`, { headers: { Authorization: `Bearer ${adminToken}` } })).status, 403, 'Revocation denies original content');
  await call(page, 'load');
  data = await readyData(page.pageId);
  assert.equal(data.granted, false);
  assert.equal(data.items.filter(x => x.id && x.path).length, 3, 'Patient retains access');
  await renderedElements(page, '.photo-preview', 3);
  console.log('PASS: mocked picker → real image inspection → two review confirmations → three actual uploads/downloads → replace → revoke; staff denied, patient access retained. Native camera/album remains a separate acceptance.');
  }
  if (process.argv.includes('--photos-only')) { console.log('Photo scenario complete.'); } else {
  const draft = { summary: '虚构 M2 验收方案', stages: [{ title: '演示阶段', body: '仅用于检查发布和显示链路' }], instructions: '演示注意事项' };
  let workspace = (await api(`/admin/appointments/${appointment.id}/care-plan/draft`, { token: adminToken, method: 'PUT', body: { version: 0, content: draft } })).data;
  await evaluate(function route(url) { wx.reLaunch({ url }); return true; }, `/pages/care-plan/index?id=${appointment.id}`);
  page = await currentPage('pages/care-plan/index');
  data = await readyData(page.pageId);
  assert.equal(data.versions.length, 0, 'Draft is invisible to patient');
  await renderedElements(page, '.state-title', 1);
  const published = (await api(`/admin/appointments/${appointment.id}/care-plan/publish`, { token: adminToken, method: 'POST', body: { version: workspace.draftVersion, changeReason: 'M2验收发布' } })).data;
  await call(page, 'load');
  data = await readyData(page.pageId);
  assert.equal(data.versions[0].id, published.id);
  assert.equal(data.versions[0].content.summary, draft.summary);
  await renderedText(page, '.plan-summary', draft.summary);
  workspace = (await api(`/admin/appointments/${appointment.id}/care-plan/draft`, { token: adminToken, method: 'PUT', body: { version: workspace.draftVersion, content: { ...draft, summary: '不可见的新草稿' } } })).data;
  await call(page, 'load');
  data = await readyData(page.pageId);
  assert.equal(data.versions.length, 1);
  assert.equal(data.versions[0].content.summary, draft.summary);
  console.log('PASS: draft hidden → published version visible in actual native DOM → later draft remains hidden.');
  }
  assert.equal(exceptions.length, 0, JSON.stringify(exceptions));
} catch (error) {
  console.error('M2 scenario failed:', error);
  throw error;
} finally {
  const failures = [];
  for (const [method, active] of [['chooseImage', chooseMocked], ['chooseMedia', mediaMocked], ['showModal', modalMocked]]) {
    if (active) try { await send('App.mockWxMethod', { method }); } catch { failures.push(`restore ${method}`); }
  }
  if (appointment) try {
    const latest = (await api(`/me/appointments/${appointment.id}`, { token: session.token })).data;
    if (latest.canCancel) await api(`/me/appointments/${appointment.id}/cancel`, { token: session.token, method: 'POST', body: { version: latest.version } });
  } catch { failures.push('cancel test appointment'); }
  if (storageIsolated && connected) try {
    await evaluate(function route() { wx.reLaunch({ url: '/pages/doctors/index' }); return true; });
    try { await currentPage('pages/doctors/index'); } catch { /* still restore owned storage */ }
    await evaluate(function restore(marker) {
      const app = getApp(), state = app.__blkM2Smoke;
      if (!state || state.marker !== marker) throw new Error('Cleanup marker mismatch');
      for (const backup of state.backups) {
        if (backup.present) wx.setStorageSync(backup.key, backup.value);
        else wx.removeStorageSync(backup.key);
      }
      try { wx.getFileSystemManager().unlinkSync(wx.env.USER_DATA_PATH + '/m2-synthetic.png'); } catch {}
      delete app.__blkM2Smoke;
      return true;
    }, isolationMarker);
    console.log('Restored original identity/storage and released test appointment; audit records retained.');
  } catch (error) { console.error('Storage cleanup:', error.message); failures.push('restore patient storage'); }
  socket.close();
  if (employeeSessionCreated) try { await api('/admin/session', { token: adminToken, method: 'DELETE' }); } catch { failures.push('close test employee session'); }
  for (const callback of pending.values()) clearTimeout(callback.timer);
  if (failures.length) throw new Error(`Cleanup failed: ${failures.join(', ')}`);
}
