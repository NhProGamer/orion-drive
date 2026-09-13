// WebDAV SEARCH (RFC 5323 / DASL). x/net/webdav does not implement SEARCH, so
// the auth handler intercepts the method and answers it here, reusing
// OrionDrive's recursive name search and rendering a 207 multistatus.
package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/repository"
)

// handleSearch answers a WebDAV SEARCH request. It extracts the search term from
// the DASL query (a <D:like>/<D:literal> or <D:contains> value), runs the
// recursive name search, and returns matching resources as a multistatus.
func (h *authHandler) handleSearch(w http.ResponseWriter, r *http.Request, user *model.User) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	term := extractSearchTerm(body)
	if term == "" {
		http.Error(w, "empty search query", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	hits, _, err := h.fs.mgr.Search(ctx, user, repository.SearchFilters{Query: term}, "")
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}

	ms := searchMultistatus{Xmlns: "DAV:"}
	pathCache := map[uint]string{}
	for i := range hits {
		f := &hits[i]
		p := h.pathOf(ctx, user.ID, f, pathCache)
		resp := searchResponse{Href: Prefix + encodePath(p)}
		prop := searchProp{
			DisplayName:     f.Name,
			GetLastModified: f.UpdatedAt.UTC().Format(http.TimeFormat),
		}
		if f.IsFolder() {
			prop.ResourceType.Collection = &struct{}{}
			resp.Href += "/"
		} else {
			size := f.Size
			prop.GetContentLength = &size
		}
		resp.Propstat = searchPropstat{Prop: prop, Status: "HTTP/1.1 200 OK"}
		ms.Responses = append(ms.Responses, resp)
	}

	out, err := xml.Marshal(ms)
	if err != nil {
		http.Error(w, "encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(out)
}

// pathOf builds a file's absolute virtual path ("/a/b/c") by walking its
// ancestors, memoising resolved ancestor paths per request.
func (h *authHandler) pathOf(ctx context.Context, ownerID uint, f *model.File, cache map[uint]string) string {
	segs := []string{f.Name}
	pid := f.ParentID
	for pid != nil {
		if base, ok := cache[*pid]; ok {
			return base + "/" + strings.Join(segs, "/")
		}
		p, err := h.repo.File.GetByID(ctx, ownerID, *pid)
		if err != nil {
			break
		}
		segs = append([]string{p.Name}, segs...)
		pid = p.ParentID
	}
	full := "/" + strings.Join(segs, "/")
	if f.ParentID != nil {
		cache[*f.ParentID] = strings.TrimSuffix(full, "/"+f.Name)
	}
	return full
}

// encodePath percent-encodes each path segment for use in an href.
func encodePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

// extractSearchTerm pulls the search string out of a DASL query, tolerating any
// namespace: it grabs the text inside a <literal> (used by <like>) or a
// <contains> element and strips SQL-style % wildcards.
func extractSearchTerm(body []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(body))
	inTerm := false
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch strings.ToLower(t.Name.Local) {
			case "literal", "contains":
				inTerm = true
			}
		case xml.EndElement:
			switch strings.ToLower(t.Name.Local) {
			case "literal", "contains":
				inTerm = false
			}
		case xml.CharData:
			if inTerm {
				sb.Write(t)
			}
		}
	}
	return strings.Trim(strings.TrimSpace(sb.String()), "%")
}

// --- multistatus response shapes -------------------------------------------

type searchMultistatus struct {
	XMLName   xml.Name         `xml:"D:multistatus"`
	Xmlns     string           `xml:"xmlns:D,attr"`
	Responses []searchResponse `xml:"D:response"`
}

type searchResponse struct {
	Href     string         `xml:"D:href"`
	Propstat searchPropstat `xml:"D:propstat"`
}

type searchPropstat struct {
	Prop   searchProp `xml:"D:prop"`
	Status string     `xml:"D:status"`
}

type searchProp struct {
	DisplayName      string             `xml:"D:displayname"`
	GetLastModified  string             `xml:"D:getlastmodified"`
	GetContentLength *int64             `xml:"D:getcontentlength,omitempty"`
	ResourceType     searchResourceType `xml:"D:resourcetype"`
}

type searchResourceType struct {
	Collection *struct{} `xml:"D:collection"`
}
