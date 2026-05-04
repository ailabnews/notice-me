// internal/dispatcher/dispatcher.go
package dispatcher

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var ErrQueueFull = errors.New("queue full")

type Options struct {
	QueueSize int
	// OnActive is invoked synchronously when a notification becomes the active
	// one. Implementations should NOT block — typically emit a Wails event then
	// return immediately.
	OnActive func(*Notification)
}

type Dispatcher struct {
	opts     Options
	queue    chan *Notification
	mu       sync.Mutex
	inFlight map[int64]*Notification
	paused   atomic.Bool
}

func New(opts Options) *Dispatcher {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 100
	}
	return &Dispatcher{
		opts:     opts,
		queue:    make(chan *Notification, opts.QueueSize),
		inFlight: map[int64]*Notification{},
	}
}

// Pause prevents the HTTP server from enqueuing new notifications.
// IMPORTANT: this is advisory. Honored by HTTP server before Enqueue;
// the dispatcher itself does not check it. Already-queued items continue.
func (d *Dispatcher) Pause()         { d.paused.Store(true) }
func (d *Dispatcher) Resume()        { d.paused.Store(false) }
func (d *Dispatcher) IsPaused() bool { return d.paused.Load() }

func (d *Dispatcher) Enqueue(n *Notification) error {
	select {
	case d.queue <- n:
		d.mu.Lock()
		d.inFlight[n.ID] = n
		d.mu.Unlock()
		return nil
	default:
		return ErrQueueFull
	}
}

func (d *Dispatcher) Resolve(id int64, r Result) {
	d.mu.Lock()
	n, ok := d.inFlight[id]
	if ok {
		delete(d.inFlight, id)
	}
	d.mu.Unlock()
	if ok {
		n.Resolve(r)
	}
}

func (d *Dispatcher) Cancel(id int64) {
	d.Resolve(id, Result{Decision: "cancelled", Reason: "cancelled"})
}

func (d *Dispatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.drainOnShutdown()
			return
		case n := <-d.queue:
			// Skip if cancelled while waiting in queue.
			d.mu.Lock()
			_, alive := d.inFlight[n.ID]
			d.mu.Unlock()
			if !alive {
				continue
			}
			d.run(ctx, n)
		}
	}
}

func (d *Dispatcher) drainOnShutdown() {
	d.mu.Lock()
	pending := make([]*Notification, 0, len(d.inFlight))
	for _, n := range d.inFlight {
		pending = append(pending, n)
	}
	d.inFlight = map[int64]*Notification{}
	d.mu.Unlock()
	for _, n := range pending {
		n.Resolve(Result{Decision: "cancelled", Reason: "shutdown"})
	}
}

func (d *Dispatcher) run(ctx context.Context, n *Notification) {
	d.opts.OnActive(n)

	var doneCh <-chan struct{}
	if n.Done != nil {
		doneCh = n.Done
	}

	// Timeout == 0 means wait indefinitely for user action.
	if n.Timeout > 0 {
		timer := time.NewTimer(n.Timeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			d.Resolve(n.ID, Result{Decision: "cancelled", Reason: "shutdown"})
		case <-timer.C:
			decision := "timeout"
			if n.TimeoutAct == "denied" {
				decision = "denied"
			}
			d.Resolve(n.ID, Result{Decision: decision, Reason: "timeout"})
		case <-doneCh:
		}
	} else {
		select {
		case <-ctx.Done():
			d.Resolve(n.ID, Result{Decision: "cancelled", Reason: "shutdown"})
		case <-doneCh:
		}
	}
}
