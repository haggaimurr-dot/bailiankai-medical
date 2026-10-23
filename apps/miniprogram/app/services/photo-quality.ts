export type PhotoAngle = 'front' | 'left' | 'right';
export type PhotoCheckStatus = 'pass' | 'warning' | 'manual' | 'block';
export interface PhotoCheck {
  key: 'resolution' | 'clarity' | 'exposure' | 'angle';
  label: string;
  status: PhotoCheckStatus;
  message: string;
}
export interface PhotoInspection {
  checks: PhotoCheck[];
  blocked: boolean;
  needsManualConfirmation: boolean;
  width: number;
  height: number;
}
export interface PixelMetrics {
  sharpness: number;
  contrast: number;
  brightness: number;
  darkFraction: number;
  brightFraction: number;
}
interface PixelImage { data: Uint8ClampedArray; width: number; height: number; orientationUnverified?: boolean; }
interface Region { x: number; y: number; width: number; height: number; }
type AnalysisCanvas = WechatMiniprogram.Canvas | WechatMiniprogram.OffscreenCanvas;
interface FaceObservation { faces: WechatMiniprogram.VKFaceAnchor[]; reason: string; }

const IMAGE_TIMEOUT_MS = 5000;
const FACE_TIMEOUT_MS = 3500;
const SAMPLE_MAX_DIMENSION = 512;
const ignoreImageEvent = () => {};

function check(key: PhotoCheck['key'], label: string, status: PhotoCheckStatus, message: string): PhotoCheck {
  return { key, label, status, message };
}
function inspection(checks: PhotoCheck[], width = 0, height = 0): PhotoInspection {
  return { checks, width, height, blocked: checks.some(item => item.status === 'block'),
    needsManualConfirmation: checks.some(item => item.status === 'manual' || item.status === 'warning') };
}

export function resolutionCheck(width: number, height: number): PhotoCheck {
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return check('resolution', '尺寸', 'block', '无法读取有效照片尺寸，请重新拍摄或选择其他照片。');
  }
  const shortEdge = Math.min(width, height);
  if (shortEdge < 320) return check('resolution', '尺寸', 'block', `照片仅 ${width} × ${height} 像素，细节不足，请选择原图或重新拍摄。`);
  if (shortEdge < 640) return check('resolution', '尺寸', 'warning', `照片为 ${width} × ${height} 像素，建议短边至少 640 像素，请尽量选择原图。`);
  return check('resolution', '尺寸', 'pass', `${width} × ${height} 像素，达到建议尺寸。`);
}

/** Central/face ROI reduces background influence. Thresholds are advisory, not a clinical quality score. */
export function measurePixels(image: PixelImage, region?: Region): PixelMetrics {
  const { data, width, height } = image;
  if (!Number.isInteger(width) || !Number.isInteger(height) || width < 4 || height < 4 || data.length !== width * height * 4) {
    throw new Error('像素数据不完整');
  }
  const roi = region || { x: width * 0.2, y: height * 0.15, width: width * 0.6, height: height * 0.7 };
  const x1 = Math.max(1, Math.min(width - 2, Math.floor(roi.x)));
  const y1 = Math.max(1, Math.min(height - 2, Math.floor(roi.y)));
  const x2 = Math.max(x1 + 1, Math.min(width - 1, Math.ceil(roi.x + roi.width)));
  const y2 = Math.max(y1 + 1, Math.min(height - 1, Math.ceil(roi.y + roi.height)));
  const gray = new Float32Array(width * height);
  for (let i = 0; i < gray.length; i += 1) {
    const offset = i * 4;
    gray[i] = data[offset] * 0.299 + data[offset + 1] * 0.587 + data[offset + 2] * 0.114;
  }
  let count = 0, total = 0, squared = 0, lapTotal = 0, lapSquared = 0, dark = 0, bright = 0;
  for (let y = y1; y < y2; y += 1) {
    for (let x = x1; x < x2; x += 1) {
      const i = y * width + x, value = gray[i];
      const laplacian = gray[i - 1] + gray[i + 1] + gray[i - width] + gray[i + width] - value * 4;
      count += 1; total += value; squared += value * value;
      lapTotal += laplacian; lapSquared += laplacian * laplacian;
      if (value < 20) dark += 1;
      if (value > 240) bright += 1;
    }
  }
  const mean = total / count;
  return { brightness: mean, contrast: Math.sqrt(Math.max(0, squared / count - mean * mean)),
    sharpness: Math.max(0, lapSquared / count - (lapTotal / count) ** 2),
    darkFraction: dark / count, brightFraction: bright / count };
}

