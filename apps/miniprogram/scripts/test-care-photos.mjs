import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createContext, Script } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const appRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../app');
const source = ts.transpileModule(readFileSync(resolve(appRoot, 'pages/care-photos/index.ts'), 'utf8'), {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
}).outputText;
const event = (dataset) => ({ currentTarget: { dataset } });
const tick = () => new Promise(setImmediate);
function deferred() {
  let resolve, reject;
  const promise = new Promise((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}

function inspection(overrides = {}) {
  return {
    blocked: false,
    needsManualConfirmation: true,
    width: 1080,
    height: 1440,
    checks: [
      { key: 'resolution', label: '画面尺寸', status: 'pass', message: '画面尺寸符合上传要求' },
      { key: 'clarity', label: '清晰度', status: 'pass', message: '清晰度符合上传要求' },
      { key: 'exposure', label: '光线', status: 'pass', message: '光线符合上传要求' },
      { key: 'angle', label: '角度', status: 'manual', message: '请确认所选拍摄角度' },
    ],
    ...overrides,
  };
}

function harness({ media = true, inspect = async () => inspection(), upload = async () => ({ id: 'saved-new', angle: 'front' }), consent = true, status = 'confirmed', photos = [{ id: 'saved-old', angle: 'front' }] } = {}) {
  let page;
  const pickers = [], uploads = [], inspections = [], toasts = [], previews = [], calls = [], settings = [];
  const service = {
    me: async () => { calls.push('me'); return { id: 'patient-1' }; },
    photoConsent: async () => ({ granted: consent }),
    photos: async () => photos,
    appointment: async () => ({ id: 'appointment-1', status }),
    photoFile: async (id) => `/tmp/${id}.jpg`,
    setPhotoConsent: async (_, granted) => ({ granted }),
    uploadPhoto: async (...args) => { uploads.push(args); return upload(...args); },
  };
  class ApiError extends Error {
    constructor(message, status, code) { super(message); this.status = status; this.code = code; }
  }
  const wx = {
    chooseImage: (options) => pickers.push({ kind: 'image', ...options }),
    stopPullDownRefresh() {},
    showToast: (options) => toasts.push(options),
    previewImage: (options) => previews.push(options),
    pageScrollTo() {},
    getSetting: (options) => options.success({ authSetting: {} }),
    openSetting: (options) => { settings.push(options); options.success?.({ authSetting: {} }); },
  };
  if (media) wx.chooseMedia = (options) => pickers.push({ kind: 'media', ...options });
  const context = createContext({
    Page(definition) {
      page = definition;
      page.setData = (data) => {
        for (const [key, value] of Object.entries(data)) {
          const segments = key.replace(/\[(\d+)\]/g, '.$1').split('.');
          let target = page.data;
          for (const segment of segments.slice(0, -1)) target = target[segment];
          target[segments.at(-1)] = value;
        }
      };
    },
    wx,
    console,
    setTimeout,
    clearTimeout,
  });
  const deps = {
    '../../services/api': { ApiError, errorMessage: (error) => error.message || '请求失败' },
    '../../services/patient': { patientApi: service, confirmDialog: async () => true, confirmNewSession: async () => false, isSessionExpired: (error) => error.status === 401 || error.code === 'SESSION_EXPIRED', requirePatientIdentity: (id) => { assert.equal(id, 'patient-1'); return { patient: { id }, token: 'test-token' }; } },
    '../../services/photo-quality': {
      inspectPhoto: async (...args) => { inspections.push(args); return inspect(...args); },
    },
  };
  new Script(`(function(require,exports){${source}\n})`).runInContext(context)((id) => {
    assert.ok(deps[id], `Unexpected dependency ${id}`);
    return deps[id];
  }, {});
  page.appointmentId = 'appointment-1';
  const ready = async () => { await page.load(); return page; };
  const pick = async (angle = 'front', sourceType = 'camera', path = '/tmp/new.jpg') => {
    const pending = page.choose(event({ angle, source: sourceType }));
    const picker = pickers.at(-1);
    assert.ok(picker, 'A real native picker must be opened');
    if (picker.kind === 'media') picker.success({ tempFiles: [{ tempFilePath: path, fileType: 'image', size: 250000 }] });
    else picker.success({ tempFilePaths: [path], tempFiles: [{ path, size: 250000 }] });
    await pending;
    await tick();
  };
  const review = () => {
    page.toggleReview(event({ field: 'clarity' }));
    page.toggleReview(event({ field: 'angle' }));
  };
  return { page, ready, pick, review, pickers, uploads, inspections, toasts, previews, calls, settings, service, ApiError };
}

test('camera and album selections use native media and stage a review before uploading', async () => {
  for (const sourceType of ['camera', 'album']) {
    const h = harness(); await h.ready();
    await h.pick('left', sourceType);
    assert.equal(h.pickers[0].kind, 'media');
    assert.deepEqual(Array.from(h.pickers[0].sourceType), [sourceType]);
    assert.deepEqual(Array.from(h.pickers[0].mediaType), ['image']);
    assert.equal(h.pickers[0].count, 1);
    assert.equal(h.uploads.length, 0);
    assert.equal(h.page.data.draft.angle, 'left');
    assert.equal(h.page.data.draft.source, sourceType);
    assert.equal(h.page.data.draft.path, '/tmp/new.jpg');
    assert.equal(h.inspections[0][0], '/tmp/new.jpg');
    assert.equal(h.inspections[0][1], 'left');
    assert.equal(h.page.data.items[0].id, 'saved-old');
    assert.equal(h.page.data.items[0].path, '/tmp/saved-old.jpg');
  }
});

test('older WeChat falls back to chooseImage with the explicit source', async () => {
  const h = harness({ media: false }); await h.ready();
  await h.pick('right', 'album');
  assert.equal(h.pickers[0].kind, 'image');
  assert.deepEqual(Array.from(h.pickers[0].sourceType), ['album']);
  assert.equal(h.uploads.length, 0);
  assert.equal(h.page.data.draft.angle, 'right');
});

test('picker cancellation stays silent and preserves the saved image', async () => {
  const h = harness(); await h.ready();
  const pending = h.page.choose(event({ angle: 'front', source: 'camera' }));
  h.pickers[0].fail({ errMsg: 'chooseMedia:fail cancel' });
  await pending;
  assert.equal(h.uploads.length, 0);
  assert.equal(h.page.data.draft, null);
  assert.equal(h.page.data.error, '');
  assert.equal(h.page.data.items[0].path, '/tmp/saved-old.jpg');
  assert.equal(h.page.data.selecting, false);
});

test('picker permission and device failures show actionable errors without uploading', async () => {
  for (const errMsg of ['chooseMedia:fail auth deny', 'chooseMedia:fail camera unavailable']) {
    const h = harness(); await h.ready();
    const pending = h.page.choose(event({ angle: 'front', source: 'camera' }));
    h.pickers[0].fail({ errMsg });
    await pending;
    assert.ok(h.page.data.actionError || h.page.data.error || h.toasts.some((toast) => toast.title));
    assert.equal(h.uploads.length, 0);
    assert.equal(h.page.data.selecting, false);
    assert.equal(h.page.data.items[0].id, 'saved-old');
  }
});

test('authorization, appointment state, expiry and busy state guard native picker entry', async () => {
  for (const state of [{ granted: false }, { uploadAllowed: false }, { expired: true }, { loading: true }, { uploading: true }, { consentLoading: true }, { selecting: true }, { checking: true }]) {
    const h = harness(); await h.ready();
    h.page.setData(state);
    await h.page.choose(event({ angle: 'front', source: 'camera' }));
    assert.equal(h.pickers.length, 0, JSON.stringify(state));
  }
  const invalid = harness(); await invalid.ready();
  await invalid.page.choose(event({ angle: 'other', source: 'camera' }));
  await invalid.page.choose(event({ angle: 'front', source: 'other' }));
  assert.equal(invalid.pickers.length, 0);
});

test('both human review confirmations are required before replacing a saved photograph', async () => {
  const h = harness(); await h.ready(); await h.pick();
  await h.page.confirmDraft();
  assert.equal(h.uploads.length, 0);
  h.page.toggleReview(event({ field: 'clarity' }));
  await h.page.confirmDraft();
  assert.equal(h.uploads.length, 0);
  h.page.toggleReview(event({ field: 'angle' }));
  await h.page.confirmDraft();
  assert.equal(h.uploads.length, 1);
  assert.deepEqual(h.uploads[0], ['appointment-1', 'front', '/tmp/new.jpg', 'patient-1']);
  assert.equal(h.page.data.items[0].id, 'saved-new');
  assert.equal(h.page.data.items[0].path, '/tmp/saved-new.jpg');
  assert.equal(h.page.data.draft, null);
});

test('a blocked quality result cannot upload despite human confirmation', async () => {
  const h = harness({ inspect: async () => inspection({ blocked: true }) });
  await h.ready(); await h.pick(); h.review();
  await h.page.confirmDraft();
  assert.equal(h.uploads.length, 0);
  assert.equal(h.page.data.items[0].id, 'saved-old');
  assert.equal(h.page.data.items[0].path, '/tmp/saved-old.jpg');
});

test('discarding a replacement leaves the previously saved photograph available', async () => {
  const h = harness(); await h.ready(); await h.pick();
  h.page.discardDraft();
  assert.equal(h.page.data.draft, null);
  assert.equal(h.page.data.items[0].id, 'saved-old');
  h.page.preview(event({ angle: 'front' }));
  assert.equal(h.previews[0].current, '/tmp/saved-old.jpg');
  assert.equal(h.uploads.length, 0);
});

test('inspection must finish before confirmation and native picker return does not reload away the draft', async () => {
  const check = deferred();
  const h = harness({ inspect: () => check.promise }); await h.ready();
  const pending = h.page.choose(event({ angle: 'front', source: 'camera' }));
  h.page.onShow();
  assert.equal(h.calls.length, 1);
  h.pickers[0].success({ tempFiles: [{ tempFilePath: '/tmp/new.jpg', fileType: 'image' }] });
  await tick();
  assert.equal(h.page.data.checking, true);
  h.page.onShow();
  assert.equal(h.calls.length, 1);
  h.review(); await h.page.confirmDraft();
  assert.equal(h.uploads.length, 0);
  check.resolve(inspection()); await pending; await tick();
  assert.equal(h.page.data.checking, false);
  assert.equal(h.page.data.draft.path, '/tmp/new.jpg');
});

test('failed replacement preserves the saved image and retry passes through draft review', async () => {
  const h = harness({ upload: async () => { throw new Error('照片上传中断，请检查连接后重试。'); } });
  await h.ready(); await h.pick(); h.review(); await h.page.confirmDraft();
  assert.equal(h.uploads.length, 1);
  assert.equal(h.page.data.items[0].id, 'saved-old');
  assert.equal(h.page.data.items[0].path, '/tmp/saved-old.jpg');
  assert.match(h.page.data.draft.error, /上传中断/);
  h.page.toggleReview(event({ field: 'angle' }));
  h.page.retry(event({ angle: 'front' })); await tick();
  assert.equal(h.uploads.length, 1);
  assert.ok(h.page.data.draft);
  assert.equal(h.page.data.draft.angleConfirmed, false);
});

test('state changes after selection guard confirmation and prevent unauthorized upload', async () => {
  for (const state of [{ granted: false }, { uploadAllowed: false }, { expired: true }, { loading: true }, { consentLoading: true }]) {
    const h = harness(); await h.ready(); await h.pick(); h.review(); h.page.setData(state);
    await h.page.confirmDraft();
    assert.equal(h.uploads.length, 0, JSON.stringify(state));
    assert.equal(h.page.data.items[0].id, 'saved-old');
  }
});

test('unloading ignores a late picker callback and a late inspection result', async () => {
  const picker = harness(); await picker.ready();
  const choose = picker.page.choose(event({ angle: 'front', source: 'camera' }));
  picker.page.onUnload();
  picker.pickers[0].success({ tempFiles: [{ tempFilePath: '/tmp/new.jpg', fileType: 'image' }] });
  await choose; await tick();
  assert.equal(picker.inspections.length, 0);
  assert.equal(picker.uploads.length, 0);
  const check = deferred();
  const h = harness({ inspect: () => check.promise }); await h.ready();
  const pending = h.page.choose(event({ angle: 'front', source: 'camera' }));
  h.pickers[0].success({ tempFiles: [{ tempFilePath: '/tmp/new.jpg', fileType: 'image' }] });
  await tick(); h.page.onUnload();
  const afterUnload = JSON.stringify(h.page.data);
  check.resolve(inspection()); await pending; await tick();
  assert.equal(JSON.stringify(h.page.data), afterUnload);
  assert.equal(h.uploads.length, 0);
});

test('an unloaded page ignores a delayed successful upload response', async () => {
  const saved = deferred();
  const h = harness({ upload: () => saved.promise }); await h.ready(); await h.pick(); h.review();
  const pending = h.page.confirmDraft(); await tick();
  assert.equal(h.uploads.length, 1);
  h.page.onUnload();
  const afterUnload = JSON.stringify(h.page.data);
  saved.resolve({ id: 'saved-new', angle: 'front' }); await pending;
  assert.equal(JSON.stringify(h.page.data), afterUnload);
  assert.equal(h.toasts.length, 0);
});

test('repeated confirmation while saving submits the replacement only once', async () => {
  const saved = deferred();
  const h = harness({ upload: () => saved.promise }); await h.ready(); await h.pick(); h.review();
  const pending = h.page.confirmDraft(); await tick();
  await h.page.confirmDraft();
  assert.equal(h.uploads.length, 1);
  saved.resolve({ id: 'saved-new', angle: 'front' }); await pending;
  assert.equal(h.page.data.uploading, false);
});

test('a saved photo whose preview download fails retries reading instead of uploading twice', async () => {
  const h = harness(); await h.ready(); await h.pick(); h.review();
  h.service.photoFile = async () => { throw new Error('照片读取失败，请检查连接后重试。'); };
  await h.page.confirmDraft();
  assert.equal(h.uploads.length, 1);
  assert.equal(h.page.data.items[0].id, 'saved-new');
  assert.equal(h.page.data.items[0].pendingPath, '');
  assert.equal(h.page.data.draft, null);
  h.page.retry(event({ angle: 'front' })); await tick();
  assert.equal(h.uploads.length, 1);
});

test('a failed reload clears editable identity and prevents picker entry', async () => {
  const h = harness(); await h.ready();
  h.service.me = async () => { throw new h.ApiError('登录已失效，请重新登录。', 401, 'SESSION_EXPIRED'); };
  await h.page.load();
  assert.equal(h.page.data.expired, true);
  assert.equal(h.page.data.patientId, '');
  await h.page.choose(event({ angle: 'front', source: 'camera' }));
  assert.equal(h.pickers.length, 0);
});

test('withdrawn consent and completed appointments are rechecked before uploading a reviewed draft', async () => {
  for (const change of ['consent', 'appointment']) {
    const h = harness(); await h.ready(); await h.pick(); h.review();
    if (change === 'consent') h.service.photoConsent = async () => ({ granted: false });
    else h.service.appointment = async () => ({ id: 'appointment-1', status: 'completed' });
    await h.page.confirmDraft();
    assert.equal(h.uploads.length, 0, change);
    assert.equal(h.page.data.draft, null);
    assert.equal(h.page.data.items[0].id, 'saved-old');
    assert.equal(h.page.data.items[0].path, '/tmp/saved-old.jpg');
    assert.ok(h.page.data.actionError);
    if (change === 'consent') assert.equal(h.page.data.granted, false);
    else assert.equal(h.page.data.uploadAllowed, false);
    assert.equal(h.page.data.uploading, false);
  }
});
