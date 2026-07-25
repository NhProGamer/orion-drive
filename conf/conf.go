// Package conf loads OrionDrive configuration from an INI file, with per-key
// overrides from environment variables (OD_CONF_<Section>_<Key>).
package conf

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

// Config is the fully-parsed application configuration.
type Config struct {
	System   System
	Database Database
	OIDC     OIDC
	Storage  Storage
	Slave    Slave
	WOPI     WOPI
	WebDAV   WebDAV
	Redis    Redis
	Archive   Archive
	Security  Security
	Thumbnail Thumbnail
}

// Thumbnail tunes preview generation. All generators are enabled by default when
// their tool is present; the Disable* flags force one off, and the *Path fields
// override the binary location. Zero MaxDim/Quality keep the built-in defaults.
type Thumbnail struct {
	Disable         bool `ini:"Disable"`
	MaxDim          int  `ini:"MaxDim"`
	Quality         int  `ini:"Quality"`
	DisableVideo    bool `ini:"DisableVideo"`
	DisableAudio    bool `ini:"DisableAudio"`
	DisableVips     bool `ini:"DisableVips"`
	DisableRaw      bool `ini:"DisableRaw"`
	DisablePDF      bool `ini:"DisablePDF"`
	DisableDocument bool `ini:"DisableDocument"`
	FFmpegPath      string `ini:"FFmpegPath"`
	VipsPath        string `ini:"VipsPath"`
	PopplerPath     string `ini:"PopplerPath"`
	LibreOfficePath string `ini:"LibreOfficePath"`
	LibRawPath      string `ini:"LibRawPath"`
}

// Security holds hardening knobs. A zero value uses the built-in default.
type Security struct {
	// SharePublicRatePerMin caps requests per client IP per minute on the public
	// share endpoints (password guessing, anonymous writes). Default 120; set to
	// a negative value to disable.
	SharePublicRatePerMin int `ini:"SharePublicRatePerMin"`
}

// SharePublicRate resolves the effective public-share rate limit.
func (s Security) SharePublicRate() int {
	if s.SharePublicRatePerMin == 0 {
		return 120
	}
	return s.SharePublicRatePerMin
}

// Archive tunes the safety limits applied when extracting archives (defence
// against decompression bombs and quota abuse). A zero value uses the built-in
// default. Sizes are in mebibytes for readability.
type Archive struct {
	MaxEntries     int   `ini:"MaxEntries"`     // max members per archive
	MaxSizeMB      int64 `ini:"MaxSizeMB"`      // ceiling when the user quota is unlimited
	MaxRatio       int64 `ini:"MaxRatio"`       // max uncompressed:compressed ratio
	RatioFloorMB   int64 `ini:"RatioFloorMB"`   // ratio is only enforced above this size
	TimeoutSeconds int   `ini:"TimeoutSeconds"` // per-extraction wall-clock cap
}

// System holds server-wide settings.
type System struct {
	Listen        string `ini:"Listen"`
	Mode          string `ini:"Mode"`
	SessionSecret string `ini:"SessionSecret"`
	SiteURL       string `ini:"SiteURL"`
	// AdminEmails is a comma-separated allowlist of emails always granted admin
	// access (bootstrap, independent of group permissions).
	AdminEmails string `ini:"AdminEmails"`
	// AdminGroups is a comma-separated list of SSO groups/roles whose members are
	// granted admin access, independent of their OrionDrive storage group.
	AdminGroups string `ini:"AdminGroups"`
	// TrashRetentionDays is how long trashed files are kept before the background
	// cleanup purges them permanently. 0 disables auto-purge.
	TrashRetentionDays int `ini:"TrashRetentionDays"`
	// TrustedProxies is a comma-separated list of reverse-proxy IPs/CIDRs whose
	// X-Forwarded-For is trusted for client-IP resolution. Empty (default) trusts
	// none — the client IP is the direct peer — so a client cannot spoof its IP
	// to evade per-IP rate limits. Set it to your reverse proxy for correct
	// per-client limiting.
	TrustedProxies string `ini:"TrustedProxies"`
}

// AdminEmailSet returns the lower-cased admin emails as a lookup set.
func (s System) AdminEmailSet() map[string]bool { return commaSet(s.AdminEmails) }

// AdminGroupSet returns the lower-cased admin SSO groups as a lookup set.
func (s System) AdminGroupSet() map[string]bool { return commaSet(s.AdminGroups) }