export function pixelChecks(metrics: PixelMetrics): PhotoCheck[] {
  const clarity = metrics.sharpness < 80 || metrics.contrast < 12
    ? check('clarity', '清晰度', 'warning', '面部区域纹理较少或可能模糊，请放大确认眼睛、鼻翼和皮肤细节；建议对焦后重拍。')
    : check('clarity', '清晰度', 'pass', '纹理检查未发现明显模糊，仍请放大确认五官与皮肤细节。');
  const exposure = metrics.brightness < 45 || metrics.darkFraction > 0.35
    ? check('exposure', '光线', 'warning', '面部区域偏暗，请面向均匀光源，避免背光。')
    : metrics.brightness > 220 || metrics.brightFraction > 0.35
      ? check('exposure', '光线', 'warning', '面部区域可能过曝，请避开直射光，确认皮肤细节没有泛白。')
      : check('exposure', '光线', 'pass', '亮度检查未发现明显过暗或过曝。');
  return [clarity, exposure];
}

function manualAngle(angle: PhotoAngle, reason: string): PhotoCheck {
  const guide = angle === 'front' ? '确认脸正对镜头，双眼平齐，无明显低头或仰头。'
    : `确认展示本人${angle === 'left' ? '左' : '右'}侧脸约 45°，无需按预览镜像判断左右。`;
  return check('angle', '角度', 'manual', `${reason}${guide}`);
}

export function faceAngleCheck(angle: PhotoAngle, observation: FaceObservation): PhotoCheck {
  const { faces, reason } = observation;
  if (reason) return manualAngle(angle, reason);
  if (faces.length !== 1) return manualAngle(angle, faces.length > 1
    ? '检测到多张人脸，建议重拍单人照片。' : '未检测到可靠人脸，请确认面部完整、无遮挡。');
  const face = faces[0];
  const bounds = faceRegion(face);
  if (!bounds) return manualAngle(angle, '已识别人脸，但取景信息不完整。');
  if (bounds.width < 0.22 || bounds.height < 0.22) return manualAngle(angle, '人脸占比偏小，建议靠近镜头后重拍。');
  if (bounds.x < 0.01 || bounds.y < 0.01 || bounds.x + bounds.width > 0.99 || bounds.y + bounds.height > 0.99) {
    return manualAngle(angle, '面部靠近照片边缘，请确认额头与下巴没有被裁切。');
  }
  // Official VKFaceAnchor docs do not specify angle units. Do not invent a 45° pass
  // or infer anatomical side from the sign of a possibly mirrored selfie.
  const hasPose = Array.isArray(face.angle) && face.angle.length >= 3 && face.angle.slice(0, 3).every(Number.isFinite);
  return manualAngle(angle, hasPose ? '已检测到单人脸与姿态信息，角度阈值尚需真机校准，请人工核对。' : '已检测到单人脸，未获得可靠姿态信息。');
}

function faceRegion(face: WechatMiniprogram.VKFaceAnchor): Region | undefined {
  const x = face.origin?.x, y = face.origin?.y, width = face.size?.width, height = face.size?.height;
  if (![x, y, width, height].every(value => Number.isFinite(value)) || width <= 0 || height <= 0 || x < 0 || y < 0 || x > 1 || y > 1 || width > 1 || height > 1) return undefined;
  return { x, y, width, height };
}

function imageInfo(path: string): Promise<WechatMiniprogram.GetImageInfoSuccessCallbackResult> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('照片读取超时，请重新选择照片。')), IMAGE_TIMEOUT_MS);
    try {
      wx.getImageInfo({ src: path, success: result => { clearTimeout(timer); resolve(result); },
        fail: () => { clearTimeout(timer); reject(new Error('无法读取照片，请选择可打开的原图。')); } });
    } catch (error) { clearTimeout(timer); reject(error); }
  });
}

