package thumb

import (
	"archive/zip"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// Ebook extracts the cover image embedded in an e-book / comic file and returns
// it scaled down as a JPEG. It is pure Go (no external tool) and supports:
//   - EPUB, CBZ, CBT   — ZIP containers (cover metadata for EPUB, first image otherwise)
//   - FB2              — XML with base64-embedded binaries
//   - MOBI, AZW, AZW3  — Palm database with embedded image records (EXTH cover)
//
// It fails (leaving the UI to fall back to a type icon) when no cover is found.
func Ebook(path_ string, ext string) ([]byte, error) {
	var cover []byte
	var err error
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "epub":
		cover, err = epubCover(path_)
	case "cbz", "cbt":
		cover, err = zipFirstImage(path_)
	case "fb2":
		cover, err = fb2Cover(path_)
	case "mobi", "azw", "azw3":
		cover, err = mobiCover(path_)
	default:
		return nil, fmt.Errorf("thumb: unsupported e-book type %q", ext)
	}
	if err != nil {
		return nil, err
	}
	// Scale/re-encode the extracted cover through the built-in image pipeline.
	return Image(cover)
}

// maxCoverSource caps how many bytes are read from an e-book source when
// extracting its cover, guarding against pathologically large inputs.
const maxCoverSource = 100 << 20

// imageZipExts are the raster formats the built-in decoder can read, used to
// spot cover pages inside ZIP-based containers.
var imageZipExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".bmp": true, ".tif": true, ".tiff": true,
}

// zipFirstImage returns the first image entry (by sorted path) of a ZIP,
// which for CBZ/CBT comics is the cover page.
func zipFirstImage(p string) ([]byte, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	names := make([]string, 0, len(zr.File))
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		if imageZipExts[strings.ToLower(path.Ext(f.Name))] {
			names = append(names, f.Name)
			byName[f.Name] = f
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("thumb: no image in archive")
	}
	sort.Strings(names)
	return readZipEntry(byName[names[0]])
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxCoverSource))
}

// --- EPUB ------------------------------------------------------------------

// epubCover resolves the cover declared in the OPF package, falling back to the
// first image in the archive when no cover metadata is present.
func epubCover(p string) ([]byte, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
	}

	if href := epubCoverHref(byName); href != "" {
		if f := byName[href]; f != nil {
			if data, err := readZipEntry(f); err == nil {
				return data, nil
			}
		}
	}
	// Fallback: first image entry.
	return zipFirstImage(p)
}

// epubCoverHref parses container.xml then the OPF to find the cover image path
// (normalised to a full archive path), or "" if none is declared.
func epubCoverHref(byName map[string]*zip.File) string {
	cf := byName["META-INF/container.xml"]
	if cf == nil {
		return ""
	}
	cdata, err := readZipEntry(cf)
	if err != nil {
		return ""
	}
	var container struct {
		Rootfiles []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(cdata, &container); err != nil || len(container.Rootfiles) == 0 {
		return ""
	}
	opfPath := container.Rootfiles[0].FullPath
	of := byName[opfPath]
	if of == nil {
		return ""
	}
	odata, err := readZipEntry(of)
	if err != nil {
		return ""
	}
	var pkg struct {
		Meta []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"metadata>meta"`
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"manifest>item"`
	}
	if err := xml.Unmarshal(odata, &pkg); err != nil {
		return ""
	}

	// EPUB3: item with properties="cover-image". EPUB2: <meta name="cover"> → item id.
	var href string
	for _, it := range pkg.Items {
		if strings.Contains(it.Properties, "cover-image") {
			href = it.Href
			break
		}
	}
	if href == "" {
		var coverID string
		for _, m := range pkg.Meta {
			if m.Name == "cover" {
				coverID = m.Content
				break
			}
		}
		if coverID != "" {
			for _, it := range pkg.Items {
				if it.ID == coverID {
					href = it.Href
					break
				}
			}
		}
	}
	if href == "" {
		return ""
	}
	// href is relative to the OPF directory.
	return path.Join(path.Dir(opfPath), href)
}

// --- FB2 -------------------------------------------------------------------

// fb2Cover extracts the cover binary from an FB2 (FictionBook) XML file. It
// prefers the binary referenced by <coverpage>, else the first image binary.
func fb2Cover(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := xml.NewDecoder(io.LimitReader(f, maxCoverSource))
	var coverID string              // id referenced by <coverpage><image href="#id">
	binaries := map[string]string{} // id -> base64 payload (image content-type only)
	first := ""                     // first image binary id seen

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "image":
			if coverID == "" {
				for _, a := range se.Attr {
					if a.Name.Local == "href" {
						coverID = strings.TrimPrefix(a.Value, "#")
					}
				}
			}
		case "binary":
			var id, ctype string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "id":
					id = a.Value
				case "content-type":
					ctype = a.Value
				}
			}
			if !strings.HasPrefix(ctype, "image/") {
				continue
			}
			var payload string
			if err := dec.DecodeElement(&payload, &se); err != nil {
				continue
			}
			binaries[id] = payload
			if first == "" {
				first = id
			}
		}
	}

	pick := coverID
	if pick == "" || binaries[pick] == "" {
		pick = first
	}
	if pick == "" || binaries[pick] == "" {
		return nil, fmt.Errorf("thumb: no cover binary in fb2")
	}
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, binaries[pick])
	return base64.StdEncoding.DecodeString(clean)
}

