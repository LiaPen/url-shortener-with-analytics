package links

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCreateWithCustomCode(t *testing.T) {
	s := NewStore()
	l, err := s.Create("https://go.dev", "gofmt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if l.ID != 1 || l.Code != "gofmt" || l.URL != "https://go.dev" {
		t.Errorf("unexpected link: %+v", l)
	}
	if _, err := s.Create("https://example.org", "gofmt"); !errors.Is(err, ErrCodeTaken) {
		t.Errorf("second create with same code: got %v, want ErrCodeTaken", err)
	}
}

func TestGeneratedCodeHasLengthL(t *testing.T) {
	s := NewStore()
	l, err := s.Create("https://go.dev", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(l.Code) != GeneratedLength {
		t.Errorf("generated code %q has length %d, want %d", l.Code, len(l.Code), GeneratedLength)
	}
	if err := ValidateCode(l.Code); err != nil {
		t.Errorf("generated code %q does not pass validation: %v", l.Code, err)
	}
}

func TestReservedCodesAreRefused(t *testing.T) {
	s := NewStore()
	for _, c := range []string{"health", "version", "reset", "links"} {
		if _, err := s.Create("https://go.dev", c); !errors.Is(err, ErrCodeTaken) {
			t.Errorf("code %q: got %v, want ErrCodeTaken", c, err)
		}
	}
}

func TestValidation(t *testing.T) {
	cases := []struct {
		name, url, code string
	}{
		{"empty url", "", ""},
		{"no scheme", "go.dev", ""},
		{"ftp scheme", "ftp://go.dev", ""},
		{"javascript scheme", "javascript:alert(1)", ""},
		{"no host", "https://", ""},
		{"code too short", "https://go.dev", "abc"},
		{"code too long", "https://go.dev", "abcdefghijk"},
		{"code bad char", "https://go.dev", "ab/cd"},
	}
	s := NewStore()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Create(tc.url, tc.code)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("got %v, want a ValidationError", err)
			}
		})
	}
}

// The requirement: two simultaneous requests for the same custom code must not
// create two entries or lose one. Run with -race (CI does).
func TestConcurrentSameCodeCreatesExactlyOne(t *testing.T) {
	s := NewStore()
	const n = 50
	var wg sync.WaitGroup
	results := make(chan error, n)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release all goroutines at once
			_, err := s.Create("https://go.dev", "race")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	ok, taken := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrCodeTaken):
			taken++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || taken != n-1 {
		t.Errorf("got %d created and %d taken, want 1 and %d", ok, taken, n-1)
	}
	if got := len(s.List()); got != 1 {
		t.Errorf("store holds %d links, want 1", got)
	}
}

func TestResolveCountsButGetDoesNot(t *testing.T) {
	s := NewStore()
	if _, err := s.Create("https://go.dev", "gofmt"); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Get("gofmt")
	if l.Visits != 0 || l.LastVisitAt != nil {
		t.Fatalf("new link should have no visits: %+v", l)
	}
	if _, err := s.Resolve("gofmt"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		l, _ = s.Get("gofmt")
	}
	if l.Visits != 1 || l.LastVisitAt == nil {
		t.Errorf("after one redirect and three reads: %+v, want visits=1", l)
	}
	if _, err := s.Resolve("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown code: got %v, want ErrNotFound", err)
	}
}

func TestListNewestFirstTiesByID(t *testing.T) {
	s := NewStore()
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	times := []time.Time{base, base.Add(time.Second), base.Add(time.Second)}
	i := 0
	s.now = func() time.Time { t := times[i]; i++; return t }

	s.Create("https://a.example", "aaaa") // id 1, oldest
	s.Create("https://b.example", "bbbb") // id 2, same time as id 3
	s.Create("https://c.example", "cccc") // id 3

	got := s.List()
	want := []int64{2, 3, 1}
	for k, l := range got {
		if l.ID != want[k] {
			t.Fatalf("order = %v, want ids %v", ids(got), want)
		}
	}
}

func TestDeleteAndReset(t *testing.T) {
	s := NewStore()
	s.Create("https://go.dev", "gofmt")
	if err := s.Delete("gofmt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("gofmt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: got %v, want ErrNotFound", err)
	}

	s.Create("https://go.dev", "one1")
	s.Reset()
	l, _ := s.Create("https://go.dev", "two2")
	if l.ID != 1 {
		t.Errorf("after reset first id = %d, want 1", l.ID)
	}
}

func ids(ls []Link) []int64 {
	out := make([]int64, len(ls))
	for i, l := range ls {
		out[i] = l.ID
	}
	return out
}
