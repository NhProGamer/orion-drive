package thumb

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
)

// docServerURL records whether a WOPI document server is configured (from
// WOPI.ServerURL); it only gates Available(KindDocument). The actual rendering
// is driven from the caller via CollaboraConvert / OnlyOfficeConvert, which
// receive the server URL (and, for OnlyOffice, a fetchable file URL + JWT
// secret) explicitly.
var docServerURL string

// CollaboraConvert renders the first page of a document to a JPEG thumbnail
// through a Collabora Online / CODE server's convert-to API, uploading the file
// directly. Hosts running Collabora get document previews without a local
// LibreOffice.
func CollaboraConvert(ctx context.Context, serverURL, path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "document")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, io.LimitReader(f, maxCoverSource)); err != nil {
		return nil, err
	}
	w.Close()

	base := strings.TrimRight(serverURL, "/")
	var lastErr error
	// /cool is the current Collabora path; /lool is the legacy alias.
	for _, ep := range []string{"/cool/convert-to/png", "/lool/convert-to/png"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+ep, bytes.NewReader(body.Bytes()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		png, _ := io.ReadAll(io.LimitReader(resp.Body, maxCoverSource))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && len(png) > 0 {
			return Image(png)
		}
		lastErr = fmt.Errorf("thumb: convert-to %s: status %d", ep, resp.StatusCode)
	}
	return nil, lastErr
}

// OnlyOfficeConvert renders a document thumbnail through an OnlyOffice Document
// Server's conversion API. OnlyOffice fetches the file itself from fileURL
// (which must be reachable from the document server), so the caller supplies a
// tokenised download URL; the request is signed with the server's JWT secret.
func OnlyOfficeConvert(ctx context.Context, serverURL, secret, fileURL, filetype, key string) ([]byte, error) {
	params := map[string]any{
		"async":      false,
		"filetype":   filetype,
		"outputtype": "png",
		"key":        key,
		"title":      "document." + filetype,
		"url":        fileURL,
		"thumbnail":  map[string]any{"first": true, "width": maxDim, "height": maxDim, "aspect": 1},
	}
	if secret != "" {
		tok, err := signJWT(params, secret)
		if err != nil {
			return nil, err
		}
		params["token"] = tok
	}
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(serverURL, "/") + "/ConvertService.ashx"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if secret != "" {
		if tok, ok := params["token"].(string); ok {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		EndConvert bool   `json:"endConvert"`
		FileURL    string `json:"fileUrl"`
		Error      int    `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("thumb: onlyoffice convert: %w", err)
	}
	if out.Error != 0 || out.FileURL == "" {
		return nil, fmt.Errorf("thumb: onlyoffice convert error %d", out.Error)
	}

	// Download the rendered PNG.
	imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, out.FileURL, nil)
	if err != nil {
		return nil, err
	}
	imgResp, err := http.DefaultClient.Do(imgReq)
	if err != nil {
		return nil, err
	}
	defer imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("thumb: onlyoffice fetch png: status %d", imgResp.StatusCode)
	}
	png, err := io.ReadAll(io.LimitReader(imgResp.Body, maxCoverSource))
	if err != nil {
		return nil, err
	}
	return Image(png)
}

// signJWT builds a compact HS256 JWT for the given claims and secret.
func signJWT(claims map[string]any, secret string) (string, error) {
	b64 := base64.RawURLEncoding
	header := b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := header + "." + b64.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + b64.EncodeToString(mac.Sum(nil)), nil
}
