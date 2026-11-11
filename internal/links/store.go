// Package links holds the short-code -> URL mappings and the visit counters.
//
// Every operation that reads and then writes (create, resolve, delete) runs
// under one mutex, so "is this code free?" and "take this code" are a single
// step. That is what makes two simultaneous requests for the same custom code
// end with exactly one link and one 409, never two links.
package links

import (
	"crypto/rand"
	"errors"
	"math/big"
	"sort"
	"sync"
	"time"
)

// GeneratedLength is L from the project description: the length of a code the
// service picks itself.
const GeneratedLength = 7

// maxGenerateAttempts bounds the retry loop when a generated code collides.
// With 64^7 (about 4.4e12) possible codes a collision is rare, but rare is not
// never: on a collision we draw again, and after this many failures we give up
// with an error instead of looping forever.
const maxGenerateAttempts = 10

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

var (
	ErrNotFound  = errors.New("link not found")
	ErrCodeTaken = errors.New("code already taken")
	ErrExhausted = errors.New("could not generate a free code")
)

// Link is one stored mapping plus its analytics.
type Link struct {
	ID          int64
	Code        string
	URL         string
	CreatedAt   time.Time
	Visits      int64
	LastVisitAt *time.Time // nil until the first redirect is served
}

// Store is an in-memory, concurrency-safe set of links.
type Store struct {
	mu     sync.Mutex
	byCode map[string]*Link
	nextID int64
	now    func() time.Time // replaceable in tests
}

func NewStore() *Store {
	return &Store{byCode: make(map[string]*Link), nextID: 1, now: time.Now}
}

// Create validates the input and stores a new link. An empty code means
// "generate one for me".
func (s *Store) Create(rawURL, code string) (Link, error) {
	if err := ValidateURL(rawURL); err != nil {
		return Link{}, err
	}
	if code != "" {
		if err := ValidateCode(code); err != nil {
			return Link{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if code == "" {
		var err error
		if code, err = s.freeCodeLocked(); err != nil {
			return Link{}, err
		}
	} else if s.takenLocked(code) {
		return Link{}, ErrCodeTaken
	}

	l := &Link{
		ID:        s.nextID,
		Code:      code,
		URL:       rawURL,
		CreatedAt: s.now().UTC(),
	}
	s.nextID++
	s.byCode[code] = l
	return *l, nil
}

// Resolve returns the target URL for a code and counts the visit. It is the
// only method that increments Visits, so reading the stats never counts.
func (s *Store) Resolve(code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.byCode[code]
	if !ok {
		return "", ErrNotFound
	}
	t := s.now().UTC()
	l.Visits++
	l.LastVisitAt = &t
	return l.URL, nil
}

// Get returns a copy of the link without touching its counters.
func (s *Store) Get(code string) (Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.byCode[code]
	if !ok {
		return Link{}, ErrNotFound
	}
	return copyLink(l), nil
}

// List returns all links, newest first; ties broken by ascending id.
func (s *Store) List() []Link {
	s.mu.Lock()
	out := make([]Link, 0, len(s.byCode))
	for _, l := range s.byCode {
		out = append(out, copyLink(l))
	}
	s.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *Store) Delete(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byCode[code]; !ok {
		return ErrNotFound
	}
	delete(s.byCode, code)
	return nil
}

// Reset discards every link and restarts id numbering at 1.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byCode = make(map[string]*Link)
	s.nextID = 1
}

// takenLocked reports whether code is unavailable. Reserved words count as
// taken because the router already answers them. Caller holds s.mu.
func (s *Store) takenLocked(code string) bool {
	if IsReserved(code) {
		return true
	}
	_, ok := s.byCode[code]
	return ok
}

// freeCodeLocked draws random codes until one is free. Caller holds s.mu, so
// no other request can take the code between the check and the insert.
func (s *Store) freeCodeLocked() (string, error) {
	for range maxGenerateAttempts {
		c, err := randomCode(GeneratedLength)
		if err != nil {
			return "", err
		}
		if !s.takenLocked(c) {
			return c, nil
		}
	}
	return "", ErrExhausted
}

func randomCode(n int) (string, error) {
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[k.Int64()]
	}
	return string(b), nil
}

func copyLink(l *Link) Link {
	c := *l
	if l.LastVisitAt != nil {
		t := *l.LastVisitAt
		c.LastVisitAt = &t
	}
	return c
}
