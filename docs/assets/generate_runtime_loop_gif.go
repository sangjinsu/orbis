//go:build ignore

package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"math"
	"os"
)

type node struct {
	label string
	x     int
	y     int
	w     int
	h     int
	fill  uint8
}

var palette = []color.Color{
	color.RGBA{10, 14, 24, 255},    // 0 background
	color.RGBA{22, 28, 44, 255},    // 1 panel
	color.RGBA{55, 65, 90, 255},    // 2 line
	color.RGBA{235, 244, 255, 255}, // 3 text
	color.RGBA{36, 211, 194, 255},  // 4 cyan
	color.RGBA{250, 183, 80, 255},  // 5 amber
	color.RGBA{171, 118, 255, 255}, // 6 violet
	color.RGBA{250, 104, 91, 255},  // 7 red
	color.RGBA{80, 226, 128, 255},  // 8 green
	color.RGBA{255, 255, 255, 255}, // 9 white
}

var nodes = []node{
	{"CLIENT", 34, 66, 96, 42, 4},
	{"GATEWAY", 170, 66, 112, 42, 5},
	{"QUEUE", 322, 66, 96, 42, 6},
	{"LANE", 458, 66, 92, 42, 6},
	{"REDUCER", 590, 66, 116, 42, 7},
	{"DISPATCH", 746, 66, 122, 42, 5},
	{"WORKER", 908, 66, 108, 42, 4},
	{"BROKER", 594, 188, 108, 42, 8},
	{"DONE", 908, 188, 108, 42, 8},
}

var path = []int{0, 1, 2, 3, 4, 5, 6, 2, 3, 4, 7, 8}

func main() {
	if err := os.MkdirAll("docs/assets", 0o755); err != nil {
		panic(err)
	}

	out := &gif.GIF{}
	for frame := 0; frame < 36; frame++ {
		img := image.NewPaletted(image.Rect(0, 0, 1080, 320), palette)
		drawFrame(img, frame)
		out.Image = append(out.Image, img)
		out.Delay = append(out.Delay, 8)
	}

	file, err := os.Create("docs/assets/orbis-runtime-loop.gif")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if err := gif.EncodeAll(file, out); err != nil {
		panic(err)
	}
}

func drawFrame(img *image.Paletted, frame int) {
	fill(img, img.Bounds(), 0)
	rect(img, 18, 18, 1044, 284, 1)
	stroke(img, 18, 18, 1044, 284, 2)
	text(img, 34, 34, "ORBIS RUNTIME LOOP", 3)
	text(img, 34, 252, "PROMPT -> ACK -> EVENT -> ACTION -> RESULT EVENT -> FINAL", 2)

	activeStep := frame * (len(path) - 1) / 35
	for i := 0; i < len(path)-1; i++ {
		a := nodes[path[i]]
		b := nodes[path[i+1]]
		colorIndex := uint8(2)
		if i <= activeStep {
			colorIndex = 4
		}
		line(img, centerX(a), centerY(a), centerX(b), centerY(b), colorIndex)
	}

	for i, n := range nodes {
		fillIndex := uint8(1)
		border := uint8(2)
		if i == path[activeStep] {
			fillIndex = n.fill
			border = 9
		}
		rect(img, n.x, n.y, n.w, n.h, fillIndex)
		stroke(img, n.x, n.y, n.w, n.h, border)
		text(img, n.x+10, n.y+17, n.label, 9)
	}

	a := nodes[path[activeStep]]
	b := nodes[path[min(activeStep+1, len(path)-1)]]
	t := float64((frame*100)%300) / 300
	if activeStep == len(path)-1 {
		t = 1
	}
	x := int(float64(centerX(a)) + (float64(centerX(b))-float64(centerX(a)))*t)
	y := int(float64(centerY(a)) + (float64(centerY(b))-float64(centerY(a)))*t)
	circle(img, x, y, 11, 9)
	circle(img, x, y, 7, 4)
}

func centerX(n node) int { return n.x + n.w/2 }
func centerY(n node) int { return n.y + n.h/2 }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fill(img *image.Paletted, r image.Rectangle, c uint8) {
	draw.Draw(img, r, &image.Uniform{palette[c]}, image.Point{}, draw.Src)
}

func rect(img *image.Paletted, x, y, w, h int, c uint8) {
	fill(img, image.Rect(x, y, x+w, y+h), c)
}

func stroke(img *image.Paletted, x, y, w, h int, c uint8) {
	for i := 0; i < 2; i++ {
		line(img, x+i, y+i, x+w-i, y+i, c)
		line(img, x+i, y+h-i, x+w-i, y+h-i, c)
		line(img, x+i, y+i, x+i, y+h-i, c)
		line(img, x+w-i, y+i, x+w-i, y+h-i, c)
	}
}

func line(img *image.Paletted, x0, y0, x1, y1 int, c uint8) {
	dx := math.Abs(float64(x1 - x0))
	dy := -math.Abs(float64(y1 - y0))
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		setThick(img, x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func setThick(img *image.Paletted, x, y int, c uint8) {
	for yy := -1; yy <= 1; yy++ {
		for xx := -1; xx <= 1; xx++ {
			p := image.Pt(x+xx, y+yy)
			if p.In(img.Bounds()) {
				img.SetColorIndex(p.X, p.Y, c)
			}
		}
	}
}

func circle(img *image.Paletted, cx, cy, r int, c uint8) {
	rr := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= rr && image.Pt(x, y).In(img.Bounds()) {
				img.SetColorIndex(x, y, c)
			}
		}
	}
}

var font = map[rune][]string{
	' ': {"000", "000", "000", "000", "000", "000", "000"},
	'-': {"00000", "00000", "00000", "11111", "00000", "00000", "00000"},
	'>': {"10000", "01000", "00100", "00010", "00100", "01000", "10000"},
	'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
	'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"},
	'C': {"01111", "10000", "10000", "10000", "10000", "10000", "01111"},
	'D': {"11110", "10001", "10001", "10001", "10001", "10001", "11110"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'F': {"11111", "10000", "10000", "11110", "10000", "10000", "10000"},
	'G': {"01111", "10000", "10000", "10011", "10001", "10001", "01110"},
	'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
	'I': {"11111", "00100", "00100", "00100", "00100", "00100", "11111"},
	'K': {"10001", "10010", "10100", "11000", "10100", "10010", "10001"},
	'L': {"10000", "10000", "10000", "10000", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10011", "10001", "10001", "10001"},
	'O': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
	'P': {"11110", "10001", "10001", "11110", "10000", "10000", "10000"},
	'Q': {"01110", "10001", "10001", "10001", "10101", "10010", "01101"},
	'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
	'S': {"01111", "10000", "10000", "01110", "00001", "00001", "11110"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'U': {"10001", "10001", "10001", "10001", "10001", "10001", "01110"},
	'V': {"10001", "10001", "10001", "10001", "10001", "01010", "00100"},
	'W': {"10001", "10001", "10001", "10101", "10101", "10101", "01010"},
	'Y': {"10001", "10001", "01010", "00100", "00100", "00100", "00100"},
}

func text(img *image.Paletted, x, y int, s string, c uint8) {
	cursor := x
	for _, r := range s {
		glyph, ok := font[r]
		if !ok {
			cursor += 8
			continue
		}
		for yy, row := range glyph {
			for xx, bit := range row {
				if bit == '1' {
					rect(img, cursor+xx*2, y+yy*2, 2, 2, c)
				}
			}
		}
		cursor += len(glyph[0])*2 + 4
	}
}
