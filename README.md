# Queue Manager

An internal service that decouples third-party systems (e.g. SAP) from calling
backend services (e.g. Inventory) directly. Instead of a direct call that can
spike production DB/CPU, third parties POST to a Queue-owned entry point,
Queue persists the request as pending, and a throttled Go worker forwards it
to the real destination endpoint at a controlled pace. Console admins get a
web console to manage integrations and manually retry failures.

No message broker to install or operate — MongoDB is the only external
dependency. Dispatch is handled in-process: each destination gets its own
goroutine that claims its oldest pending request straight out of MongoDB
(the same atomic claim regardless of how many app instances end up polling
it), forwards it, and paces the next claim by that destination's configured
throttle.

**Terminology note:** the UI and this doc use product-facing names that
differ from the underlying code/schema names (kept as-is internally to
avoid a breaking migration): **Console Admin** (code: `admin`), **Integration**
(code: `product`), **Transmission Channel** (code: `template`), **Transmission
Request** (code: `request`). Where this doc references an actual field, route,
or collection name, it uses the code name verbatim in backticks.

## How it works

1. A third party POSTs an AES-256-CBC encrypted payload to
   `POST /ingest/<entry-point>`.
2. Queue looks up the owning transmission channel by entry point and
   decrypts the payload using that channel's integration key/IV.