// --- MOBI / AZW / AZW3 -----------------------------------------------------

// mobiCover extracts the cover from a Palm-database e-book. It reads the EXTH
// cover-offset record when present, and otherwise falls back to the first
// embedded image record.
func mobiCover(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCoverSource))
	if err != nil {
		return nil, err
	}
	records, err := palmRecords(data)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("thumb: empty palm database")
	}

	// Try the EXTH cover pointer for an exact match.
	if idx, ok := mobiCoverIndex(records[0]); ok && idx < len(records) && isImageMagic(records[idx]) {
		return records[idx], nil
	}
	// Fallback: first record that looks like an image.
	for _, r := range records {
		if isImageMagic(r) {
			return r, nil
		}
	}
	return nil, fmt.Errorf("thumb: no image record in mobi")
}

// palmRecords splits a PalmDB file into its record byte-slices.
func palmRecords(data []byte) ([][]byte, error) {
	if len(data) < 78 {
		return nil, fmt.Errorf("thumb: short palm header")
	}
	n := int(binary.BigEndian.Uint16(data[76:78]))
	if n == 0 || len(data) < 78+n*8 {
		return nil, fmt.Errorf("thumb: bad palm record count")
	}
	offsets := make([]int, n)
	for i := 0; i < n; i++ {
		offsets[i] = int(binary.BigEndian.Uint32(data[78+i*8 : 82+i*8]))
	}
	recs := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		start := offsets[i]
		end := len(data)
		if i+1 < n {
			end = offsets[i+1]
		}
		if start < 0 || end > len(data) || start > end {
			return nil, fmt.Errorf("thumb: bad palm offsets")
		}
		recs = append(recs, data[start:end])
	}
	return recs, nil
}

// mobiCoverIndex reads record 0 (PalmDOC + MOBI header + EXTH) and returns the
// absolute record index of the cover image, if the EXTH cover record is present.
func mobiCoverIndex(rec0 []byte) (int, bool) {
	if len(rec0) < 132 || string(rec0[16:20]) != "MOBI" {
		return 0, false
	}
	mobiLen := int(binary.BigEndian.Uint32(rec0[20:24]))
	firstImage := binary.BigEndian.Uint32(rec0[108:112])
	exthFlags := binary.BigEndian.Uint32(rec0[128:132])
	if exthFlags&0x40 == 0 || firstImage == 0xFFFFFFFF {
		return 0, false
	}
	exthStart := 16 + mobiLen
	if exthStart+12 > len(rec0) || string(rec0[exthStart:exthStart+4]) != "EXTH" {
		return 0, false
	}
	count := int(binary.BigEndian.Uint32(rec0[exthStart+8 : exthStart+12]))
	pos := exthStart + 12
	for i := 0; i < count; i++ {
		if pos+8 > len(rec0) {
			break
		}
		rtype := binary.BigEndian.Uint32(rec0[pos : pos+4])
		rlen := int(binary.BigEndian.Uint32(rec0[pos+4 : pos+8]))
		if rlen < 8 || pos+rlen > len(rec0) {
			break
		}
		if rtype == 201 && rlen == 12 { // 201 = cover offset (uint32)
			coverOff := binary.BigEndian.Uint32(rec0[pos+8 : pos+12])
			if coverOff != 0xFFFFFFFF {
				return int(firstImage + coverOff), true
			}
		}
		pos += rlen
	}
	return 0, false
}

// isImageMagic reports whether b starts with a JPEG/PNG/GIF signature.
func isImageMagic(b []byte) bool {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF: // JPEG
		return true
	case len(b) >= 8 && string(b[0:8]) == "\x89PNG\r\n\x1a\n": // PNG
		return true
	case len(b) >= 6 && (string(b[0:6]) == "GIF87a" || string(b[0:6]) == "GIF89a"): // GIF
		return true
	default:
		return false
	}
}
