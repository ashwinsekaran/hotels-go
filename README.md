# hotels

A Go HTTP/REST service that **normalizes heterogeneous hotel data** from multiple
providers into one canonical model, built in the onion (clean) architecture with
built-in observability (structured logs, Prometheus metrics, OpenTelemetry
tracing) and a one-command local stack. Dependencies point strictly inward:
`ent → uc → {repo, handlers} → main`. An inner layer never imports an outer one.

## The problem

Providers send wildly inconsistent JSON — different keys, different types for the
same field, missing fields, HTML/marketing prose, and duplicate records for the
same hotel:

| Concern | Variations across feeds |
|---|---|
| Name | `hotel_name` vs `name` (+ typos/word order: "Hotel Mare Azzurro" vs "Mare Azzuro Hotel") |
| Location | `city`+`country` (ISO code) vs one `location` string (`"Rimini, Italien"`) |
| Stars | int `4`, `null`, string `"3 stars"` |
| Amenities | comma string, JSON array, `null`, quirky (`"3 pools"`) |
| Price | int `price_from_eur` vs string `price_from: "180 USD"` |
| Coordinates | nested `coords`, sometimes wrong (Berlin record points at Munich) |
| Description | HTML, empty, multiple languages |

## Layers

- `ent/` — domain entities (plain structs, json tags, no third-party deps).
  Holds both the canonical `Hotel` and the permissive inbound `RawHotel`.
- `uc/` — use-cases as function types built by factories; **owns** the repository
  port (`HotelStore`). **All domain validation lives here** (see below). Depends
  only on `ent`.
- `repo/` — in-memory adapter satisfying the `uc` port *structurally*.
- `handlers/` — HTTP delivery (httprouter); owns transport concerns only.
- `auth/`, `metrics/`, `tracing/`, `health/` — cross-cutting.
- `main.go` — composition root.

## Where validation happens

Validation is staged; each layer validates a different *kind* of correctness:

| Stage | Layer | Validates | On failure |
|---|---|---|---|
| Decode (structural) | `handlers` | valid JSON, is an array, body/batch size | reject request → `400`/`413` |
| Parse + coerce (per field) | `uc` | each raw field → a valid canonical value | soft repair → warning (optional fields); fatal → error (required) |
| Invariants | `uc` | assembled `Hotel` consistency | fatal → error |
| Merge/reconcile | `uc` | cross-source conflicts | pick by trust tier + warning |
| Injection safety | `repo` | parameterized queries (future DB) | structural — content stored as inert data |

**Fatal** (record dropped, `RecordError`): unknown source, missing name,
unresolvable location. **Soft** (record kept, `RecordWarning`): out-of-range
stars → null, bad price → null, junk amenities dropped, implausible coordinates
flagged, cross-source conflicts.

## Design decisions (locked)

- **Stars** = official classification, integer **1–5**, never averaged. Conflicts
  are resolved by **source trust tier** (partner feeds outrank the scraper); ties
  keep the existing value. The disagreement is surfaced as a `stars_conflict`
  warning, never silently discarded.
- **Currency**: the **incoming (native) currency is the source of truth**. Prices
  are stored as integer minor units + ISO-4217 code and **never converted** at
  ingest. The system only defines the allow-list and representation.
- **Dedup**: two records are the same hotel **only** when **city + ISO country**
  match and the **name matches exactly** (case/whitespace-insensitive, but no
  fuzzy/typo tolerance). This deliberately keeps "Hotel Mare Azzurro" and "Mare
  Azzuro Hotel" **distinct** — a spelling/word-order difference may well be two
  different hotels, and merging them would be wrong. Genuine duplicates (same
  name bar trivial formatting) merge: unioning amenities + sources and
  reconciling scalars by trust tier.
- **Coordinates**: absent coords stay `nil` (**no geocoding**). Supplied coords
  are range-checked and, against a small city-centroid table, flagged when
  implausibly far (catches the Berlin-record-pointing-at-Munich case).
- **Security**: HTML is stripped from descriptions; all strings are UTF-8
  coerced, control-char stripped, and length-capped; batch and body sizes are
  bounded. SQL/prompt-injection strings are stored **verbatim as data** — the
  defenses are parameterized queries (persistence boundary) and treat-as-data
  discipline (any future LLM boundary), not input blacklisting.

