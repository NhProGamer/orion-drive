package controllers

import "testing"

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