async function canvasFor(page: object, width: number, height: number): Promise<AnalysisCanvas> {
  if (typeof wx.createOffscreenCanvas === 'function') {
    try {
      const canvas = wx.createOffscreenCanvas({ type: '2d', width, height });
      if (canvas?.getContext('2d')) return canvas;
      if (canvas) { canvas.width = 1; canvas.height = 1; }
    } catch { /* Older devices may expose the API without a usable 2D context. */ }
  }
  return new Promise((resolve, reject) => {
    let settled = false;
    const timer = setTimeout(() => { settled = true; reject(new Error('画布不可用')); }, IMAGE_TIMEOUT_MS);
    try {
      wx.createSelectorQuery().in(page as WechatMiniprogram.Page.TrivialInstance).select('#photo-analysis-canvas').fields({ node: true }).exec(results => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        const canvas = results?.[0]?.node as WechatMiniprogram.Canvas | undefined;
        if (!canvas) return reject(new Error('画布不可用'));
        canvas.width = width; canvas.height = height; resolve(canvas);
      });
    } catch (error) { settled = true; clearTimeout(timer); reject(error); }
  });
}

async function readPixels(path: string, width: number, height: number, page: object, orientation: string): Promise<PixelImage> {
  const scale = Math.min(1, SAMPLE_MAX_DIMENSION / Math.max(width, height));
  const sampledWidth = Math.max(4, Math.round(width * scale)), sampledHeight = Math.max(4, Math.round(height * scale));
  const canvas = await canvasFor(page, sampledWidth, sampledHeight);
  try {
    const image = canvas.createImage();
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => { image.onload = ignoreImageEvent; image.onerror = ignoreImageEvent; reject(new Error('照片解码超时')); }, IMAGE_TIMEOUT_MS);
      image.onload = () => { clearTimeout(timer); image.onload = ignoreImageEvent; image.onerror = ignoreImageEvent; resolve(); };
      image.onerror = () => { clearTimeout(timer); image.onload = ignoreImageEvent; image.onerror = ignoreImageEvent; reject(new Error('照片解码失败')); };
      try { image.src = path; }
      catch (error) { clearTimeout(timer); image.onload = ignoreImageEvent; image.onerror = ignoreImageEvent; reject(error); }
    });
    // getImageInfo dimensions explicitly ignore EXIF. Use decoded Image dimensions
    // to avoid stretching a portrait photo that the platform rotated into landscape.
    const decodedWidth = image.width, decodedHeight = image.height;
    if (![decodedWidth, decodedHeight].every(value => Number.isFinite(value) && value > 0)) throw new Error('无法确认解码尺寸');
    const decodedScale = Math.min(1, SAMPLE_MAX_DIMENSION / Math.max(decodedWidth, decodedHeight));
    const drawWidth = Math.max(4, Math.round(decodedWidth * decodedScale)), drawHeight = Math.max(4, Math.round(decodedHeight * decodedScale));
    canvas.width = drawWidth; canvas.height = drawHeight;
    const context = canvas.getContext('2d') as WechatMiniprogram.CanvasRenderingContext.CanvasRenderingContext2D;
    context.clearRect(0, 0, drawWidth, drawHeight);
    context.drawImage(image, 0, 0, drawWidth, drawHeight);
    const pixels = context.getImageData(0, 0, drawWidth, drawHeight);
    if (!pixels?.data || pixels.data.length !== drawWidth * drawHeight * 4) throw new Error('像素数据不完整');
    const quarterTurn = orientation === 'left' || orientation === 'right' || orientation === 'left-mirrored' || orientation === 'right-mirrored';
    // A decoder reporting the raw, unswapped dimensions leaves 90° handling ambiguous.
    // Do not rotate twice or claim a clarity pass without a known decoded orientation.
    const orientationUnverified = quarterTurn && width !== height && !(decodedWidth === height && decodedHeight === width);
    return { data: pixels.data, width: drawWidth, height: drawHeight, orientationUnverified };
  } finally {
    // Release the sampled canvas without altering the selected original file.
    canvas.width = 1; canvas.height = 1;
  }
}

