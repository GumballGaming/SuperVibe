package browser

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	userAgent       = "SuperVibe/0.2"
	fetchTimeout    = 15 * time.Second
	searchTimeout   = 15 * time.Second
	lookupTimeout   = 3 * time.Second
	maxRedirects    = 4
	defaultMaxBytes = int64(1 << 20)
	textRuneCap     = 12000
	searchLimitDef  = 8
	searchLimitMax  = 15
)

type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Page struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	ContentType string `json:"contentType"`
	Text        string `json:"text"`
	Bytes       int    `json:"bytes"`
	Truncated   bool   `json:"truncated"`
	TimestampMs int64  `json:"timestampMs"`
}

var (
	resolveHost = func(ctx context.Context, host string) ([]net.IP, error) {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		ips := make([]net.IP, 0, len(addrs))
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
		return ips, nil
	}
	validate = ValidateURL
)

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("missing host")
	}
	lit := host
	if i := strings.IndexByte(lit, '%'); i >= 0 {
		lit = lit[:i]
	}
	if ip := net.ParseIP(lit); ip != nil {
		if blockedIP(ip) {
			return fmt.Errorf("host %q is a blocked address", host)
		}
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("host %q is blocked", host)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	ips, err := resolveHost(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve host %q: no addresses", host)
	}
	for _, ip := range ips {
		if blockedIP(ip) {
			return fmt.Errorf("host %q resolves to a blocked address", host)
		}
	}
	return nil
}

func blockedIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		if ip4.IsLoopback() || ip4.IsPrivate() || ip4.IsLinkLocalUnicast() || ip4.IsLinkLocalMulticast() || ip4.IsUnspecified() {
			return true
		}
		return (ip4[0] == 100 && ip4[1] >= 64 && ip4[1] < 128) ||
			(ip4[0] == 255 && ip4[1] == 255 && ip4[2] == 255 && ip4[3] == 255)
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

var fetchClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return validate(req.URL.String())
	},
}

func Fetch(ctx context.Context, raw string, maxBytes int64) (*Page, error) {
	if err := validate(raw); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", raw, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %q: status %d", raw, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	truncated := int64(len(data)) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	body := string(data)
	return &Page{
		URL:         resp.Request.URL.String(),
		Title:       extractTitle(body),
		ContentType: resp.Header.Get("Content-Type"),
		Text:        ExtractText(body, textRuneCap),
		Bytes:       len(data),
		Truncated:   truncated,
		TimestampMs: time.Now().UnixMilli(),
	}, nil
}

var (
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	blockTags = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script\b.*?</script\s*>`),
		regexp.MustCompile(`(?is)<style\b.*?</style\s*>`),
		regexp.MustCompile(`(?is)<noscript\b.*?</noscript\s*>`),
		regexp.MustCompile(`(?is)<svg\b.*?</svg\s*>`),
	}
	reBreakTags = regexp.MustCompile(`(?i)<br\s*/?>|</p\s*>|</div\s*>|</li\s*>|</h[1-6]\s*>|</tr\s*>`)
	reAnyTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	reNLRun     = regexp.MustCompile(`\n{3,}`)
	reTitle     = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)
)

func ExtractText(pageHTML string, maxRunes int) string {
	s := reComment.ReplaceAllString(pageHTML, " ")
	for _, re := range blockTags {
		s = re.ReplaceAllString(s, " ")
	}
	s = reBreakTags.ReplaceAllString(s, "\n")
	s = reAnyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimLeft(ln, " \t\r\f\v")
	}
	s = reNLRun.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	s = strings.TrimSpace(s)
	if maxRunes > 0 {
		if r := []rune(s); len(r) > maxRunes {
			s = string(r[:maxRunes]) + "…"
		}
	}
	return s
}

func extractTitle(body string) string {
	m := reTitle.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	t := html.UnescapeString(reAnyTag.ReplaceAllString(m[1], ""))
	return strings.Join(strings.Fields(t), " ")
}

var searchClient = &http.Client{}

func Search(ctx context.Context, query string, limit int) ([]Result, error) {
	return SearchURL(ctx, "https://html.duckduckgo.com/html/", query, limit)
}

func SearchURL(ctx context.Context, base, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = searchLimitDef
	}
	if limit > searchLimitMax {
		limit = searchLimitMax
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := searchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search %q: status %d", query, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return parseResults(string(body), limit), nil
}

var (
	reAnchor   = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a\s*>`)
	reClassAtt = regexp.MustCompile(`(?i)\bclass\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	reHRefAtt  = regexp.MustCompile(`(?i)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	reSnippet  = regexp.MustCompile(`(?is)<[a-z][a-z0-9]*\b[^>]*class\s*=\s*"[^"]*result__snippet[^"]*"[^>]*>(.*?)</(?:a|span|div|td|p)\s*>`)
)

func parseResults(doc string, limit int) []Result {
	type snip struct {
		start int
		text  string
	}
	var snips []snip
	for _, loc := range reSnippet.FindAllStringSubmatchIndex(doc, -1) {
		snips = append(snips, snip{loc[0], cleanInline(doc[loc[2]:loc[3]])})
	}
	results := []Result{}
	si := 0
	for _, loc := range reAnchor.FindAllStringSubmatchIndex(doc, -1) {
		attrs := doc[loc[2]:loc[3]]
		if !strings.Contains(attrValue(attrs, reClassAtt), "result__a") {
			continue
		}
		href := strings.TrimSpace(attrValue(attrs, reHRefAtt))
		if href == "" {
			continue
		}
		title := cleanInline(doc[loc[4]:loc[5]])
		end := loc[1]
		for si < len(snips) && snips[si].start < end {
			si++
		}
		snippet := ""
		if si < len(snips) {
			snippet = snips[si].text
			si++
		}
		results = append(results, Result{Title: title, URL: resultURL(href), Snippet: snippet})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results
}

func attrValue(attrs string, re *regexp.Regexp) string {
	m := re.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

func resultURL(href string) string {
	href = html.UnescapeString(href)
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if v := u.Query().Get("uddg"); v != "" {
		return v
	}
	return href
}

func cleanInline(s string) string {
	s = reAnyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}
