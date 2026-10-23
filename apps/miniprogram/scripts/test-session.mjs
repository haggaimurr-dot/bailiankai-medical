import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createContext, Script } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

// Execute the actual service modules. Only the WeChat platform boundary is mocked.
const appRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const turn = () => new Promise(setImmediate);
const session = (id) => ({ token: `test-token-${id}`, patient: { id, displayName: '演示访客' }, expiresAt: new Date(Date.now() + 600000).toISOString() });
const body = (slotId = 'slot-demo') => ({ slotId, contactName: '演示访客', contactPhone: '13800000000', note: '演示备注' });
const selection = (slotId = 'slot-demo') => ({ slotId, clinicId: 'clinic-demo', doctorId: 'doctor-demo', date: '2099-01-01' });
const draft = (patientId, key) => ({ patientId, key, createdAt: Date.now(), body: body(), selection: selection() });

function harness(mode = 'demo') {
  const storage = new Map();
  const requests = [], dialogs = [], randomRequests = [], logins = [];
  const wx = {
    login: (options) => logins.push(options),
    getStorageSync: (key) => structuredClone(storage.get(key)),
    setStorageSync: (key, value) => storage.set(key, structuredClone(value)),
    removeStorageSync: (key) => storage.delete(key),
    showModal: (options) => dialogs.push(options),
    getRandomValues: (options) => randomRequests.push(options),
    request: (options) => requests.push({
      options,
      respond: (statusCode, data) => options.success({ statusCode, data }),
      fail: () => options.fail({ errMsg: 'request:fail simulated network failure' }),
    }),
  };
  const context = createContext({ wx });
  const modules = new Map();
  function load(filename) {
    if (modules.has(filename)) return modules.get(filename).exports;
    const module = { exports: {} };
    modules.set(filename, module);
    const source = ts.transpileModule(readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
    const execute = new Script(`(function(require, module, exports) { ${source}\n })`, { filename }).runInContext(context);
    execute((specifier) => {
      assert.ok(specifier.startsWith('.'), `Unexpected runtime dependency: ${specifier}`);
      return load(resolve(dirname(filename), `${specifier}.ts`));
    }, module, module.exports);
    if (filename === resolve(appRoot, 'config/api.ts')) module.exports.PATIENT_LOGIN_MODE = mode;
    return module.exports;
  }
  return {
    storage, requests, dialogs, randomRequests, logins,
    keys: load(resolve(appRoot, 'config/storage.ts')),
    patient: load(resolve(appRoot, 'services/patient.ts')),
    submission: load(resolve(appRoot, 'services/submission.ts')),
  };
}

function respondSession(request, value) { request.respond(201, { data: value }); }
function unauthorized(request) { request.respond(401, { error: { code: 'PATIENT_SESSION_INVALID', message: '演示会话失效' } }); }
function randomResult(request, byte) { request.success({ randomValues: new Uint8Array(16).fill(byte).buffer }); }

test('binding replaces the session and blocks late responses from the original identity', async () => {
  const h = harness('wechat');
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('original'));
  const oldRead = h.patient.patientApi.me();
  const oldRejected = assert.rejects(oldRead, error => error.code === 'PATIENT_IDENTITY_CHANGED');
  const binding = h.patient.patientApi.bindChart('a'.repeat(32), 1, 'original');
  await turn();
  const bindRequest = h.requests.find(request => request.options.url.endsWith('/me/patient-binding'));
  assert.deepEqual(JSON.parse(JSON.stringify(bindRequest.options.data)), { code: 'a'.repeat(32), version: 1 });
  bindRequest.respond(200, { data: session('chart') });
  assert.equal((await binding).id, 'chart');
  h.requests.find(request => request.options.url.endsWith('/me')).respond(200, { data: session('original').patient });
  await oldRejected;
  assert.equal(h.patient.cachedSession().patient.id, 'chart');
});