function detectFaces(pixels: PixelImage): Promise<FaceObservation> {
  if (typeof wx.createVKSession !== 'function') return Promise.resolve({ faces: [], reason: '当前微信不支持自动人脸检查。' });
  return new Promise(resolve => {
    let session: WechatMiniprogram.VKSession | undefined;
    let settled = false;
    let detectRequested = false;
    const finish = (value: FaceObservation) => {
      if (settled) return;
      settled = true; clearTimeout(timer);
      if (session) {
        try { session.off('addAnchors', receive); } catch { /* optional on older implementations */ }
        try { session.off('updateAnchors', receive); } catch { /* optional on older implementations */ }
        try { session.stop(); } catch { /* session may never have started */ }
        try { session.destroy(); } catch { /* release best effort */ }
      }
      resolve(value);
    };
    const receive = (anchors: WechatMiniprogram.VKFaceAnchor[]) => {
      if (!detectRequested) return;
      finish({ faces: Array.isArray(anchors) ? anchors.filter(anchor => anchor && anchor.type === 3) : [], reason: '' });
    };
    const timer = setTimeout(() => finish({ faces: [], reason: '自动人脸检查超时，请人工核对。' }), FACE_TIMEOUT_MS);
    try {
      // Installed typings require a plane even though the official face-only example omits it.
      session = wx.createVKSession({ track: { face: { mode: 2 } } as WechatMiniprogram.Track, version: 'v1' });
      if (!session || typeof session.detectFace !== 'function') return finish({ faces: [], reason: '当前设备不支持静态照片人脸检查。' });
      session.on('addAnchors', receive); session.on('updateAnchors', receive);
      session.start(error => {
        if (settled) return;
        if (error) return finish({ faces: [], reason: '自动人脸检查暂不可用，请人工核对。' });
        try {
          // Copy only the RGBA view; its backing buffer may contain other bytes.
          const buffer = new Uint8Array(pixels.data).buffer;
          detectRequested = true;
          session!.detectFace({ frameBuffer: buffer, width: pixels.width, height: pixels.height, sourceType: 1, scoreThreshold: 0.8 });
        } catch { finish({ faces: [], reason: '自动人脸检查暂不可用，请人工核对。' }); }
      });
    } catch { finish({ faces: [], reason: '当前设备不支持自动人脸检查。' }); }
  });
}

/** Reads the original locally; no upload, persistence, face template, or diagnostic inference. */
export async function inspectPhoto(path: string, angle: PhotoAngle, page: object): Promise<PhotoInspection> {
  let info: WechatMiniprogram.GetImageInfoSuccessCallbackResult;
  try { info = await imageInfo(path); }
  catch (error) { return inspection([check('resolution', '照片读取', 'block', error instanceof Error ? error.message : '无法读取照片，请重新选择。')]); }
  const dimensions = resolutionCheck(info.width, info.height);
  if (dimensions.status === 'block') return inspection([dimensions], info.width, info.height);
  let pixels: PixelImage;
  try { pixels = await readPixels(info.path || path, info.width, info.height, page, info.orientation || 'up'); }
  catch {
    return inspection([dimensions,
      check('clarity', '清晰度', 'manual', '当前设备未能读取照片像素，请放大确认五官与皮肤细节。'),
      check('exposure', '光线', 'manual', '请人工确认面部光线均匀，无明显过暗、泛白或背光。'),
      manualAngle(angle, '自动检查不可用。')], info.width, info.height);
  }
  if (pixels.orientationUnverified) {
    return inspection([dimensions,
      check('clarity', '清晰度', 'manual', '照片含旋转方向信息，当前设备的解码方向未确认，请在预览中放大核对细节。'),
      check('exposure', '光线', 'manual', '请确认照片方向正确，面部光线均匀，无明显过暗或泛白。'),
      manualAngle(angle, '照片方向需要人工确认。')], info.width, info.height);
  }
  const observation = await detectFaces(pixels);
  const normalized = observation.faces.length === 1 ? faceRegion(observation.faces[0]) : undefined;
  const roi = normalized ? { x: normalized.x * pixels.width, y: normalized.y * pixels.height,
    width: normalized.width * pixels.width, height: normalized.height * pixels.height } : undefined;
  try {
    return inspection([dimensions, ...pixelChecks(measurePixels(pixels, roi)), faceAngleCheck(angle, observation)], info.width, info.height);
  } catch {
    return inspection([dimensions, check('clarity', '清晰度', 'manual', '照片像素检查未完成，请放大核对细节。'),
      check('exposure', '光线', 'manual', '请人工确认照片光线。'), faceAngleCheck(angle, observation)], info.width, info.height);
  }
}
