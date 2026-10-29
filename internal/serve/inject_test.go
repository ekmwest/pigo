package serve

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInsertScript(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "before </body>", body: "<p>hi</p></body></html>", want: "<p>hi</p>" + reloadScript + "</body></html>"},
		{name: "uppercase", body: "<P>hi</P></BODY></HTML>", want: "<P>hi</P>" + reloadScript + "</BODY></HTML>"},
		{name: "last of several", body: "<pre></body></pre></body>", want: "<pre></body></pre>" + reloadScript + "</body>"},
		{name: "no </body>", body: "<p>hi</p>", want: "<p>hi</p>" + reloadScript},
		{name: "empty", body: "", want: reloadScript},
		// İ lowercases to more bytes, which would shift offsets if the
		// search lowercased the body.
		{name: "non-ASCII", body: "İİİ</body>", want: "İİİ" + reloadScript + "</body>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(insertScript([]byte(tt.body))); got != tt.want {
				t.Errorf("insertScript(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestInjectReload(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"index.html": "<html><body><p>home</p></body></html>",
		"page.htm":   "<p>no body tag</p>",
		"style.css":  "body { color: red }",
		"a.txt":      "<p>not html</p>",
	})
	h := injectReload(newHandler(dir, log.New(io.Discard, "", 0)))

	tests := []struct {
		name       string
		method     string
		path       string
		header     map[string]string
		wantStatus int
		wantScript bool
	}{
		{name: "html page", path: "/", wantStatus: http.StatusOK, wantScript: true},
		{name: "html without </body>", path: "/page.htm", wantStatus: http.StatusOK, wantScript: true},
		{name: "css", path: "/style.css", wantStatus: http.StatusOK},
		{name: "text that looks like html", path: "/a.txt", wantStatus: http.StatusOK},
		{name: "not found", path: "/missing.html", wantStatus: http.StatusNotFound},
		{name: "head", method: http.MethodHead, path: "/", wantStatus: http.StatusOK},
		{
			name:       "if-modified-since is ignored",
			path:       "/",
			header:     map[string]string{"If-Modified-Since": time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)},
			wantStatus: http.StatusOK,
			wantScript: true,
		},
		{
			name:       "range",
			path:       "/",
			header:     map[string]string{"Range": "bytes=0-5"},
			wantStatus: http.StatusPartialContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.path, nil)
			for k, v := range tt.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			body := rec.Body.String()
			if got := strings.Contains(body, reloadScript); got != tt.wantScript {
				t.Errorf("body has script = %v, want %v; body = %q", got, tt.wantScript, body)
			}
			if cl := rec.Header().Get("Content-Length"); cl != "" && method == http.MethodGet && cl != strconv.Itoa(len(body)) {
				t.Errorf("Content-Length = %s, body is %d bytes", cl, len(body))
			}
			if tt.wantScript {
				if got := rec.Header().Get("Accept-Ranges"); got != "" {
					t.Errorf("Accept-Ranges = %q, want it removed", got)
				}
				if got := rec.Header().Get("Cache-Control"); got != "no-store" {
					t.Errorf("Cache-Control = %q, want %q", got, "no-store")
				}
			}
		})
	}
}

func TestWithReloadInjects(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"index.html": "<body>home</body>"})
	b := newBroadcaster()
	defer b.close()
	srv := httptest.NewServer(withReload(newHandler(dir, log.New(io.Discard, "", 0)), b))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if want := "<body>home" + reloadScript + "</body>"; string(body) != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}
