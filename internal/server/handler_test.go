package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"notify-me/internal/config"
	"notify-me/internal/dispatcher"
	"notify-me/internal/storage"
)

func ep() config.EndpointConfig {
	return config.EndpointConfig{Path: "confirm", Title: "T", OkText: "OK", CancelText: "Cancel", Mode: "two-button"}
}

func TestParsePlainText(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/confirm", strings.NewReader("hello"))
	n, err := parseRequest(r, ep(), 60)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "T" || n.Message != "hello" || n.OkText != "OK" {
		t.Fatalf("got %+v", n)
	}
	if n.ResultCh == nil || n.Done == nil {
		t.Fatalf("channels not initialised: ResultCh=%v Done=%v", n.ResultCh, n.Done)
	}
}

func TestParseHeadersOverride(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/confirm", strings.NewReader("body"))
	r.Header.Set("X-Title", "Hdr")
	r.Header.Set("X-Timeout", "30")
	r.Header.Set("X-Ok", "yes")
	n, _ := parseRequest(r, ep(), 60)
	if n.Title != "Hdr" || n.OkText != "yes" {
		t.Fatalf("got %+v", n)
	}
	if int(n.Timeout.Seconds()) != 30 {
		t.Fatalf("timeout %s", n.Timeout)
	}
}

func TestParseQueryOverride(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/confirm?title=Q&ok=ja&timeout=10", strings.NewReader("b"))
	n, _ := parseRequest(r, ep(), 60)
	if n.Title != "Q" || n.OkText != "ja" {
		t.Fatalf("got %+v", n)
	}
	if int(n.Timeout.Seconds()) != 10 {
		t.Fatalf("timeout %s", n.Timeout)
	}
}

