package httpapi

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestAssessmentImageBoundariesOrientationAndMetadata(t *testing.T) {
	for _, size := range []image.Point{{511, 512}, {512, 511}, {4097, 512}, {512, 4097}} {
		t.Run(size.String(), func(t *testing.T) {
			var data bytes.Buffer
			if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, size.X, size.Y))); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := normalizeAssessmentPhoto(data.Bytes()); err == nil {
				t.Fatal("invalid dimensions accepted")
			}
		})
	}
	if _, _, _, err := normalizeAssessmentPhoto([]byte("not an image")); err == nil {
		t.Fatal("corrupt encoding accepted")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 512, 640))
	for y := 0; y < 640; y++ {
		for x := 0; x < 512; x++ {
			canvas.Set(x, y, color.RGBA{R: 220, G: 50, B: 30, A: 255})
		}
	}
	var original bytes.Buffer
	if err := jpeg.Encode(&original, canvas, nil); err != nil {
		t.Fatal(err)
	}
	for orientation := 1; orientation <= 8; orientation++ {
		segment := make([]byte, 32)
		copy(segment, []byte("Exif\x00\x00II"))
		binary.LittleEndian.PutUint16(segment[8:10], 42)
		binary.LittleEndian.PutUint32(segment[10:14], 8)
		binary.LittleEndian.PutUint16(segment[14:16], 1)
		binary.LittleEndian.PutUint16(segment[16:18], 0x112)
		binary.LittleEndian.PutUint16(segment[18:20], 3)
		binary.LittleEndian.PutUint32(segment[20:24], 1)
		binary.LittleEndian.PutUint16(segment[24:26], uint16(orientation))
		marker := []byte{0xff, 0xe1, 0, byte(len(segment) + 2)}
		raw := append([]byte{}, original.Bytes()[:2]...)
		raw = append(raw, marker...)
		raw = append(raw, segment...)
		raw = append(raw, original.Bytes()[2:]...)
		normalized, w, h, err := normalizeAssessmentPhoto(raw)
		if err != nil {
			t.Fatal(orientation, err)
		}
		if orientation >= 5 {
			if w != 640 || h != 512 {
				t.Fatal(w, h)
			}
		} else if w != 512 || h != 640 {
			t.Fatal(w, h)
		}
		if bytes.Contains(normalized, []byte("Exif")) {
			t.Fatal("metadata retained")
		}
		if got, err := assessmentOrientation(raw); err != nil || got != orientation {
			t.Fatal(got, err)
		}
	}
	var transparent bytes.Buffer
	png.Encode(&transparent, image.NewNRGBA(image.Rect(0, 0, 512, 512)))
	normalized, _, _, err := normalizeAssessmentPhoto(transparent.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(normalized))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decoded.At(0, 0).RGBA()
	if r < 65000 || g < 65000 || b < 65000 {
		t.Fatal("transparent background not flattened")
	}
}
