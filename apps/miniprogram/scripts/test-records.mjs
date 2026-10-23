import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createContext, Script } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const record = (id, overrides = {}) => ({ id: `initial:${id}`, resourceId: id, kind: 'initial', appointmentId: '', occurredAt: '2026-10-04T16:30:00Z', title: '独立问诊', history: '既往诊疗情况', summary: '评估', advice: '注意事项', adviceVersion: 'a'.repeat(64), readAt: null, nextVisitAt: null, stageIndex: 0, stageStatus: '', parentConsultationId: '', ...overrides });
function harness(pageName = 'records') {
  let page;
  const calls = [], navigation = [];
  const api = {
    me: async () => ({ id: 'patient-a', displayName: '患者' }),
    timeline: async (number, patientId) => { calls.push({ number, patientId }); return { data: [record(`record-${number}`)], meta: { page: number, pageSize: 1, total: 2 } }; },
    confirmAdviceRead: async (...args) => { calls.push(args); return { adviceVersion: args[2], readAt: '2026-10-05T01:00:00Z' }; },
    planVersions: async () => [{ id: 'plan-a', version: 1, content: { summary: '方案', stages: [], instructions: '方案医嘱' }, publishedAt: '2026-10-04T16:30:00Z' }],
    advice: async () => record('plan-a', { kind: 'plan' }),
  };
  let confirms = true;
  const context = createContext({ Page: definition => { page = definition; page.setData = data => Object.assign(page.data, data); }, wx: { stopPullDownRefresh() {}, navigateTo: value => navigation.push(value.url) } });
  const source = ts.transpileModule(readFileSync(resolve(root, `pages/${pageName}/index.ts`), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  const module = { exports: {} };
  new Script(`(function(require,module,exports){${source}\n})`).runInContext(context)(specifier => {
    if (specifier.endsWith('/services/patient')) return { patientApi: api, confirmDialog: async () => confirms, isSessionExpired: error => error.code === 'SESSION_EXPIRED', confirmNewSession: async () => true };
    if (specifier.endsWith('/services/api')) return { errorMessage: error => error.message };
    throw new Error(specifier);
  }, module, module.exports);
  return { page, api, calls, navigation, cancel: () => { confirms = false; } };
}
const tap = id => ({ currentTarget: { dataset: { id } } });
const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; };

test('loads independent consultations with Beijing time and paginates events for the same patient', async () => {
  const h = harness(); await h.page.load();
  assert.equal(h.page.data.records[0].dateLabel, '2026/10/05 00:30');
  assert.equal(h.page.data.records[0].kindLabel, '问诊');
  assert.equal(h.page.data.records[0].appointmentId, '');
  await h.page.load(true);
  assert.deepEqual(h.calls, [{ number: 1, patientId: 'patient-a' }, { number: 2, patientId: 'patient-a' }]);
  assert.equal(h.page.data.records.length, 2); assert.equal(h.page.data.hasMore, false);
});
test('explicit confirmation saves content version and receipt; repeat taps do not submit again', async () => {
  const h = harness(); await h.page.load();
  await h.page.confirmRead(tap('initial:record-1'));
  assert.deepEqual(h.calls[1], ['initial', 'record-1', 'a'.repeat(64), 'patient-a']);
  assert.equal(h.page.data.records[0].readAt, '2026-10-05T01:00:00Z');
  await h.page.confirmRead(tap('initial:record-1')); assert.equal(h.calls.length, 2);
});
test('cancellation does not acknowledge; failed confirmation remains retryable', async () => {
  const h = harness(); await h.page.load(); h.cancel();
  await h.page.confirmRead(tap('initial:record-1')); assert.equal(h.calls.length, 1); assert.equal(h.page.data.confirmingId, '');
  const failed = harness(); await failed.page.load(); failed.api.confirmAdviceRead = async () => { throw new Error('网络中断'); };
  await failed.page.confirmRead(tap('initial:record-1')); assert.equal(failed.page.data.records[0].readAt, null); assert.equal(failed.page.data.error, '网络中断'); assert.equal(failed.page.data.confirmingId, '');
});
test('stale requests cannot render after unload and account changes cannot append old records', async () => {
  const h = harness(), pending = deferred(); h.api.timeline = () => pending.promise;
  const load = h.page.load(); await Promise.resolve(); h.page.onUnload();
  pending.resolve({ data: [record('old')], meta: { page: 1, pageSize: 1, total: 1 } }); await load;
  assert.equal(h.page.data.records.length, 0);
  const changed = harness(); await changed.page.load(); changed.api.me = async () => ({ id: 'patient-b' }); await changed.page.load(true);
  assert.equal(changed.calls.length, 1); assert.match(changed.page.data.error, /身份已变化/);
  assert.equal(changed.page.data.records.length, 0); assert.equal(changed.page.data.patientName, ''); assert.equal(changed.page.data.hasMore, false);
});
test('expired read request clears visible records and presents login recovery', async () => {
  const h = harness(); await h.page.load(); h.api.confirmAdviceRead = async () => { throw Object.assign(new Error('已过期'), { code: 'SESSION_EXPIRED' }); };
  await h.page.confirmRead(tap('initial:record-1')); assert.equal(h.page.data.expired, true); assert.equal(h.page.data.records.length, 0);
});
test('plan detail reads persisted state and acknowledges the specific published version', async () => {
  const h = harness('care-plan'); h.page.appointmentId = 'appointment-a'; h.page.linkedVersionId = 'plan-a'; await h.page.load();
  assert.equal(h.page.data.versions[0].linked, true);
  h.api.confirmAdviceRead = async (...args) => { h.calls.push(args); h.api.advice = async () => record('plan-a', { kind: 'plan', readAt: '2026-10-05T01:00:00Z' }); return { readAt: '2026-10-05T01:00:00Z' }; };
  await h.page.confirmRead(tap('plan-a')); assert.deepEqual(h.calls[0], ['plan', 'plan-a', 'a'.repeat(64), 'patient-a']); assert.ok(h.page.data.versions[0].readAt);
});
test('navigation selects the published plan version and leaves independent consultations in place', async () => {
  const h = harness(); await h.page.load(); h.page.openDetail(tap('initial:record-1')); assert.equal(h.navigation.length, 0);
  h.page.data.records = [record('plan-a', { id: 'plan:plan-a', kind: 'plan', appointmentId: 'appointment-a' })]; h.page.openDetail(tap('plan:plan-a'));
  assert.equal(h.navigation[0], '/pages/care-plan/index?id=appointment-a&versionId=plan-a');
});

test('revisits preserve history, previous consultation, stage progress and recommended visit time', async () => {
  const h = harness();
  h.api.timeline = async () => ({ data: [record('revisit-a', { kind: 'revisit', parentConsultationId: 'initial-a', stageIndex: 2, stageStatus: 'in_progress', nextVisitAt: '2026-10-10T16:30:00Z' })], meta: { page: 1, pageSize: 20, total: 1 } });
  await h.page.load();
  const item = h.page.data.records[0];
  assert.equal(item.kindLabel, '复诊'); assert.equal(item.history, '既往诊疗情况');
  assert.equal(item.parentConsultationId, 'initial-a');
  assert.equal(item.stageLabel, '第 2 阶段 · 进行中');
  assert.equal(item.nextVisitLabel, '2026/10/11 00:30');
});

test('plan confirmation failure keeps the content visible and retries without reloading', async () => {
  const h = harness('care-plan'); h.page.appointmentId = 'appointment-a'; await h.page.load();
  h.api.confirmAdviceRead = async () => { throw new Error('网络中断'); };
  await h.page.confirmRead(tap('plan-a'));
  assert.equal(h.page.data.error, ''); assert.equal(h.page.data.readError, '网络中断');
  assert.equal(h.page.data.versions.length, 1); assert.equal(h.page.data.versions[0].readAt, null);
  assert.equal(h.page.data.confirmingId, '');
  h.api.planVersions = async () => { throw new Error('确认成功无需再次读取方案'); };
  h.api.confirmAdviceRead = async () => ({ readAt: '2026-10-05T01:00:00Z' });
  await h.page.confirmRead(tap('plan-a'));
  assert.equal(h.page.data.versions[0].readAt, '2026-10-05T01:00:00Z');
  assert.equal(h.page.data.readError, ''); assert.equal(h.page.data.error, '');
});

test('plan cancellation and empty advice never create a receipt', async () => {
  const h = harness('care-plan'); h.page.appointmentId = 'appointment-a'; await h.page.load(); h.cancel();
  await h.page.confirmRead(tap('plan-a')); assert.equal(h.calls.length, 0);
  const empty = harness('care-plan'); empty.page.appointmentId = 'appointment-a';
  empty.api.planVersions = async () => [{ id: 'plan-a', version: 1, content: { summary: '方案', stages: [], instructions: '   ' }, publishedAt: '2026-10-04T16:30:00Z' }];
  empty.api.advice = async () => { throw new Error('没有医嘱无需读取状态'); };
  await empty.page.load(); await empty.page.confirmRead(tap('plan-a'));
  assert.equal(empty.page.data.error, ''); assert.equal(empty.page.data.versions[0].adviceVersion, ''); assert.equal(empty.calls.length, 0);
});

test('plan expiry clears patient content and late confirmation cannot update a refreshed version', async () => {
  const expired = harness('care-plan'); expired.page.appointmentId = 'appointment-a'; await expired.page.load();
  expired.api.confirmAdviceRead = async () => { throw Object.assign(new Error('已过期'), { code: 'SESSION_EXPIRED' }); };
  await expired.page.confirmRead(tap('plan-a'));
  assert.equal(expired.page.data.expired, true); assert.equal(expired.page.data.versions.length, 0);
  const h = harness('care-plan'), pending = deferred(); h.page.appointmentId = 'appointment-a'; await h.page.load();
  h.api.confirmAdviceRead = () => pending.promise;
  const confirming = h.page.confirmRead(tap('plan-a')); await Promise.resolve();
  await h.page.load(); pending.resolve({ readAt: '2026-10-05T01:00:00Z' }); await confirming;
  assert.equal(h.page.data.versions[0].readAt, null);
  h.page.onUnload(); h.api.planVersions = async () => { throw new Error('已卸载页面不能重新请求'); };
  await h.page.load(); assert.equal(h.page.data.error, '');
});