func TestParseJSONOverride(t *testing.T) {
	body := `{"title":"J","message":"jm","ok_text":"go","cancel_text":"stop","timeout":5}`
	r := httptest.NewRequest("POST", "/api/confirm?title=ignored", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Title", "ignored")
	n, _ := parseRequest(r, ep(), 60)
	if n.Title != "J" || n.Message != "jm" || n.OkText != "go" || n.CancelText != "stop" {
		t.Fatalf("got %+v", n)
	}
	if int(n.Timeout.Seconds()) != 5 {
		t.Fatalf("timeout %s", n.Timeout)
	}
}

func TestParsePriorityFallback(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/confirm", strings.NewReader("body"))
	n, _ := parseRequest(r, ep(), 60)
	if n.Timeout.Seconds() != 60 {
		t.Fatalf("default timeout not used: %s", n.Timeout)
	}
}

func TestParseSourceHeader(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/confirm", strings.NewReader("x"))
	r.Header.Set("X-Source", "ci-bot")
	n, _ := parseRequest(r, ep(), 60)
	if n.SourceHdr != "ci-bot" {
		t.Fatalf("SourceHdr=%q", n.SourceHdr)
	}
}

func TestParseMalformedJSONErrors(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/confirm", strings.NewReader("{not json"))
	r.Header.Set("Content-Type", "application/json")
	if _, err := parseRequest(r, ep(), 60); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestParseInvalidTimeoutFallsBack(t *testing.T) {
	cases := []string{"abc", "-5", "0"}
	for _, v := range cases {
		r := httptest.NewRequest("POST", "/api/confirm?timeout="+v, strings.NewReader("x"))
		n, _ := parseRequest(r, ep(), 60)
		if n.Timeout.Seconds() != 60 {
			t.Errorf("timeout=%s expected 60s fallback, got %s", v, n.Timeout)
		}
	}
}

func TestParseRejectsJSONPatchVariant(t *testing.T) {
	body := `{"title":"J","message":"jm"}`
	r := httptest.NewRequest("POST", "/api/confirm", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json-patch+json")
	n, _ := parseRequest(r, ep(), 60)
	if n.Title == "J" || n.Message != body {
		t.Fatalf("expected raw body as message and endpoint title, got %+v", n)
	}
}

type fakeStorage struct{ id int64 }

func (f *fakeStorage) Insert(ctx context.Context, r storage.Record) (int64, error) {
	f.id++
	return f.id, nil
}
func (f *fakeStorage) UpdateStatus(ctx context.Context, id int64, status string, ts int64) error {
	return nil
}
func (f *fakeStorage) List(ctx context.Context, fl storage.ListFilter) ([]storage.Record, int, error) {
	return nil, 0, nil
}
func (f *fakeStorage) Delete(ctx context.Context, id int64) error { return nil }
func (f *fakeStorage) DeleteAll(ctx context.Context) error       { return nil }

func TestEndToEndApproved(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, err := config.LoadOrInit()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apply(config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 0, EndpointPrefix: "/api", MaxQueueSize: 4},
		Endpoints: []config.EndpointConfig{ep()},
		Behavior:  config.BehaviorConfig{DefaultTimeoutSeconds: 5, TimeoutAction: "timeout"},
	})

	var d *dispatcher.Dispatcher
	d = dispatcher.New(dispatcher.Options{
		QueueSize: 4,
		OnActive: func(n *dispatcher.Notification) {
			go d.Resolve(n.ID, dispatcher.Result{Decision: "approved"})
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	s := New(cfg, d, &fakeStorage{}, zerolog.Nop())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/confirm", "text/plain", strings.NewReader("hi"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "approved" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestEndToEndTimeout(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, _ := config.LoadOrInit()
	cfg.Apply(config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 0, EndpointPrefix: "/api", MaxQueueSize: 4},
		Endpoints: []config.EndpointConfig{ep()},
		Behavior:  config.BehaviorConfig{DefaultTimeoutSeconds: 1, TimeoutAction: "timeout"},
	})
	var d *dispatcher.Dispatcher
	d = dispatcher.New(dispatcher.Options{
		QueueSize: 4,
		OnActive:  func(*dispatcher.Notification) {}, // never resolves — let timer fire
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	s := New(cfg, d, &fakeStorage{}, zerolog.Nop())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/confirm", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "timeout" {
		t.Fatalf("got %q", body)
	}
}

func TestEndToEndPaused(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, _ := config.LoadOrInit()
	cfg.Apply(config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 0, EndpointPrefix: "/api", MaxQueueSize: 4},
		Endpoints: []config.EndpointConfig{ep()},
		Behavior:  config.BehaviorConfig{DefaultTimeoutSeconds: 5},
	})
	var d *dispatcher.Dispatcher
	d = dispatcher.New(dispatcher.Options{QueueSize: 4, OnActive: func(*dispatcher.Notification) {}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	d.Pause()

	s := New(cfg, d, &fakeStorage{}, zerolog.Nop())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, _ := http.Post(ts.URL+"/api/confirm", "text/plain", strings.NewReader("x"))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 503 || string(body) != "paused" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestEndToEndClientDisconnect(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, _ := config.LoadOrInit()
	cfg.Apply(config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 0, EndpointPrefix: "/api", MaxQueueSize: 4},
		Endpoints: []config.EndpointConfig{ep()},
		Behavior:  config.BehaviorConfig{DefaultTimeoutSeconds: 60},
	})
	activated := make(chan int64, 4)
	var d *dispatcher.Dispatcher
	d = dispatcher.New(dispatcher.Options{
		QueueSize: 4,
		OnActive:  func(n *dispatcher.Notification) { activated <- n.ID },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	s := New(cfg, d, &fakeStorage{}, zerolog.Nop())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// Issue a request whose client we will cancel before resolution.
	cctx, ccancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(cctx, "POST", ts.URL+"/api/confirm", strings.NewReader("x"))
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		errCh <- err
	}()
	// Wait until dispatcher activated this notification.
	id := <-activated
	_ = id
	// Disconnect the client.
	ccancel()
	<-errCh

	// Verify dispatcher cleared its slot: enqueue a second notification and
	// ensure it activates within 2 seconds. (If the original was still active,
	// we'd time out at 60s.)
	n2 := dispatcher.NewNotification()
	n2.ID = 999
	n2.Mode = dispatcher.ModeTwoButton
	n2.Timeout = time.Second
	if err := d.Enqueue(n2); err != nil {
		t.Fatal(err)
	}
	select {
	case <-activated:
		// good — second notification became active
	case <-time.After(2 * time.Second):
		t.Fatalf("dispatcher stuck after client disconnect")
	}
	d.Resolve(n2.ID, dispatcher.Result{Decision: "approved"})
	<-n2.ResultCh
}

func TestTokenAuthRequired(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, _ := config.LoadOrInit()
	cfg.Apply(config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 0, EndpointPrefix: "/api", AuthToken: "secret-xyz", MaxQueueSize: 4},
		Endpoints: []config.EndpointConfig{ep()},
		Behavior:  config.BehaviorConfig{DefaultTimeoutSeconds: 5},
	})
	var d *dispatcher.Dispatcher
	d = dispatcher.New(dispatcher.Options{
		QueueSize: 4,
		OnActive:  func(n *dispatcher.Notification) { go d.Resolve(n.ID, dispatcher.Result{Decision: "approved"}) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	s := New(cfg, d, &fakeStorage{}, zerolog.Nop())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// No token — expect 401.
	resp, _ := http.Post(ts.URL+"/api/confirm", "text/plain", strings.NewReader("x"))
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 without token, got %d", resp.StatusCode)
	}

	// Wrong token — expect 401.
	req, _ := http.NewRequest("POST", ts.URL+"/api/confirm", strings.NewReader("x"))
	req.Header.Set("X-Token", "wrong")
	resp2, _ := http.DefaultClient.Do(req)
	resp2.Body.Close()
	if resp2.StatusCode != 401 {
		t.Fatalf("expected 401 with wrong token, got %d", resp2.StatusCode)
	}

	// Correct token — expect 200 + approved.
	req3, _ := http.NewRequest("POST", ts.URL+"/api/confirm", strings.NewReader("x"))
	req3.Header.Set("X-Token", "secret-xyz")
	resp3, _ := http.DefaultClient.Do(req3)
	body, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	if resp3.StatusCode != 200 || string(body) != "approved" {
		t.Fatalf("expected 200 approved, got %d %q", resp3.StatusCode, body)
	}
}
