package server

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.writeError(w, r, http.StatusNotFound, codeNotFound, "no such endpoint: "+r.URL.Path)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.writeError(w, r, http.StatusMethodNotAllowed, codeBadRequest, "method not allowed")
		return
	}
	if s.static == nil {
		http.Error(w, "the admin panel was not built into this binary", http.StatusNotFound)
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	file, err := s.static.Open(name)
	if err == nil {
		info, statErr := file.(fs.File).Stat()
		if statErr == nil && info.IsDir() {
			_ = file.Close()
			name = path.Join(name, "index.html")
			file, err = s.static.Open(name)
		}
	}
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	defer file.Close()

	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		w.Header().Set("Content-Type", contentTypeFor(name))
		_, _ = io.Copy(w, file)
		return
	}
	info, err := file.(fs.File).Stat()
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), seeker)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	file, err := s.static.Open("index.html")
	if err != nil {
		http.Error(w, "the admin panel was not built into this binary", http.StatusNotFound)
		return
	}
	defer file.Close()
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.Copy(w, file)
}

var inlineScriptPattern = regexp.MustCompile(`(?is)<script(?:\s[^>]*)?>(.*?)</script>`)

func inlineScriptHashes(static fs.FS) []string {
	if static == nil {
		return nil
	}
	body, err := fs.ReadFile(static, "index.html")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, match := range inlineScriptPattern.FindAllSubmatch(body, -1) {
		script := match[1]
		if len(strings.TrimSpace(string(script))) == 0 {
			continue
		}
		sum := sha256.Sum256(script)
		hash := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
		if seen[hash] {
			continue
		}
		seen[hash] = true
		out = append(out, hash)
	}
	return out
}

func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".woff"):
		return "font/woff"
	default:
		return "application/octet-stream"
	}
}
