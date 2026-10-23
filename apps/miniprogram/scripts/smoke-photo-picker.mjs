import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { createInterface } from 'node:readline/promises';

// Interactive, unmocked acceptance in WeChat DevTools. The operator handles
// native dialogs and review; this script observes the real API and page state.
// It never substitutes chooseMedia, chooseImage, getImageInfo, canvas, or upload.
const options = Object.fromEntries(process.argv.slice(2).filter(value => value.startsWith('--')).map(value => {
  const [key, ...rest] = value.slice(2).split('=');
  return [key, rest.join('=') || true];
}));
if (options.help) {
  console.log('node scripts/smoke-photo-picker.mjs --source=album|camera --angle=front|left|right --expect=success|cancel|rejected [--timeout=180] [--evidence=path.json]');
  console.log('需要 Node 22+、本地 API 和微信 auto WebSocket。使用隔离演示身份；请在真实系统窗口操作，并手动核对清晰度/角度。');
  process.exit(0);
}
const source = options.source || 'album', angle = options.angle || 'front', expected = options.expect || 'success';
assert.ok(['album', 'camera'].includes(source), 'Invalid --source');
assert.ok(['front', 'left', 'right'].includes(angle), 'Invalid --angle');
assert.ok(['success', 'cancel', 'rejected'].includes(expected), 'Invalid --expect');
const timeoutSeconds = Number(options.timeout || 180);
assert.ok(Number.isFinite(timeoutSeconds) && timeoutSeconds >= 30 && timeoutSeconds <= 900, '--timeout must be 30–900 seconds');
const timeoutMs = timeoutSeconds * 1000;
const evidencePath = resolve(String(options.evidence || `tmp/photo-picker-${source}-${angle}-${expected}.json`));
const apiBase = 'http://127.0.0.1:8080/api/v1';
const storageKeys = ['blk_dev_patient_session_v1', 'blk_dev_patient_expired_v1', 'blk_appointment_submission_v1'];
const marker = randomUUID(), pending = new Map();
const evidence = { startedAt: new Date().toISOString(), source, angle, expected, pickerMockedByScript: false, manualReview: true, sourceInvocation: 'rendered-dataset-handler', sourceButtonBindingTapVerified: false, trueDeviceVerified: false, status: 'not_completed', nativeUiObservedByOperator: false, cleanup: 'pending' };
const socket = new WebSocket(process.env.MINIPROGRAM_WS || 'ws://127.0.0.1:9420');
let sequence = 0, connected = false, isolationAttempted = false, interrupted = false, exceptions = 0, session, appointment;
const onInterrupt = () => { interrupted = true; };
process.on('SIGINT', onInterrupt);
socket.addEventListener('message', ({ data }) => {
  const message = JSON.parse(data);
  if (message.method === 'App.exceptionThrown') exceptions++;
  const callback = pending.get(message.id);
  if (!callback) return;
  pending.delete(message.id); clearTimeout(callback.timer);
  if (message.error) callback.reject(new Error(`WeChat protocol failed: ${callback.method}`));
  else callback.resolve(message.result);
});
socket.addEventListener('close', () => {
  connected = false;
  for (const callback of pending.values()) { clearTimeout(callback.timer); callback.reject(new Error('WeChat connection closed')); }
  pending.clear();
});
function send(method, params = {}, waitMs = 20000) {
  return new Promise((resolveResult, reject) => {
    if (socket.readyState !== WebSocket.OPEN) return reject(new Error('WeChat connection unavailable'));
    const id = String(++sequence);
    const timer = setTimeout(() => { pending.delete(id); reject(new Error(`WeChat protocol timeout: ${method}`)); }, waitMs);
    pending.set(id, { method, resolve: resolveResult, reject, timer });
    socket.send(JSON.stringify({ id, method, params }));
  });
}
async function evaluate(fn, ...args) {
  return (await send('App.callFunction', { functionDeclaration: fn.toString(), args })).result;
}
async function waitUntil(check, label, duration = 15000) {
  const deadline = Date.now() + duration;
  while (Date.now() < deadline) {
    if (interrupted) throw new Error('Operator interrupted acceptance');
    const result = await check();
    if (result) return result;
    await new Promise(resolveWait => setTimeout(resolveWait, 250));
  }
  throw new Error(`Acceptance timeout: ${label}`);
}
async function api(path, { method = 'GET', body, token = session?.token, requestKey } = {}) {
  const response = await fetch(apiBase + path, {
    method, signal: AbortSignal.timeout(15000),
    headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}), ...(requestKey ? { 'Idempotency-Key': requestKey } : {}) },
    ...(body ? { body: JSON.stringify(body) } : {}),
  });
  const result = await response.json();
  if (!response.ok) throw new Error(`API HTTP ${response.status}: ${result.error?.code || 'REQUEST_FAILED'}`);
  return result.data;
}
async function currentPage(path) {
  await waitUntil(async () => (await send('App.getCurrentPage')).path === path, 'route');
  await new Promise(resolveWait => setTimeout(resolveWait, 3000));
  return waitUntil(async () => { const page = await send('App.getCurrentPage'); return page.path === path ? page : null; }, 'settled route');
}
async function assertCurrentPage(page) {
  const current = await send('App.getCurrentPage');
  assert.equal(current.path, page.path, 'Acceptance page route changed');
  assert.equal(current.pageId, page.pageId, 'Acceptance page instance changed');
}
async function pageData(page) {
  await assertCurrentPage(page);
  return evaluate(function currentData(path) {
    const current = getCurrentPages()[getCurrentPages().length - 1];
    if (!current || current.route !== path) throw new Error('Acceptance page route changed');
    if (getApp().__blkPhotoPickerSmoke?.methodFailure) throw new Error('Acceptance page method failed');
    return current.data;
  }, page.path);
}
async function call(page, method, dataset) {
  await assertCurrentPage(page);
  return evaluate(function invoke(path, name, eventDataset, patientId, runMarker) {
    const current = getCurrentPages()[getCurrentPages().length - 1];
    if (!current || current.route !== path || typeof current[name] !== 'function') throw new Error('Acceptance page method unavailable');
    const stored = wx.getStorageSync('blk_dev_patient_session_v1');
    if (getApp().__blkPhotoPickerSmoke?.marker !== runMarker || current.data.patientId !== patientId || stored?.patient?.id !== patientId) throw new Error('Acceptance identity changed');
    const work = eventDataset ? current[name]({ currentTarget: { dataset: eventDataset } }) : current[name]();
    // Start the real asynchronous method and observe its page state. Returning
    // immediately allows the operator time to handle native consent dialogs.
    if (work && typeof work.catch === 'function') work.catch(() => {
      const state = getApp().__blkPhotoPickerSmoke;
      if (state) state.methodFailure = true;
    });
    return true;
  }, page.path, method, dataset, session.patient.id, marker);
}
async function sourceButton(page) {
  await assertCurrentPage(page);
  await evaluate(function query(path, selectedSource, runMarker) {
    const current = getCurrentPages()[getCurrentPages().length - 1], state = getApp().__blkPhotoPickerSmoke;
    if (!current || current.route !== path || state?.marker !== runMarker) throw new Error('Acceptance page query unavailable');
    state.sourceButtons = null;
    wx.createSelectorQuery().in(current).selectAll('.source-' + selectedSource).fields({ dataset: true, rect: true, size: true }).exec(results => {
      if (getApp().__blkPhotoPickerSmoke?.marker !== runMarker) return;
      state.sourceButtons = (results[0] || []).map(node => ({
        dataset: { angle: node.dataset?.angle, source: node.dataset?.source },
        width: node.width, height: node.height,
      }));
    });
    return true;
  }, page.path, source, marker);
  const nodes = await waitUntil(() => evaluate(function queryResult(runMarker) {
    const state = getApp().__blkPhotoPickerSmoke;
    if (state?.marker !== runMarker) throw new Error('Acceptance query owner changed');
    return state.sourceButtons;
  }, marker), 'rendered source button');
  const button = nodes.find(node => node.dataset.angle === angle && node.dataset.source === source);
  assert.ok(button && button.width > 0 && button.height > 0, 'Rendered source button with matching dataset required');
  return button;
}
async function photos() { return api(`/me/appointments/${appointment.id}/photos`); }
function summarizeInspection(inspection) {
  if (!inspection) return null;
  return {
    width: inspection.width, height: inspection.height,
    blocked: Boolean(inspection.blocked), needsManualConfirmation: Boolean(inspection.needsManualConfirmation),
    checks: (inspection.checks || []).map(check => ({ key: check.key, status: check.status })),
  };
}
async function attestNativeUi() {
  if (!process.stdin.isTTY) throw new Error('Interactive terminal required to attest native UI');
  const input = createInterface({ input: process.stdin, output: process.stdout });
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), Math.min(timeoutMs, 60000));
  try {
    const answer = await input.question('请确认本次确已看到并操作原生相册/相机窗口；输入 NATIVE（未看到请直接回车）：', { signal: controller.signal });
    assert.equal(answer.trim(), 'NATIVE', 'Native UI observation was not confirmed');
    evidence.nativeUiObservedByOperator = true;
  } finally { clearTimeout(timer); input.close(); }
}

