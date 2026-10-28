#!/usr/bin/env python3
"""Real-model gateway adapter for the Bailiankai assessment pipeline.

Implements the assessment-gateway-v1 protocol (POST /quality, POST /analyze)
on top of an OpenAI-compatible chat relay with a vision model.

Env: RELAY_BASE (e.g. https://api.apikey.fan/v1), RELAY_KEY, RELAY_MODEL.
Binds 127.0.0.1:$GATEWAY_PORT (default 18087; production uses 18094).
Stdlib only.
"""
import base64
import json
import os
import urllib.error
import urllib.request
from datetime import datetime, timezone, timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

RELAY_BASE = os.environ.get("RELAY_BASE", "https://api.apikey.fan/v1").rstrip("/")
RELAY_KEY = os.environ.get("RELAY_KEY", "")
RELAY_MODEL = os.environ.get("RELAY_MODEL", "gpt-6.1-sol")
GATEWAY_PORT = int(os.environ.get("GATEWAY_PORT", "18087"))
CST = timezone(timedelta(hours=8))

CODES = ["encoding", "dimensions", "face", "clarity", "exposure", "occlusion", "angle"]
OBS_CODES = {"surface_irregularity", "color_variation", "other_visible_change"}
REGIONS = {"forehead", "left_cheek", "right_cheek", "nose", "chin", "multiple", "unknown"}
CERTAINTY = {"low", "medium", "high", "unknown"}


