#!/usr/bin/env python3
"""Unit tests for the plain-language summary and care advice fields in
relay_gateway.py's analysis validation (P2 result page: plain version +
care advice zones, drafted by the AI and published only after doctor review).

Run: python3 services/relay-gateway/test_analysis_content.py
No network, no key needed.
"""
import copy
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from relay_gateway import validate_analysis

PHOTOS = [{"id": "photo-1", "angle": "front"}, {"id": "photo-2", "angle": "left"}]


def valid_analysis():
    return {
        "summary": "照片中可见面部皮肤表面有轻微色素沉着与不平整。",
        "observations": [{
            "id": "obs-1", "code": "color_variation",
            "description": "两颊可见轻微色素沉着",
            "region": "multiple", "certainty": "medium",
            "evidence": [{"photoId": "photo-1", "angle": "front"}],
        }],
        "limitations": ["照片光线略暗，部分细节难以判断"],
        "consultationAdvice": {
            "recommended": True,
            "reason": "建议到院面诊确认色素沉着的类型",
            "preparation": ["带好既往护肤品清单"],
        },
        "plainSummary": "从照片上看，脸颊两侧有一些淡淡的色素沉着，皮肤表面摸起来不太平整，这在日常护肤中比较常见，不必过于担心，具体情况还需要医生面诊确认。",
        "careAdvice": ["每天早晚用温水洗脸，动作轻柔", "白天外出注意防晒", "不要用手挤压或抠抓"],
    }


class AnalysisContentTest(unittest.TestCase):
    def test_valid_analysis_with_plain_summary_and_care_advice(self):
        errs, outcome = validate_analysis(valid_analysis(), PHOTOS)
        self.assertEqual(errs, [])
        self.assertEqual(outcome, "observations_available")

    def test_missing_plain_summary_fails(self):
        a = valid_analysis()
        del a["plainSummary"]
        errs, _ = validate_analysis(a, PHOTOS)
        self.assertTrue(any("plainSummary" in e for e in errs))

    def test_too_short_plain_summary_fails(self):
        a = valid_analysis()
        a["plainSummary"] = "太短"
        errs, _ = validate_analysis(a, PHOTOS)
        self.assertTrue(any("plainSummary" in e for e in errs))

    def test_missing_care_advice_fails(self):
        a = valid_analysis()
        del a["careAdvice"]
        errs, _ = validate_analysis(a, PHOTOS)
        self.assertTrue(any("careAdvice" in e for e in errs))

    def test_empty_care_advice_fails(self):
        a = valid_analysis()
        a["careAdvice"] = []
        errs, _ = validate_analysis(a, PHOTOS)
        self.assertTrue(any("careAdvice" in e for e in errs))

    def test_blank_care_advice_item_fails(self):
        a = valid_analysis()
        a["careAdvice"] = ["每天温水洗脸", "   "]
        errs, _ = validate_analysis(a, PHOTOS)
        self.assertTrue(any("careAdvice" in e for e in errs))

    def test_other_fields_still_validated(self):
        a = valid_analysis()
        a["summary"] = "短"
        errs, _ = validate_analysis(a, PHOTOS)
        self.assertTrue(any("summary" in e for e in errs))


if __name__ == "__main__":
    unittest.main()