3. A channel has one or more **destinations**. With one, every request goes
   to it — no further decision needed. With several, the channel also has a
   `template_routing_field`: Queue reads that field out of the decrypted
   JSON payload and matches its value against each destination's configured
   `destination_routing_value` to pick exactly one. No match → `400`
   immediately, nothing persisted or queued (see "Routing between multiple
   destinations" below).
4. The request is persisted to `requests` as `pending` with the resolved
   `destination_id`, and that destination's consumer goroutine is woken
   (a non-blocking in-process signal) so it picks it up right away instead
   of waiting for its next poll tick.
5. A per-destination goroutine consumer atomically claims the oldest
   `pending` request for that destination (`FindOneAndUpdate` to
   `processing` — the same primitive that would keep this correct even if
   more than one app instance ends up polling the same destination),
   forwards it to the destination's endpoint, and records the outcome
   (`success`/`failed`) — pacing claims by that destination's own
   `destination_throttling_second`, independent of any other destination
   on the same channel. If nothing's pending, the consumer blocks until
   woken or its next poll tick (~300ms), rather than busy-looping.
6. Failures sit until a console admin clicks **Retry** on the transmission
   request grid — there is no automatic retry, and retry flows through the
   exact same claim-and-forward code path as normal ingestion.
7. Transmission requests expire and are purged automatically via a MongoDB
   TTL index, based on each transmission channel's
   `template_request_ttl_days` (shared across all of a channel's
   destinations).

On startup, any request left stuck in `processing` by a previous crash
(claimed but never finished) is reset back to `pending` before consumers
start, so nothing is silently dropped by an unclean shutdown.

## Requirements

- Go 1.21+ (or just Docker — see "Running with Docker" below)
- MongoDB 3.6+ reachable at `MONGO_URI` (the Mongo driver is pinned to the
  v1.x line specifically for 3.6 compatibility — do not upgrade to
  `go.mongodb.org/mongo-driver/v2`, which requires Mongo ≥4.0)

That's it — no message broker or other service to stand up. Dispatch is
in-process, backed entirely by MongoDB.

## Running locally

```sh
cp .env.example .env      # adjust if needed; JWT_SECRET can be left blank for local dev
go run ./cmd/server
```

The server must be run with the project root as its working directory,
since it serves the console frontend from `WEB_DIR` (default `web/`) — that
matters if you deploy it as a systemd service (`WorkingDirectory=`) rather
than running via `go run`/`go build` from this directory.

There is no public sign-up for the console — create the first console
admin with the seed command before logging in:

```sh
go run ./cmd/seedadmin                                       # admin@example.com / 12345678
go run ./cmd/seedadmin -email=you@example.com -password=... -name="You"
```

It's idempotent (skips if the email already exists). Every console admin
after the first is added by an already-logged-in console admin from the
**Console Admins** page in the console (`/api/admins`) — there's no other
way to create one, so an unauthenticated visitor can never register
themselves.

### Seeding a console admin (dev, staging, production)

`cmd/seedadmin` is a one-off CLI, not an HTTP endpoint — it talks to Mongo
directly, using the exact same config loading as the server (`godotenv.Load()`
reads a `.env` in the current working directory if present, then falls back
to real environment variables). That means "which Mongo it seeds into"
depends entirely on which `MONGO_URI`/`MONGO_DB_NAME` are in scope when you
run it — there's no `-env` flag or separate prod/dev switch.

**Local dev** — run it straight from the project root, where `go run` and
your local `.env` both already work:

```sh
go run ./cmd/seedadmin -email=you@example.com -password=yourpass -name="You"
```

**Staging / production** — the target host generally won't have the Go
toolchain wired up for `go run`, and you don't want to rebuild from source
on every seed. Build the binary once (e.g. as part of your normal deploy
build alongside `cmd/server`) and ship it next to the server binary:

```sh
go build -o seedadmin ./cmd/seedadmin
```

Then, on that host, from whichever directory has the production `.env` (or
with `MONGO_URI`/`MONGO_DB_NAME` exported as real env vars — e.g. set in the
systemd unit), run:

```sh
./seedadmin -email=admin@yourcompany.com -password='<strong-password>' -name="Your Name"
```

Things that matter specifically outside local dev:

- **Always pass explicit flags.** Never run it bare (no flags) anywhere
  reachable from outside your laptop — the flag defaults are the public
  `admin@example.com` / `12345678` shown above, which is a guessable
  credential to anyone who's read this file or the source.
- **Safe to bake into deploy scripts.** Because it's idempotent, re-running
  it on every deploy is fine — it just skips with a "already exists"
  message if that email is already a console admin.
- **Double-check which Mongo you're pointed at.** It resolves config the
  same lazy way the server does, so running it from the wrong working
  directory (wrong/no `.env`) or without the right env vars exported will
  silently seed the wrong database (commonly: your local Mongo instead of
  production) rather than erroring.
- **It only creates the *first* console admin per email.** There's no route
  to seed a second "break-glass" console admin later except running this
  same command again with a different `-email` — after that, additional
  console admins are created from inside the app by an existing console
  admin, not via this CLI.

## Running with Docker

This is the fastest path to a working instance — no Go toolchain needed on
the host at all, just Docker. `docker-compose.yml` runs exactly two
services, `mongo` and `app` (built from the `Dockerfile`); nothing else to
install.

### Prerequisites

- Docker with the Compose v2 plugin — `docker compose version` should print
  something, not error (the old standalone `docker-compose` v1 binary isn't
  what these instructions use)

### 1. Get the source

```sh
git clone <this-repo-url> queue-manager
cd queue-manager
```

### 2. (Recommended) set a stable JWT_SECRET

Skip this for a quick local try — the server will generate a random secret
and log a warning. Set a real one for anything you'll come back to, since
without a stable secret every admin session is invalidated whenever the
container restarts:

```sh
cp -n .env.example .env      # only copies if you don't already have one
```

Then open `.env` and set `JWT_SECRET=` to a real value, e.g. the output of
`openssl rand -hex 32`. Docker Compose loads this same `.env` file
automatically for its own variable substitution — it's the same file the
non-Docker path below uses, so if you've already run through "Running
locally" you likely have one set up already and can skip this step. (Note
the mechanism is different even though the file is shared: the container
never sees `.env` itself — Compose reads it and passes `JWT_SECRET` in as a
real environment variable, same as any other `environment:` entry.)

### 3. Build and start

```sh
docker compose up -d --build
```

First run builds the image (a minute or two); after that, `up -d` just
starts the existing one. Mongo's data lives in a named Docker volume
(`mongo_data`), so it survives `docker compose down` and container
restarts — only `docker compose down -v` (or deleting the volume directly)
throws it away.

### 4. Create the first console admin

There's no public sign-up — same as the non-Docker path, the first admin
is created via `seedadmin`, run once inside the running `app` container:

```sh
docker compose run --rm app /app/seedadmin -email=you@example.com -password=yourpass -name="You"
```

Always pass explicit flags here, the same as outside Docker — the bare-flag
defaults (`admin@example.com` / `12345678`) are public and guessable to
anyone who's read this file.

### 5. Open the console

