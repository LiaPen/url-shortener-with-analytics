// Package api is the HTTP layer: it decodes requests, calls the store, and
// turns the result (or the error) into the status code and JSON the contract
// asks for. It holds no state of its own.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"time"

	"atad-project/internal/links"
	"atad-project/internal/version"
)

// Identity shown on GET /. The marking script reads this page.
const (
	studentName  = "Pentek Iulia"
	studentGroup = "[group]" // TODO: grupa ta, ex. "3.1"
	projectLine  = "Project 1: URL Shortener with Analytics"
)

type server struct {
	store   *links.Store
	started time.Time
}

// New returns the handler for the whole service: the four contract routes and
// the project's own routes.
func New(store *links.Store, started time.Time) http.Handler {
	s := &server{store: store, started: started}
	mux := http.NewServeMux()

	// ---- the common contract -------------------------------------------------
	mux.HandleFunc("GET /version", s.version)
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /{$}", s.identity)
	mux.HandleFunc("POST /reset", s.reset)

	// ---- this project --------------------------------------------------------
	// Go's router picks the most specific pattern, so GET /health and
	// GET /links win over GET /{code}; that is why those words are reserved.
	mux.HandleFunc("POST /links", s.createLink)
	mux.HandleFunc("GET /links", s.listLinks)
	mux.HandleFunc("GET /links/{code}/stats", s.stats)
	mux.HandleFunc("DELETE /links/{code}", s.deleteLink)
	mux.HandleFunc("GET /{code}", s.redirect)

	return mux
}

func (s *server) version(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = version.WriteJSON(w)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"uptime_seconds": int(time.Since(s.started).Seconds()),
	})
}

func (s *server) identity(w http.ResponseWriter, r *http.Request) {
	app := html.EscapeString(version.Current().App)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8">
<title>%s</title><body>
<h1>%s</h1>
<p>%s, group %s</p>
<p>%s</p>
</body></html>`, app, app, studentName, studentGroup, projectLine)
}

func (s *server) reset(w http.ResponseWriter, r *http.Request) {
	s.store.Reset()
	w.WriteHeader(http.StatusNoContent)
}

// ---- links -------------------------------------------------------------------

type createRequest struct {
	URL  string `json:"url"`
	Code string `json:"code"`
}

type linkResponse struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

type statsResponse struct {
	Code        string  `json:"code"`
	Visits      int64   `json:"visits"`
	LastVisitAt *string `json:"last_visit_at"` // null before the first visit
}

func toLinkResponse(l links.Link) linkResponse {
	return linkResponse{ID: l.ID, Code: l.Code, URL: l.URL, CreatedAt: l.CreatedAt.Format(time.RFC3339)}
}

func (s *server) createLink(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "body must be JSON like {\"url\":\"https://...\",\"code\":\"optional\"}")
		return
	}
	l, err := s.store.Create(req.URL, req.Code)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toLinkResponse(l))
}

func (s *server) listLinks(w http.ResponseWriter, r *http.Request) {
	all := s.store.List()
	out := make([]linkResponse, 0, len(all))
	for _, l := range all {
		out = append(out, toLinkResponse(l))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.Get(r.PathValue("code"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	resp := statsResponse{Code: l.Code, Visits: l.Visits}
	if l.LastVisitAt != nil {
		v := l.LastVisitAt.Format(time.RFC3339)
		resp.LastVisitAt = &v
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) deleteLink(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("code")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// redirect is the hot path. The visit is counted synchronously, under the
// store's lock, before the 302 is written: a client that got the redirect has
// been counted. In memory this costs a map lookup and two writes.
func (s *server) redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	// A code that could never have been created is a broken request (400),
	// which is distinguishable from a well-formed code nobody created (404).
	if err := links.ValidateCode(code); err != nil {
		writeStoreError(w, err)
		return
	}
	target, err := s.store.Resolve(code)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// ---- responses ---------------------------------------------------------------

// writeStoreError maps store errors to the contract's status codes in one place.
func writeStoreError(w http.ResponseWriter, err error) {
	var ve *links.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, "validation", ve.Message)
	case errors.Is(err, links.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no link with this code")
	case errors.Is(err, links.ErrCodeTaken):
		writeError(w, http.StatusConflict, "conflict", "this code is already taken")
	case errors.Is(err, links.ErrExhausted):
		writeError(w, http.StatusUnprocessableEntity, "unprocessable", "could not generate a free code, try again")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "unexpected error")
	}
}

// writeJSON is the one place that sets the content type, so every response in
// the service is consistent about it.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError produces the error shape the common contract requires.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}
