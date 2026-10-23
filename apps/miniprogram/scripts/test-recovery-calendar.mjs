import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createContext, Script } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const task = (id, overrides = {}) => ({ id, appointmentId: 'appointment-a', title: '恢复随访', dueAt: '2026-09-30T16:30:00Z',
  status: 'pending', assigneeName: '演示医护', symptoms: '', response: '', disposition: '', submittedAt: null, reviewedAt: null,
  overdue: true, photos: [], ...overrides });
function harness() {
  let page;
  const navigation = [], calls = [];
  const api = { me: async () => ({ id: 'patient-a' }), followups: async (_status, number, patientId, dates) => {
    calls.push({ number, patientId, dates: { ...dates } });
    return { data: [task(`task-${number}`)], meta: { page: number, pageSize: 1, total: 2 } };
  }};
  const context = createContext({ Page: definition => { page = definition; page.setData = data => Object.assign(page.data, data); },
    wx: { stopPullDownRefresh() {}, navigateTo: value => navigation.push(value.url) } });
  const cache = new Map();
  function load(filename) {
    if (cache.has(filename)) return cache.get(filename).exports;
    const module = { exports: {} }; cache.set(filename, module);
    const source = ts.transpileModule(readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
    new Script(`(function(require,module,exports){${source}\n})`).runInContext(context)(specifier => {
      if (specifier.endsWith('/services/patient')) return { patientApi: api, requirePatientIdentity() {}, isSessionExpired: error => error.code === 'SESSION_EXPIRED', confirmNewSession: async () => true };
      if (specifier.endsWith('/services/api')) return { errorMessage: error => error.message };
      return load(resolve(dirname(filename), `${specifier}.ts`));
    }, module, module.exports);
    return module.exports;
  }
  const utils = load(resolve(root, 'utils/recovery-calendar.ts'));
  load(resolve(root, 'pages/recovery-calendar/index.ts'));
  page.render('2026-10', '2026-10-01');
  return { page, utils, api, calls, navigation };
}
test('events use Beijing dates for due, submission and review, preserving response and disposition', () => {
  const { utils } = harness();
  const events = utils.calendarEvents([task('reviewed', { status: 'handled', submittedAt: '2026-10-01T16:00:00Z',
    reviewedAt: '2026-10-02T16:00:00Z', symptoms: '演示反馈', response: '演示回评', disposition: '演示处理' })]);
  assert.deepEqual(Array.from(events, item => item.date), ['2026-10-01', '2026-10-02', '2026-10-03']);
  assert.equal(events[0].pending, false);
  assert.equal(events[1].content, '演示反馈');
  assert.equal(events[2].content, '演示回评');
  assert.equal(events[2].disposition, '演示处理');
});
test('month grid handles leap days, adjacent months and year boundaries', () => {
  const { utils } = harness();
  const events = utils.calendarEvents([task('pending')]);
  const days = utils.calendarDays('2026-10', '2026-10-01', events, '2026-10-01');
  assert.equal(days.length, 42);
  assert.equal(days[0].date, '2026-09-28');
  assert.ok(days.find(day => day.selected && day.today && day.hasPending && day.hasTask));
  assert.equal(utils.calendarDays('2028-02', '', [], '').filter(day => day.inMonth).length, 29);
  assert.equal(utils.shiftMonth('2026-12', 1), '2027-01');
  assert.equal(utils.shiftMonth('2026-01', -1), '2025-12');
});
test('all pages are loaded for the same identity and links target the selected task', async () => {
  const h = harness(); await h.page.load();
  assert.deepEqual(h.calls, [{ number: 1, patientId: 'patient-a', dates: { dateFrom: '2026-09-28', dateTo: '2026-11-08' } }, { number: 2, patientId: 'patient-a', dates: { dateFrom: '2026-09-28', dateTo: '2026-11-08' } },
    { number: 1, patientId: 'patient-a', dates: {} }, { number: 2, patientId: 'patient-a', dates: {} }]);
  assert.equal(h.page.data.pendingCount, 2);
  assert.equal(h.page.data.selectedEvents.length, 2);
  h.page.openTask({ currentTarget: { dataset: { id: 'task-2-task' } } });
  assert.equal(h.navigation[0], '/pages/followups/index?id=appointment-a&taskId=task-2');
  h.api.followups = async () => ({ data: [task('task-2', { status: 'submitted', submittedAt: '2026-10-01T17:00:00Z' })], meta: { page: 1, pageSize: 20, total: 1 } });
  await h.page.load();
  assert.equal(h.page.data.pendingCount, 0);
  assert.equal(h.page.data.events.length, 2);
});
test('pagination failure clears old content and supports retry', async () => {
  const h = harness(); await h.page.load();
  const original = h.api.followups;
  h.api.followups = async (...args) => { if (args[1] === 2) throw new Error('网络异常'); return original(...args); };
  await h.page.load();
  assert.equal(h.page.data.error, '网络异常');
  assert.equal(h.page.data.events.length, 0);
  h.api.followups = original; await h.page.load();
  assert.equal(h.page.data.taskCount, 2);
});
test('expired sessions and unloaded pages never publish task data', async () => {
  const h = harness();
  h.api.me = async () => { throw Object.assign(new Error('身份过期'), { code: 'SESSION_EXPIRED' }); };
  await h.page.load(); assert.equal(h.page.data.expired, true);
  const stale = harness(); let respond;
  stale.api.followups = () => new Promise(resolve => { respond = resolve; });
  const pending = stale.page.load(); await new Promise(resolve => setImmediate(resolve));
  stale.page.onUnload();
  respond({ data: [task('late')], meta: { page: 1, pageSize: 20, total: 1 } });
  await pending; assert.equal(stale.page.data.events.length, 0);
});

test('month changes fetch a new date range and discard responses from the previous month', async () => {
  const h = harness(); let oldResolve;
  h.api.followups = () => new Promise(resolve => { oldResolve = resolve; });
  const oldLoad = h.page.load(); await new Promise(resolve => setImmediate(resolve));
  h.api.followups = async (_status, number, patientId, dates) => {
    h.calls.push({ number, patientId, dates: { ...dates } });
    return { data: [task('new-month', { dueAt: '2026-11-20T08:00:00Z' }), task('outside', { dueAt: '2026-01-01T08:00:00Z', status: 'reviewed', reviewedAt: '2026-11-20T09:00:00Z' })], meta: { page: 1, pageSize: 20, total: 2 } };
  };
  h.page.nextMonth(); await new Promise(resolve => setImmediate(resolve));
  assert.equal(h.page.data.month, '2026-11');
  assert.deepEqual(h.calls[0].dates, { dateFrom: '2026-10-26', dateTo: '2026-12-06' });
  assert.equal(h.page.data.events.length, 2);
  assert.equal(h.page.data.events.find(event => event.taskId === 'outside').kind, 'review');
  oldResolve({ data: [task('stale')], meta: { page: 1, pageSize: 20, total: 1 } }); await oldLoad;
  assert.equal(h.page.data.events.some(event => event.taskId === 'stale'), false);
});

test('all-date pending tasks include months outside the grid and navigate directly', async () => {
  const h = harness();
  h.api.followups = async (_status, _page, _identity, dates) => ({ data: dates ? [task('in-range')] : [task('in-range'),
    task('past', { dueAt: '2026-01-02T08:00:00Z' }), task('future', { dueAt: '2027-01-02T08:00:00Z', overdue: false }),
    task('done', { status: 'reviewed' })], meta: { page: 1, pageSize: 20, total: dates ? 1 : 4 } });
  await h.page.load();
  assert.equal(h.page.data.pendingCount, 1);
  assert.equal(h.page.data.visiblePendingEvents.length, 1);
  assert.equal(h.page.data.allPendingEvents.length, 3);
  assert.equal(h.page.data.rangeLabel, '2026-09-28 至 2026-11-08');
  h.page.changePendingScope({ currentTarget: { dataset: { scope: 'all' } } });
  assert.deepEqual(Array.from(h.page.data.visiblePendingEvents, item => item.taskId), ['past', 'in-range', 'future']);
  assert.equal(h.page.data.visiblePendingEvents[0].crossMonth, true);
  h.page.openTask({ currentTarget: { dataset: { id: 'past-task' } } });
  assert.equal(h.navigation[0], '/pages/followups/index?id=appointment-a&taskId=past');
  h.page.changePendingScope({ currentTarget: { dataset: { scope: 'range' } } });
  assert.equal(h.page.data.visiblePendingEvents.length, 1);
});

test('recovery comparisons use actual submitted dates across months within one appointment', async () => {
  const h = harness();
  h.api.followups = async (_status, _page, _identity, dates) => ({ data: dates ? [] : [
    task('late', { status: 'reviewed', submittedAt: '2026-10-02T16:00:00Z', severity: 1, response: '回评', symptoms: '后续反馈' }),
    task('early', { status: 'handled', submittedAt: '2026-08-01T16:00:00Z', severity: 3, concern: true, disposition: '已处理' }),
    task('other', { appointmentId: 'appointment-b', status: 'submitted', submittedAt: '2026-09-01T08:00:00Z', severity: 0 }),
    task('pending', { severity: 0 })], meta: { page: 1, pageSize: 20, total: dates ? 0 : 4 } });
  await h.page.load();
  assert.equal(h.page.data.series.length, 2);
  assert.equal(h.page.data.before.id, 'early');
  assert.equal(h.page.data.before.date, '2026-08-02 00:00');
  assert.equal(h.page.data.after.id, 'late');
  assert.equal(h.page.data.after.response, '回评');
  assert.equal(h.page.data.trendLabel, '自述不适程度降低');
  h.page.data.series[0].points[1].severity = 3;
  h.page.renderComparison(0);
  assert.equal(h.page.data.trendLabel, '自述不适程度持平');
  h.page.data.series[0].points[0].severity = 0;
  h.page.renderComparison(0);
  assert.equal(h.page.data.trendLabel, '自述不适程度升高');
  h.page.changePoint({ detail: { value: '1' }, currentTarget: { dataset: { side: 'before' } } });
  assert.equal(h.page.data.trendLabel, '至少需要两次已提交反馈才能对照');
  h.page.renderComparison(0, 1, 0);
  assert.equal(h.page.data.trendLabel, '请选择早于对照反馈的基准反馈');
  h.page.changeSeries({ detail: { value: '1' } });
  assert.equal(h.page.data.before.appointmentId, 'appointment-b');
  assert.equal(h.page.data.trendLabel, '至少需要两次已提交反馈才能对照');
});

test('history pagination failure and late history responses cannot expose partial records', async () => {
  const h = harness();
  h.api.followups = async (_status, page, _identity, dates) => {
    if (!dates && page === 2) throw new Error('历史记录读取失败');
    return { data: [task('partial')], meta: { page, pageSize: 1, total: dates ? 1 : 2 } };
  };
  await h.page.load();
  assert.equal(h.page.data.error, '历史记录读取失败');
  assert.equal(h.page.data.allPendingEvents.length, 0);
  assert.equal(h.page.data.events.length, 0);
  let respond;
  h.api.followups = async (_status, _page, _identity, dates) => dates ? { data: [], meta: { page: 1, pageSize: 20, total: 0 } }
    : new Promise(resolve => { respond = resolve; });
  const load = h.page.load(); await new Promise(resolve => setImmediate(resolve));
  h.page.onUnload(); respond({ data: [task('late')], meta: { page: 1, pageSize: 20, total: 1 } });
  await load;
  assert.equal(h.page.data.allPendingEvents.length, 0);
});
