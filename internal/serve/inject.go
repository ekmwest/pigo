package serve

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
)

// reloadScript returns the script that reloads a page served at version.
// It holds a stream only while the page is visible (see Broadcaster), and
// reloads on a reload event or when a new stream reports another version:
// files changed while the page was hidden, or the server restarted.
// EventSource reconnects on its own after a restart.
func reloadScript(version string) string {
	return `<script>
(() => {
	const version = "` + version + `";
	let events = null;
	function connect() {
		if (events || document.visibilityState !== "visible") return;
		events = new EventSource("` + reloadPath + `");
		events.addEventListener("version", (e) => {
			if (e.data !== version) location.reload();
		});
		events.addEventListener("reload", () => location.reload());
	}
	function disconnect() {
		if (events) events.close();
		events = null;
	}
	document.addEventListener("visibilitychange", () => {
		if (document.visibilityState === "visible") connect(); else disconnect();
	});
	connect();
})();
</script>
`
}

// injectReload adds reloadScript to full HTML responses and HTML 404 pages,
// so a page that is missing now reloads once it exists. Everything else,
// including partial (206) responses, passes through unchanged.
func injectReload(next http.Handler, version func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		// Read the version before the file: if a change lands in between,
		// the page has an older version and reloads once more, instead of
		// showing old content under the new version.
		iw := &injectWriter{ResponseWriter: w, script: reloadScript(version())}
		next.ServeHTTP(iw, r)
		iw.finish()
	})
}

// injectWriter decides at WriteHeader whether to inject. If it does, it
// buffers the body so finish can insert the script and fix Content-Length.
type injectWriter struct {
	http.ResponseWriter
	script      string
	wroteHeader bool
	status      int
	inject      bool
	buf         bytes.Buffer
}

func (w *injectWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.inject = (status == http.StatusOK || status == http.StatusNotFound) && strings.HasPrefix(w.Header().Get("Content-Type"), "text/html")
	if !w.inject {
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *injectWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.inject {
		return w.buf.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

func (w *injectWriter) finish() {
	if !w.inject {
		return
	}
	body := insertScript(w.buf.Bytes(), w.script)
	h := w.Header()
	h.Set("Content-Length", strconv.Itoa(len(body)))
	// Byte ranges of the file on disk don't match the modified body.
	h.Del("Accept-Ranges")
	w.ResponseWriter.WriteHeader(w.status)
	w.ResponseWriter.Write(body)
}

// insertScript puts script before the last </body>, or at the end if there
// is none; browsers run a trailing script either way.
func insertScript(body []byte, script string) []byte {
	i := lastIndexFold(body, []byte("</body>"))
	if i < 0 {
		i = len(body)
	}
	out := make([]byte, 0, len(body)+len(script))
	out = append(out, body[:i]...)
	out = append(out, script...)
	return append(out, body[i:]...)
}

// lastIndexFold is bytes.LastIndex ignoring case. Lowercasing the body first
// would be simpler, but it can change byte offsets of non-ASCII text.
func lastIndexFold(s, sep []byte) int {
	for i := len(s) - len(sep); i >= 0; i-- {
		if bytes.EqualFold(s[i:i+len(sep)], sep) {
			return i
		}
	}
	return -1
}
