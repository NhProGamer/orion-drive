package wopi

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// discoveryDoc is the parsed shape of a WOPI discovery.xml.
type discoveryDoc struct {
	XMLName xml.Name `xml:"wopi-discovery"`
	NetZone struct {
		Apps []struct {
			Name    string `xml:"name,attr"`
			Actions []struct {
				Name   string `xml:"name,attr"`   // edit | view | editnew | embedview | ...
				Ext    string `xml:"ext,attr"`    // file extension, no dot
				URLSrc string `xml:"urlsrc,attr"` // editor URL template with <PLACEHOLDER=…&> tokens
			} `xml:"action"`
		} `xml:"app"`
	} `xml:"net-zone"`
	ProofKey *proofKeyXML `xml:"proof-key"`
}

// placeholderRe matches the optional <NAME=VALUE&> tokens in a WOPI urlsrc.
var placeholderRe = regexp.MustCompile(`<[^>]*>`)

// Discovery fetches and caches a document server's WOPI discovery document,
// exposing which extensions can be edited or viewed and how to launch each.
type Discovery struct {
	url    string
	ttl    time.Duration
	client *http.Client

	mu      sync.RWMutex
	edit    map[string]string // ext → edit urlsrc
	view    map[string]string // ext → view urlsrc
	create  map[string]string // ext → editnew urlsrc (blank-document creation)
	proof   *ProofKeys
	fetched time.Time
	ok      bool
}

// NewDiscovery builds a discovery cache for the given discovery.xml URL. A zero
// url yields a disabled cache.
func NewDiscovery(discoveryURL string, ttl time.Duration) *Discovery {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &Discovery{
		url:    discoveryURL,
		ttl:    ttl,
		client: &http.Client{Timeout: 10 * time.Second},
		edit:   map[string]string{},
		view:   map[string]string{},
		create: map[string]string{},
	}
}

// Configured reports whether a discovery URL is set.
func (d *Discovery) Configured() bool { return d != nil && d.url != "" }

// ensure refreshes the cache when it is empty or stale (best-effort: on a fetch
// error the previous snapshot is kept).
func (d *Discovery) ensure(ctx context.Context) {
	if !d.Configured() {
		return
	}
	d.mu.RLock()
	fresh := d.ok && time.Since(d.fetched) < d.ttl
	d.mu.RUnlock()
	if fresh {
		return
	}
	if err := d.refresh(ctx); err != nil {
		// Keep whatever we had; a stale map is better than none.
		d.mu.Lock()
		d.fetched = time.Now() // back off so we don't hammer a broken server
		d.mu.Unlock()
	}
}

// refresh fetches and parses the discovery document.
func (d *Discovery) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wopi discovery: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	var doc discoveryDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return err
	}

	edit := map[string]string{}
	view := map[string]string{}
	create := map[string]string{}
	for _, app := range doc.NetZone.Apps {
		for _, a := range app.Actions {
			ext := strings.ToLower(strings.TrimPrefix(a.Ext, "."))
			if ext == "" || a.URLSrc == "" {
				continue
			}
			switch a.Name {
			case "edit":
				if _, seen := edit[ext]; !seen {
					edit[ext] = a.URLSrc
				}
			case "editnew":
				// editnew implies the format is both editable and creatable.
				if _, seen := edit[ext]; !seen {
					edit[ext] = a.URLSrc
				}
				if _, seen := create[ext]; !seen {
					create[ext] = a.URLSrc
				}
			case "view", "embedview":
				if _, seen := view[ext]; !seen {
					view[ext] = a.URLSrc
				}
			}
		}
	}

	d.mu.Lock()
	d.edit, d.view, d.create = edit, view, create
	d.proof = parseProofKeys(doc.ProofKey)
	d.fetched = time.Now()
	d.ok = true
	d.mu.Unlock()
	return nil
}

// Action returns the launch URL for opening ext, preferring an editable action
// when wantEdit is set. editable reports whether the returned action grants
// write; ok is false when the format is not supported at all.
func (d *Discovery) Action(ctx context.Context, ext string, wantEdit bool) (urlsrc string, editable, ok bool) {
	d.ensure(ctx)
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	d.mu.RLock()
	defer d.mu.RUnlock()
	if wantEdit {
		if u := d.edit[ext]; u != "" {
			return u, true, true
		}
	}
	if u := d.view[ext]; u != "" {
		return u, false, true
	}
	// Only an edit action exists but the caller wanted view (or no write right):
	// launch it view-only (UserCanWrite=false in CheckFileInfo enforces it).
	if u := d.edit[ext]; u != "" {
		return u, false, true
	}
	return "", false, false
}

// NewAction returns the editnew launch URL for creating a blank ext document.
func (d *Discovery) NewAction(ctx context.Context, ext string) (urlsrc string, ok bool) {
	d.ensure(ctx)
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	d.mu.RLock()
	defer d.mu.RUnlock()
	u := d.create[ext]
	return u, u != ""
}

// NewExts returns the sorted extensions the server can create blank (editnew).
func (d *Discovery) NewExts(ctx context.Context) []string {
	d.ensure(ctx)
	d.mu.RLock()
	defer d.mu.RUnlock()
	return sortedKeys(d.create)
}

// EditExts returns the sorted extensions that can be edited.
func (d *Discovery) EditExts(ctx context.Context) []string {
	d.ensure(ctx)
	d.mu.RLock()
	defer d.mu.RUnlock()
	return sortedKeys(d.edit)
}

// ViewExts returns the sorted extensions that can be viewed (this includes most
// editable formats, which can always be opened read-only).
func (d *Discovery) ViewExts(ctx context.Context) []string {
	d.ensure(ctx)
	d.mu.RLock()
	defer d.mu.RUnlock()
	seen := map[string]bool{}
	for ext := range d.view {
		seen[ext] = true
	}
	for ext := range d.edit {
		seen[ext] = true
	}
	return sortedKeys(seen)
}

// ProofKeys returns the current proof keys, or nil when unavailable.
func (d *Discovery) ProofKeys(ctx context.Context) *ProofKeys {
	d.ensure(ctx)
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.proof
}

// BuildActionURL fills a discovery urlsrc: it drops the optional <…> placeholder
// tokens and appends the (URL-encoded) WOPISrc the editor must call back into.
func BuildActionURL(urlsrc, wopiSrc string) string {
	u := placeholderRe.ReplaceAllString(urlsrc, "")
	// Tidy separators left behind by removed placeholders.
	u = strings.ReplaceAll(u, "?&", "?")
	for strings.Contains(u, "&&") {
		u = strings.ReplaceAll(u, "&&", "&")
	}
	u = strings.TrimRight(u, "?&")
	enc := url.QueryEscape(wopiSrc)
	if strings.HasSuffix(u, "WOPISrc=") {
		return u + enc
	}
	if strings.Contains(u, "WOPISrc=") {
		return u // already carries a source (unusual)
	}
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + "WOPISrc=" + enc
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
