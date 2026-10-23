import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Script, createContext } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const appointment = { id: 'appointment-one', patientId: 'patient-one', slotId: 'slot-old', doctorId: 'doctor-one', clinicId: 'clinic-one', status: 'confirmed', startsAt: '2099-01-01T10:00:00+08:00', endsAt: '2099-01-01T10:30:00+08:00', confirmationDeadline: '2099-01-01T09:00:00+08:00', createdAt: '2026-10-07T00:00:00Z', version: 3, canReschedule: true, canCancel: true };
const target = { id: 'slot-new', startsAt: '2099-01-02T10:00:00+08:00', endsAt: '2099-01-02T10:30:00+08:00', capacity: 1, reserved: 0, status: 'open', available: true, label: '目标时间' };

function harness(name, overrides = {}) {
  let page;
  const storage = new Map(), navigation = [], calls = [];
  const patientApi = {
    appointment: async () => ({ ...appointment }),
    appointmentTimeline: async () => [{ id: 'event-one', action: 'reschedule', toStatus: 'pending', detail: '原时间 → 新时间', createdAt: '2026-10-07T00:00:00Z' }],
    reschedule: async (...args) => { calls.push(args); return { ...appointment, slotId: target.id, status: 'pending' }; },
    ...overrides,
  };
  const apiModule = {};
  const context = createContext({
    Date, Uint8Array, Error,
    Page(definition) { page = definition; page.setData = data => Object.assign(page.data, data); },
    wx: { getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key), getRandomValues: o => o.success({ randomValues: new Uint8Array(16).buffer }), stopPullDownRefresh() {}, redirectTo: o => navigation.push(o.url), navigateTo: o => navigation.push(o.url) },
  });
  const cache = new Map();
  function load(file) {
    if (file.replaceAll('\\', '/').endsWith('services/patient.ts')) return { patientApi, ensureSession: async () => ({ patient: { id: 'patient-one' } }), requirePatientIdentity: () => {}, confirmDialog: async () => true, isSessionExpired: e => e.status === 401 };
    if (cache.has(file)) return cache.get(file).exports;
    const module = { exports: {} }; cache.set(file, module);
    const source = ts.transpileModule(readFileSync(file, 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
    new Script(`(function(require,module,exports){${source}\n})`, { filename: file }).runInContext(context)(specifier => load(resolve(dirname(file), `${specifier}.ts`)), module, module.exports);
    if (file.replaceAll('\\', '/').endsWith('services/api.ts')) {
      Object.assign(apiModule, module.exports);
      module.exports.api.slots = async () => [{ ...target }];
    }
    return module.exports;
  }
  load(resolve(root, `pages/${name}/index.ts`));
  page.id = appointment.id;
  return { page, storage, navigation, calls, patientApi, apiModule };
}

test('reschedule atomically submits original version and redirects to the same appointment', async () => {
  const h = harness('reschedule'); await h.page.load();
  h.page.selectSlot({ currentTarget: { dataset: { id: target.id } } });
  await h.page.submit();
  assert.equal(h.calls.length, 1); assert.equal(h.calls[0][1].version, 3);
  assert.equal(h.calls[0][1].slotId, target.id); assert.equal(h.calls[0][3], 'patient-one');
  assert.equal(h.storage.size, 0); assert.match(h.navigation[0], /appointment-detail\/index\?id=appointment-one/);
});

test('unknown mutation result locks selection and retries exactly the saved request after reopening', async () => {
  const h = harness('reschedule', { reschedule: async (...args) => { h.calls.push(args); throw new Error('network timeout'); } });
  await h.page.load(); h.page.selectSlot({ currentTarget: { dataset: { id: target.id } } }); await h.page.submit();
  const draft = h.storage.get('booking-change:appointment-one');
  assert.equal(h.page.data.recovering, true);
  h.page.selectSlot({ currentTarget: { dataset: { id: 'other-slot' } } });
  assert.equal(h.page.data.selected, target.id);
  await h.page.load(); await h.page.submit();
  assert.equal(h.calls[1][2], draft.key); assert.equal(h.calls[1][1].version, draft.body.version);
  assert.equal(h.calls[1][1].slotId, draft.body.slotId); assert.equal(h.navigation.length, 0);
});

test('definite capacity failure clears recovery and preserves the displayed original appointment', async () => {
  const h = harness('reschedule', { reschedule: async () => { throw new h.apiModule.ApiError('已满', 409, 'SLOT_FULL'); } });
  await h.page.load(); h.page.selectSlot({ currentTarget: { dataset: { id: target.id } } }); await h.page.submit();
  assert.equal(h.storage.size, 0); assert.equal(h.page.data.appointment.slotId, appointment.slotId);
  assert.equal(h.page.data.notice, '已满'); assert.equal(h.page.data.recovering, false);
});

test('identity mismatch cannot restore a mutation from a different patient', async () => {
  const h = harness('reschedule');
  h.storage.set('booking-change:appointment-one', { patientId: 'someone-else', key: 'old-request-key', body: { slotId: target.id, version: 3 } });
  await h.page.load(); assert.match(h.page.data.error, /身份已变化/); assert.equal(h.calls.length, 0);
});

test('waiting detail can select its original slot when a vacancy becomes available', async () => {
  const h = harness('reschedule', { appointment: async () => ({ ...appointment, status: 'waiting', slotId: target.id }) });
  await h.page.load(); assert.equal(h.page.data.slots.length, 1);
  h.page.selectSlot({ currentTarget: { dataset: { id: target.id } } }); await h.page.submit();
  assert.equal(h.calls[0][1].slotId, target.id);
});

test('unloaded reschedule ignores late slot responses', async () => {
  const h = harness('reschedule'); await h.page.load();
  let respond; h.apiModule.api.slots = () => new Promise(resolve => { respond = resolve; });
  const pending = h.page.loadSlots(); h.page.onUnload(); respond([target]); await pending;
  assert.equal(h.page.data.slots.length, 0);
});

test('detail renders real timeline events in Beijing time', async () => {
  const h = harness('appointment-detail'); await h.page.load();
  assert.equal(h.page.data.timeline[0].label, '改期申请');
  assert.equal(h.page.data.timeline[0].timeLabel, '2026-10-07 08:00');
  assert.equal(h.page.data.timeline[0].detail, '原时间 → 新时间');
  h.page.openReschedule(); assert.match(h.navigation[0], /reschedule\/index/);
});

test('timeline failure retains appointment and can independently retry', async () => {
  const h = harness('appointment-detail', { appointmentTimeline: async () => { throw new Error('history unavailable'); } });
  await h.page.load(); assert.equal(h.page.data.appointment.id, appointment.id); assert.equal(h.page.data.error, '');
  assert.equal(h.page.data.timelineError, 'history unavailable');
  h.patientApi.appointmentTimeline = async () => []; await h.page.loadTimeline(); assert.equal(h.page.data.timelineError, '');
});

test('schedule offers waitlist only for future open full positive-capacity slots', async () => {
  const h = harness('schedule');
  h.page.allDoctors = [{ id: 'doctor-one', name: '医生' }];
  h.page.setData({ clinic: { id: 'clinic-one' }, date: '2099-01-02' });
  h.apiModule.api.slots = async () => [
    { ...target, id: 'full', reserved: 1 }, { ...target, id: 'closed', status: 'closed', reserved: 1 },
    { ...target, id: 'zero', capacity: 0 }, { ...target, id: 'past', startsAt: '2020-01-01T00:00:00Z', reserved: 1 },
  ];
  await h.page.loadSlots();
  assert.deepEqual(Array.from(h.page.data.slots.filter(s => s.waitlistAvailable), s => s.id), ['full']);
  h.page.bookSlot({ currentTarget: { dataset: { id: 'full' } } }); assert.match(h.navigation[0], /waitlist=1/);
});
