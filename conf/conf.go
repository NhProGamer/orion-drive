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
	Redis    Redis
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
}

// AdminEmailSet returns the lower-cased admin emails as a lookup set.
func (s System) AdminEmailSet() map[string]bool {
	set := map[string]bool{}
	for _, e := range strings.Split(s.AdminEmails, ",") {
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
}

// OIDC holds the OpenID Connect provider credentials.
type OIDC struct {
	Issuer       string `ini:"Issuer"`
	ClientID     string `ini:"ClientID"`
	ClientSecret string `ini:"ClientSecret"`
	RedirectURI  string `ini:"RedirectURI"`
	Scopes       string `ini:"Scopes"`
}

// Storage configures storage backends.
type Storage struct {
	LocalBasePath string `ini:"LocalBasePath"`
	// EncryptionKey is a 64-char hex (32-byte) AES-256 key enabling at-rest
	// encryption for storage policies that request it. Empty disables encryption.
	EncryptionKey string `ini:"EncryptionKey"`
}

// Slave turns this node into a storage slave when Secret is set: it exposes the
// signed slave storage API, backed by a local directory.
type Slave struct {
	Secret      string `ini:"Secret"`
	StoragePath string `ini:"StoragePath"`
}

// WOPI configures online Office editing via a WOPI client (Collabra Online,
// OnlyOffice Docs, ...). When ServerURL is empty, Office editing is disabled.
// EditURLTemplate builds the editor URL from the WOPI source; it must contain
// the {src} placeholder (URL-encoded WOPISrc), e.g. for Collabora:
// https://collabora.example.com/browser/dist/cool.html?WOPISrc={src}
type WOPI struct {
	ServerURL       string `ini:"ServerURL"`
	EditURLTemplate string `ini:"EditURLTemplate"`
}

// Enabled reports whether online Office editing is configured.
func (w WOPI) Enabled() bool { return w.ServerURL != "" && w.EditURLTemplate != "" }

// Redis optionally replaces the in-memory cache.
type Redis struct {
	Server   string `ini:"Server"`
	Password string `ini:"Password"`
	DB       int    `ini:"DB"`
}

// Default returns a configuration populated with sane development defaults.
func Default() *Config {
	return &Config{
		System: System{
			Listen:        ":5212",
			Mode:          "debug",
			SessionSecret: "change-me-to-a-long-random-string",
			SiteURL:       "http://localhost:5212",
		},
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
