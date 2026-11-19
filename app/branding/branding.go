package branding

import (
	"bytes"
	"embed"
	"encoding/binary"
	"fmt"
)

//go:embed m2-*.png
var files embed.FS

var Sizes = []int{16, 20, 24, 32, 48, 64, 256}

func PNG(size int) []byte {
	pick := Sizes[len(Sizes)-1]
	for _, s := range Sizes {
		if s >= size {
			pick = s
			break
		}
	}
	b, err := files.ReadFile(fmt.Sprintf("m2-%d.png", pick))
	if err != nil {
		panic(err)
	}
	return b
}

type Image struct {
	Size int
	PNG  []byte
}

func Images() []Image {
	out := make([]Image, len(Sizes))
	for i, s := range Sizes {
		out[i] = Image{s, PNG(s)}
	}
	return out
}

func ICO() []byte {
	imgs := Images()
	var b bytes.Buffer
	le := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	le([3]uint16{0, 1, uint16(len(imgs))})
	off := 6 + 16*len(imgs)
	for _, im := range imgs {
		b.Write([]byte{dim(im.Size), dim(im.Size), 0, 0})
		le([2]uint16{1, 32})
		le([2]uint32{uint32(len(im.PNG)), uint32(off)})
		off += len(im.PNG)
	}
	for _, im := range imgs {
		b.Write(im.PNG)
	}
	return b.Bytes()
}

func dim(size int) byte {
	if size >= 256 {
		return 0
	}
	return byte(size)
}