test('binding failure preserves the current session and original booking recovery', async () => {
  const h = harness('wechat');
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('original'));
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, draft('original', 'unresolved'));
  const binding = h.patient.patientApi.bindChart('a'.repeat(32), 1, 'original');
  const rejected = assert.rejects(binding, error => error.code === 'PATIENT_BINDING_CONFLICT');
  await turn();
  h.requests[0].respond(409, { error: { code: 'PATIENT_BINDING_CONFLICT', message: '已有业务记录' } });
  await rejected;
  assert.equal(h.patient.cachedSession().patient.id, 'original');
  assert.equal(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY).key, 'unresolved');
});

test('a late binding response cannot replace a newly selected account', async () => {
  const h = harness('wechat');
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('original'));
  const binding = h.patient.patientApi.bindChart('a'.repeat(32), 1, 'original');
  const rejected = assert.rejects(binding, error => error.code === 'PATIENT_IDENTITY_CHANGED');
  await turn();
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('new-account'));
  h.requests[0].respond(200, { data: session('chart') });
  await rejected;
  assert.equal(h.patient.cachedSession().patient.id, 'new-account');
});

test('wechat login is shared and sends only the temporary code', async () => {
  const h = harness('wechat');
  const first = h.patient.ensureSession();
  const second = h.patient.ensureSession();
  assert.equal(h.logins.length, 1);
  h.logins[0].success({ code: 'wx-code' });
  await turn();
  assert.equal(h.requests.length, 1);
  assert.ok(h.requests[0].options.url.endsWith('/patient/session'));
  assert.deepEqual(JSON.parse(JSON.stringify(h.requests[0].options.data)), { code: 'wx-code' });
  const value = { ...session('stable'), patient: { id: 'stable', displayName: '患者甲', loginKind: 'wechat', version: 1 } };
  h.requests[0].respond(200, { data: value });
  const result = await Promise.all([first, second]);
  assert.equal(result[0].patient.id, 'stable');
  assert.equal(result[1].patient.id, 'stable');
});

test('wechat relogin preserves unresolved booking recovery for the same patient', async () => {
  const h = harness('wechat');
  h.storage.set(h.keys.SESSION_EXPIRED_KEY, { patientId: 'stable' });
  const original = draft('stable', 'pending-request');
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, original);
  const first = h.patient.confirmNewSession();
  const second = h.patient.confirmNewSession();
  assert.equal(h.dialogs.length, 0);
  assert.equal(h.logins.length, 1);
  h.logins[0].success({ code: 'fresh-code' });
  await turn();
  h.requests[0].respond(200, { data: session('stable') });
  assert.deepEqual(await Promise.all([first, second]), [true, true]);
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), original);
  assert.equal(h.storage.has(h.keys.SESSION_EXPIRED_KEY), false);
});

test('a stale successful profile response cannot replace a new identity', async () => {
  const h = harness('wechat');
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('old'));
  const pending = h.patient.patientApi.me();
  await turn();
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('new'));
  h.requests[0].respond(200, { data: { id: 'old', displayName: '旧患者', version: 1 } });
  await assert.rejects(pending, (error) => error.code === 'PATIENT_IDENTITY_CHANGED');
  assert.equal(h.storage.get(h.keys.SESSION_STORAGE_KEY).patient.id, 'new');
});

test('late profile reads do not regress cached profile version', async () => {
  const h = harness('wechat');
  const current = session('stable');
  current.patient.version = 1;
  h.storage.set(h.keys.SESSION_STORAGE_KEY, current);
  const read = h.patient.patientApi.me();
  await turn();
  const update = h.patient.patientApi.updateProfile('新姓名', 1, 'stable');
  await turn();
  h.requests[1].respond(200, { data: { id: 'stable', displayName: '新姓名', version: 2 } });
  await update;
  h.requests[0].respond(200, { data: { id: 'stable', displayName: '旧姓名', version: 1 } });
  await read;
  assert.equal(h.storage.get(h.keys.SESSION_STORAGE_KEY).patient.displayName, '新姓名');
});

test('logout invalidates the session without discarding pending booking recovery', async () => {
  const h = harness('wechat');
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('stable'));
  const original = draft('stable', 'pending-request');
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, original);
  const pending = h.patient.patientApi.logout();
  await turn();
  assert.ok(h.requests[0].options.url.endsWith('/me/logout'));
  h.requests[0].respond(200, { data: true });
  await pending;
  assert.equal(h.storage.has(h.keys.SESSION_STORAGE_KEY), false);
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), original);
});


