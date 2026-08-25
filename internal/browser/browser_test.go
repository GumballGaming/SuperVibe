package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func withResolver(t *testing.T, ips []net.IP, err error) {
	t.Helper()
	orig := resolveHost
	resolveHost = func(ctx context.Context, host string) ([]net.IP, error) {
		return ips, err
	}
	t.Cleanup(func() { resolveHost = orig })
}

func allowHost(t *testing.T, hostport string) {
	t.Helper()
	orig := validate
	validate = func(raw string) error {
		if u, err := url.Parse(raw); err == nil && u.Host == hostport {
			return nil
		}
		return ValidateURL(raw)
	}
	t.Cleanup(func() { validate = orig })
}

func TestValidateURL(t *testing.T) {
	blocked := []string{
		"",
		"ftp://example.com/file",
		"http://localhost/x",
		"http://LOCALHOST:8080/",
		"http://foo.localhost/",
		"http://printer.local/",
		"http://127.0.0.1/",
		"http://[::1]/",
		"http://[::ffff:127.0.0.1]/",
		"http://0.0.0.0/",
		"http://169.254.169.254/latest/meta-data",
		"http://192.168.1.4/",
		"http://10.1.2.3/",
		"http://172.16.5.5/",
		"http://100.64.0.7/",
		"http://[fe80::1]/",
		"http://[fc00::1]/",
	}
	for _, raw := range blocked {
		if err := ValidateURL(raw); err == nil {
			t.Errorf("ValidateURL(%q) = nil, want error", raw)
		}
	}
	withResolver(t, []net.IP{net.ParseIP("93.184.216.34")}, nil)
	for _, raw := range []string{"https://example.com", "http://example.com:8080/a?b=c"} {
		if err := ValidateURL(raw); err != nil {
			t.Errorf("ValidateURL(%q) = %v, want nil", raw, err)
		}
	}
	withResolver(t, []net.IP{net.ParseIP("192.168.0.9")}, nil)
	if err := ValidateURL("https://example.com"); err == nil {
		t.Error("ValidateURL resolving to RFC1918 = nil, want error")
	}
	withResolver(t, nil, errors.New("no such host"))
	if err := ValidateURL("https://example.org/x"); err == nil {
		t.Error("ValidateURL with failed lookup = nil, want error")
	}
}

const extractFixture = `<html><head><style>.x{color:red}</style><title>Ignore</title></head>` +
	`<body><!-- hidden note --><h1>Hello</h1><script>alert(1)</script><p>One<br>Two</p>` +
	`<div>Three</div><ul><li>A</li><li>B</li></ul><table><tr><td>Cell</td></tr></table>` +
	`<noscript>NoJS</noscript><svg><path d="M0"/></svg><p>End &amp; done</p></body></html>`

func TestExtractText(t *testing.T) {
	got := ExtractText(extractFixture, 0)
	want := "Ignore Hello\nOne\nTwo\nThree\nA\nB\nCell\nEnd & done"
	if got != want {
		t.Fatalf("ExtractText full:\n got %q\nwant %q", got, want)
	}
	for _, banned := range []string{"alert", "hidden", "NoJS", "color:red", "<", ">"} {
		if strings.Contains(got, banned) {
			t.Errorf("ExtractText output contains %q:\n%q", banned, got)
		}
	}
	if got := ExtractText(extractFixture, 7); got != "Ignore …" {
		t.Errorf("ExtractText truncated = %q, want %q", got, "Ignore …")
	}
	if got := ExtractText("<p>a<br>b<br/>c<br />d</p>", 0); got != "a\nb\nc\nd" {
		t.Errorf("ExtractText br variants = %q", got)
	}
	if got := ExtractText("<div>x</div>\n\n\n\n\n<p>y</p>", 0); got != "x\n\ny" {
		t.Errorf("ExtractText collapse = %q, want %q", got, "x\n\ny")
	}
}

const ddgCustom = `<!DOCTYPE html><html><body><div class="results">
<div class="result"><h2 class="result__title">
<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa&amp;rut=aa1">Example <b>Site</b></a>
</h2><a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa">First <i>snippet</i> &amp; more</a></div>
<div class="result"><h2 class="result__title">
<a class="result__a" href="https://sample.org/page">Sample Org</a>
</h2><span class="result__snippet">Second snippet</span></div>
<div class="result"><h2 class="result__title">
<a class="result__a" href="https://third.net/x">Third</a>
</h2><span class="result__snippet">Third snippet</span></div>
</div></body></html>`

func genDDG(n int) string {
	var b strings.Builder
	b.WriteString("<html><body>")
	for i := 1; i <= n; i++ {
		target := fmt.Sprintf("https://site%d.example/page", i)
		b.WriteString(`<h2 class="result__title"><a class="result__a" href="/l/?uddg=`)
		b.WriteString(url.QueryEscape(target))
		b.WriteString(`">Result ` + strconv.Itoa(i) + `</a></h2>`)
		b.WriteString(`<span class="result__snippet">Snippet ` + strconv.Itoa(i) + `</span>`)
	}
	b.WriteString("</body></html>")
	return b.String()
}

func TestSearchParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "golang testing" {
			t.Errorf("query param q = %q, want %q", q, "golang testing")
		}
		if ua := r.Header.Get("User-Agent"); ua != userAgent {
			t.Errorf("user agent = %q, want %q", ua, userAgent)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, ddgCustom)
	}))
	defer srv.Close()

	res, err := SearchURL(context.Background(), srv.URL, "golang testing", 10)
	if err != nil {
		t.Fatalf("SearchURL: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("got %d results, want 3", len(res))
	}
	if res[0].Title != "Example Site" || res[0].URL != "https://example.com/a" || res[0].Snippet != "First snippet & more" {
		t.Errorf("res[0] = %+v", res[0])
	}
	if res[1].Title != "Sample Org" || res[1].URL != "https://sample.org/page" || res[1].Snippet != "Second snippet" {
		t.Errorf("res[1] = %+v", res[1])
	}
	if res[2].Title != "Third" || res[2].URL != "https://third.net/x" || res[2].Snippet != "Third snippet" {
		t.Errorf("res[2] = %+v", res[2])
	}
}

func TestSearchLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, genDDG(20))
	}))
	defer srv.Close()

	cases := []struct {
		limit, want int
	}{
		{limit: 2, want: 2},
		{limit: 0, want: 8},
		{limit: -5, want: 8},
		{limit: 50, want: 15},
	}
	for _, tc := range cases {
		res, err := SearchURL(context.Background(), srv.URL, "q", tc.limit)
		if err != nil {
			t.Fatalf("SearchURL(limit=%d): %v", tc.limit, err)
		}
		if len(res) != tc.want {
			t.Errorf("SearchURL(limit=%d) returned %d results, want %d", tc.limit, len(res), tc.want)
			continue
		}
		if tc.limit > len(cases)-1 && len(res) > 0 {
			i := len(res) - 1
			wantTitle := fmt.Sprintf("Result %d", i+1)
			if res[i].Title != wantTitle {
				t.Errorf("res[%d].Title = %q, want %q", i, res[i].Title, wantTitle)
			}
		}
	}
}

func TestSearchNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := SearchURL(context.Background(), srv.URL, "q", 5); err == nil {
		t.Fatal("SearchURL non-200 = nil error, want error")
	}
}

func TestFetchPlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != userAgent {
			t.Errorf("user agent = %q, want %q", ua, userAgent)
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "hello world")
	}))
	defer srv.Close()
	allowHost(t, mustHost(t, srv.URL))

	page, err := Fetch(context.Background(), srv.URL, 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want text/plain", page.ContentType)
	}
	if page.Text != "hello world" || page.Title != "" {
		t.Errorf("Text = %q Title = %q", page.Text, page.Title)
	}
	if page.Bytes != 11 || page.Truncated {
		t.Errorf("Bytes = %d Truncated = %v, want 11 false", page.Bytes, page.Truncated)
	}
	if page.URL != srv.URL {
		t.Errorf("URL = %q, want %q", page.URL, srv.URL)
	}
	if page.TimestampMs <= 0 || page.TimestampMs > time.Now().UnixMilli()+1000 {
		t.Errorf("TimestampMs = %d out of range", page.TimestampMs)
	}
}

func TestFetchHTMLTitleAndScriptStripped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><head><title>Hello &amp; <b>World</b></title></head><body><p>Body text</p><script>x()</script></body></html>")
	}))
	defer srv.Close()
	allowHost(t, mustHost(t, srv.URL))

	page, err := Fetch(context.Background(), srv.URL, 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.Title != "Hello & World" {
		t.Errorf("Title = %q, want %q", page.Title, "Hello & World")
	}
	if !strings.Contains(page.Text, "Body text") || strings.Contains(page.Text, "x()") {
		t.Errorf("Text = %q", page.Text)
	}
}

func TestFetchTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, strings.Repeat("abcdefghij", 10))
	}))
	defer srv.Close()
	allowHost(t, mustHost(t, srv.URL))

	page, err := Fetch(context.Background(), srv.URL, 10)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !page.Truncated || page.Bytes != 10 || page.Text != "abcdefghij" {
		t.Errorf("Truncated=%v Bytes=%d Text=%q, want true 10 abcdefghij", page.Truncated, page.Bytes, page.Text)
	}
}

func TestFetchRedirectToBlockedHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer srv.Close()
	allowHost(t, mustHost(t, srv.URL))

	if _, err := Fetch(context.Background(), srv.URL+"/start", 0); err == nil {
		t.Fatal("Fetch redirect to blocked IP = nil error, want error")
	} else if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error = %v, want mention of blocked host", err)
	}
}

func TestFetchTooManyRedirects(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/hop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hop2", http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop"))
		http.Redirect(w, r, fmt.Sprintf("/hop%d", n+1), http.StatusFound)
	})
	defer srv.Close()
	allowHost(t, mustHost(t, srv.URL))

	if _, err := Fetch(context.Background(), srv.URL+"/hop", 0); err == nil {
		t.Fatal("Fetch infinite redirects = nil error, want error")
	} else if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("error = %v, want redirect limit message", err)
	}
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Host
}
