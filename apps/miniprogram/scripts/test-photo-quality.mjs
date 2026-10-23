import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createContext, Script } from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const filename = resolve(dirname(fileURLToPath(import.meta.url)), '../app/services/photo-quality.ts');
const source = ts.transpileModule(readFileSync(filename, 'utf8'), {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
}).outputText;

function pixels(width, height, sample = x => Math.floor(x / 3) % 2 ? 185 : 75) {
  const data = new Uint8ClampedArray(width * height * 4);
  for (let y = 0; y < height; y += 1) for (let x = 0; x < width; x += 1) {
    const i = (y * width + x) * 4, value = sample(x, y);
    data[i] = value; data[i + 1] = value; data[i + 2] = value; data[i + 3] = 255;
  }
  return { data, width, height };
}
const face = overrides => ({ type: 3, origin: { x: 0.2, y: 0.15 }, size: { width: 0.6, height: 0.7 },
  angle: [0, 0, 0], confidence: [0.98], ...overrides });

function harness(options = {}) {
  const timers = new Map(), calls = [], canvases = [], images = [], listeners = new Map();
  let timerId = 0, startCallback, selectorCallback;
  const session = {
    on: (event, fn) => listeners.set(event, fn),
    off(event, fn) { assert.equal(listeners.get(event), fn); listeners.delete(event); calls.push(['off', event]); },
    stop() { calls.push(['stop']); }, destroy() { calls.push(['destroy']); },
    start(fn) {
      startCallback = fn;
      if (options.initialAnchors) listeners.get('addAnchors')?.(options.initialAnchors);
      if (!options.startPending) fn(options.startError || null);
    },
    detectFace(request) {
      calls.push(['detectFace', request]);
      if (options.detectThrows) throw new Error('capability failure');
      if (!options.detectPending) listeners.get(options.faceEvent || 'updateAnchors')?.(options.faces || [face()]);
    },
  };
  const canvas = (width, height) => {
    const value = {
      width, height,
      createImage() {
        const img = { width: options.decodedWidth ?? options.width ?? 1200,
          height: options.decodedHeight ?? options.height ?? 1600, set src(path) {
          calls.push(['image', path]);
          if (options.decodeFails) this.onerror?.();
          else if (!options.decodePending) this.onload?.();
        } };
        images.push(img); return img;
      },
      getContext() {
        return {
          clearRect() {}, drawImage(_img, x, y, w, h) { calls.push(['draw', x, y, w, h]); },
          getImageData(_x, _y, w, h) {
            if (options.badPixels) return { data: new Uint8ClampedArray(0) };
            const result = pixels(w, h, options.sample);
            if (options.offsetPixels) {
              const padded = new Uint8ClampedArray(result.data.length + 24); padded.fill(251);
              padded.set(result.data, 12); result.data = padded.subarray(12, padded.length - 12);
            }
            return result;
          },
        };
      },
    };
    canvases.push(value); return value;
  };
  const wx = {
    getImageInfo(request) {
      calls.push(['getImageInfo', request.src]);
      if (options.infoPending) return;
      if (options.infoFails) return request.fail({ errMsg: 'invalid image' });
      request.success({ width: options.width ?? 1200, height: options.height ?? 1600,
        orientation: options.orientation ?? 'up', path: '/local/decoded.jpg' });
    },
    createOffscreenCanvas({ width, height }) { if (options.offscreenThrows) throw new Error('unsupported 2d'); return canvas(width, height); },
    createSelectorQuery() {
      const query = { in(page) { calls.push(['page', page]); return this; },
        select(selector) { calls.push(['selector', selector]); return this; }, fields() { return this; },
        exec(fn) { selectorCallback = fn; if (!options.selectorPending) fn(options.noCanvas ? [] : [{ node: canvas(1, 1) }]); } };
      return query;
    },
    createVKSession(config) { calls.push(['session', config]); if (options.sessionThrows) throw new Error('unsupported'); return session; },
    uploadFile() { assert.fail('inspection must not upload photos'); }, request() { assert.fail('inspection must not transmit pixels'); },
  };
  if (options.noVK) delete wx.createVKSession;
  if (options.noOffscreen) delete wx.createOffscreenCanvas;
  const context = createContext({ wx, Uint8Array, Uint8ClampedArray, Float32Array,
    setTimeout: (fn, ms) => { const id = ++timerId; timers.set(id, { fn, ms }); return id; }, clearTimeout: id => timers.delete(id) });
  const module = { exports: {} };
  new Script(`(function(module,exports){${source}\n})`).runInContext(context)(module, module.exports);
  return { service: module.exports, calls, canvases, images, timers, listeners,
    start: value => startCallback?.(value), resolveSelector: value => selectorCallback?.(value),
    expire(ms) { for (const [id, timer] of timers) if (timer.ms === ms) { timers.delete(id); timer.fn(); } } };
}
const status = (result, key) => result.checks.find(check => check.key === key)?.status;
const flush = () => new Promise(resolve => setImmediate(resolve));

