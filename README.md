# Ditto

Duplicate file finder for Synology NAS (and any Linux host). Deployed as a
single Docker container with a web UI and REST API.

---

## Quick start

```bash
# Copy and edit the example config
cp config.yaml.example config.yaml
$EDITOR config.yaml          # set scan_paths, db_path, trash_dir

# Build and run
make build
./ditto --config config.yaml

# Open the web UI
open http://localhost:8080
```

---

## Configuration

`config.yaml` (see [`config.yaml.example`](config.yaml.example) for all fields):

| Key | Default | Description |
|---|---|---|
| `scan_paths` | — | Directories to scan (required) |
| `exclude_paths` | — | Directories to skip |
| `schedule` | `0 2 * * 0` | Cron expression for scheduled scans |
| `scan_paused` | `false` | Disable the scheduler without removing the cron |
| `db_path` | `/data/ditto.db` | SQLite database location |
| `trash_dir` | `/data/trash` | Holding area for deleted files |
| `trash_retention_days` | `30` | Days before auto-purge |
| `http_addr` | `:8080` | Listen address |
| `scan_workers.walkers` | `4` | Parallel directory walker goroutines |
| `scan_workers.partial_hashers` | `4` | Parallel partial-hash workers |
| `scan_workers.full_hashers` | `2` | Parallel full-hash workers |

---

## Docker / Podman

The project uses Podman (`docker` works identically as a drop-in replacement).

### Build

```bash
podman build -t ditto .
```

### Run — quick smoke test (no config needed)

The container starts with built-in defaults when no config file is mounted.
Data is written to `/data` inside the container; use a volume to persist it.

```bash
podman run --rm -p 8080:8080 ditto
```

Then verify it's alive:

```bash
curl http://localhost:8080/api/status
# → {"active_scan":null,"schedule":{...},"last_completed_scan":null}
```

Open `http://localhost:8080` in a browser and click **Scan Now** — the scan
path defaults to `/data`, so nothing interesting will be found, but it confirms
the full pipeline runs.

### Run — with real data

```bash
podman run --rm \
  -p 8080:8080 \
  -v /path/to/ditto-data:/data \          # persists DB + trash
  -v /your/photos:/mnt/photos:ro \        # directory to scan (read-only)
  ditto
```

Then open `http://localhost:8080`, trigger a scan, and review duplicates in the
Groups page.

### Run — with a config file

Mount a `config.yaml` to set scan paths, schedule, retention, etc.:

```bash
cat > config.yaml <<'EOF'
scan_paths:
  - /mnt/photos
  - /mnt/documents
schedule: "0 2 * * 0"   # weekly, Sunday 2am
trash_retention_days: 30
EOF

podman run --rm \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v /path/to/ditto-data:/data \
  -v /your/photos:/mnt/photos:ro \
  ditto
```

### Run — with Podman Compose / Docker Compose

Edit `docker-compose.yml` to uncomment your scan path volume, then:

```bash
podman compose up -d
podman compose logs -f
podman compose down
```

### Podman machine (macOS)

On macOS, Podman requires a Linux VM:

```bash
podman machine init    # first time only
podman machine start
```

---

## Synology NAS deployment

