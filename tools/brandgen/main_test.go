package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestPortableBrandTransparencyAndIconSizes(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 4; y < 12; y++ {
		for x := 4; x < 12; x++ {
			source.SetNRGBA(x, y, color.NRGBA{A: 255})
		}
	}
	mark := scaled(source, 32, false)
	if mark.NRGBAAt(0, 0).A != 0 || mark.NRGBAAt(16, 16) != (color.NRGBA{A: 255}) {
		t.Fatal("transparent black product mark changed")
	}
	icon := nativeIcon(source)
	if icon.NRGBAAt(0, 0).A != 0 || icon.NRGBAAt(50, 512) != (color.NRGBA{A: 255}) || icon.NRGBAAt(512, 512) != (color.NRGBA{255, 255, 255, 255}) {
		t.Fatal("fixed black tile or white mark invalid")
	}
	data, err := icoBytes(icon)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(data[2:]) != 1 || binary.LittleEndian.Uint16(data[4:]) != 9 {
		t.Fatal("ICO header invalid")
	}
	for index, want := range []int{16, 20, 24, 32, 40, 48, 64, 128, 256} {
		entry := data[6+16*index : 6+16*(index+1)]
		count, offset := binary.LittleEndian.Uint32(entry[8:]), binary.LittleEndian.Uint32(entry[12:])
		img, err := png.Decode(bytes.NewReader(data[offset : offset+count]))
		if err != nil || img.Bounds().Dx() != want || img.Bounds().Dy() != want {
			t.Fatalf("ICO size %d: %v", want, err)
		}
	}
}
