package httpapi

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"

	"example.com/bailiankai/api/internal/store"
)

func assessmentOrientation(data []byte) (int, error) {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1, nil
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xff {
			return 0, store.ErrAssessmentInput
		}
		marker := data[pos+1]
		pos += 2
		if marker == 0xda || marker == 0xd9 {
			return 1, nil
		}
		if marker == 0xff {
			pos--
			continue
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if pos+2 > len(data) {
			return 0, store.ErrAssessmentInput
		}
		length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if length < 2 || pos+length > len(data) {
			return 0, store.ErrAssessmentInput
		}
		segment := data[pos+2 : pos+length]
		pos += length
		if marker != 0xe1 || !bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			continue
		}
		tiff := segment[6:]
		if len(tiff) < 8 {
			return 0, store.ErrAssessmentInput
		}
		var order binary.ByteOrder
		switch string(tiff[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			return 0, store.ErrAssessmentInput
		}
		if order.Uint16(tiff[2:4]) != 42 {
			return 0, store.ErrAssessmentInput
		}
		offset := int(order.Uint32(tiff[4:8]))
		if offset < 8 || offset+2 > len(tiff) {
			return 0, store.ErrAssessmentInput
		}
		count := int(order.Uint16(tiff[offset : offset+2]))
		if count > 1024 || offset+2+count*12 > len(tiff) {
			return 0, store.ErrAssessmentInput
		}
		for n := 0; n < count; n++ {
			entry := tiff[offset+2+n*12 : offset+2+(n+1)*12]
			if order.Uint16(entry[:2]) != 0x112 {
				continue
			}
			if order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
				return 0, store.ErrAssessmentInput
			}
			orientation := int(order.Uint16(entry[8:10]))
			if orientation < 1 || orientation > 8 {
				return 0, store.ErrAssessmentInput
			}
			return orientation, nil
		}
		return 1, nil
	}
	return 1, nil
}
func normalizeOrientedAssessmentPhoto(raw []byte) ([]byte, error) {
	orientation, err := assessmentOrientation(raw)
	if err != nil {
		return nil, err
	}
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	canvas := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx, sy := x, y
			switch orientation {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			r, g, b, a := source.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
			canvas.SetRGBA(x, y, color.RGBA{R: uint8((r + 65535 - a) >> 8), G: uint8((g + 65535 - a) >> 8), B: uint8((b + 65535 - a) >> 8), A: 255})
		}
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, canvas, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}
