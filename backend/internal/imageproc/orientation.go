package imageproc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

// Only the inline IFD0 orientation is read. All metadata is discarded on encode.
func jpegOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xff {
			return 1
		}
		pos++
		for pos < len(data) && data[pos] == 0xff {
			pos++
		}
		if pos >= len(data) {
			return 1
		}
		marker := data[pos]
		pos++
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if pos+2 > len(data) {
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if size < 2 || size > len(data)-pos {
			return 1
		}
		segment := data[pos+2 : pos+size]
		pos += size
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(t[2:4]) != 42 {
		return 1
	}
	offset := uint64(order.Uint32(t[4:8]))
	if offset < 8 || offset+2 > uint64(len(t)) {
		return 1
	}
	n := int(order.Uint16(t[offset : offset+2]))
	start := int(offset) + 2
	if n > (len(t)-start)/12 {
		return 1
	}
	for i := 0; i < n; i++ {
		e := t[start+i*12 : start+(i+1)*12]
		if order.Uint16(e[:2]) != 0x112 {
			continue
		}
		if order.Uint16(e[2:4]) != 3 || order.Uint32(e[4:8]) != 1 {
			return 1
		}
		v := int(order.Uint16(e[8:10]))
		if v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// orientedImage maps output coordinates to source without an extra pixel buffer.
type orientedImage struct {
	image.Image
	orientation int
}

func (o orientedImage) Bounds() image.Rectangle {
	b := o.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	if o.orientation >= 5 {
		w, h = h, w
	}
	return image.Rect(0, 0, w, h)
}
func (o orientedImage) At(x, y int) color.Color {
	b := o.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	switch o.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return o.Image.At(x+b.Min.X, y+b.Min.Y)
}
