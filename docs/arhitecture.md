# Architecture

## The shape

Three packages, one direction of dependency: `cmd/app` → `internal/api` →
`internal/links`.

- `cmd/app` starts the process: creates the store, builds the handler, runs
  the HTTP server on :8080 and shuts it down cleanly on SIGTERM.
- `internal/api` is the HTTP layer. It decodes JSON, calls the store, and maps
  the result or error to the contract's status code and error shape
  (`writeStoreError` is the single place where errors become status codes).
- `internal/links` holds the data and the rules: validation, code generation,
  collision handling, visit counting.

A redirect, from request to response: Go's `ServeMux` matches `GET /{code}`
(literal routes like `/health` and `/links` are more specific and win) →
`redirect` validates the code's format (malformed → 400) →
`Store.Resolve` takes the mutex, looks the code up in a map (missing → 404),
increments `Visits`, sets `LastVisitAt`, releases the mutex →
`http.Redirect` writes `302` with the `Location` header.

## The data

One `Link` per code: id, code, url, created_at, visits, last_visit_at. They
live in a `map[string]*Link` keyed by code, guarded by one `sync.Mutex`, plus a
`nextID` counter. Everything is in memory: **at the moment nothing survives a
restart.** `/reset` empties the map and sets `nextID` back to 1.

## Three decisions

1. **One mutex around check-and-insert, not "check, then insert".** Looking up
   whether a code is free and storing the new link happen inside the same
   locked section, so two simultaneous requests for the same custom code give
   one `201` and one `409`; a test runs 50 at once to show it. Rejected: a
   `sync.Map` or a read-write lock, which make the lookup cheaper but leave a
   gap between the check and the insert unless I add the lock back anyway.

2. **Generated-code collisions are retried, not assumed away.** A generated
   code is 7 random characters from 64 (crypto/rand), about 4.4·10¹² values.
   If it is already taken, or is a reserved word like `version`, the store
   draws again, still under the lock, up to 10 times, then fails with `422`
   rather than looping forever. Rejected: deriving the code from a hash of the
   URL (same collisions, plus the same URL always gets the same code) and a
   counter encoded in base 64 (no collisions, but codes become guessable).

3. **The visit is counted before the redirect is written, in the request
   path.** A client that received a `302` has been counted, and a stats read
   never counts. In memory this costs one map lookup under the lock, so it does
   not slow the redirect noticeably. Rejected for now: counting in a background
   goroutine through a channel, which is faster but loses the clicks still in
   the channel if the process dies. This choice has to be revisited once
   persistence exists, because then counting synchronously means a disk write
   on every redirect.

Also: the words `health`, `version`, `reset` and `links` are reserved and
refused as codes, because the router would never send them to the redirect.

## What is not built yet

- **Persistence.** Links must survive a container restart; today they do not.
  Plan: an append-only file (one JSON line per create/delete) replayed at
  startup, or SQLite. Undecided, and it decides how visits are stored.
- **Durable visit counts.** Linked to the above: whether every click is
  written to disk before the redirect (correct, slower) or batched in the
  background (fast, loses the last few seconds on a crash).
- **Deleted vs never-created codes.** A deleted code currently answers `404`
  like an unknown one; I have not decided whether it should stay reserved
  (or answer 410) so it cannot be reused for a different URL.
- **Load measurement.** I have not measured redirect latency yet, so I cannot
  yet say with evidence which part I would optimise first.