### Parked TODOs (intentional, documented in code)

- **Cross-field consistency** (e.g. "adults-only" description + "kids_club"
  amenity) — needs a rule table or LLM; hook stub in `uc/hotel.go`, reserved
  skipped test in `uc/consistency_test.go`.
- **Geocoding reference** — the city-centroid table is hand-seeded; replace with a
  real gazetteer (`uc/coords.go`).

## API

Business routes require `Authorization: Bearer $AUTH_TOKEN`.

| Method | Route | Behavior |
|---|---|---|
| `POST` | `/hotels` | Ingest a raw provider array → normalize + dedup + merge. Returns `{normalized, errors, warnings}`. `200` all-ok, `207` partial, `400` malformed/all-failed. |
| `GET` | `/hotels` | List canonical hotels; filters `?city=`, `?country=`, `?min_stars=`. |
| `GET` | `/hotels/{id}` | One hotel; `404` if unknown. |

Infra endpoints are unauthenticated: `/metrics`, `/.well-known/live`,
`/.well-known/ready`.

## Configuration (env)

| Var | Default | Meaning |
|-----|---------|---------|
| `HTTP_ADDRESS` | `:8080` | listen address |
| `SHUTDOWN_DELAY` | `5s` | drain window after readiness flips to not-ready |
| `AUTH_TOKEN` | `changeme` | bearer token for business routes |
| `SERVICE_NAME` | `hotels` | `service.name` reported to Jaeger |
| `OTEL_ENDPOINT` | `localhost:4317` | OTLP/gRPC collector (Jaeger) endpoint |

## Run locally — bare `go run`

```
go mod tidy
AUTH_TOKEN=devtoken go run .
```

```
# ingest the sample feed
curl -H "Authorization: Bearer devtoken" -X POST localhost:8080/hotels \
  -d '[{"source":"partner-feed-a","hotel_name":"Hotel Mare Azzurro","city":"Rimini","country":"IT","stars":4,"amenities":"pool,wifi,parking","price_from_eur":89},
       {"source":"scrape-booking-sites","name":"Mare Azzuro Hotel","location":"Rimini, Italien","rating":"3 stars","features":["swimming pool","free WiFi","pets allowed"]}]'

curl -H "Authorization: Bearer devtoken" "localhost:8080/hotels?country=IT"
curl localhost:8080/.well-known/ready     # 200/503, no auth
curl localhost:8080/metrics               # Prometheus, no auth
```

Without a collector running, traces are simply dropped — the service still works.

## Tests

```
go test -race ./...
```

Coverage spans all seven levels: field parsers, domain invariants, ingest +
merge/dedup (against the real `hotels.json` records), currency, security, HTTP
handlers (`httptest`), and repo concurrency. The `uc` layer is tested directly
via a fake `HotelStore` (no HTTP, no DB) — that is the payoff of dependency
injection.

## Run the full stack — Docker Compose

```
echo "AUTH_TOKEN=devtoken"      >  .env
echo "GRAFANA_PASSWORD=admin"   >> .env
go mod tidy
docker compose build
docker compose up -d
```

| Service | URL | Purpose |
|---------|-----|---------|
| App | http://localhost:8080 | the REST API |
| Jaeger UI | http://localhost:16686 | traces (service `hotels`) |
| Prometheus | http://localhost:9090 | metrics (`gateway_requests_total`) |
| Grafana | http://localhost:3000 | logs + metrics + traces in one pane |

Tear down: `docker compose down` (add `-v` to drop volumes).

## Swapping the in-memory repo for a real database

Only `repo/` and one line of `main.go` change:

1. Write a new type in `repo/` (e.g. `PgHotelRepo`) whose method set matches
   `uc.HotelStore` (`GetById`, `Save`, `All`). Because the port is satisfied
   structurally, `repo` still does not import `uc`. Use **parameterized queries**
   (this is the SQL-injection defense).
2. In `main.go`, replace `repo.NewHotelRepo()` with `repo.NewPgHotelRepo(db)`.
   Nothing in `ent/`, `uc/`, or `handlers/` changes.
