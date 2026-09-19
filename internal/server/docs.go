package server

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/docs"
)

type docsIndexItem struct {
	Path        string         `json:"path"`
	Title       string         `json:"title"`
	Section     string         `json:"section"`
	Description string         `json:"description"`
	Headings    []docs.Heading `json:"headings"`
}

type docsIndexResponse struct {
	Items    []docsIndexItem `json:"items"`
	Sections []string        `json:"sections"`
}

type docsPageResponse struct {
	Path     string         `json:"path"`
	Title    string         `json:"title"`
	Section  string         `json:"section"`
	Headings []docs.Heading `json:"headings"`
	Markdown string         `json:"markdown"`
	Words    int            `json:"words"`
	Prev     *docs.Link     `json:"prev"`
	Next     *docs.Link     `json:"next"`
}

type docsSearchResponse struct {
	Items []docs.SearchHit `json:"items"`
}

func headingsOrEmpty(values []docs.Heading) []docs.Heading {
	if values == nil {
		return []docs.Heading{}
	}
	return values
}

func (s *Server) handleDocsIndex(w http.ResponseWriter, r *http.Request) {
	if s.docs == nil {
		writeJSON(w, http.StatusOK, docsIndexResponse{Items: []docsIndexItem{}, Sections: []string{}})
		return
	}
	pages := s.docs.Pages()
	items := make([]docsIndexItem, 0, len(pages))
	for _, page := range pages {
		items = append(items, docsIndexItem{
			Path:        page.Path,
			Title:       page.Title,
			Section:     page.Section,
			Description: page.Description,
			Headings:    headingsOrEmpty(page.Headings),
		})
	}
	sections := s.docs.Sections()
	if sections == nil {
		sections = []string{}
	}
	writeJSON(w, http.StatusOK, docsIndexResponse{Items: items, Sections: sections})
}

func (s *Server) handleDocsPage(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("path"))
	if name == "" || strings.Contains(name, "..") || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "/") {
		s.writeErrorFields(w, r, http.StatusBadRequest, codeValidation, "path must be a documentation page such as sources/postgres.md", map[string]string{"path": "invalid"})
		return
	}
	if s.docs == nil {
		s.writeError(w, r, http.StatusNotFound, codeNotFound, "no such documentation page: "+name)
		return
	}
	page, ok := s.docs.Lookup(name)
	if !ok {
		s.writeError(w, r, http.StatusNotFound, codeNotFound, "no such documentation page: "+name)
		return
	}
	prev, next := s.docs.Neighbours(name)
	writeJSON(w, http.StatusOK, docsPageResponse{
		Path:     page.Path,
		Title:    page.Title,
		Section:  page.Section,
		Headings: headingsOrEmpty(page.Headings),
		Markdown: page.Markdown(),
		Words:    page.Words,
		Prev:     prev,
		Next:     next,
	})
}

func (s *Server) handleDocsSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 20 {
			limit = parsed
		}
	}
	if s.docs == nil {
		writeJSON(w, http.StatusOK, docsSearchResponse{Items: []docs.SearchHit{}})
		return
	}
	writeJSON(w, http.StatusOK, docsSearchResponse{Items: s.docs.Search(query, limit)})
}

func (s *Server) handleDocsAsset(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "*")
	if s.docs == nil {
		s.writeError(w, r, http.StatusNotFound, codeNotFound, "no such documentation asset")
		return
	}
	file, contentType, ok := s.docs.Asset(name)
	if !ok {
		s.writeError(w, r, http.StatusNotFound, codeNotFound, "no such documentation asset")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if seeker, isSeeker := file.(io.ReadSeeker); isSeeker {
		info, err := file.Stat()
		if err == nil {
			http.ServeContent(w, r, name, info.ModTime(), seeker)
			return
		}
	}
	_, _ = io.Copy(w, file)
}
