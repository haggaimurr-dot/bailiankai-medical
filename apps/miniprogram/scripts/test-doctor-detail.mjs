import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createContext, Script } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const appRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const clinic = { id: 'clinic-demo', name: '演示院区', address: '演示地址' };
const doctor = { id: 'doctor-demo', clinicId: clinic.id, name: '演示医生', title: '演示资料', summary: '简介', initials: '医', focus: ['面诊咨询'] };

function harness(name) {
  const requests = [], navigation = [];
  let page;
  const context = createContext({
    Page: (definition) => { page = definition; page.setData = (data) => Object.assign(page.data, data); },
    wx: { request: (options) => requests.push(options), navigateTo: (options) => navigation.push(options.url), stopPullDownRefresh() {} },
  });
  const cache = new Map();
  function load(filename) {
    if (cache.has(filename)) return cache.get(filename).exports;
    const module = { exports: {} };
    cache.set(filename, module);
    const source = ts.transpileModule(readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
    new Script(`(function(require, module, exports) { ${source}\n })`, { filename }).runInContext(context)(
      (specifier) => load(resolve(dirname(filename), `${specifier}.ts`)), module, module.exports,
    );
    return module.exports;
  }
  load(resolve(appRoot, `pages/${name}/index.ts`));
  return { page, requests, navigation };
}

function respond(request, data) { request.success({ statusCode: 200, data: { data } }); }
function loadProfile(h, doctors = [doctor]) {
  h.page.clinicId = clinic.id; h.page.doctorId = doctor.id;
  const pending = h.page.load();
  respond(h.requests[0], [clinic]); respond(h.requests[1], doctors);
  return pending;
}

test('approved mock qualifications show Shanghai review date and clear it on reload failure', async () => {
  const h = harness('doctor-detail');
  await loadProfile(h, [{ ...doctor, presentation: { isDemo: true, credentials: [] }, qualificationReviewedAt: '2026-10-07T16:30:00Z' }]);
  assert.equal(h.page.data.reviewedDate, '2026-10-08');
  assert.equal(h.page.data.isDemo, true);
  const pending = h.page.load();
  h.requests[2].fail(); respond(h.requests[3], []);
  await pending;
  assert.equal(h.page.data.reviewedDate, '');
});

test('home and directory entries preserve doctor and clinic in the detail URL', () => {
  for (const name of ['home', 'doctors']) {
    const h = harness(name);
    h.page.setData({ loading: false, doctors: [doctor] });
    h.page.openDoctorDetail({ currentTarget: { dataset: { doctorId: doctor.id } } });
    assert.equal(h.navigation[0], `/pages/doctor-detail/index?clinicId=${clinic.id}&doctorId=${doctor.id}`);
  }
});

test('detail loads API profile and navigates to the same doctor schedule', async () => {
  const h = harness('doctor-detail');
  await loadProfile(h);
  assert.equal(h.requests[1].data.clinicId, clinic.id);
  assert.equal(h.page.data.doctor.name, doctor.name);
  assert.equal(h.page.data.error, '');
  h.page.openSchedule();
  assert.equal(h.navigation[0], `/pages/schedule/index?clinicId=${clinic.id}&doctorId=${doctor.id}`);
});

test('missing, unknown and mismatched doctors never allow booking', async () => {
  const missing = harness('doctor-detail');
  await missing.page.load();
  assert.equal(missing.requests.length, 0);
  assert.ok(missing.page.data.error);
  for (const doctors of [[], [{ ...doctor, clinicId: 'other-clinic' }]]) {
    const h = harness('doctor-detail');
    await loadProfile(h, doctors);
    assert.ok(h.page.data.error);
    assert.equal(h.page.data.doctor, null);
    h.page.openSchedule();
    assert.equal(h.navigation.length, 0);
  }
});

test('network failure can retry and unloaded pages ignore pending responses', async () => {
  const h = harness('doctor-detail');
  h.page.clinicId = clinic.id; h.page.doctorId = doctor.id;
  const failed = h.page.load();
  h.requests[0].fail(); respond(h.requests[1], [doctor]);
  await failed;
  assert.ok(h.page.data.error);
  const retry = h.page.load();
  respond(h.requests[2], [clinic]); respond(h.requests[3], [doctor]);
  await retry;
  assert.equal(h.page.data.error, '');
  const pending = h.page.load();
  h.page.onUnload();
  respond(h.requests[4], [clinic]); respond(h.requests[5], [doctor]);
  await pending;
  assert.equal(h.page.data.doctor, null);
});

test('schedule filters the selected doctor and carries identity into booking', async () => {
  const h = harness('schedule');
  const pending = h.page.initialize({ clinicId: clinic.id, doctorId: doctor.id });
  respond(h.requests[0], [clinic]); respond(h.requests[1], [doctor]);
  await new Promise(setImmediate);
  assert.equal(h.requests[2].data.doctorId, doctor.id);
  const start = new Date(Date.now() + 3600000).toISOString();
  respond(h.requests[2], [{ id: 'slot-demo', clinicId: clinic.id, doctorId: doctor.id, startsAt: start, endsAt: new Date(Date.now() + 5400000).toISOString(), capacity: 3, reserved: 0, status: 'open', version: 1 }]);
  await pending;
  h.page.bookSlot({ currentTarget: { dataset: { id: 'slot-demo' } } });
  const query = new URLSearchParams(h.navigation[0].split('?')[1]);
  assert.equal(query.get('doctorId'), doctor.id);
  assert.equal(query.get('clinicId'), clinic.id);
  assert.equal(query.get('slotId'), 'slot-demo');
});

test('a removed doctor fails schedule initialization instead of showing all doctors', async () => {
  const h = harness('schedule');
  const options = { clinicId: clinic.id, doctorId: 'removed-doctor' };
  const pending = h.page.initialize(options);
  respond(h.requests[0], [clinic]); respond(h.requests[1], [doctor]);
  await pending;
  assert.ok(h.page.data.initialError);
  assert.equal(h.requests.length, 2);
  h.page.retry();
  respond(h.requests[2], [clinic]); respond(h.requests[3], [doctor]);
  await new Promise(setImmediate);
  assert.ok(h.page.data.initialError);
  assert.equal(h.requests.length, 4);
});
