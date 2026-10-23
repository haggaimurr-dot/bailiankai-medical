import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Script, createContext } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const record = () => ({ id: 'case-1', version: 4, revision: 1, consentRevision: 1, policyVersion: 'case-display-v1', policyContent: '独立展示授权正文', content: { title: '案例', body: '全文', summary: '', pairs: [{ beforeImageId: 'before-1', afterImageId: 'after-1', angle: 'front' }] } });
function harness({ mine = true, imageFailure = false, modal = async () => true } = {}) {
 let page; const removed = [], saved = [], timers = [], queries = [];
 let current = record();
 const service = {
  me: async () => ({ id: 'patient-1' }),
  cases: async (page, patient, filters) => { queries.push({ page, patient, filters }); return { data: [current], meta: { total: 40 } }; },
  caseDetail: async () => structuredClone(current),
  casePhotoFile: async (_, imageId) => { if (imageFailure) throw new Error('storage unavailable'); return '/tmp/' + imageId; },
  caseConsent: async (id, body, patient) => { saved.push({ id, body, patient }); return current; },
 };
 const context = createContext({
  Page(definition) { page = definition; page.setData = data => Object.assign(page.data, data); },
  setTimeout(fn) { timers.push(fn); return timers.length; }, clearTimeout() {},
  wx: { getFileSystemManager: () => ({ unlink: ({ filePath }) => removed.push(filePath) }), showToast() {} },
 });
 const source = ts.transpileModule(readFileSync(resolve(root, 'pages/cases/index.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
 const deps = { '../../config/api': { API_BASE_URL: 'http://localhost/api/v1' }, '../../services/api': { errorMessage: e => e.message, request: async () => { if (!current) throw new Error('withdrawn'); return structuredClone(current); }, requestEnvelope: async () => ({ data: current ? [current] : [], meta: { total: current ? 1 : 0 } }) }, '../../services/patient': { patientApi: service, confirmDialog: modal } };
 new Script(`(function(require,exports){${source}\n})`).runInContext(context)(id => deps[id], {});
 page.data.mine = mine; page.visible = true; page.patientId = 'patient-1';
 page.download = service.casePhotoFile;
 return { page, saved, removed, timers, queries, revoke() { current = null; } };
}
test('photo storage failure blocks grants but patient can still revoke', async () => {
 const h = harness({ imageFailure: true }); await h.page.detail('case-1');
 assert.equal(h.page.data.imagesReady, false); assert.equal(h.page.data.selected.id, 'case-1');
 await h.page.consent({ currentTarget: { dataset: { granted: 'true' } } }); assert.equal(h.saved.length, 0);
 await h.page.consent({ currentTarget: { dataset: { granted: 'false' } } });
 assert.equal(h.saved.length, 1); assert.equal(h.saved[0].body.granted, false);
});
test('authorization uses the exact preview version and never submits after hide', async () => {
 let finish; const h = harness({ modal: () => new Promise(resolve => { finish = resolve; }) });
 await h.page.detail('case-1'); assert.equal(h.page.data.imagesReady, false);
 for (const id of ['before-1', 'after-1']) h.page.imageLoaded({ currentTarget: { dataset: { id } } });
 assert.equal(h.page.data.imagesReady, true);
 const pending = h.page.consent({ currentTarget: { dataset: { granted: 'true' } } });
 h.page.onHide(); finish(true); await pending;
 assert.equal(h.saved.length, 0); assert.equal(h.page.data.selected, null); assert.equal(h.removed.length, 2);
 const next = harness(); await next.page.detail('case-1');
 for (const id of ['before-1', 'after-1']) next.page.imageLoaded({ currentTarget: { dataset: { id } } });
 await next.page.consent({ currentTarget: { dataset: { granted: 'true' } } });
 assert.equal(next.saved[0].body.version, 4); assert.equal(next.saved[0].patient, 'patient-1');
});
test('public detail and local image files clear when renewal finds a withdrawal', async () => {
 const h = harness({ mine: false }); await h.page.detail('case-1');
 assert.equal(h.page.data.selected.id, 'case-1'); assert.equal(h.page.files.length, 2);
 h.revoke(); await h.page.detail('case-1');
 assert.equal(h.page.data.selected, null); assert.equal(h.page.data.pairs.length, 0); assert.equal(h.page.data.rows.length, 0); assert.equal(h.removed.length, 2);
});

test('category and tag filters reset pagination and keep drafts out of subsequent pages', async () => {
 const h = harness(); h.page.setData({ category: ' 瘢痕修复 ', tag: ' 正面 ', page: 3 });
 await h.page.search();
 assert.equal(h.queries[0].page, 1);
 assert.deepEqual(JSON.parse(JSON.stringify(h.queries[0].filters)), { category: '瘢痕修复', tag: '正面' });
 h.page.filterInput({ currentTarget: { dataset: { field: 'category' } }, detail: { value: '未提交筛选' } });
 await h.page.more();
 assert.equal(h.queries[1].page, 2); assert.equal(h.queries[1].filters.category, '瘢痕修复');
});
test('comparison waits for complete image display and clears on hiding', async () => {
 const h = harness(); await h.page.detail('case-1');
 h.page.compare({ currentTarget: { dataset: { index: 0 } }, detail: { value: 80 } });
 assert.equal(h.page.data.pairs[0].position, 50);
 for (const id of ['before-1', 'after-1']) h.page.imageLoaded({ currentTarget: { dataset: { id } } });
 h.page.compare({ currentTarget: { dataset: { index: 0 } }, detail: { value: 80 } });
 assert.equal(h.page.data.pairs[0].position, 80);
 h.page.imageFailed(); assert.equal(h.page.data.imagesReady, false);
 h.page.onHide(); assert.equal(h.page.data.pairs.length, 0); assert.equal(h.removed.length, 2);
});

test('unfiltered lists omit empty query values rejected by the API', async () => {
 const h = harness(); await h.page.load();
 assert.deepEqual(JSON.parse(JSON.stringify(h.queries[0].filters)), {});
});
