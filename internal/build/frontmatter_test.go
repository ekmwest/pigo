package build

import (
	"maps"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantData    map[string]string
		wantContent string
		wantErr     bool
	}{
		{
			name:        "none",
			text:        "  <p>hi</p>\n",
			wantData:    map[string]string{},
			wantContent: "  <p>hi</p>\n",
		},
		{
			name:        "basic, content trimmed",
			text:        "---\ntitle: Home\nlayout: base.html\n---\n\n<p>hi</p>\n\n",
			wantData:    map[string]string{"title": "Home", "layout": "base.html"},
			wantContent: "<p>hi</p>",
		},
		{
			name:        "crlf",
			text:        "---\r\ntitle: Home\r\n---\r\n<p>hi</p>\r\n",
			wantData:    map[string]string{"title": "Home"},
			wantContent: "<p>hi</p>",
		},
		{
			name:        "quotes, colons, dashes and underscores",
			text:        "---\nmeta-title: \"A: B\"\nsub_title: 'x'\nhalf: \"open\nempty:\n---\nc",
			wantData:    map[string]string{"meta-title": "A: B", "sub_title": "x", "half": "\"open", "empty": ""},
			wantContent: "c",
		},
		{
			name:        "blank and comment lines",
			text:        "---\n\n# note\ntitle: T\n---\nc",
			wantData:    map[string]string{"title": "T"},
			wantContent: "c",
		},
		{
			name:        "not on the first line",
			text:        "\n---\ntitle: T\n---\nc",
			wantData:    map[string]string{},
			wantContent: "\n---\ntitle: T\n---\nc",
		},
		{
			name:        "no closing line",
			text:        "---\ntitle: T\nc",
			wantData:    map[string]string{},
			wantContent: "---\ntitle: T\nc",
		},
		{
			name:        "closing line at the end",
			text:        "---\ntitle: T\n---",
			wantData:    map[string]string{"title": "T"},
			wantContent: "",
		},
		{name: "no colon", text: "---\njust text\n---\nc", wantErr: true},
		{name: "no space after colon", text: "---\nurl:http://x\n---\nc", wantErr: true},
		{name: "indented", text: "---\n  title: T\n---\nc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, content, err := parseFrontmatter(tt.text)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("got no error, want one")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !maps.Equal(data, tt.wantData) {
				t.Errorf("data = %v, want %v", data, tt.wantData)
			}
			if content != tt.wantContent {
				t.Errorf("content = %q, want %q", content, tt.wantContent)
			}
		})
	}
}
