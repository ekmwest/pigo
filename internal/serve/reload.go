package serve

import (
	"io"
	"net/http"
	"sync"
)

// reloadPath is where browsers listen for reload events. It starts with "_"
// rather than "." because hideDotfiles would hide it otherwise, and it's
// unlikely to clash with a real file.
const reloadPath = "/_pigo/reload"

// broadcaster streams a reload event over Server-Sent Events to every
// connected browser each time notify is called.
type broadcaster struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
	done    chan struct{}
	closed  bool
}

func newBroadcaster() *broadcaster {
	return &broadcaster{
		clients: make(map[chan struct{}]struct{}),
		done:    make(chan struct{}),
	}
}

// notify tells every connected browser to reload.
func (b *broadcaster) notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		// Each channel holds one pending event; if one is already queued,
		// the browser will reload anyway, so there's nothing to add.
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// close ends all open streams and makes new ones end immediately. Streams
// never finish on their own, so without this http.Server.Shutdown would wait
// for its full timeout.
func (b *broadcaster) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.done)
	}
}

func (b *broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// Send the headers now so the browser's EventSource reports the
	// connection as open instead of waiting for the first event.
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		return
	}

	for {
		select {
		case <-ch:
			if _, err := io.WriteString(w, "event: reload\ndata: reload\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		case <-b.done:
			return
		}
	}
}

// withReload serves reload events at reloadPath and everything else from
// next, with the reload script added to HTML pages. The reload stream
// bypasses next's request logging on purpose: a log line per reload,
// redirected into the served folder, would trigger the watcher again.
func withReload(next http.Handler, b *broadcaster) http.Handler {
	pages := injectReload(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == reloadPath {
			b.ServeHTTP(w, r)
			return
		}
		pages.ServeHTTP(w, r)
	})
}