test('concurrent explicit renewals share one dialog and one new identity', async () => {
  const h = harness();
  h.storage.set(h.keys.SESSION_EXPIRED_KEY, { patientId: 'old' });
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, draft('old', 'old-request'));
  const first = h.patient.confirmNewSession();
  const second = h.patient.confirmNewSession();
  assert.equal(h.dialogs.length, 1);
  h.dialogs[0].success({ confirm: true });
  await turn();
  assert.equal(h.requests.length, 1);
  respondSession(h.requests[0], session('new'));
  assert.deepEqual(await Promise.all([first, second]), [true, true]);
  assert.equal(h.storage.get(h.keys.SESSION_STORAGE_KEY).patient.id, 'new');
  assert.equal(h.storage.has(h.keys.SUBMISSION_STORAGE_KEY), false);
  const newDraft = draft('new', 'new-request');
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, newDraft);
  await turn();
  assert.equal(h.requests.length, 1, 'No late second identity request may overwrite the new session');
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), newDraft);
});

test('renewal preserves a recovery record that replaced the original draft', async () => {
  const h = harness();
  h.storage.set(h.keys.SESSION_EXPIRED_KEY, { patientId: 'old' });
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, draft('old', 'old-request'));
  const renewal = h.patient.confirmNewSession();
  h.dialogs[0].success({ confirm: true });
  await turn();
  const replacement = draft('new', 'new-request');
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, replacement);
  respondSession(h.requests[0], session('new'));
  await renewal;
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), replacement);
});

test('an old 401 cannot clear a newer identity or its recovery draft', async () => {
  const h = harness();
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('old'));
  const delayed = h.patient.patientApi.me();
  const probe = h.patient.patientApi.me();
  await turn();
  assert.equal(h.requests[0].options.header.Authorization, 'Bearer test-token-old');
  const probeRejected = assert.rejects(probe, (error) => error.code === 'SESSION_EXPIRED');
  unauthorized(h.requests[1]);
  await probeRejected;
  const renewal = h.patient.confirmNewSession();
  h.dialogs[0].success({ confirm: true });
  await turn();
  respondSession(h.requests[2], session('new'));
  await renewal;
  const replacement = draft('new', 'new-request');
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, replacement);
  const delayedRejected = assert.rejects(delayed, (error) => error.code === 'SESSION_EXPIRED');
  unauthorized(h.requests[0]);
  await delayedRejected;
  assert.equal(h.storage.get(h.keys.SESSION_STORAGE_KEY).patient.id, 'new');
  assert.equal(h.storage.has(h.keys.SESSION_EXPIRED_KEY), false);
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), replacement);
});

test('failed identity renewal preserves expiry and recovery data and permits retry', async () => {
  const h = harness();
  const original = draft('old', 'old-request');
  h.storage.set(h.keys.SESSION_EXPIRED_KEY, { patientId: 'old' });
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, original);
  const renewal = h.patient.confirmNewSession();
  h.dialogs[0].success({ confirm: true });
  await turn();
  const rejected = assert.rejects(renewal, (error) => error.code === 'NETWORK_ERROR');
  h.requests[0].fail();
  await rejected;
  assert.equal(h.storage.has(h.keys.SESSION_EXPIRED_KEY), true);
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), original);
  await assert.rejects(h.patient.ensureSession(), (error) => error.code === 'SESSION_EXPIRED');
  const retry = h.patient.confirmNewSession();
  h.dialogs[1].success({ confirm: true });
  await turn();
  respondSession(h.requests[1], session('new'));
  assert.equal(await retry, true);
});

test('concurrent draft creation never overwrites the first unresolved submission', async () => {
  const h = harness();
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('same'));
  const first = h.submission.saveSubmission('same', body('slot-A'), selection('slot-A'));
  const second = h.submission.saveSubmission('same', body('slot-B'), selection('slot-B'));
  randomResult(h.randomRequests[0], 1);
  const saved = await first;
  const rejected = assert.rejects(second, /已有一笔申请/);
  randomResult(h.randomRequests[1], 2);
  await rejected;
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), structuredClone(saved));
  h.submission.clearSubmission();
  h.submission.clearSubmission('unrelated-request');
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), structuredClone(saved));
});

