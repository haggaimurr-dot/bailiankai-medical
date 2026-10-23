import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createContext, Script } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const clinic = { id: 'clinic-demo', name: '演示院区', address: '示例地址', phone: '待配置' };
const doctor = { id: 'doctor-demo-1', clinicId: clinic.id, name: '演示医生', title: '演示资料', summary: '简介', initials: '医', focus: [] };
const article = { id: 'published-1', categoryId: 'category-1', title: '已发布文章', summary: '摘要', body: '<script>plain text</script>', reviewerName: '审核医生', reviewedAt: '2026-10-01T20:00:00Z', publishedAt: '2026-10-02T01:00:00Z', sortOrder: 0 };
const category = { id: 'category-1', clinicId: clinic.id, name: '科普分类', sortOrder: 0, active: true, version: 1 };

function harness(name) {
  let page;
  const requests = [], navigation = [], locations = [], previews = [], modals = [];
  const context = createContext({ Page(definition) { page = definition; page.setData = data => Object.assign(page.data, data); },
    wx: { request: options => requests.push(options), navigateTo: options => navigation.push(options.url), stopPullDownRefresh() {},
      openLocation: options => locations.push(options), showModal: options => modals.push(options), previewImage: options => previews.push(options), showToast() {}, getStorageSync() {}, setStorageSync() {}, removeStorageSync() {},
    },
  });
  const cache = new Map();
  function load(filename) {
    if (cache.has(filename)) return cache.get(filename).exports;
    const module = { exports: {} }; cache.set(filename, module);
    const source = ts.transpileModule(readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
    new Script(`(function(require,module,exports){${source}\n})`).runInContext(context)(specifier => load(resolve(dirname(filename), `${specifier}.ts`)), module, module.exports);
    return module.exports;
  }
  if (name) load(resolve(root, `pages/${name}/index.ts`));
  return { get page() { return page; }, requests, navigation, locations, previews, modals, load: name => load(resolve(root, name)) };
}
const tick = () => new Promise(setImmediate);
function respond(request, data, total) { request.success({ statusCode: 200, data: { data, ...(total === undefined ? {} : { meta: { page: 1, pageSize: 20, total } }) } }); }
const touch = dataset => ({ currentTarget: { dataset } });

test('home loads doctors independently from failed knowledge and preserves navigation identities', async () => {
  const h = harness('home'); const pending = h.page.load(); respond(h.requests[0], [clinic]); await tick();
  respond(h.requests.find(row => row.url.endsWith('/doctors')), [doctor]);
  h.requests.find(row => row.url.endsWith('/knowledge/articles')).fail(); await pending;
  assert.equal(h.page.data.doctors.length, 1); assert.equal(h.page.data.error, ''); assert.ok(h.page.data.knowledgeError);
  h.page.openClinic(); assert.equal(h.navigation[0], '/pages/clinic/index?clinicId=clinic-demo');
});

test('home ignores prior clinic responses after a switch', async () => {
  const h = harness('home'); const first = h.page.load(); respond(h.requests[0], [clinic]); await tick();
  const nextClinic = { ...clinic, id: 'other' }; const second = h.page.load(nextClinic.id); respond(h.requests[3], [clinic, nextClinic]); await tick();
  respond(h.requests[4], []); respond(h.requests[5], [article], 1); await second;
  respond(h.requests[1], [doctor]); respond(h.requests[2], [], 0); await first;
  assert.equal(h.page.data.clinic.id, nextClinic.id); assert.equal(h.page.data.doctors.length, 0); assert.equal(h.page.data.articles[0].id, article.id);
});

test('mock only fills successful empty responses and never replaces published articles', async () => {
  const h = harness(); const knowledge = h.load('services/knowledge.ts').knowledge;
  const mock = knowledge.articles(clinic.id); respond(h.requests[0], [], 0); assert.match((await mock).data[0].title, /演示/);
  const real = knowledge.articles(clinic.id); respond(h.requests[1], [article], 1); assert.equal((await real).data[0].id, article.id);
  const formal = knowledge.articles(clinic.id, '', 1, 20, false); respond(h.requests[2], [], 0); assert.equal((await formal).data.length, 0);
  const failed = knowledge.articles(clinic.id); h.requests[3].fail(); await assert.rejects(failed);
});

test('clinic mock map requires an explicit acknowledgement and does not call a mock number', async () => {
  const h = harness('clinic'); h.page.clinicId = clinic.id;
  const pending = h.page.load(); respond(h.requests[0], [clinic]); await pending;
  assert.equal(h.page.data.hasLocation, true); assert.equal(h.page.data.canCall, false);
  h.page.openLocation(); assert.equal(h.locations.length, 0); assert.equal(h.modals.length, 1);
  h.modals[0].success({ confirm: true }); assert.equal(h.locations[0].latitude, 31.2304); assert.match(h.locations[0].name, /演示/);
});

test('unknown clinic has no map fallback; invalid coordinates never navigate', async () => {
  const h = harness('clinic'); h.page.clinicId = 'other';
  const pending = h.page.load(); respond(h.requests[0], [{ ...clinic, id: 'other' }]); await pending;
  assert.equal(h.page.data.hasLocation, false); h.page.openLocation(); assert.equal(h.locations.length, 0);
  const presentation = h.load('services/presentation.ts');
  assert.equal(presentation.validLocation({ latitude: 91, longitude: 121 }), false);
  assert.equal(presentation.validLocation({ latitude: 31, longitude: NaN }), false);
  assert.equal(presentation.validLocation({ latitude: 0, longitude: 0 }), true);
});

test('published profile overrides mock material and credentials preview only public images', async () => {
  const h = harness('clinic'); h.page.clinicId = clinic.id;
  const presentation = { ...h.load('services/presentation.ts').clinicPresentation(clinic), isDemo: false, brandName: '正式品牌', location: { latitude: 30, longitude: 120 }, credentials: [{ name: '执业证照', number: '001', issuer: '机构', imageUrl: 'https://example.com/license.jpg' }] };
  const pending = h.page.load(); respond(h.requests[0], [{ ...clinic, phone: '021-12345678', presentation }]); await pending;
  assert.equal(h.page.data.canCall, true); h.page.openLocation(); assert.equal(h.modals.length, 0); assert.equal(h.locations[0].latitude, 30);
  h.page.preview(touch({ index: 0 })); assert.equal(h.previews[0].current, presentation.credentials[0].imageUrl);
});

test('knowledge list filters categories, paginates, and links to the public detail', async () => {
  const h = harness('knowledge'); h.page.requestedClinicId = 'other'; const pending = h.page.initialize();
  respond(h.requests[0], [{ ...clinic, id: 'other' }]); await tick(); respond(h.requests[1], [category]); respond(h.requests[2], [article], 2); await pending;
  h.page.open(touch({ id: article.id })); assert.equal(h.navigation[0], '/pages/knowledge-detail/index?id=published-1');
  const more = h.page.load(true); assert.equal(h.requests[3].data.page, 2); respond(h.requests[3], [{ ...article, id: 'published-2' }], 2); await more;
  assert.equal(h.page.data.rows.length, 2);
  h.page.changeCategory(touch({ id: category.id })); assert.equal(h.requests[5].data.categoryId, category.id); respond(h.requests[4], [category]); respond(h.requests[5], [], 0); await tick();
  assert.equal(h.page.data.rows.length, 0);
});

test('detail renders text and Shanghai dates, and clears an article when withdrawn', { timeout: 10_000 }, async () => {
  const h = harness('knowledge-detail'); h.page.id = article.id;
  const pending = h.page.load(); respond(h.requests[0], article); await tick(); respond(h.requests[1], []); await pending;
  assert.equal(h.page.data.article.body, article.body); assert.equal(h.page.data.reviewedDate, '2026-10-02');
  const failed = h.page.load(); h.requests[2].success({ statusCode: 404, data: { error: { message: '内容已下架' } } }); await failed;
  assert.equal(h.page.data.article, null); assert.match(h.page.data.error, /下架/);
  const markup = readFileSync(resolve(root, 'pages/knowledge-detail/index.wxml'), 'utf8'); assert.ok(!markup.includes('<rich-text'));
});

test('unloaded clinic ignores pending response and cannot navigate', async () => {
  const h = harness('clinic'); h.page.clinicId = clinic.id; const pending = h.page.load(); h.page.onUnload(); respond(h.requests[0], [clinic]); await pending;
  assert.equal(h.page.data.clinic, null); h.page.openLocation(); assert.equal(h.locations.length, 0);
});
