package serve

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
)

// reloadScript reloads the page on a reload event. EventSource reconnects on
// its own after the server restarts; the page then reloads too, so edits made
// while the server was down show up.
const reloadScript = `<script>
(() => {
	let connected = false;
	const events = new EventSource("` + reloadPath + `");
	events.addEventListener("reload", () => location.reload());
	events.addEventListener("open", () => {
		if (connected) location.reload();
		connected = true;
	});
})();
</script>
`

// injectReload adds reloadScript to full HTML responses. Everything else,
// including partial (206) responses, passes through unchanged.
func injectReload(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		iw := &injectWriter{ResponseWriter: w}
		next.ServeHTTP(iw, r)
		iw.finish()
	})
}

// injectWriter decides at WriteHeader whether to inject. If it does, it
// buffers the body so finish can insert the script and fix Content-Length.
type injectWriter struct {
	http.ResponseWriter
	wroteHeader bool
	inject      bool
	buf         bytes.Buffer
}

func (w *injectWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.inject = status == http.StatusOK && strings.HasPrefix(w.Header().Get("Content-Type"), "text/html")
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
	body := insertScript(w.buf.Bytes())
	h := w.Header()
	h.Set("Content-Length", strconv.Itoa(len(body)))
	// Byte ranges of the file on disk don't match the modified body.
	h.Del("Accept-Ranges")
	w.ResponseWriter.WriteHeader(http.StatusOK)
	w.ResponseWriter.Write(body)
}

// insertScript puts reloadScript before the last </body>, or at the end if
// there is none; browsers run a trailing script either way.
func insertScript(body []byte) []byte {
	i := lastIndexFold(body, []byte("</body>"))
	if i < 0 {
		i = len(body)
	}
	out := make([]byte, 0, len(body)+len(reloadScript))
	out = append(out, body[:i]...)
	out = append(out, reloadScript...)
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