Ditto is designed to run as a container on a Synology NAS using
[Container Manager](https://www.synology.com/en-global/dsm/feature/container-manager)
(DSM 7.2+). A ready-to-use Compose file and config are in
[`deploy/synology/`](deploy/synology/).

### Prerequisites

- Synology DSM 7.2+ with Container Manager installed
- SSH access to the NAS, or use the Container Manager UI

### 1. Create a directory for Ditto on the NAS

SSH into your NAS and create a home for Ditto's files:

```bash
mkdir -p /volume1/docker/ditto/data
```

### 2. Copy the Compose file

Either SSH and create the file directly, or use File Station to upload it.

```bash
# On your NAS (via SSH):
mkdir -p /volume1/docker/ditto
curl -fsSL https://raw.githubusercontent.com/eargollo/ditto2/main/deploy/synology/docker-compose.yml \
  -o /volume1/docker/ditto/docker-compose.yml
```

Edit the file and adjust the volume mounts to match your shared folders:

```yaml
volumes:
  - /volume1/photos:/volume1/photos:ro
  - /volume1/documents:/volume1/documents:ro
```

### 3. (Optional) Add a config file

Skip this step if you prefer to configure everything through the Settings UI.

```bash
curl -fsSL https://raw.githubusercontent.com/eargollo/ditto2/main/deploy/synology/config.yaml \
  -o /volume1/docker/ditto/config.yaml
```

Edit `scan_paths` to point at the shared folders you want to scan.
If you skip this, configure scan paths at `http://<nas-ip>:8080` after starting.

### 4. Start the container

```bash
cd /volume1/docker/ditto
docker compose up -d
docker compose logs -f   # watch startup logs
```

Or in Container Manager UI: **Project → Create → Upload compose file**.

### 5. Open the web UI

```
http://<nas-ip>:8080
```

Trigger a manual scan from the dashboard, or wait for the scheduled scan
(default: Sundays at 2 am). Review duplicate groups and delete or ignore files
from the Groups page. Files moved to trash are auto-purged after 30 days.

### Updating to a new release

```bash
cd /volume1/docker/ditto
docker compose pull
docker compose up -d
```

---

## Development

### Prerequisites

- Go 1.25+
- `make`
- (Optional) Node.js / npx for Tailwind CSS rebuild

### Build

```bash
make build        # produces ./ditto binary
make run          # build + run with config.yaml.example (uses /tmp paths)
make lint         # golangci-lint
make tidy         # go mod tidy
make tailwind     # rebuild web/static/css/tailwind.css from src
```

---

## Testing

### Unit tests

Unit tests live alongside each package under `internal/`.

```bash
go test ./internal/...
```

These are fast, standalone, and require no running server.

---

### Regression tests

Regression tests in `tests/regression/` are **black-box end-to-end tests** that
talk to a running Ditto server over HTTP. They verify the full pipeline from
filesystem walk to API response.

#### 1. Start the server

```bash
# Using the example config (scan_paths point to /tmp — fine for testing)
./ditto --config config.yaml.example
```

Or with a dedicated test config:

```bash
cat > /tmp/ditto-test.yaml << 'EOF'
scan_paths:
  - /tmp
db_path: /tmp/ditto-test.db
trash_dir: /tmp/ditto-trash
http_addr: ":8080"
EOF
./ditto --config /tmp/ditto-test.yaml
```

#### 2. Run the tests

```bash
# Run all regression tests (server must be reachable)
go test ./tests/regression/... -v -timeout 5m

# Point at a different host/port
DITTO_TEST_URL=http://nas.local:8080 go test ./tests/regression/... -v

# Run a specific test
go test ./tests/regression/... -v -run TestScan_FindsDuplicates
```

If the server is **not** reachable, tests are **skipped** (not failed), so
`go test ./...` is always safe to run.

#### 3. What the regression tests cover

| Test | What it verifies |
|---|---|
| `TestStatus_ReturnsOK` | `GET /api/status` returns HTTP 200 |
| `TestStatus_ContentTypeJSON` | Response has `Content-Type: application/json` |
| `TestStatus_Shape` | Response contains `schedule.cron`, `active_scan`, `last_completed_scan` keys |
| `TestManualScan_StartsAndCompletes` | `POST /api/scans` starts a scan; it reaches a terminal state within 2 min |
| `TestScan_FindsDuplicates` | Full pipeline: creates duplicate files on disk, triggers a scan via API, asserts `GET /api/groups` returns the expected duplicate group |

See [`.context/docs/filedup/regression-tests.md`](.context/docs/filedup/regression-tests.md) for detailed coverage notes.

#### 4. Run everything at once

```bash
# In one shell:
./ditto --config config.yaml.example &

# In another:
go test ./... -timeout 5m
```

---

## Architecture

See [`.context/docs/filedup/requirements.md`](.context/docs/filedup/requirements.md) for the full spec.

### Pipeline (all stages concurrent)

```
Walker pool ──► Size accumulator ──► Cache check ──┬──► Partial hash pool
                                                    │         │
                                                    │    Partial hash grouper
                                                    │         │
                                                    │    Full hash pool
                                                    │         │
                                                    └──► Merge ──► DB writer
```

1. **Walker pool** — parallel `os.ReadDir` traversal via an unbounded
   `dirQueue` with a pending counter for safe termination.
2. **Size accumulator** — emits candidate pairs (files with same byte count).
3. **Cache check** — looks up `(path, size, mtime)` in `file_cache`; hits skip
   hashing entirely.
4. **Partial hash pool** — SHA-256 of first 64 KB.
5. **Partial hash grouper** — filters to files with colliding partial hashes.
6. **Full hash pool** — SHA-256 of entire file.
7. **DB writer** — batched upserts into `duplicate_groups` / `duplicate_files`
   and `file_cache`.