test('dimensions reject tiny/invalid files and distinguish guidance from upload blocks', () => {
  const { service } = harness();
  for (const [w, h] of [[0, 700], [NaN, 700], [700, Infinity], [319, 900]]) assert.equal(service.resolutionCheck(w, h).status, 'block');
  assert.equal(service.resolutionCheck(320, 900).status, 'warning');
  assert.equal(service.resolutionCheck(639, 900).status, 'warning');
  assert.equal(service.resolutionCheck(640, 900).status, 'pass');
});

test('sharpness drops under real smoothing; uncertain blur warnings remain advisory', () => {
  const { service } = harness();
  const sharp = pixels(100, 100), smooth = pixels(100, 100, x => 130 + 3 * Math.sin(x / 18));
  const sharpMetrics = service.measurePixels(sharp), smoothMetrics = service.measurePixels(smooth);
  assert.ok(sharpMetrics.sharpness > smoothMetrics.sharpness * 100);
  assert.equal(service.pixelChecks(sharpMetrics)[0].status, 'pass');
  assert.equal(service.pixelChecks(smoothMetrics)[0].status, 'warning');
  assert.ok(service.pixelChecks(smoothMetrics).every(check => check.status !== 'block'));
});

test('exposure measures the central face ROI rather than a dark background', () => {
  const { service } = harness();
  const photo = pixels(100, 100, (x, y) => x >= 20 && x <= 80 && y >= 15 && y <= 85 ? 130 : 0);
  const measured = service.measurePixels(photo);
  assert.equal(Math.round(measured.brightness), 130);
  assert.equal(service.pixelChecks(measured)[1].status, 'pass');
  assert.equal(service.pixelChecks(service.measurePixels(pixels(100, 100, () => 10)))[1].status, 'warning');
  assert.equal(service.pixelChecks(service.measurePixels(pixels(100, 100, () => 250)))[1].status, 'warning');
});

test('a high-texture background cannot mask a smooth detected face', () => {
  const { service } = harness();
  const photo = pixels(100, 100, (x, y) => x > 39 && x < 61 && y > 39 && y < 61 ? 130 : x % 2 ? 40 : 220);
  const region = { x: 42, y: 42, width: 16, height: 16 };
  assert.equal(service.pixelChecks(service.measurePixels(photo, region))[0].status, 'warning');
});

test('angle checks never infer anatomical side from mirrored yaw or claim an uncalibrated angle pass', () => {
  const { service } = harness();
  for (const yaw of [-45, 45, -0.785, 0.785]) {
    const result = service.faceAngleCheck('left', { faces: [face({ angle: [0, yaw, 0] })], reason: '' });
    assert.equal(result.status, 'manual'); assert.match(result.message, /本人左侧脸约 45°/);
  }
  const empty = service.faceAngleCheck('front', { faces: [], reason: '' });
  assert.equal(empty.status, 'manual'); assert.match(empty.message, /未检测到/);
  const multiple = service.faceAngleCheck('right', { faces: [face(), face()], reason: '' });
  assert.equal(multiple.status, 'manual'); assert.match(multiple.message, /多张人脸/);
});

test('framing checks report cropped/small/malformed faces without fabricating a pass', () => {
  const { service } = harness();
  for (const data of [face({ origin: { x: 0, y: 0.15 } }), face({ size: { width: 0.1, height: 0.1 } }), face({ size: { width: NaN, height: 0.7 } })]) {
    assert.equal(service.faceAngleCheck('front', { faces: [data], reason: '' }).status, 'manual');
  }
});

