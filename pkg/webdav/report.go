// WebDAV sync-collection REPORT (RFC 6578): incremental collection sync. The
// client sends a sync-token; the server returns only the members changed since
// then (upserts + removed tombstones) plus a fresh token. x/net/webdav does not
// implement REPORT, so the auth handler dispatches it here.
package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/NhProGamer/orion-drive/model"
)

const syncTokenPrefix = "urn:orion:sync:"

// handleReport answers a REPORT request. Only DAV:sync-collection is supported.
func (h *authHandler) handleReport(w http.ResponseWriter, r *http.Request, user *model.User) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	token, isSync := parseSyncCollection(body)
	if !isSync {
		http.Error(w, "unsupported REPORT", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	collPath := strings.TrimPrefix(r.URL.Path, Prefix)
	if collPath == "" {
		collPath = "/"
	}
	target, err := h.fs.resolve(ctx, user, collPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if target != nil && !target.IsFolder() {
		http.Error(w, "not a collection", http.StatusForbidden)
		return
	}

	ms := syncMultistatus{Xmlns: "DAV:"}
	newToken, _ := h.repo.FileChange.MaxID(ctx, user.ID)

	if strings.TrimSpace(token) == "" {
		// Initial sync: enumerate the whole subtree as upserts.
		base := strings.TrimRight(collPath, "/")
		var rootID *uint
		if target != nil {
			id := target.ID
			rootID = &id
		}
		h.enumerate(ctx, user.ID, rootID, base, &ms)
	} else {
		// Incremental: only the members changed since the client's token. Collapse
		// to the latest change per path so a create+edit is one upsert.
		since := parseSyncToken(token)
		changes, _ := h.repo.FileChange.Since(ctx, user.ID, since, cleanCollPrefix(collPath))
		latest := map[string]model.FileChange{}
		order := []string{}
		for _, c := range changes {
			if _, seen := latest[c.Path]; !seen {
				order = append(order, c.Path)
			}
			latest[c.Path] = c
		}
		for _, p := range order {
			c := latest[p]
			if c.Deleted {
				ms.Responses = append(ms.Responses, removedResponse(p, c.IsDir))
				continue
			}
			// Re-resolve for fresh props; if it's gone now, report it removed.
			f, rerr := h.fs.resolve(ctx, user, p)
			if rerr != nil || f == nil {
				ms.Responses = append(ms.Responses, removedResponse(p, c.IsDir))
				continue
			}
			ms.Responses = append(ms.Responses, upsertResponse(p, f))
		}
	}

	ms.SyncToken = syncTokenPrefix + strconv.FormatUint(uint64(newToken), 10)
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

// enumerate walks the subtree under rootID (nil = drive root) breadth-first,
// adding each member as an upsert. Paths are built as we descend (no per-file
// ancestor walk).
func (h *authHandler) enumerate(ctx context.Context, ownerID uint, rootID *uint, rootPath string, ms *syncMultistatus) {
	type node struct {
		id   *uint
		path string
	}
	queue := []node{{rootID, rootPath}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		kids, err := h.repo.File.ListChildren(ctx, ownerID, cur.id)
		if err != nil {
			continue
		}
		for i := range kids {
			k := &kids[i]
			p := cur.path + "/" + k.Name
			ms.Responses = append(ms.Responses, upsertResponse(p, k))
			if k.IsFolder() {
				id := k.ID
				queue = append(queue, node{&id, p})
			}
		}
	}
}

func upsertResponse(path string, f *model.File) syncResponse {
	href := Prefix + encodePath(path)
	prop := searchProp{DisplayName: f.Name, GetLastModified: f.UpdatedAt.UTC().Format(http.TimeFormat)}
	if f.IsFolder() {
		prop.ResourceType.Collection = &struct{}{}
		href += "/"
	} else {
		size := f.Size
		prop.GetContentLength = &size
	}
	return syncResponse{Href: href, Propstat: &searchPropstat{Prop: prop, Status: "HTTP/1.1 200 OK"}}
}

func removedResponse(path string, isDir bool) syncResponse {
	href := Prefix + encodePath(path)
	if isDir {
		href += "/"
	}
	return syncResponse{Href: href, Status: "HTTP/1.1 404 Not Found"}
}

// cleanCollPrefix normalises a collection path for journal prefix matching.
func cleanCollPrefix(p string) string {
	p = "/" + strings.Trim(p, "/")
	return p
}

// parseSyncToken extracts the numeric id from a sync token (our urn:orion:sync:N
// form, tolerating a bare number).
func parseSyncToken(s string) uint {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return uint(n)
}

// parseSyncCollection reports whether the body is a DAV:sync-collection report
// and returns the client's sync-token text (namespace-tolerant).
func parseSyncCollection(body []byte) (token string, ok bool) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	inToken := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch strings.ToLower(t.Name.Local) {
			case "sync-collection":
				ok = true
			case "sync-token":
				inToken = true
			}
		case xml.EndElement:
			if strings.ToLower(t.Name.Local) == "sync-token" {
				inToken = false
			}
		case xml.CharData:
			if inToken {
				token += string(t)
			}
		}
	}
	return strings.TrimSpace(token), ok
}

// --- sync multistatus shapes (reuses searchProp/searchPropstat) --------------

type syncMultistatus struct {
	XMLName   xml.Name       `xml:"D:multistatus"`
	Xmlns     string         `xml:"xmlns:D,attr"`
	Responses []syncResponse `xml:"D:response"`
	SyncToken string         `xml:"D:sync-token"`
}

type syncResponse struct {
	Href     string          `xml:"D:href"`
	Propstat *searchPropstat `xml:"D:propstat,omitempty"`
	Status   string          `xml:"D:status,omitempty"`
}