`http://localhost:8080` (or whatever `APP_HOST_PORT` you set — see below).

### Configuration

Set these in the `.env` file from step 2, or export them in your shell
before `docker compose up` — either way Compose picks them up:

| Variable        | Default  | Purpose                                                          |
|-----------------|----------|-------------------------------------------------------------------|
| `JWT_SECRET`    | *(none)* | Signs admin session/2FA tokens — set a stable value (step 2)     |
| `APP_HOST_PORT` | `8080`   | Host port the console/ingest endpoint is published on            |

`APP_HOST_PORT` matters if 8080 is already taken on your host, or if
you're running more than one instance side by side (this is also what
`scripts/e2e-docker-test.sh` uses to test without colliding with a real
deployment — see "Tests" below).

### Day-to-day operations

```sh
docker compose logs -f app      # tail the app's logs
docker compose restart app      # restart just the app (Mongo keeps running)
docker compose down             # stop everything, keep the mongo_data volume
docker compose down -v          # stop everything AND delete all Mongo data
```

To pick up a code change (yours, or a `git pull` of a new release), rebuild
and recreate just the app container — Mongo and its data are untouched:

```sh
git pull
docker compose up -d --build app
```

### Verifying it actually works

`scripts/e2e-docker-test.sh` drives this exact `docker compose` setup
through a real ingest → decrypt → route → throttle → forward → retry cycle
end to end (see "Tests" below) — worth running once after your first setup,
or after pulling an update, to confirm the whole stack is healthy.

### Running without Compose

If you'd rather point the container at a Mongo you already run elsewhere,
skip Compose and run the image directly:

```sh
docker build -t queue-manager .
docker run -d --name queue-manager -p 8080:8080 \
  -e MONGO_URI="mongodb://your-mongo-host:27017" \
  -e MONGO_DB_NAME="queue_manager" \
  -e JWT_SECRET="$(openssl rand -hex 32)" \
  queue-manager
docker exec queue-manager /app/seedadmin -email=you@example.com -password=yourpass -name="You"
```

## Project layout

Directory/file/field names below are the internal code and schema names —
they don't always match the product-facing UI names (`admin` = Console
Admin, `product` = Integration, `template` = Transmission Channel,
`request` = Transmission Request; see the terminology note above).

```
cmd/server/main.go        wiring, graceful shutdown, worker bootstrap
cmd/seedadmin/main.go     one-off CLI to create the first console admin (no public sign-up exists)
internal/
  config/                 env/.env -> Config
  db/                     Mongo connect + index setup (see EnsureIndexes for the full index list)
  models/                 queue_admins / products / request_templates / requests
  crypto/                 AES-256-CBC decrypt matching third parties' openssl_encrypt(OPENSSL_RAW_DATA)
  authtoken/               JWT issue/verify shared by handlers + middleware
  worker/                 per-destination consumer goroutines (claim from Mongo, throttled forwarding), bootstrap + crash recovery
  handlers/               HTTP handlers (auth, admins, products, templates, requests, ingest)
  middleware/             session auth, request logging
  httpclient/             shared HTTP client used to call a destination's endpoint
web/                      console frontend — plain HTML/CSS/JS, no build step, served directly
Dockerfile, docker-compose.yml   containerized app + Mongo, no other services
```

## Encryption

Third parties (including PHP-based senders) encrypt the JSON payload like:

```php
$encrypted = openssl_encrypt(json_encode($data), 'AES-256-CBC', $key, OPENSSL_RAW_DATA, $iv);
$encoded   = urlencode(base64_encode($encrypted));
```

`product_authentication_key` (32 bytes) / `product_authentication_iv` (16
bytes) are stored per integration and used as-is (no hashing/derivation).
`internal/crypto` reverses this exactly; its test suite validates against a
ciphertext generated by real PHP `openssl_encrypt`, not just Go-to-Go
round-trips.

## Console admin auth

Bcrypt-hashed passwords, JWT session in an httpOnly cookie, optional TOTP
2FA (Google Authenticator-compatible). 2FA enrollment is stateless — the
generated secret travels in a short-lived signed token between
`/api/auth/2fa/setup` and `/api/auth/2fa/setup/confirm` rather than being
written to the database until the console admin proves they've set it up
with one valid code.

## Retry semantics