// commaSet splits a comma-separated list into a lower-cased lookup set.
func commaSet(v string) map[string]bool {
	set := map[string]bool{}
	for _, e := range strings.Split(v, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			set[e] = true
		}
	}
	return set
}

// Database selects and configures the backing store.
type Database struct {
	Type     string `ini:"Type"`
	DBFile   string `ini:"DBFile"`
	Host     string `ini:"Host"`
	Port     string `ini:"Port"`
	User     string `ini:"User"`
	Password string `ini:"Password"`
	Name     string `ini:"Name"`
	// SSLMode is the PostgreSQL sslmode (disable, require, verify-full, ...).
	// Ignored by SQLite/MySQL. Defaults to "disable".
	SSLMode string `ini:"SSLMode"`
}

// OIDC holds the OpenID Connect provider credentials.
type OIDC struct {
	Issuer       string `ini:"Issuer"`
	ClientID     string `ini:"ClientID"`
	ClientSecret string `ini:"ClientSecret"`
	RedirectURI  string `ini:"RedirectURI"`
	Scopes       string `ini:"Scopes"`
	// GroupsClaim is the ID-token claim carrying the user's SSO groups/roles,
	// mapped onto OrionDrive groups. Defaults to "groups".
	GroupsClaim string `ini:"GroupsClaim"`
}

// Storage configures storage backends.
type Storage struct {
	LocalBasePath string `ini:"LocalBasePath"`
	// EncryptionKey is a 64-char hex (32-byte) AES-256 key enabling at-rest
	// encryption for storage policies that request it. Empty disables encryption.
	EncryptionKey string `ini:"EncryptionKey"`
	// Dedup controls content-addressed deduplication of stored objects:
	//   off    (default) — never share blobs
	//   user   — reuse a blob only among the same user's uploads (safe)
	//   global — reuse a blob across all users (max disk savings, but a dedup hit
	//            can reveal to one user that another already stored that exact
	//            content — an existence-leak side channel). Encrypted policies are
	//            never deduplicated (random IV per object).
	Dedup string `ini:"Dedup"`
}

// Slave turns this node into a storage slave when Secret is set: it exposes the
// signed slave storage API, backed by a local directory.
type Slave struct {
	Secret      string `ini:"Secret"`
	StoragePath string `ini:"StoragePath"`
}

// WOPI configures online Office editing via a WOPI client (Collabora Online,
// OnlyOffice Docs, ...). When ServerURL is empty, Office editing is disabled.
//
// The set of editable/viewable formats and their editor URLs is discovered from
// the document server's WOPI discovery.xml (DiscoveryURL, defaulting to
// ServerURL + /hosting/discovery). EditURLTemplate is an optional legacy
// fallback used only when discovery is unavailable and has no entry for a file;
// it must contain the {src} placeholder (URL-encoded WOPISrc), e.g.
// https://collabora.example.com/browser/dist/cool.html?WOPISrc={src}
type WOPI struct {
	ServerURL       string `ini:"ServerURL"`
	DiscoveryURL    string `ini:"DiscoveryURL"`
	EditURLTemplate string `ini:"EditURLTemplate"`
	// VerifyProof enforces WOPI proof-key signature checks on editor callbacks
	// (X-WOPI-Proof). Off by default since some servers (e.g. OnlyOffice) do not
	// sign requests; enable it with Collabora/Office Online for defence in depth.
	VerifyProof bool `ini:"VerifyProof"`
}

// Enabled reports whether online Office editing is configured.
func (w WOPI) Enabled() bool { return w.ServerURL != "" }

// Discovery returns the WOPI discovery document URL, defaulting to the
// conventional /hosting/discovery path on the document server.
func (w WOPI) Discovery() string {
	if w.DiscoveryURL != "" {
		return w.DiscoveryURL
	}
	if w.ServerURL == "" {
		return ""
	}
	return strings.TrimRight(w.ServerURL, "/") + "/hosting/discovery"
}

// WebDAV toggles the WebDAV endpoint (/dav). It is enabled by default; users
// still need to create dedicated WebDAV credentials to connect. Set Enable to
// false to shut the endpoint off entirely.
type WebDAV struct {
	Enable bool `ini:"Enable"`
}

