package controllers

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/gin-gonic/gin"
)

// TestSniffImageType checks the upload type comes from the bytes: accepted
// image formats pass, anything else (HTML posing as an image) is refused.
func TestSniffImageType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	cases := []struct {
		name string
		data []byte
		want string
		ok   bool
	}{
		{"png", png, "image/png", true},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "image/svg+xml", true},
		{"svg with prolog", []byte(`<?xml version="1.0"?><SVG></SVG>`), "image/svg+xml", true},
		{"html", []byte(`<html><body>hi</body></html>`), "", false},
		{"text", []byte("hello"), "", false},
		{"empty", nil, "", false},
	}
	for _, tc := range cases {
		got, ok := sniffImageType(tc.data)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// TestSiteAssetCaching checks only the hash-versioned URL is cached forever:
// any other URL revalidates, and every response carries the sandbox CSP.
func TestSiteAssetCaching(t *testing.T) {
	ctl, _, _ := ogEnv(t)
	a := &model.SiteAsset{Name: "favicon", ContentType: "image/png", ETag: "abc123", Data: ogPNG(t)}
	if err := ctl.dep.Repo.SiteAsset.Put(context.Background(), a); err != nil {
		t.Fatalf("put: %v", err)
	}
	serve := func(query, ifNoneMatch string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/site/asset/favicon"+query, nil)
		if ifNoneMatch != "" {
			c.Request.Header.Set("If-None-Match", ifNoneMatch)
		}
		c.Params = gin.Params{{Key: "name", Value: "favicon"}}
		ctl.SiteAsset(c)
		c.Writer.WriteHeaderNow()
		return w
	}
	cases := []struct{ query, cache string }{
		{"?v=abc123", "public, max-age=31536000, immutable"},
		{"?v=stale", "no-cache"},
		{"", "no-cache"},
	}
	for _, tc := range cases {
		w := serve(tc.query, "")
		if w.Code != 200 || w.Header().Get("Cache-Control") != tc.cache {
			t.Errorf("%q: status %d, Cache-Control %q; want 200, %q", tc.query, w.Code, w.Header().Get("Cache-Control"), tc.cache)
		}
		if w.Header().Get("Content-Security-Policy") != siteAssetCSP {
			t.Errorf("%q: missing sandbox CSP", tc.query)
		}
	}
	if w := serve("", `"abc123"`); w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != `"abc123"` {
		t.Errorf("If-None-Match: status %d, body %d bytes, ETag %q; want an empty 304", w.Code, w.Body.Len(), w.Header().Get("ETag"))
	}
	if w := serve("", `"old"`); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), a.Data) {
		t.Errorf("stale If-None-Match: status %d, %d bytes; want 200 with the image", w.Code, w.Body.Len())
	}
	if w := serve("", ""); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), a.Data) {
		t.Errorf("plain GET: status %d, %d bytes; want 200 with the image", w.Code, w.Body.Len())
	}
}
