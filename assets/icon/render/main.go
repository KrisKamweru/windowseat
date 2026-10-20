// Command render draws the tray PNGs, state ICOs and favicon from windowseat.svg.
//
// Run it from the repository root with `go run ./assets/icon/render`.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
)

const (
	dir    = "assets/icon"
	accent = "#18B9AC"
)

var (
	pngSizes = []int{16, 32, 64, 256}
	icoSizes = []int{16, 32, 48}
	variants = map[string]string{
		"windowseat":    accent,
		"state-ok":      "#2480C2",
		"state-warning": "#D17A16",
		"state-stale":   "#8D989F",
		"state-error":   "#C74C72",
	}
)

func main() {
	source, err := os.ReadFile(filepath.Join(dir, "windowseat.svg"))
	if err != nil {
		log.Fatal(err)
	}
	for name, colour := range variants {
		svg := strings.ReplaceAll(string(source), accent, colour)
		for _, size := range pngSizes {
			writePNG(fmt.Sprintf("%s-%d.png", name, size), render(svg, size))
		}
		icoName := name + ".ico"
		if name == "windowseat" {
			icoName = "favicon.ico"
		}
		writeICO(icoName, svg)
	}
}

// render draws svg at its own viewBox size and scales the result down.
// oksvg does not scale stroke widths with the target, so drawing straight
// at a small size would leave the strokes at full viewBox width.
func render(svg string, size int) image.Image {
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg), oksvg.StrictErrorMode)
	if err != nil {
		log.Fatal(err)
	}
	native := int(icon.ViewBox.W)
	icon.SetTarget(0, 0, icon.ViewBox.W, icon.ViewBox.H)
	canvas := image.NewRGBA(image.Rect(0, 0, native, native))
	icon.Draw(rasterx.NewDasher(native, native, rasterx.NewScannerGV(native, native, canvas, canvas.Bounds())), 1)

	small := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(small, small.Bounds(), canvas, canvas.Bounds(), draw.Src, nil)
	return small
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

func writePNG(name string, img image.Image) {
	if err := os.WriteFile(filepath.Join(dir, name), encodePNG(img), 0o644); err != nil {
		log.Fatal(err)
	}
}

// writeICO stores one PNG per size, which Windows Vista and later read directly.
func writeICO(name, svg string) {
	var images [][]byte
	for _, size := range icoSizes {
		images = append(images, encodePNG(render(svg, size)))
	}

	var buf bytes.Buffer
	write := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	write([3]uint16{0, 1, uint16(len(images))})
	offset := 6 + 16*len(images)
	for i, data := range images {
		size := uint8(icoSizes[i])
		write([4]uint8{size, size, 0, 0})
		write([2]uint16{1, 32})
		write([2]uint32{uint32(len(data)), uint32(offset)})
		offset += len(data)
	}
	for _, data := range images {
		buf.Write(data)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}