test('selected file pixels are sampled locally, inspected, and all VisionKit resources released', async () => {
  const h = harness(); const result = await h.service.inspectPhoto('/local/selected.jpg', 'front', {});
  assert.equal(result.width, 1200); assert.equal(result.height, 1600); assert.equal(result.blocked, false);
  assert.equal(status(result, 'clarity'), 'pass'); assert.equal(status(result, 'angle'), 'manual');
  assert.equal(result.needsManualConfirmation, true);
  const request = h.calls.find(call => call[0] === 'detectFace')[1];
  assert.equal(request.width, 384); assert.equal(request.height, 512); assert.equal(request.frameBuffer.byteLength, 384 * 512 * 4);
  assert.equal(request.sourceType, 1);
  assert.equal(h.calls.filter(call => call[0] === 'stop').length, 1);
  assert.equal(h.calls.filter(call => call[0] === 'destroy').length, 1);
  assert.equal(h.listeners.size, 0); assert.equal(h.timers.size, 0);
  assert.equal(h.canvases[0].width, 1); assert.equal(h.canvases[0].height, 1);
});

test('unsupported VisionKit preserves pixel results and requires explicit manual angle confirmation', async () => {
  const h = harness({ noVK: true }); const result = await h.service.inspectPhoto('/local/photo.jpg', 'right', {});
  assert.equal(status(result, 'clarity'), 'pass'); assert.equal(status(result, 'angle'), 'manual');
  assert.match(result.checks.find(check => check.key === 'angle').message, /当前微信不支持/);
  assert.equal(h.calls.some(call => call[0] === 'session'), false); assert.equal(result.blocked, false);
});

test('on-page 2D canvas fallback scopes to the requesting page and releases image data', async () => {
  const h = harness({ offscreenThrows: true, noVK: true }), page = { id: 'photo-page' };
  const result = await h.service.inspectPhoto('/local/photo.jpg', 'front', page);
  assert.equal(status(result, 'clarity'), 'pass');
  assert.equal(h.calls.find(call => call[0] === 'page')[1], page);
  assert.equal(h.calls.find(call => call[0] === 'selector')[1], '#photo-analysis-canvas');
  assert.equal(h.canvases[0].width, 1); assert.equal(h.timers.size, 0);
});

test('tiny and unreadable files block before pixel analysis', async () => {
  for (const options of [{ width: 100, height: 200 }, { infoFails: true }]) {
    const h = harness(options); const result = await h.service.inspectPhoto('/local/bad.jpg', 'front', {});
    assert.equal(result.blocked, true); assert.equal(status(result, 'resolution'), 'block');
    assert.equal(h.calls.some(call => call[0] === 'draw' || call[0] === 'session'), false);
  }
});

test('decode, canvas and malformed RGBA failures never mark unavailable checks passed', async () => {
  for (const options of [{ decodeFails: true }, { noOffscreen: true, noCanvas: true }, { badPixels: true }]) {
    const h = harness(options); const result = await h.service.inspectPhoto('/local/photo.jpg', 'front', {});
    assert.equal(status(result, 'clarity'), 'manual'); assert.equal(status(result, 'exposure'), 'manual');
    assert.equal(status(result, 'angle'), 'manual'); assert.equal(result.needsManualConfirmation, true);
    assert.equal(h.timers.size, 0);
  }
});

test('decode timeout cleans callbacks and canvas, and late image completion is harmless', async () => {
  const h = harness({ decodePending: true }); const pending = h.service.inspectPhoto('/local/photo.jpg', 'front', {});
  await flush(); h.expire(5000); const result = await pending;
  assert.equal(status(result, 'clarity'), 'manual'); assert.equal(h.canvases[0].width, 1);
  h.images[0].onload(); assert.equal(h.calls.some(call => call[0] === 'session'), false);
  assert.equal(h.timers.size, 0);
});

test('face timeout releases the session and suppresses late start callbacks', async () => {
  const h = harness({ startPending: true }); const pending = h.service.inspectPhoto('/local/photo.jpg', 'front', {});
  await flush(); h.expire(3500); const result = await pending;
  assert.equal(status(result, 'angle'), 'manual'); assert.match(result.checks.find(check => check.key === 'angle').message, /超时/);
  h.start(null); assert.equal(h.calls.some(call => call[0] === 'detectFace'), false);
  assert.equal(h.calls.filter(call => call[0] === 'destroy').length, 1); assert.equal(h.timers.size, 0);
});

test('VisionKit start and detection failures degrade to manual checking and clean up', async () => {
  for (const options of [{ startError: 1000 }, { detectThrows: true }, { sessionThrows: true }]) {
    const h = harness(options); const result = await h.service.inspectPhoto('/local/photo.jpg', 'left', {});
    assert.equal(status(result, 'angle'), 'manual'); assert.equal(status(result, 'clarity'), 'pass');
    assert.equal(h.timers.size, 0); assert.equal(h.listeners.size, 0);
  }
});