// Redis optionally replaces the in-memory cache.
type Redis struct {
	Server   string `ini:"Server"`
	Password string `ini:"Password"`
	DB       int    `ini:"DB"`
}

// DefaultSessionSecret is the placeholder shipped in the example config. It is
// rejected at startup outside debug mode (it keys session and WOPI tokens, so a
// known value means anyone can forge an authenticated session).
const DefaultSessionSecret = "change-me-to-a-long-random-string"

// minSessionSecretLen is the shortest session secret accepted in release mode.
const minSessionSecretLen = 16

// Default returns a configuration populated with sane defaults. Mode defaults to
// "release" (fail closed): debug mode relaxes the session-secret check and, when
// built with -tags dev, exposes the unauthenticated dev-login shortcut.
func Default() *Config {
	return &Config{
		System: System{
			Listen:             ":5212",
			Mode:               "release",
			SessionSecret:      "change-me-to-a-long-random-string",
			SiteURL:            "http://localhost:5212",
			TrashRetentionDays: 30,
		},
		WebDAV:   WebDAV{Enable: true},
		Database: Database{Type: "sqlite", DBFile: "data/orion.db", Name: "orion"},
		OIDC:     OIDC{Scopes: "openid profile email"},
		Storage:  Storage{LocalBasePath: "data/storage"},
		Slave:    Slave{StoragePath: "data/slave-storage"},
	}
}

// Load reads the INI file at path (if it exists) on top of the defaults, then
// applies environment-variable overrides. A missing file is not an error.
func Load(path string) (*Config, error) {
	cfg := Default()

	if _, err := os.Stat(path); err == nil {
		f, err := ini.Load(path)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := mapSections(f, cfg); err != nil {
			return nil, err
		}
	}

	applyEnv(cfg)
	return cfg, nil
}

// SessionSecretWeak reports whether the session secret is empty, the shipped
// default, or shorter than the minimum length — any of which lets an attacker
// forge session and WOPI tokens.
func (c *Config) SessionSecretWeak() bool {
	s := c.System.SessionSecret
	return s == "" || s == DefaultSessionSecret || len(s) < minSessionSecretLen
}

// Validate enforces security-critical invariants after Load. In release mode a
// weak session secret is fatal; debug mode only warns (see bootstrap) so local
// development with the example config still works.
func (c *Config) Validate() error {
	if c.System.Mode != "debug" && c.SessionSecretWeak() {
		return fmt.Errorf("System.SessionSecret is empty, the shipped default, or shorter than %d bytes: set a long random value (it keys session and WOPI tokens; a known value allows full authentication bypass)", minSessionSecretLen)
	}
	return nil
}

// mapSections maps each INI section onto the matching struct field.
func mapSections(f *ini.File, cfg *Config) error {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Name
		if sec, err := f.GetSection(name); err == nil {
			if err := sec.MapTo(v.Field(i).Addr().Interface()); err != nil {
				return fmt.Errorf("map section [%s]: %w", name, err)
			}
		}
	}
	return nil
}

// applyEnv overrides string/int fields from OD_CONF_<Section>_<Field>.
func applyEnv(cfg *Config) {
	root := reflect.ValueOf(cfg).Elem()
	rt := root.Type()
	for i := 0; i < rt.NumField(); i++ {
		section := rt.Field(i).Name
		secVal := root.Field(i)
		secType := secVal.Type()
		for j := 0; j < secType.NumField(); j++ {
			field := secType.Field(j).Name
			key := fmt.Sprintf("OD_CONF_%s_%s", section, field)
			raw, ok := os.LookupEnv(key)
			if !ok {
				continue
			}
			setField(secVal.Field(j), raw)
		}
	}
}

func setField(f reflect.Value, raw string) {
	if !f.CanSet() {
		return
	}
	switch f.Kind() {
	case reflect.String:
		f.SetString(raw)
	case reflect.Int, reflect.Int64:
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			f.SetInt(n)
		}
	case reflect.Bool:
		if b, err := strconv.ParseBool(raw); err == nil {
			f.SetBool(b)
		}
	}
}

// ScopeList splits the space-separated OIDC scopes into a slice.
func (o OIDC) ScopeList() []string {
	return strings.Fields(o.Scopes)
}

// IsSQLite reports whether the configured database is SQLite.
func (d Database) IsSQLite() bool { return strings.ToLower(d.Type) == "sqlite" || d.Type == "" }
