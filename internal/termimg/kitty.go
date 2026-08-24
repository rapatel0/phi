package termimg

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/media"
)

const (
	chunkSize = 4096
	maxCols   = 48
	maxRows   = 16
	minCols   = 8
	minRows   = 3
)

// Placement is one image box in screen cells (0-based).
type Placement struct {
	X, Y       int
	Cols, Rows int
	Image      llm.Image
}

type painter struct {
	mu     sync.Mutex
	nextID uint32
	ids    map[[32]byte]uint32
	sent   map[uint32]bool
}

var defaultPainter = &painter{
	nextID: 1,
	ids:    make(map[[32]byte]uint32),
	sent:   make(map[uint32]bool),
}

// CellSize picks a cell box that fits maxCols while keeping aspect ratio.
// Terminal cells are treated as ~2:1 (taller than wide).
func CellSize(img llm.Image, maxAvail int) (cols, rows int) {
	pw, ph := media.PixelSize(img.Data)
	if pw < 1 {
		pw = 1
	}
	if ph < 1 {
		ph = 1
	}
	cols = maxAvail
	if cols > maxCols {
		cols = maxCols
	}
	if cols < minCols {
		cols = min(minCols, max(maxAvail, 1))
	}
	rows = int(float64(ph)/float64(pw)*float64(cols)*0.5 + 0.5)
	if rows < minRows {
		rows = minRows
	}
	if rows > maxRows {
		rows = maxRows
	}
	return cols, rows
}

// Paint overlays Kitty graphics after the cell grid has been written.
func Paint(vx *xui.XUI, places []Placement) {
	if vx == nil || !Supported() {
		return
	}
	var b []byte
	b = append(b, "\x1b_Ga=d,d=a,q=2\x1b\\"...)
	for _, p := range places {
		if p.Cols < 1 || p.Rows < 1 || len(p.Image.Data) == 0 {
			continue
		}
		id, first := defaultPainter.idFor(p.Image.Data)
		if first {
			b = appendTransmit(b, id, p.Image)
		}
		b = fmt.Appendf(b, "\x1b[%d;%dH\x1b_Ga=p,i=%d,c=%d,r=%d,C=1,q=2\x1b\\", p.Y+1, p.X+1, id, p.Cols, p.Rows)
	}
	_, _ = vx.WriteRaw(b)
}

func (p *painter) idFor(data []byte) (id uint32, first bool) {
	sum := sha256.Sum256(data)
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := p.ids[sum]; ok {
		return id, !p.sent[id]
	}
	id = p.nextID
	p.nextID++
	if p.nextID == 0 {
		p.nextID = 1
	}
	p.ids[sum] = id
	p.sent[id] = true
	return id, true
}

func appendTransmit(dst []byte, id uint32, img llm.Image) []byte {
	format := 100 // PNG
	if img.MIME == "image/jpeg" {
		format = 80
	}
	payload := base64.StdEncoding.EncodeToString(img.Data)
	for i := 0; i < len(payload); i += chunkSize {
		end := i + chunkSize
		if end > len(payload) {
			end = len(payload)
		}
		more := 0
		if end < len(payload) {
			more = 1
		}
		if i == 0 {
			dst = fmt.Appendf(dst, "\x1b_Ga=t,f=%d,i=%d,m=%d,q=2;%s\x1b\\", format, id, more, payload[i:end])
		} else {
			dst = fmt.Appendf(dst, "\x1b_Gm=%d;%s\x1b\\", more, payload[i:end])
		}
	}
	return dst
}

// EncodeTransmit is exported for tests.
func EncodeTransmit(id uint32, img llm.Image) string {
	return string(appendTransmit(nil, id, img))
}