test('metadata timeout settles the inspection instead of hanging the photo flow', async () => {
  const h = harness({ infoPending: true }); const pending = h.service.inspectPhoto('/local/photo.jpg', 'front', {});
  h.expire(5000); const result = await pending;
  assert.equal(result.blocked, true); assert.match(result.checks[0].message, /超时/);
  assert.equal(h.timers.size, 0);
});

test('EXIF quarter-turn sampling uses decoded dimensions without stretching or double rotating', async () => {
  const h = harness({ orientation: 'right', decodedWidth: 1600, decodedHeight: 1200 });
  const result = await h.service.inspectPhoto('/local/portrait.jpg', 'front', {});
  const draw = h.calls.find(call => call[0] === 'draw');
  assert.deepEqual(draw, ['draw', 0, 0, 512, 384]);
  const detected = h.calls.find(call => call[0] === 'detectFace')[1];
  assert.equal(detected.width, 512); assert.equal(detected.height, 384);
  assert.equal(status(result, 'clarity'), 'pass'); assert.equal(status(result, 'angle'), 'manual');
  assert.equal(result.width, 1200); assert.equal(result.height, 1600);
});

test('ambiguous EXIF rotation never claims clarity or angle success', async () => {
  for (const orientation of ['left', 'right', 'left-mirrored', 'right-mirrored']) {
    const h = harness({ orientation }); const result = await h.service.inspectPhoto('/local/portrait.jpg', 'left', {});
    assert.equal(status(result, 'clarity'), 'manual'); assert.equal(status(result, 'angle'), 'manual');
    assert.equal(result.blocked, false); assert.equal(result.needsManualConfirmation, true);
    assert.equal(h.calls.some(call => call[0] === 'session'), false);
    assert.equal(h.canvases[0].width, 1);
  }
});

test('official addAnchors and updateAnchors events both produce actual single-face observations', async () => {
  for (const faceEvent of ['addAnchors', 'updateAnchors']) {
    const h = harness({ faceEvent }); const result = await h.service.inspectPhoto('/local/photo.jpg', 'front', {});
    assert.match(result.checks.find(check => check.key === 'angle').message, /已检测到单人脸与姿态信息/);
    const config = h.calls.find(call => call[0] === 'session')[1];
    assert.equal(config.track.face.mode, 2);
    assert.equal(h.calls.filter(call => call[0] === 'destroy').length, 1);
    assert.equal(h.listeners.size, 0);
  }
});

test('VisionKit receives only the selected RGBA view, excluding unrelated backing-buffer bytes', async () => {
  const h = harness({ offsetPixels: true }); await h.service.inspectPhoto('/local/photo.jpg', 'front', {});
  const request = h.calls.find(call => call[0] === 'detectFace')[1];
  assert.equal(request.frameBuffer.byteLength, request.width * request.height * 4);
  const expected = pixels(request.width, request.height).data;
  assert.deepEqual(new Uint8ClampedArray(request.frameBuffer), expected);
});

test('incomplete pixel data never reaches the face detector', async () => {
  const h = harness({ badPixels: true }); const result = await h.service.inspectPhoto('/local/photo.jpg', 'front', {});
  assert.equal(status(result, 'clarity'), 'manual'); assert.equal(status(result, 'angle'), 'manual');
  assert.equal(h.calls.some(call => call[0] === 'session' || call[0] === 'detectFace'), false);
});

test('late canvas selector callbacks cannot resize or revive a timed-out inspection', async () => {
  const h = harness({ noOffscreen: true, selectorPending: true });
  const pending = h.service.inspectPhoto('/local/photo.jpg', 'front', {});
  await flush(); h.expire(5000); const result = await pending;
  const lateCanvas = { width: 1, height: 1 };
  h.resolveSelector([{ node: lateCanvas }]);
  assert.equal(lateCanvas.width, 1); assert.equal(lateCanvas.height, 1);
  assert.equal(status(result, 'clarity'), 'manual'); assert.equal(h.calls.some(call => call[0] === 'session'), false);
});

test('initial session events cannot substitute for inspection of the selected still image', async () => {
  const h = harness({ initialAnchors: [face()], faces: [] });
  const result = await h.service.inspectPhoto('/local/photo.jpg', 'front', {});
  assert.ok(h.calls.some(call => call[0] === 'detectFace'));
  assert.match(result.checks.find(check => check.key === 'angle').message, /未检测到可靠人脸/);
  assert.equal(h.timers.size, 0);
});
