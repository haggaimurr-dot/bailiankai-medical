#!/usr/bin/env python3
"""Unit tests for the per-check retake advice in relay_gateway.py.

Run: python3 services/relay-gateway/test_retake_advice.py
No network, no key needed.
"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from relay_gateway import retake_advice_for


def check(code, status="fail"):
    return {"code": code, "status": status, "score": 0.0, "threshold": 0.5, "message": ""}


class RetakeAdviceTest(unittest.TestCase):
    def test_blurry_photo_gets_clarity_advice_not_lighting(self):
        advice = retake_advice_for([check("encoding", "pass"), check("dimensions", "pass"),
                                    check("face", "pass"), check("clarity"),
                                    check("exposure", "pass"), check("occlusion", "pass"),
                                    check("angle", "pass")])
        self.assertEqual(len(advice), 1)
        self.assertIn("不够清晰", advice[0])
        self.assertIn("稳定", advice[0])
        self.assertNotIn("光线均匀", advice[0])

    def test_dark_photo_gets_exposure_advice(self):
        advice = retake_advice_for([check("clarity", "pass"), check("exposure"),
                                    check("occlusion", "pass")])
        self.assertEqual(len(advice), 1)
        self.assertIn("曝光异常", advice[0])

    def test_multiple_failures_each_get_their_own_tip(self):
        advice = retake_advice_for([check("face"), check("occlusion"), check("angle")])
        self.assertEqual(len(advice), 3)
        self.assertIn("人脸", advice[0])
        self.assertIn("遮挡", advice[1])
        self.assertIn("角度", advice[2])

    def test_each_code_has_advice(self):
        for code in ["encoding", "dimensions", "face", "clarity", "exposure", "occlusion", "angle"]:
            with self.subTest(code=code):
                advice = retake_advice_for([check(code)])
                self.assertEqual(len(advice), 1)
                self.assertTrue(advice[0])

    def test_all_pass_gives_no_advice(self):
        advice = retake_advice_for([check(c, "pass") for c in
                                    ["encoding", "dimensions", "face", "clarity",
                                     "exposure", "occlusion", "angle"]])
        self.assertEqual(advice, [])

    def test_unknown_code_falls_back_to_generic(self):
        self.assertEqual(retake_advice_for([check("something-else")]),
                         ["请在光线均匀处重新拍摄该角度照片"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
