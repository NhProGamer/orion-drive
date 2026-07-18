package wopi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleDiscovery = `<?xml version="1.0"?>
<wopi-discovery>
  <net-zone name="external-https">
    <app name="Word">
      <action name="edit" ext="docx" urlsrc="https://docs.example/word/edit?&lt;ui=UI_LLCC&amp;&gt;&lt;rs=DC_LLCC&amp;&gt;WOPISrc="/>
      <action name="editnew" ext="docx" urlsrc="https://docs.example/word/new?WOPISrc="/>
      <action name="view" ext="pdf" urlsrc="https://docs.example/word/view?WOPISrc="/>
    </app>
    <app name="Excel">
      <action name="edit" ext="xlsx" urlsrc="https://docs.example/cell/edit?WOPISrc="/>
    </app>
  </net-zone>
</wopi-discovery>`

func TestDiscoveryParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(sampleDiscovery))
	}))
	defer srv.Close()

	d := NewDiscovery(srv.URL, time.Hour)
	ctx := context.Background()

	if u, editable, ok := d.Action(ctx, "docx", true); !ok || !editable || u == "" {
		t.Fatalf("docx edit: u=%q editable=%v ok=%v", u, editable, ok)
	}
	// PDF has only a view action: a write request must downgrade to view-only.
	if _, editable, ok := d.Action(ctx, "pdf", true); !ok || editable {
		t.Fatalf("pdf should be view-only: editable=%v ok=%v", editable, ok)
	}
	// Unknown format is not supported at all.
	if _, _, ok := d.Action(ctx, "zip", true); ok {
		t.Fatal("zip should not be supported")
	}
	if got := d.EditExts(ctx); len(got) != 2 { // docx, xlsx
		t.Fatalf("edit exts = %v", got)
	}
	if got := d.ViewExts(ctx); len(got) != 3 { // docx, xlsx, pdf
		t.Fatalf("view exts = %v", got)
	}
	// docx has an editnew action; xlsx does not.
	if _, ok := d.NewAction(ctx, "docx"); !ok {
		t.Fatal("docx should be creatable")
	}
	if _, ok := d.NewAction(ctx, "xlsx"); ok {
		t.Fatal("xlsx should not be creatable")
	}
	if got := d.NewExts(ctx); len(got) != 1 || got[0] != "docx" {
		t.Fatalf("new exts = %v", got)
	}
}

func TestBuildActionURL(t *testing.T) {
	got := BuildActionURL("https://docs.example/word/edit?<ui=UI_LLCC&><rs=DC_LLCC&>WOPISrc=", "https://od/wopi/files/7")
	want := "https://docs.example/word/edit?WOPISrc=https%3A%2F%2Fod%2Fwopi%2Ffiles%2F7"
	if got != want {
		t.Fatalf("BuildActionURL = %q, want %q", got, want)
	}
}

func TestProofVerify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pk := &ProofKeys{Current: &key.PublicKey}
	token, url, ts := "acc-token", "https://od/wopi/files/7?access_token=acc-token", int64(638000000000000000)

	sum := sha256.Sum256(expected(token, url, ts))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	proof := base64.StdEncoding.EncodeToString(sig)

	if !pk.Verify(token, url, ts, proof, "") {
		t.Fatal("valid proof rejected")
	}
	if pk.Verify(token, "https://od/OTHER", ts, proof, "") {
		t.Fatal("proof accepted for wrong URL")
	}
	if pk.Verify(token, url, ts+1, proof, "") {
		t.Fatal("proof accepted for wrong timestamp")
	}
}
