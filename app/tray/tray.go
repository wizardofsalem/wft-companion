package tray

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"unicode/utf16"

	"github.com/wizardofsalem/wft-companion/app/branding"
)

var ErrUnsupported = errors.New("tray: not supported on this system")

type Item struct {
	Label string
	Do    func() string
}

type Menu struct {
	Tooltip   string
	Items     []Item
	Default   int
	State     func() State
	AlertItem Item
}

type State struct {
	Tooltip string
	Alert   bool
	Notice  string
}

type watch struct {
	started bool
	tip     string
	alert   bool
}

func (w *watch) next(st State) (update bool, notice string) {
	if !w.started {
		w.started, w.tip, w.alert = true, st.Tooltip, st.Alert
		return true, ""
	}
	update = st.Tooltip != w.tip || st.Alert != w.alert
	if st.Alert && !w.alert {
		notice = st.Notice
	}
	w.tip, w.alert = st.Tooltip, st.Alert
	return update, notice
}

func AlertPNG(size int) []byte {
	src, err := png.Decode(bytes.NewReader(IconPNG(size)))
	if err != nil {
		return IconPNG(size)
	}
	b := src.Bounds()
	img := image.NewNRGBA(b)
	draw.Draw(img, b, src, b.Min, draw.Src)
	r := float64(b.Dx()) * 0.25
	cx, cy := float64(b.Max.X)-r, float64(b.Max.Y)-r
	red := color.NRGBA{R: 0xd9, G: 0x2d, B: 0x20, A: 0xff}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, red)
			}
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return IconPNG(size)
	}
	return out.Bytes()
}

func IconPNG(size int) []byte { return branding.PNG(size) }

func copyUTF16(dst []uint16, s string) {
	if len(dst) == 0 {
		return
	}
	u := utf16.Encode([]rune(s))
	if len(u) > len(dst)-1 {
		u = u[:len(dst)-1]
		if n := len(u); n > 0 && utf16.IsSurrogate(rune(u[n-1])) && u[n-1] < 0xdc00 {
			u = u[:n-1]
		}
	}
	n := copy(dst, u)
	dst[n] = 0
}
