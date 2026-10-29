package store

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	xdraw "golang.org/x/image/draw"
)

// DisplayVariantSuffix is appended to a photo's media ID for its compressed,
// watermarked display copy. The original is never modified.
const DisplayVariantSuffix = "-display"

// displayMaxSide is the longest side of the display variant in pixels.
const displayMaxSide = 1280

// displayJPEGQuality balances size vs. visible quality for display copies.
const displayJPEGQuality = 72

var (
	watermarkFaceOnce sync.Once
	watermarkFace     font.Face
	watermarkFaceName string
)

func watermarkFontFace() (font.Face, string) {
	watermarkFaceOnce.Do(func() {
		path := os.Getenv("WATERMARK_FONT_PATH")
		if path == "" {
			path = "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc"
		}
		if data, err := os.ReadFile(path); err == nil {
			if coll, err := opentype.ParseCollection(data); err == nil {
				if sf, err := coll.Font(0); err == nil {
					if face, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: 28, DPI: 72}); err == nil {
						watermarkFace, watermarkFaceName = face, "wqy"
						return
					}
				}
			} else if f, err := opentype.Parse(data); err == nil {
				if face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 28, DPI: 72}); err == nil {
					watermarkFace, watermarkFaceName = face, "ttf"
					return
				}
			}
		}
		// Fallback: built-in ASCII bitmap font (no CJK glyphs).
		watermarkFace, watermarkFaceName = basicfont.Face7x13, "basic"
	})
	return watermarkFace, watermarkFaceName
}

func watermarkText() string {
	if _, name := watermarkFontFace(); name == "basic" {
		return "BALINCAN"
	}
	return "百莲凯医疗美容 · 展示图"
}

// GenerateDisplayVariant decodes a JPEG/PNG photo, downscales it so the
// longest side is at most displayMaxSide, overlays the clinic watermark, and
// re-encodes it as JPEG. The input (diagnostic original) is never modified.
func GenerateDisplayVariant(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Fall back to explicit JPEG/PNG decoders for a clearer error.
		if img2, err2 := jpeg.Decode(bytes.NewReader(data)); err2 == nil {
			img, err = img2, nil
		} else if img3, err3 := png.Decode(bytes.NewReader(data)); err3 == nil {
			img, err = img3, nil
		}
		if err != nil {
			return nil, err
		}
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := 1.0
	if m := max(w, h); m > displayMaxSide {
		scale = float64(displayMaxSide) / float64(m)
	}
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(w)*scale), int(float64(h)*scale)))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)

	// Watermark: semi-transparent band at the bottom with clinic text.
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	bandH := dh / 12
	if bandH < 24 {
		bandH = 24
	}
	band := image.NewRGBA(image.Rect(0, dh-bandH, dw, dh))
	draw.Draw(band, band.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 110}}, image.Point{}, draw.Over)
	draw.Draw(dst, band.Bounds(), band, image.Point{}, draw.Over)

	face, _ := watermarkFontFace()
	text := watermarkText()
	dr := &font.Drawer{Dst: dst, Src: image.NewUniform(color.RGBA{255, 255, 255, 235}), Face: face}
	tw := dr.MeasureString(text)
	// Vertically center the text in the band.
	metrics := face.Metrics()
	y := fixed.I(dh-bandH) + (fixed.I(bandH)+metrics.Ascent-metrics.Descent)/2
	dr.Dot = fixed.Point26_6{X: (fixed.I(dw) - tw) / 2, Y: y}
	dr.DrawString(text)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: displayJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SavePhotoWithVariant saves the original photo bytes and a compressed,
// watermarked display copy (<id>-display). The original is stored untouched;
// variant generation is best-effort so a watermark failure never blocks the
// upload itself.
func SavePhotoWithVariant(ctx context.Context, m Media, id string, data []byte) error {
	if err := m.Save(ctx, id, data); err != nil {
		return err
	}
	variant, err := GenerateDisplayVariant(data)
	if err != nil {
		slog.Warn("display variant skipped", "id", id, "error", err)
		return nil
	}
	if err := m.Save(ctx, id+DisplayVariantSuffix, variant); err != nil {
		slog.Warn("display variant save failed", "id", id, "error", err)
		return nil
	}
	return nil
}
