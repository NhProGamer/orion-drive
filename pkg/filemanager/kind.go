package filemanager

import (
	"path"
	"strings"
)

// kindExtensions groups file extensions into the coarse categories used by the
// search filter and the storage breakdown. Categories are disjoint. Anything
// not listed falls into "other".
var kindExtensions = map[string]string{
	// images
	"jpg": "images", "jpeg": "images", "png": "images", "gif": "images",
	"webp": "images", "svg": "images", "bmp": "images", "tiff": "images",
	"heic": "images", "avif": "images", "fig": "images", "sketch": "images",
	// media (video + audio)
	"mp4": "media", "mov": "media", "mkv": "media", "webm": "media", "avi": "media", "m4v": "media",
	"mp3": "media", "wav": "media", "flac": "media", "m4a": "media", "aac": "media", "ogg": "media", "opus": "media",
	// documents
	"pdf": "documents", "doc": "documents", "docx": "documents", "odt": "documents",
	"rtf": "documents", "txt": "documents", "md": "documents",
	"xls": "documents", "xlsx": "documents", "csv": "documents", "ods": "documents",
	"ppt": "documents", "pptx": "documents", "odp": "documents", "key": "documents",
	// archives & disc images
	"zip": "archives", "tar": "archives", "gz": "archives", "rar": "archives",
	"7z": "archives", "iso": "archives", "img": "archives",
}

// fileKind returns the coarse category for a file name.
func fileKind(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if k, ok := kindExtensions[ext]; ok {
		return k
	}
	return "other"
}

// matchesKind reports whether name belongs to the given category. An empty
// category matches everything.
func matchesKind(name, kind string) bool {
	if kind == "" {
		return true
	}
	return fileKind(name) == kind
}