def chat(messages, max_tokens=1600):
    body = {"model": RELAY_MODEL, "messages": messages, "temperature": 0.2, "max_tokens": max_tokens}
    req = urllib.request.Request(RELAY_BASE + "/chat/completions", data=json.dumps(body).encode(),
                                 headers={"Authorization": "Bearer " + RELAY_KEY, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=110) as resp:
        data = json.loads(resp.read())
    return data["choices"][0]["message"]["content"], data.get("id", "")


def extract_json(text):
    text = text.strip()
    if text.startswith("```"):
        text = text.split("```", 2)[1]
        if text.startswith("json"):
            text = text[4:]
    start = text.find("{")
    end = text.rfind("}")
    return json.loads(text[start:end + 1])


def img_part(b64):
    return {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64," + b64}}


# One specific retake tip per failed quality check, in check order.
# (Previously a single generic line for every failure, so a blurry photo
# was told to "find even light" and an occluded one got the same advice.)
RETAKE_ADVICE = {
    "encoding": "照片格式无法读取，请重新选择一张 JPEG 照片上传。",
    "dimensions": "照片尺寸不符合要求，请用原图重新拍摄上传。",
    "face": "未检测到清晰人脸，请让面部居中、完整入镜后重拍。",
    "clarity": "照片不够清晰（对焦不实或手抖），请保持手机稳定、对焦面部后重拍。",
    "exposure": "曝光异常（太暗或过曝），请到光线均匀处重拍，可对照指引中的逆光与太暗示例。",
    "occlusion": "面部存在遮挡，请把头发拢到耳后、移开遮挡物后重拍。",
    "angle": "角度与标注不符，请按该角度转头、双眼平视镜头后重拍。",
}


def retake_advice_for(checks):
    """Build retake advice from the failed checks; generic fallback if none match."""
    failed = [c for c in checks if c.get("status") == "fail"]
    if not failed:
        return []
    advice = [RETAKE_ADVICE[c["code"]] for c in failed if c.get("code") in RETAKE_ADVICE]
    return advice or ["请在光线均匀处重新拍摄该角度照片"]


# ---------------------------------------------------------------- quality
def do_quality(req):
    cfg = req.get("configuration") or {}
    photos = req.get("photos") or []
    qualities = {}
    # One model call covering the visual checks for all photos.
    verdicts = {}
    try:
        content = [{"type": "text", "text":
                    "你是照片质检员。下面是同一人按顺序提供的面部照片（角度依次为：" +
                    "、".join(f"{p.get('angle')}（id={p.get('id')}）" for p in photos) +
                    "）。逐张判断：face（是否清晰可见一张人脸）、clarity（面部是否清晰对焦）、"
                    "exposure（曝光是否正常、不过曝不欠曝）、occlusion（面部是否无明显遮挡）、"
                    "angle（拍摄角度是否与标注角度大致相符）。每张给出 0 到 1 的置信分数。"
                    "只输出 JSON，不要解释：{\"photos\": [{\"id\": \"...\", \"face\": true, \"clarity\": 0.9, "
                    "\"exposure\": 0.9, \"occlusion\": 0.9, \"angle\": true, \"issue\": \"\"}]}。"
                    "issue 为空字符串表示无问题；任一项不合格时写明原因。"}]
        for p in photos:
            content.append(img_part(p["jpegBase64"]))
        raw, _ = chat([{"role": "user", "content": content}], max_tokens=900)
        parsed = extract_json(raw)
        for v in parsed.get("photos", []):
            verdicts[v.get("id")] = v
    except Exception as exc:  # model unreachable -> honest 'unavailable'
        for p in photos:
            qualities[p["id"]] = {
                "status": "unavailable", "checkerVersion": cfg.get("checkerVersion", ""),
                "thresholdProfileVersion": cfg.get("thresholdProfileVersion", ""),
                "checkedAt": datetime.now(CST).isoformat(), "checks": [],
                "issues": [f"质检服务暂时不可用：{str(exc)[:80]}"], "retakeAdvice": ["请稍后重试质检"]}
        return {"qualities": qualities}

    for p in photos:
        raw_bytes = base64.b64decode(p["jpegBase64"]) if p.get("jpegBase64") else b""
        v = verdicts.get(p["id"], {})
        def score(key, default=0.9):
            try:
                return max(0.0, min(1.0, float(v.get(key, default))))
            except Exception:
                return default
        face_ok = bool(v.get("face"))
        angle_ok = bool(v.get("angle", True))
        checks = [
            {"code": "encoding", "status": "pass" if raw_bytes[:2] == b"\xff\xd8" else "fail",
             "score": 1.0, "threshold": 0.5, "message": "JPEG 格式正确" if raw_bytes[:2] == b"\xff\xd8" else "不是有效的 JPEG"},
            {"code": "dimensions", "status": "pass" if 200 <= (p.get("width") or 0) <= 4096 and 200 <= (p.get("height") or 0) <= 4096 else "fail",
             "score": 1.0, "threshold": 0.5, "message": f"尺寸 {p.get('width')}x{p.get('height')}"},
            {"code": "face", "status": "pass" if face_ok else "fail",
             "score": score("face", 0.95 if face_ok else 0.2), "threshold": 0.5, "message": "检测到人脸" if face_ok else "未检测到清晰人脸"},
            {"code": "clarity", "status": "pass" if score("clarity") >= 0.6 else "fail",
             "score": score("clarity"), "threshold": 0.6, "message": "清晰度良好" if score("clarity") >= 0.6 else "照片不够清晰"},
            {"code": "exposure", "status": "pass" if score("exposure") >= 0.5 else "fail",
             "score": score("exposure"), "threshold": 0.5, "message": "曝光正常" if score("exposure") >= 0.5 else "曝光异常"},
            {"code": "occlusion", "status": "pass" if score("occlusion") >= 0.5 else "fail",
             "score": score("occlusion"), "threshold": 0.5, "message": "无明显遮挡" if score("occlusion") >= 0.5 else "面部存在遮挡"},
            {"code": "angle", "status": "pass" if angle_ok else "fail",
             "score": score("angle", 0.9 if angle_ok else 0.3), "threshold": 0.5, "message": "角度符合要求" if angle_ok else "角度与标注不符"},
        ]
        all_pass = all(c["status"] == "pass" for c in checks)
        issue = (v.get("issue") or "").strip()
        issues = [] if all_pass else [issue or "部分质检项未通过，请按提示重拍"]
        qualities[p["id"]] = {
            "status": "passed" if all_pass else "rejected",
            "checkerVersion": cfg.get("checkerVersion", ""),
            "thresholdProfileVersion": cfg.get("thresholdProfileVersion", ""),
            "checkedAt": datetime.now(CST).isoformat(),
            "checks": checks, "issues": issues,
            "retakeAdvice": [] if all_pass else retake_advice_for(checks),
        }
    return {"qualities": qualities}


# ---------------------------------------------------------------- analyze
def validate_analysis(a, photos):
    errs = []
    if not isinstance(a.get("summary"), str) or not (10 <= len(a["summary"]) <= 500):
        errs.append("summary 长度需在 10-500 字")
    obs = a.get("observations")
    if not isinstance(obs, list) or len(obs) > 5:
        errs.append("observations 需为不超过 5 条的数组")
        obs = obs or []
    photo_by_id = {p["id"]: p for p in photos}
    for o in obs:
        if o.get("code") not in OBS_CODES:
            errs.append(f"observation code 非法: {o.get('code')}")
        if o.get("region") not in REGIONS:
            errs.append(f"region 非法: {o.get('region')}")
        if o.get("certainty") not in CERTAINTY:
            errs.append(f"certainty 非法: {o.get('certainty')}")
        if not isinstance(o.get("description"), str) or len(o["description"]) > 500:
            errs.append("description 过长或缺失")
        ev = o.get("evidence") or []
        if not (1 <= len(ev) <= 3):
            errs.append("evidence 需 1-3 条")
        for e in ev:
            src = photo_by_id.get(e.get("photoId"))
            if src is None or src.get("angle") != e.get("angle"):
                errs.append(f"evidence 引用无效: {e}")
    lim = a.get("limitations")
    if not isinstance(lim, list) or not (1 <= len(lim) <= 10):
        errs.append("limitations 需 1-10 条")
    advice = a.get("consultationAdvice") or {}
    if not isinstance(advice.get("recommended"), bool) or not advice.get("reason"):
        errs.append("consultationAdvice 不完整")
    outcome = "observations_available" if obs else "inconclusive"
    return errs, outcome


def do_analyze(req):
    cfg = req.get("configuration") or {}
    photos = req.get("photos") or []
    photo_desc = "、".join(f"{p.get('angle')}（photoId={p.get('id')}）" for p in photos)
    prompt = (
        "你是皮肤科照片辅助观察助手。仔细观察这组面部照片（" + photo_desc + "），"
        "只描述照片中肉眼可见的皮肤表面现象（如色素沉着、泛红、丘疹、痘印、表面不平整），不做疾病诊断、不开处方。"
        "只输出一个 JSON 对象，不要任何解释文字，结构如下："
        "{\"summary\": \"50-200字中文总结\", "
        "\"observations\": [{\"id\": \"obs-1\", \"code\": \"color_variation\", \"description\": \"中文描述，100字内\", "
        "\"region\": \"multiple\", \"certainty\": \"high\", "
        "\"evidence\": [{\"photoId\": \"上面的photoId\", \"angle\": \"该照片的角度\"}]}], "
        "\"limitations\": [\"照片观察的局限，至少1条\"], "
        "\"consultationAdvice\": {\"recommended\": true, \"reason\": \"是否建议到院面诊及原因\", \"preparation\": [\"面诊前准备事项\"]}}。"
        "枚举约束（必须严格遵守）：code 只能是 surface_irregularity / color_variation / other_visible_change；"
        "region 只能是 forehead / left_cheek / right_cheek / nose / chin / multiple / unknown；"
        "certainty 只能是 low / medium / high / unknown。"
        "observations 1-3 条，evidence 必须引用给定的 photoId 且 angle 与该照片一致。"
        "若照片中确实没有可见异常，observations 输出空数组。"
    )
    content = [{"type": "text", "text": prompt}]
    for p in photos:
        content.append(img_part(p["jpegBase64"]))
    last_err = ""
    for attempt in range(2):
        msgs = [{"role": "user", "content": content}]
        if attempt == 1 and last_err:
            msgs.append({"role": "user", "content": "上次输出不符合要求：" + last_err + "。请修正后重新只输出 JSON。"})
        raw, req_id = chat(msgs, max_tokens=1800)
        try:
            a = extract_json(raw)
        except Exception:
            last_err = "输出不是合法 JSON"
            continue
        errs, outcome = validate_analysis(a, photos)
        if errs:
            last_err = "；".join(errs)
            continue
        a["schemaVersion"] = "assessment-result-v1"
        a["model"] = cfg.get("model") or {}
        a["outcome"] = outcome
        a["requiresHumanReview"] = True
        return {"analysis": a, "providerRequestId": req_id or "relay"}
    raise RuntimeError("model output invalid after retry: " + last_err)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        if self.path == "/healthz":
            body = b'{"status":"ok"}'
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        try:
            length = int(self.headers.get("Content-Length") or 0)
            req = json.loads(self.rfile.read(length) or b"{}")
            if self.path == "/quality":
                payload = do_quality(req)
            elif self.path == "/analyze":
                payload = do_analyze(req)
            else:
                self.send_response(404)
                self.end_headers()
                return
            body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        except Exception as exc:
            body = json.dumps({"error": str(exc)[:200]}).encode()
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", GATEWAY_PORT), Handler).serve_forever()
