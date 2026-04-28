// internal/dispatcher/dispatcher_test.go
package dispatcher

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSerialOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var seen int32
	var order []int64
	var d *Dispatcher
	d = New(Options{
		QueueSize: 10,
		OnActive: func(n *Notification) {
			atomic.AddInt32(&seen, 1)
			order = append(order, n.ID)
			// Simulate user clicking after 5ms
			go func(n *Notification) {
				time.Sleep(5 * time.Millisecond)
				d.Resolve(n.ID, Result{Decision: "approved"})
			}(n)
		},
	})
	go d.Run(ctx)
	for i := int64(1); i <= 3; i++ {
		n := &Notification{ID: i, Mode: ModeTwoButton, ResultCh: make(chan Result, 1), Done: make(chan struct{}), Timeout: time.Second}
		if err := d.Enqueue(n); err != nil {
			t.Fatal(err)
		}
		<-n.ResultCh
	}
	if seen != 3 {
		t.Fatalf("seen=%d", seen)
	}
	if order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("bad order %v", order)
	}
}

func TestTimeoutFires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := New(Options{QueueSize: 4, OnActive: func(n *Notification) {}})
	go d.Run(ctx)
	n := &Notification{ID: 9, Mode: ModeTwoButton, ResultCh: make(chan Result, 1), Done: make(chan struct{}), Timeout: 20 * time.Millisecond}
	_ = d.Enqueue(n)
	res := <-n.ResultCh
	if res.Decision != "timeout" {
		t.Fatalf("expected timeout got %s", res.Decision)
	}
}

func TestCancelInQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	block := make(chan struct{})
	d := New(Options{QueueSize: 4, OnActive: func(n *Notification) { <-block }})
	go d.Run(ctx)
	a := &Notification{ID: 1, Mode: ModeTwoButton, ResultCh: make(chan Result, 1), Done: make(chan struct{}), Timeout: time.Hour}
	b := &Notification{ID: 2, Mode: ModeTwoButton, ResultCh: make(chan Result, 1), Done: make(chan struct{}), Timeout: time.Hour}
	_ = d.Enqueue(a)
	_ = d.Enqueue(b)
	time.Sleep(20 * time.Millisecond)
	d.Cancel(b.ID)
	res := <-b.ResultCh
	if res.Decision != "cancelled" {
		t.Fatalf("expected cancelled got %s", res.Decision)
	}
	close(block)
	d.Resolve(a.ID, Result{Decision: "approved"})
	<-a.ResultCh
}

func TestQueueFull(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	block := make(chan struct{})
	d := New(Options{QueueSize: 1, OnActive: func(n *Notification) { <-block }})
	go d.Run(ctx)
	_ = d.Enqueue(&Notification{ID: 1, ResultCh: make(chan Result, 1), Done: make(chan struct{}), Timeout: time.Hour})
	if err := d.Enqueue(&Notification{ID: 2, ResultCh: make(chan Result, 1), Done: make(chan struct{}), Timeout: time.Hour}); err != ErrQueueFull {
		t.Fatalf("want ErrQueueFull got %v", err)
	}
	close(block)
}

func TestCancelActiveReleasesWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	activated := make(chan int64, 4)
	aResult := make(chan Result, 1)

	// Spawn the HTTP-handler goroutine from inside OnActive *before* the worker
	// returns from OnActive and enters its select. Go's channel runtime delivers
	// to receivers in FIFO order — to expose the bug, the HTTP handler must be
	// parked on a.ResultCh BEFORE the worker parks on its select.
	var (
		a              *Notification
		httpHandlerSet = make(chan struct{})
	)
	d := New(Options{QueueSize: 4, OnActive: func(n *Notification) {
		activated <- n.ID
		if n.ID == 1 {
			go func() {
				close(httpHandlerSet) // signal: about to park on ResultCh
				aResult <- <-n.ResultCh
			}()
			// Give the goroutine time to actually park on ResultCh before
			// the worker returns from OnActive and parks on its own select.
			<-httpHandlerSet
			time.Sleep(20 * time.Millisecond)
		}
	}})
	go d.Run(ctx)

	// Notification A: enqueue, wait until active.
	a = &Notification{
		ID:       1,
		Mode:     ModeTwoButton,
		ResultCh: make(chan Result, 1),
		Done:     make(chan struct{}),
		Timeout:  time.Hour, // important: long timeout so worker WOULD park if bug persisted
	}
	if err := d.Enqueue(a); err != nil {
		t.Fatal(err)
	}
	if id := <-activated; id != 1 {
		t.Fatalf("first: %d", id)
	}
	// Extra slack: ensure the worker's select has been entered too, so that
	// Cancel triggers the contested handoff between worker and HTTP handler.
	time.Sleep(10 * time.Millisecond)

	d.Cancel(a.ID)

	// The HTTP-handler goroutine must receive cancelled.
	select {
	case res := <-aResult:
		if res.Decision != "cancelled" {
			t.Fatalf("a result: %s", res.Decision)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("HTTP handler never received cancel result")
	}

	// Worker must have released its slot. Enqueue B and verify it activates
	// within a short deadline (worker is NOT parked on the ancient timer).
	b := &Notification{
		ID:       2,
		Mode:     ModeTwoButton,
		ResultCh: make(chan Result, 1),
		Done:     make(chan struct{}),
		Timeout:  time.Hour,
	}
	if err := d.Enqueue(b); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-activated:
		if id != 2 {
			t.Fatalf("second: %d", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("worker stuck after cancel-of-active — would have been parked on the original bug")
	}
	d.Resolve(b.ID, Result{Decision: "approved"})
	<-b.ResultCh
}