try {
  await new Promise((resolveOpen, reject) => {
    const timer = setTimeout(() => reject(new Error('WeChat auto WebSocket unavailable')), 10000);
    socket.addEventListener('open', () => { clearTimeout(timer); connected = true; resolveOpen(); }, { once: true });
    socket.addEventListener('error', () => { clearTimeout(timer); reject(new Error('WeChat auto WebSocket unavailable')); }, { once: true });
  });
  const runtime = await evaluate(function environment() {
    const info = wx.getSystemInfoSync();
    return { platform: info.platform, system: info.system, SDKVersion: info.SDKVersion, version: info.version, model: info.model };
  });
  evidence.runtime = runtime;
  assert.equal(runtime.platform, 'devtools', 'This script verifies DevTools native picker only; use the separate true-device checklist');
  session = await api('/dev/patient-sessions', { method: 'POST', body: {}, token: '' });
  // This consent is test setup for the synthetic identity, not a consent UX pass.
  const policy = await api('/privacy-policy', { token: '' });
  await api('/me/privacy-consent', { method: 'POST', body: { policyVersion: policy.version, action: 'accepted' } });
  isolationAttempted = true;
  await evaluate(function isolate(keys, testSession, runMarker) {
    const app = getApp();
    if (app.__blkM1Smoke || app.__blkM2Smoke || app.__blkM3Smoke || app.__blkPhotoPickerSmoke) throw new Error('Another acceptance owns the simulator');
    const present = wx.getStorageInfoSync().keys;
    app.__blkPhotoPickerSmoke = { marker: runMarker, backups: keys.map(key => ({ key, present: present.indexOf(key) >= 0, value: wx.getStorageSync(key) })) };
    wx.setStorageSync(keys[0], testSession); wx.removeStorageSync(keys[1]); wx.removeStorageSync(keys[2]);
    return true;
  }, storageKeys, session, marker);
  const doctors = await api('/doctors', { token: '' });
  let selectedSlot;
  for (let day = 1; day <= 7 && !selectedSlot; day++) {
    const date = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(Date.now() + day * 86400000));
    const slots = await api(`/slots?doctorId=${encodeURIComponent(doctors.at(-1).id)}&date=${date}`, { token: '' });
    selectedSlot = slots.find(slot => slot.status === 'open' && slot.reserved < slot.capacity);
  }
  assert.ok(selectedSlot, 'Future available demo appointment required');
  appointment = await api('/me/appointments', { method: 'POST', requestKey: randomUUID(), body: { slotId: selectedSlot.id, contactName: '原生选择器演示验收', contactPhone: '13800000000', note: '隔离演示身份；非患者资料' } });
  await evaluate(function route(url) { wx.reLaunch({ url }); return true; }, `/pages/care-photos/index?id=${appointment.id}`);
  const page = await currentPage('pages/care-photos/index');
  await waitUntil(async () => { const data = await pageData(page); if (data.error || data.expired) throw new Error('Photo page failed to load'); return !data.loading && data.uploadAllowed; }, 'photo page');
  console.log('请在微信开发者工具中手动同意本次照片诊疗授权。此步骤不使用确认框替身。');
  await call(page, 'grant');
  await waitUntil(async () => {
    const data = await pageData(page);
    if (data.error || data.actionError || data.expired) throw new Error('Native photo consent failed');
    return data.granted;
  }, 'native photo consent', timeoutMs);
  const button = await sourceButton(page);
  console.log(`即将打开真实 ${source === 'camera' ? '相机' : '相册'}，角度 ${angle}。请只使用测试图片或演示模特资料。`);
  console.log(expected === 'success' ? '选择或拍摄后，请先停留在检查预览；等待终端提示后再勾选和保存。' : expected === 'cancel' ? '请在系统窗口取消；不要选取或上传图片。' : '请选择短边小于 320 像素的测试图片；不要确认保存。');
  // This DevTools build can hang Page.* and Element.tap. Read the actual native
  // rendered node's dataset, then invoke its real handler. This is not proof of
  // the button's tap binding; no picker/platform implementation is substituted.
  await call(page, 'choose', button.dataset);
  const inspected = await waitUntil(async () => {
    const data = await pageData(page);
    if (data.expired) throw new Error('Session expired during native selection');
    if (data.error || data.actionError) throw new Error('Native selection or inspection failed');
    if (data.selecting || data.checking) return null;
    if (expected === 'cancel') return !data.draft && !data.error && !data.actionError ? data : null;
    if (data.draft?.inspection) return data;
    if (data.draft?.error) throw new Error('Native inspection failed');
    return null;
  }, 'native selection result', timeoutMs);
  evidence.inspection = summarizeInspection(inspected.draft?.inspection);
  if (expected === 'success') {
    assert.equal(inspected.draft.inspection.blocked, false, 'Chosen success fixture must pass blocking checks');
    assert.equal((await photos()).length, 0, 'Staged review must precede first upload');
    console.log('已核验保存前服务端无照片。现在请放大核对清晰度与角度，勾选两项，然后按确认保存。');
    await waitUntil(async () => {
      const data = await pageData(page), item = data.items.find(photo => photo.angle === angle);
      if (data.error || data.actionError || data.draft?.error || data.expired || item?.status === 'failed') throw new Error('Native upload or authenticated download failed');
      return !data.uploading && item?.status === 'ready' && item.id && item.path ? data : null;
    }, 'manual review and actual upload/download', timeoutMs);
    const saved = await photos();
    assert.equal(saved.length, 1); assert.equal(saved[0].angle, angle);
    const content = await fetch(`${apiBase}/me/photos/${encodeURIComponent(saved[0].id)}/content`, { headers: { Authorization: `Bearer ${session.token}` }, signal: AbortSignal.timeout(15000) });
    assert.equal(content.status, 200); assert.equal(content.headers.get('content-type'), 'image/jpeg');
    evidence.uploadVerified = true; evidence.authenticatedReadVerified = true;
  } else {
    if (expected === 'rejected') assert.equal(inspected.draft.inspection.blocked, true, 'Chosen rejection fixture must have a blocking quality result');
    assert.equal((await photos()).length, 0, 'Cancelled/rejected selection must not upload');
    if (expected === 'rejected') await call(page, 'discardDraft');
    evidence.uploadVerified = false; evidence.noUploadVerified = true;
  }
  await attestNativeUi();
  if (expected === 'cancel') {
    const cancelled = await pageData(page);
    assert.ok(!cancelled.selecting && !cancelled.checking && !cancelled.draft && !cancelled.error && !cancelled.actionError, 'Native cancellation must leave no draft or error');
    assert.equal((await photos()).length, 0, 'Native cancellation must not upload');
  }
  assert.equal(exceptions, 0, 'WeChat JavaScript exceptions detected');
  evidence.status = 'passed';
} catch (error) {
  evidence.status = 'failed';
  // All errors above are controlled descriptions. Do not serialize native
  // error payloads, patient sessions, selected paths, or image bytes.
  evidence.failure = error instanceof Error ? error.message : 'Acceptance failed';
  console.error('未完成原生选择器验收：' + evidence.failure);
  process.exitCode = 1;
} finally {
  // An operator interruption ends acceptance, but must still allow cleanup waits.
  interrupted = false;
  const cleanupErrors = [];
  if (appointment) {
    let latest;
    try {
      latest = await api(`/me/appointments/${appointment.id}`);
      // Cancelled appointments do not permit consent writes. Revoke first.
      if (['pending', 'confirmed', 'completed'].includes(latest.status)) await api(`/me/appointments/${appointment.id}/photo-consent`, { method: 'POST', body: { granted: false } });
    } catch { cleanupErrors.push('revoke script-owned photo consent'); }
    try {
      latest = await api(`/me/appointments/${appointment.id}`);
      if (latest.canCancel) await api(`/me/appointments/${appointment.id}/cancel`, { method: 'POST', body: { version: latest.version } });
    } catch { cleanupErrors.push('cancel script-owned appointment'); }
  }
  if (connected && isolationAttempted) {
    try {
      // Even if a native dialog blocks route completion, restore the owned
      // storage after requesting departure from the private page.
      await evaluate(function route() { wx.reLaunch({ url: '/pages/doctors/index' }); return true; });
      try { await currentPage('pages/doctors/index'); } catch { cleanupErrors.push('leave native dialog/private page'); }
      await evaluate(function restore(runMarker) {
        const app = getApp(), state = app.__blkPhotoPickerSmoke;
        if (!state) return true;
        if (state.marker !== runMarker) throw new Error('Storage owner mismatch');
        for (const backup of state.backups) {
          if (backup.present) wx.setStorageSync(backup.key, backup.value); else wx.removeStorageSync(backup.key);
        }
        delete app.__blkPhotoPickerSmoke;
        return true;
      }, marker);
    } catch { cleanupErrors.push('restore original identity/storage'); }
  } else if (isolationAttempted) cleanupErrors.push('restore storage after lost connection');
  evidence.cleanup = cleanupErrors.length ? 'requires_attention' : 'completed';
  if (cleanupErrors.length) { evidence.cleanupErrors = cleanupErrors; process.exitCode = 1; }
  evidence.finishedAt = new Date().toISOString(); evidence.exceptionCount = exceptions;
  socket.close();
  for (const callback of pending.values()) clearTimeout(callback.timer);
  process.removeListener('SIGINT', onInterrupt);
  await mkdir(dirname(evidencePath), { recursive: true });
  await writeFile(evidencePath, JSON.stringify(evidence, null, 2) + '\n', 'utf8');
  console.log(`${evidence.status === 'passed' && evidence.cleanup === 'completed' ? 'PASS' : 'INCOMPLETE'}: ${source}/${angle}/${expected}; evidence: ${evidencePath}`);
  console.log('此记录仅覆盖开发者工具原生选择器；iOS/Android 真机需按验收文档另行验证。演示照片与审计保留，未删除其他资料。');
}
