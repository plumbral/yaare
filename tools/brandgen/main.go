package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

type manifest struct {
	Source    string            `json:"source"`
	Generator string            `json:"generator"`
	Files     map[string]string `json:"files"`
}

func digest(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func scaled(source image.Image, size int, invert bool) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(out, out.Bounds(), source, source.Bounds(), draw.Src, nil)
	if invert {
		for p := 0; p < len(out.Pix); p += 4 {
			out.Pix[p], out.Pix[p+1], out.Pix[p+2] = 255-out.Pix[p], 255-out.Pix[p+1], 255-out.Pix[p+2]
		}
	}
	return out
}
func nativeIcon(source image.Image) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	stddraw.Draw(out, out.Bounds(), image.NewUniform(color.Black), image.Point{}, stddraw.Src)
	mark := scaled(source, 1024, true)
	stddraw.Draw(out, out.Bounds(), mark, image.Point{}, stddraw.Over)
	for y := 0; y < 1024; y++ {
		for x := 0; x < 1024; x++ {
			dx := math.Max(math.Abs(float64(x)+0.5-512)-320, 0)
			dy := math.Max(math.Abs(float64(y)+0.5-512)-320, 0)
			coverage := math.Max(0, math.Min(1, 160.5-math.Hypot(dx, dy)))
			out.Pix[out.PixOffset(x, y)+3] = uint8(math.Round(coverage * 255))
		}
	}
	return out
}
func pngBytes(source image.Image) ([]byte, error) {
	var data bytes.Buffer
	err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&data, source)
	return data.Bytes(), err
}
func icoBytes(source image.Image) ([]byte, error) {
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 128, 256}
	var header, payload bytes.Buffer
	for _, v := range []uint16{0, 1, uint16(len(sizes))} {
		_ = binary.Write(&header, binary.LittleEndian, v)
	}
	offset := 6 + 16*len(sizes)
	for _, size := range sizes {
		pngData, err := pngBytes(scaled(source, size, false))
		if err != nil {
			return nil, err
		}
		_ = header.WriteByte(byte(size % 256))
		_ = header.WriteByte(byte(size % 256))
		_ = header.WriteByte(0)
		_ = header.WriteByte(0)
		for _, v := range []uint16{1, 32} {
			_ = binary.Write(&header, binary.LittleEndian, v)
		}
		for _, v := range []uint32{uint32(len(pngData)), uint32(offset)} {
			_ = binary.Write(&header, binary.LittleEndian, v)
		}
		_, _ = payload.Write(pngData)
		offset += len(pngData)
	}
	return append(header.Bytes(), payload.Bytes()...), nil
}
func generate() error {
	const sourcePath = "docs/Yaare_logo.png"
	const cachePath = "build/branding/portable-assets.json"
	m := manifest{Source: digest(sourcePath), Generator: digest("tools/brandgen/main.go"), Files: map[string]string{}}
	if m.Source == "" || m.Generator == "" {
		return fmt.Errorf("run brandgen from the Yaare project root")
	}
	if data, err := os.ReadFile(cachePath); err == nil {
		var previous manifest
		if json.Unmarshal(data, &previous) == nil && previous.Source == m.Source && previous.Generator == m.Generator && len(previous.Files) == 7 {
			valid := true
			for path, hash := range previous.Files {
				if digest(path) != hash {
					valid = false
					break
				}
			}
			if valid {
				fmt.Println("Yaare brand assets are up to date.")
				return nil
			}
		}
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	source, err := png.Decode(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	if source.Bounds().Dx() != source.Bounds().Dy() {
		return fmt.Errorf("Yaare logo must be square")
	}
	icon := nativeIcon(source)
	images := map[string]image.Image{
		"resources/icon.png":                             icon,
		"frontend/src/shared/branding/yaare-app-32.png":  scaled(icon, 32, false),
		"frontend/src/shared/branding/yaare-app-128.png": scaled(icon, 128, false),
	}
	for _, size := range []int{32, 64, 128} {
		images[fmt.Sprintf("frontend/src/shared/branding/yaare-%d.png", size)] = scaled(source, size, false)
	}
	write := func(path string, data []byte) error {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
		m.Files[path] = digest(path)
		return nil
	}
	for path, img := range images {
		data, err := pngBytes(img)
		if err != nil {
			return err
		}
		if err = write(path, data); err != nil {
			return err
		}
	}
	ico, err := icoBytes(icon)
	if err != nil {
		return err
	}
	if err = write("docs/branding/Yaare.ico", ico); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return err
	}
	if err = os.WriteFile(cachePath, append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Println("Generated portable Yaare marks, fixed OS icon, favicon and multi-resolution ICO.")
	return nil
}
func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
