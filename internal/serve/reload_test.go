package serve

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// connect opens a reload stream on srv. When it returns, the response
// headers have arrived, so the broadcaster has registered the client.
func connect(t *testing.T, ctx context.Context, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+reloadPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// readEvent reads one event from a reload stream and returns its lines.
func readEvent(t *testing.T, r *bufio.Reader) []string {
	t.Helper()
	type result struct {
		lines []string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var lines []string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- result{lines, err}
				return
			}
			line = strings.TrimSuffix(line, "\n")
			if line == "" {
				done <- result{lines, nil}
				return
			}
			lines = append(lines, line)
		}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("read event: %v", res.err)
		}
		return res.lines
	case <-time.After(5 * time.Second):
		t.Fatal("no event received")
		return nil
	}
}

func clientCount(b *broadcaster) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

func waitForClients(t *testing.T, b *broadcaster, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for clientCount(b) != want {
		if time.Now().After(deadline) {
			t.Fatalf("clients = %d, want %d", clientCount(b), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBroadcaster(t *testing.T) {
	t.Run("headers", func(t *testing.T) {
		b := newBroadcaster()
		srv := httptest.NewServer(b)
		defer srv.Close()
		defer b.close()

		resp := connect(t, context.Background(), srv.URL)
		if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
			t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want %q", got, "no-store")
		}
	})

	t.Run("notify reaches every client", func(t *testing.T) {
		b := newBroadcaster()
		srv := httptest.NewServer(b)
		defer srv.Close()
		defer b.close()

		r1 := bufio.NewReader(connect(t, context.Background(), srv.URL).Body)
		r2 := bufio.NewReader(connect(t, context.Background(), srv.URL).Body)

		b.notify()
		want := []string{"event: reload", "data: reload"}
		for _, r := range []*bufio.Reader{r1, r2} {
			if got := readEvent(t, r); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("event = %q, want %q", got, want)
			}
		}

		// A second notify must arrive too, not just the first.
		b.notify()
		readEvent(t, r1)
		readEvent(t, r2)
	})

	t.Run("notify without clients", func(t *testing.T) {
		b := newBroadcaster()
		b.notify()
	})

	t.Run("disconnect unregisters client", func(t *testing.T) {
		b := newBroadcaster()
		srv := httptest.NewServer(b)
		defer srv.Close()
		defer b.close()

		ctx, cancel := context.WithCancel(context.Background())
		connect(t, ctx, srv.URL)
		waitForClients(t, b, 1)
		cancel()
		waitForClients(t, b, 0)
	})

	t.Run("close ends streams", func(t *testing.T) {
		b := newBroadcaster()
		srv := httptest.NewServer(b)
		defer srv.Close()

		resp := connect(t, context.Background(), srv.URL)
		b.close()
		b.close() // closing twice must not panic

		done := make(chan error, 1)
		go func() {
			_, err := io.ReadAll(resp.Body)
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("read = %v, want EOF", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("stream still open after close")
		}

		// New connections after close end right away.
		resp = connect(t, context.Background(), srv.URL)
		if _, err := io.ReadAll(resp.Body); err != nil {
			t.Errorf("read after close = %v, want EOF", err)
		}
	})
}

func TestWithReload(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.txt": "hello"})

	var logs strings.Builder
	b := newBroadcaster()
	srv := httptest.NewServer(withReload(newHandler(dir, log.New(&logs, "", 0)), b))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}

	resp = connect(t, context.Background(), srv.URL)
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
	}
	b.close()
	io.ReadAll(resp.Body)

	// Only the file request is logged; the reload stream must not be.
	if want := "GET /a.txt 200\n"; logs.String() != want {
		t.Errorf("log = %q, want %q", logs.String(), want)
	}
}

func TestServeShutdownWithReloadStream(t *testing.T) {
	b := newBroadcaster()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	context.AfterFunc(ctx, b.close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, withReload(http.NotFoundHandler(), b), io.Discard) }()

	connect(t, context.Background(), "http://"+ln.Addr().String())

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve = %v, want nil", err)
		}
	case <-time.After(shutdownTimeout + time.Second):
		t.Fatal("serve did not return")
	}
	// Without close, Shutdown would wait for the open stream until it times out.
	if elapsed := time.Since(start); elapsed > shutdownTimeout/2 {
		t.Errorf("shutdown took %v, want it well under %v", elapsed, shutdownTimeout)
	}
}
