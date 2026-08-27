package loop

import (
	"context"
	"sync"
	"time"

	"github.com/rapatel0/alpha/internal/debuglog"
)

// waker starts a turn with text the user did not type. It matches
// ext.Host.Wake, which is what the shell installs.
type waker func(text string) error

// clock is the time source. Tests substitute one so a daily schedule does not
// need a day to verify.
type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// defaultTick is how often the scheduler looks for due loops.
//
// Polling is used instead of one timer per loop because loops are few and a
// poll cannot leak a goroutine when a loop is deleted mid-wait. Half a minute
// keeps a cron loop within its promised minute.
const defaultTick = 30 * time.Second

// Scheduler fires due loops.
type Scheduler struct {
	store *Store
	wake  waker
	clock clock

	// tickEvery is the poll period. Tests shorten it so a background fire
	// does not take half a minute to observe.
	tickEvery time.Duration

	mu      sync.Mutex
	running bool
	stop    context.CancelFunc
	done    chan struct{}
}

// NewScheduler wires a store to a wake function.
func NewScheduler(store *Store, wake waker) *Scheduler {
	return &Scheduler{store: store, wake: wake, clock: realClock{}, tickEvery: defaultTick}
}

// Start begins firing in the background. Calling it twice is a no-op, so a
// second extension load cannot double-fire every loop.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	if s.tickEvery <= 0 {
		s.tickEvery = defaultTick
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.running, s.stop, s.done = true, cancel, make(chan struct{})
	go s.run(runCtx, s.done, s.tickEvery)
}

// Stop halts firing and waits for the loop to exit.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.running, s.stop, s.done = false, nil, nil
	s.mu.Unlock()

	if stop == nil {
		return
	}
	stop()
	<-done
}

func (s *Scheduler) run(ctx context.Context, done chan struct{}, every time.Duration) {
	defer close(done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.fireDue(ctx)
		}
	}
}

// fireDue wakes the agent for every loop whose time has come.
//
// One prompt per fire, in order. Batching them into a single turn would blur
// which loop asked for what, and the agent cannot report on a loop it cannot
// name.
func (s *Scheduler) fireDue(ctx context.Context) {
	now := s.clock.Now()
	for _, l := range s.store.Due(now) {
		if ctx.Err() != nil {
			return
		}
		err := s.wake(l.Prompt)
		if err != nil {
			debuglog.Logf("loop: %s could not wake the agent: %v", l.ID, err)
		}
		if _, recErr := s.store.RecordFire(l.ID, now, err); recErr != nil {
			debuglog.Logf("loop: %s: %v", l.ID, recErr)
		}
	}
}
