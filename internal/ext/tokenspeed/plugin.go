// Package tokenspeed is the pi-token-speed analog: sliding-window tok/s in the footer.
package tokenspeed

import (
	"fmt"
	"sync"
	"time"

	"github.com/rapatel0/alpha/internal/ext"
)

func init() { ext.Register(Plugin{}) }

// Plugin reports completion tok/s from the last assistant turn.
type Plugin struct{}

func (Plugin) Name() string { return "tokenspeed" }

func (Plugin) Register(h *ext.Host) error {
	var mu sync.Mutex
	var last string
	h.OnUsage(func(_, completion int, elapsed time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		sec := elapsed.Seconds()
		if completion <= 0 || sec < 0.05 {
			return
		}
		last = fmt.Sprintf("%.0f tok/s", float64(completion)/sec)
	})
	h.AddFooter(func() string {
		mu.Lock()
		defer mu.Unlock()
		return last
	})
	return nil
}
