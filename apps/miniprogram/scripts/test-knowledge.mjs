import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Script, createContext } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const article = { id: 'article-1', title: '<script>plain text</script>', categoryId: 'cat-1', body: '正文', reviewedAt: '2026-10-01T23:00:00Z', publishedAt: '2026-10-01T23:00:00Z' };
function harness(file, overrides = {}) {
 let page; const calls = [], navigations = [];
 const session = { patient: { id: 'patient-1' } };
 const service = { article: async () => article, related: async () => [ { ...article, id: 'article-2' } ], categories: async () => [], articles: async (...args) => { calls.push(args); return { data: [article], meta: { total: 1 } }; }, ...overrides };
 const patient = { cachedSession: () => session, patientApi: { knowledgeFavorite: async () => false, setKnowledgeFavorite: async (...args) => { calls.push(args); return args[1]; }, knowledgeFavorites: async (...args) => { calls.push(args); return { data: [article], meta: { total: 1 } }; }, ...overrides.patient } };
 const context = createContext({ Page(def) { page = def; page.setData = data => Object.assign(page.data, data); }, wx: { stopPullDownRefresh() {}, showToast() {}, redirectTo: ({url}) => navigations.push(url), navigateTo: ({url}) => navigations.push(url) } });
 const source = ts.transpileModule(readFileSync(resolve(root, file), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
 const deps = { '../../services/api': { errorMessage: e => e.message, api: { clinics: async () => [{ id: 'clinic-1' }] } }, '../../services/knowledge': { knowledge: service }, '../../services/patient': patient, '../../services/presentation': { clinicPresentation: () => ({ isDemo: false }) } };
 new Script(`(function(require,exports){${source}\n})`).runInContext(context)(id => deps[id], {});
 return { page, calls, navigations };
}
test('search submits trimmed keyword with pagination and preserves it on return', async () => {
 const h = harness('pages/knowledge/index.ts'); await h.page.initialize();
 h.page.input({ detail: { value: '  皮肤  ' } }); h.page.search();
 await new Promise(resolve => setImmediate(resolve));
 assert.equal(h.calls.at(-1)[5], '皮肤');
 assert.equal(h.page.data.page, 1);
 h.page.onHide(); await h.page.initialize(); assert.equal(h.calls.at(-1)[5], '皮肤');
});
test('favorites query is scoped to the current patient and filters', async () => {
 const h = harness('pages/knowledge/index.ts'); h.page.data.favorites = true; h.page.data.keyword = '准备';
 await h.page.initialize(); assert.equal(h.calls[0][0], 'patient-1'); assert.equal(h.calls[0][1].q, '准备');
});
test('detail supports favorite toggle and related navigation', async () => {
 const h = harness('pages/knowledge-detail/index.ts'); h.page.id = article.id; await h.page.load();
 assert.equal(h.page.data.reviewedDate, '2026-10-02');
 await h.page.toggleFavorite(); assert.equal(h.page.data.saved, true); assert.deepEqual(h.calls[0], ['article-1', true, 'patient-1']);
 await h.page.toggleFavorite(); assert.equal(h.page.data.saved, false);
 h.page.openRelated({ currentTarget: { dataset: { id: 'article-2' } } }); assert.match(h.navigations[0], /article-2/);
});
test('recommendation errors preserve the article; withdrawal clears it', async () => {
 let available = true;
 const h = harness('pages/knowledge-detail/index.ts', { article: async () => { if (!available) throw new Error('下架'); return article; }, related: async () => { throw new Error('推荐暂不可用'); } });
 h.page.id = article.id; await h.page.load(); assert.equal(h.page.data.article.id, article.id); assert.equal(h.page.data.relatedError, '推荐暂不可用');
 available = false; await h.page.load(); assert.equal(h.page.data.article, null); assert.equal(h.page.data.related.length, 0);
});
test('responses arriving after hide cannot restore article content', async () => {
 let finish;
 const h = harness('pages/knowledge-detail/index.ts', { article: () => new Promise(resolve => { finish = resolve; }) });
 h.page.id = article.id; const pending = h.page.load(); h.page.onHide(); finish(article); await pending;
 assert.equal(h.page.data.article, null);
});
test('pull to refresh does not interrupt a pending favorite mutation', async () => {
 let finish;
 const h = harness('pages/knowledge-detail/index.ts', { patient: { setKnowledgeFavorite: () => new Promise(resolve => { finish = resolve; }) } });
 h.page.id = article.id; await h.page.load();
 const pending = h.page.toggleFavorite(); h.page.onPullDownRefresh();
 finish(true); await pending;
 assert.equal(h.page.data.saved, true); assert.equal(h.page.data.saving, false);
});