Retry is manual only, from the transmission request grid. Clicking it:
- looks up the request's original destination (by `destination_id`) on the
  *current* version of the template — if that destination was since
  deleted, retry is refused with a clear conflict rather than guessing
- refreshes the request's `destination_end_point` snapshot to that
  destination's *current* endpoint (a retry is often triggered specifically
  because the endpoint config was just fixed)
- increments `retry_count`, sets `recent_request_retry_at`/`_by`
- resets `request_process_status` to `pending` and wakes that destination's
  consumer, the same way ingestion does

Only transmission requests currently in `failed` status can be retried.

## Routing between multiple destinations

A channel with one destination behaves exactly as if routing didn't
exist — no routing field, no per-destination matching, every request goes
to that one destination. Add a second destination and two things become
required: `template_routing_field` on the channel, and a unique
`destination_routing_value` on every destination.

On ingest, Queue parses the decrypted payload as a JSON object, reads
`template_routing_field` out of it, and matches the (string-formatted)
value against each destination's `destination_routing_value`. First and
only match wins — values are required to be unique within a channel, so
there's never ambiguity. No match, a missing field, or a non-object
payload all fail the request immediately with `400` and a message naming
what went wrong; nothing is persisted or queued in that case, so it never
shows up as a `failed` row to retry — it never entered the system.

This is deliberately simple: one shared field per channel, not a
per-destination field. Only the app can inspect the payload (it's encrypted
in transit), so *something* has to extract the routing value at ingest
time regardless — keeping that extraction to one field per channel means
it's a single lookup that resolves straight to a `destination_id`, stored
on the request before anything else happens, rather than a hand-rolled
per-destination rule engine.

## Transmission channel activate/deactivate

Both are confirmed in the UI before taking effect (each has real
consequences), and both operate on *every* destination on the channel at
once:
- **Activate** starts one consumer goroutine per destination, each of
  which immediately starts claiming any backlog already sitting `pending`
  against it.
- **Deactivate** cancels every destination's consumer. Nothing is deleted —
  backlog is just `pending` rows in `requests`, so it sits there untouched
  and picks up exactly where it left off the next time the channel is
  activated. Pausing a channel to fix a downstream issue never drops
  anything.

Editing an *active* channel's destinations restarts its consumers with the
fresh config (stop, then start against the new destination set) — this is
never destructive to backlog, since a destination that keeps its id (edit
its name/endpoint/throttle, but it's still "the same destination") keeps
every `pending`/`failed` row already tagged with that id regardless of
what else about the channel changed, including the entry point.

Permanently deleting a transmission channel stops its consumers, in
addition to removing the transmission channel record.

## Transmission channel form

- `template_name` and `template_entry_point` are both globally unique
  (enforced by Mongo unique indexes; the API returns a 409 naming which
  field collided). The entry point field auto-suggests a unique slug from
  the name as you type (stops once you edit the entry point directly —
  same UX as a CMS permalink field), and both fields get an inline
  "already in use" hint client-side before you even submit.
- Destinations are a repeatable list in the same dialog — one by default,
  with a "+ Add destination" control. The routing-value field and each
  row's "Remove" button only appear once there are two or more rows (see
  "Routing between multiple destinations" above).
- Each destination has a stable, server-assigned id (never hand-typed, so
  it can't collide by typo) that its backlog is tracked against — renaming
  a destination or reordering the list never loses track of it, and
  neither does renaming the channel's entry point.
- A "Copy URL" button (in both the transmission channel dialog and the
  transmission channels table) copies the full ingestion URL — origin +
  `/ingest/<slug>` — for handing to the third party.

## Tests

```sh
go test ./...
```

That's just `internal/crypto`'s unit tests. For an actual end-to-end check
of the worker/dispatch subsystem — ingest, decrypt, route, throttle,
forward, manual retry, deactivate/reactivate backlog preservation, and
crash recovery, all against the real Dockerized stack:

```sh
./scripts/e2e-docker-test.sh
```

It builds and runs its own isolated `docker compose` project (safe to run
alongside a real deployment on the same host), spins up two throwaway HTTP
destinations to forward to, asserts on the outcome of each step, and tears
everything down when it's done — pass or fail. Requires Docker and Go on
the host (Go is only needed to build a tiny test-payload encryption
helper; the app itself never needs it to run in Docker).
