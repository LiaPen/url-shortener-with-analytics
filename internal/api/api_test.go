package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"atad-project/internal/links"
)

func newTestServer() http.Handler {
	return New(links.NewStore(), time.Now())
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
}

// The manual test from the project description, as an automated test.
// Step 4 is the real one: reading stats twice must report visits=1 both times.
func TestProjectScenario(t *testing.T) {
	h := newTestServer()

	if w := do(t, h, "POST", "/reset", ""); w.Code != http.StatusNoContent {
		t.Fatalf("reset: %d", w.Code)
	}

	w := do(t, h, "POST", "/links", `{"url":"https://go.dev/doc/effective_go","code":"gofmt"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created struct {
		ID        int64  `json:"id"`
		Code      string `json:"code"`
		URL       string `json:"url"`
		CreatedAt string `json:"created_at"`
	}
	decode(t, w, &created)
	if created.ID != 1 || created.Code != "gofmt" || created.CreatedAt == "" {
		t.Errorf("create body: %+v", created)
	}

	w = do(t, h, "POST", "/links", `{"url":"https://example.org","code":"gofmt"}`)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate code: %d, want 409", w.Code)
	}

	w = do(t, h, "GET", "/gofmt", "")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://go.dev/doc/effective_go" {
		t.Errorf("redirect: %d Location=%q", w.Code, w.Header().Get("Location"))
	}

	for i := range 2 {
		w = do(t, h, "GET", "/links/gofmt/stats", "")
		var st struct {
			Code        string  `json:"code"`
			Visits      int     `json:"visits"`
			LastVisitAt *string `json:"last_visit_at"`
		}
		decode(t, w, &st)
		if w.Code != http.StatusOK || st.Visits != 1 || st.LastVisitAt == nil {
			t.Errorf("stats read %d: %d %+v, want visits=1", i+1, w.Code, st)
		}
	}
}

func TestStatsBeforeFirstVisitIsNull(t *testing.T) {
	h := newTestServer()
	do(t, h, "POST", "/links", `{"url":"https://go.dev","code":"fresh"}`)
	w := do(t, h, "GET", "/links/fresh/stats", "")
	if !strings.Contains(w.Body.String(), `"last_visit_at":null`) {
		t.Errorf("body %s should contain last_visit_at:null", w.Body)
	}
}

func TestErrors(t *testing.T) {
	h := newTestServer()
	cases := []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"not json", "POST", "/links", `nope`, 400, "validation"},
		{"bad scheme", "POST", "/links", `{"url":"ftp://x.org"}`, 400, "validation"},
		{"bad code", "POST", "/links", `{"url":"https://x.org","code":"a"}`, 400, "validation"},
		{"reserved code", "POST", "/links", `{"url":"https://x.org","code":"health"}`, 409, "conflict"},
		{"unknown code", "GET", "/neverx", "", 404, "not_found"},
		{"broken code", "GET", "/a.b", "", 400, "validation"},
		{"stats unknown", "GET", "/links/neverx/stats", "", 404, "not_found"},
		{"delete unknown", "DELETE", "/links/neverx", "", 404, "not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, h, tc.method, tc.path, tc.body)
			var e struct{ Error, Message string }
			decode(t, w, &e)
			if w.Code != tc.status || e.Error != tc.code || e.Message == "" {
				t.Errorf("got %d %+v, want %d %s", w.Code, e, tc.status, tc.code)
			}
		})
	}
}

func TestGeneratedCodeAndList(t *testing.T) {
	h := newTestServer()
	w := do(t, h, "POST", "/links", `{"url":"https://go.dev"}`)
	var l struct{ Code string }
	decode(t, w, &l)
	if w.Code != http.StatusCreated || len(l.Code) != links.GeneratedLength {
		t.Fatalf("generated: %d %s", w.Code, w.Body)
	}
	do(t, h, "POST", "/links", `{"url":"https://example.org","code":"second"}`)

	w = do(t, h, "GET", "/links", "")
	var all []struct{ ID int64 }
	decode(t, w, &all)
	if len(all) != 2 {
		t.Fatalf("list: %s", w.Body)
	}
}

func TestDelete(t *testing.T) {
	h := newTestServer()
	do(t, h, "POST", "/links", `{"url":"https://go.dev","code":"gone"}`)
	if w := do(t, h, "DELETE", "/links/gone", ""); w.Code != http.StatusNoContent {
		t.Errorf("delete: %d", w.Code)
	}
	if w := do(t, h, "GET", "/gone", ""); w.Code != http.StatusNotFound {
		t.Errorf("redirect after delete: %d, want 404", w.Code)
	}
}

func TestContractRoutes(t *testing.T) {
	h := newTestServer()
	w := do(t, h, "GET", "/health", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("health: %d %s", w.Code, w.Body)
	}
	w = do(t, h, "GET", "/", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), studentName) {
		t.Errorf("identity: %d", w.Code)
	}
	w = do(t, h, "GET", "/version", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"commit"`) {
		t.Errorf("version: %d %s", w.Code, w.Body)
	}
}
