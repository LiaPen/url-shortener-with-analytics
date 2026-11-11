# URL Shortener with Analytics

A small web service that turns a long URL into a short code, redirects anyone
who visits that code to the original address, and counts the visits. It is
the kind of thing bit.ly does, built as a single Go binary in a container.

- **Student:** Pentek Iulia, group DMWD
- **Project:** 1 - URL Shortener with Analytics
- **Language:** Go

## What it does

`POST /links` takes a URL (and optionally a code you choose) and returns a
short code. `GET /{code}` answers with a `302` redirect to the original URL
and counts the visit. `GET /links/{code}/stats` reports how many visits a code
has had and when the last one happened; reading the stats never counts as a
visit.

Codes are 4 to 10 characters from `A-Z a-z 0-9 - _`; when you do not choose
one, the service generates one of 7 characters. A code that is already taken
is refused with `409`, also when two requests for it arrive at the same time.

**Current state:** all routes work, with links kept in memory. Links do not
survive a restart yet; that is the next step (see `docs/architecture.md`).

## How to run it

```
docker run --rm -p 8080:8080 ghcr.io/liapen/url-shortener-with-analytics:latest
```

Without Docker (Go 1.27):

```
go run ./cmd/app
go test ./... -race
```

## Routes

| Route | Method | Result |
|---|---|---|
| `/health` | GET | `200 {"status":"ok","uptime_seconds":N}` |
| `/version` | GET | `200 {"app","version","commit","built_at"}` |
| `/` | GET | `200` HTML identity page |
| `/reset` | POST | `204`, discards all links |
| `/links` | POST | `201 {"id","code","url","created_at"}`, `400`, `409` |
| `/links` | GET | all links, newest first |
| `/{code}` | GET | `302` to the URL, `404` unknown code, `400` malformed code |
| `/links/{code}/stats` | GET | `{"code","visits","last_visit_at"}` |
| `/links/{code}` | DELETE | `204` or `404` |

Errors look like `{"error":"conflict","message":"this code is already taken"}`.

## What you should see

```
curl -i -H "Content-Type: application/json" -d '{"url":"https://go.dev/doc/effective_go","code":"gofmt"}' http://localhost:8080/links
HTTP/1.1 201 Created
{"id":1,"code":"gofmt","url":"https://go.dev/doc/effective_go","created_at":"2026-11-05T15:19:29Z"}

curl -i http://localhost:8080/gofmt
HTTP/1.1 302 Found
Location: https://go.dev/doc/effective_go

curl http://localhost:8080/links/gofmt/stats
{"code":"gofmt","visits":1,"last_visit_at":"2026-11-05T15:19:29Z"}
```

Reading the stats a second time still shows `"visits":1`.