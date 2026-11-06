package serve

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// reloadPath is where browsers listen for reload events. It starts with "_"
// rather than "." because hideDotfiles would hide it otherwise, and it's
// unlikely to clash with a real file.
const reloadPath = "/_pigo/reload"

// Broadcaster streams a reload event over Server-Sent Events to every
// connected browser each time notify is called.
//
// Pages only hold a stream while they're visible, because browsers allow
// just 6 HTTP/1.1 connections per host and a stream per tab would make the
// 7th tab hang. A page that was hidden misses reload events, so every
// stream starts with the current version, and the page reloads if it
// differs from the version it was served with.
type Broadcaster struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
	done    chan struct{}
	closed  bool
	// start makes versions differ across server restarts, so pages
	// reconnecting to a new server reload too.
	start string
	count uint64
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[chan struct{}]struct{}),
		done:    make(chan struct{}),
		start:   strconv.FormatInt(time.Now().UnixNano(), 10),
	}
}

// Version identifies the current state of the served files. It changes on
// every Notify. It holds only digits and a dash, so it can be put in a
// JavaScript string literal as is.
func (b *Broadcaster) Version() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.version()
}

func (b *Broadcaster) version() string {
	return b.start + "-" + strconv.FormatUint(b.count, 10)
}

// Notify tells every connected browser to reload.
func (b *Broadcaster) Notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count++
	for ch := range b.clients {
		// Each channel holds one pending event; if one is already queued,
		// the browser will reload anyway, so there's nothing to add.
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Close ends all open streams and makes new ones end immediately. Streams
// never finish on their own, so without this http.Server.Shutdown would wait
// for its full timeout.
func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.done)
	}
}

func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ch := make(chan struct{}, 1)
	// Registering and reading the version together means a Notify is
	// either included in the version or delivered as a reload event.
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	version := b.version()
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if _, err := io.WriteString(w, "event: version\ndata: "+version+"\n\n"); err != nil {
		return
	}
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

// WithReload serves reload events at reloadPath and everything else from
// next, with the reload script added to HTML pages. The reload stream
// bypasses next's request logging on purpose: a log line per reload,
// redirected into the served folder, would trigger the watcher again.
func WithReload(next http.Handler, b *Broadcaster) http.Handler {
	pages := injectReload(next, b.Version)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == reloadPath {
			b.ServeHTTP(w, r)
			return
		}
		pages.ServeHTTP(w, r)
	})
}
