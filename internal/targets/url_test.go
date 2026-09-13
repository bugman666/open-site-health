package targets

import "testing"

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "https://example.org", want: "https://example.org"},
		{in: "  HTTPS://EXAMPLE.ORG/  ", want: "https://example.org"},
		{in: "https://example.org:443/foo/", want: "https://example.org/foo"},
		{in: "http://example.org:80", want: "http://example.org"},
		{in: "https://example.org/path#frag", want: "https://example.org/path"},
		{in: "https://example.org/path?q=1", want: "https://example.org/path?q=1"},
		{in: "", wantErr: true},
		{in: "not-a-url", wantErr: true},
		{in: "example.org", wantErr: true},
		{in: "ftp://example.org", wantErr: true},
		{in: "http://", wantErr: true},
	}
	for _, tc := range cases {
		got, err := normalizeURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeURL(%q): want error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeURL(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeURL(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}