test('identity replacement during random key generation cannot save an old identity draft', async () => {
  const h = harness();
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('old'));
  const saving = h.submission.saveSubmission('old', body(), selection());
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('new'));
  const replacement = draft('new', 'new-request');
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, replacement);
  const rejected = assert.rejects(saving, (error) => error.code === 'PATIENT_IDENTITY_CHANGED');
  randomResult(h.randomRequests[0], 1);
  await rejected;
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), replacement);
  assert.equal(h.requests.length, 0);
});

test('submit and recovery requests remain bound to the draft patient identity', async () => {
  const h = harness();
  const original = draft('old', 'old-request');
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('new'));
  h.storage.set(h.keys.SUBMISSION_STORAGE_KEY, original);
  await assert.rejects(h.patient.patientApi.create(original.body, original.key, original.patientId), (error) => error.code === 'PATIENT_IDENTITY_CHANGED');
  await assert.rejects(h.patient.patientApi.byRequest(original.key, original.patientId), (error) => error.code === 'PATIENT_IDENTITY_CHANGED');
  assert.equal(h.requests.length, 0, 'A mismatched identity must not send the old request with a new token');
  assert.deepEqual(h.storage.get(h.keys.SUBMISSION_STORAGE_KEY), original);
});
test('privacy confirmation saves the displayed version before replaying the same booking', async () => {
  const h = harness();
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('same'));
  const result = h.patient.patientApi.create(body(), 'same-request-key', 'same');
  await turn();
  h.requests[0].respond(403, { error: { code: 'PRIVACY_CONSENT_REQUIRED', message: '请同意隐私说明' } });
  await turn();
  h.requests[1].respond(200, { data: { title: '隐私说明', version: 'v2', content: '待确认正文', effectiveAt: '2026-10-02', status: 'draft' } });
  await turn();
  assert.match(h.dialogs[0].content, /待确认正文/);
  h.dialogs[0].success({ confirm: true });
  await turn();
  assert.deepEqual(structuredClone(h.requests[2].options.data), { policyVersion: 'v2', action: 'accepted' });
  h.requests[2].respond(200, { data: { id: 'consent' } });
  await turn();
  assert.equal(h.requests[3].options.header['Idempotency-Key'], 'same-request-key');
  assert.deepEqual(structuredClone(h.requests[3].options.data), body());
  h.requests[3].respond(201, { data: { id: 'booking' } });
  assert.equal((await result).id, 'booking');
});

test('declining privacy does not save consent or retry the booking', async () => {
  const h = harness();
  h.storage.set(h.keys.SESSION_STORAGE_KEY, session('same'));
  const result = h.patient.patientApi.create(body(), 'same-request-key', 'same');
  const rejected = assert.rejects(result, error => error.code === 'PRIVACY_CONSENT_REQUIRED');
  await turn();
  h.requests[0].respond(403, { error: { code: 'PRIVACY_CONSENT_REQUIRED', message: '请同意隐私说明' } });
  await turn();
  h.requests[1].respond(200, { data: { title: '隐私说明', version: 'v2', content: '正文' } });
  await turn(); h.dialogs[0].success({ confirm: false });
  await rejected;
  assert.equal(h.requests.length, 2);
});

test('switching patient while privacy dialog is open cannot save consent for either account', async () => {
  const h = harness(); h.storage.set(h.keys.SESSION_STORAGE_KEY, session('old'));
  const result = h.patient.patientApi.create(body(), 'same-request-key', 'old');
  const rejected = assert.rejects(result, error => error.code === 'PATIENT_IDENTITY_CHANGED');
  await turn(); h.requests[0].respond(403, { error: { code: 'PRIVACY_CONSENT_REQUIRED', message: '请同意隐私说明' } });
  await turn(); h.requests[1].respond(200, { data: { title: '隐私说明', version: 'v2', content: '正文' } });
  await turn(); h.storage.set(h.keys.SESSION_STORAGE_KEY, session('new')); h.dialogs[0].success({ confirm: true });
  await rejected; assert.equal(h.requests.length, 2);
});
