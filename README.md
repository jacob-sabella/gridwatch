# gridwatch

**Self-hosted esports TV guide.** One browser tab shows every live, upcoming, and recently-finished esports match across Rocket League, League of Legends, CS2, Dota 2, Valorant, and more — with direct click-through to the stream.

Single static Go binary. Single Docker image (<15 MB). No database. No build tooling required.

```
                   ┌──────────────────────────┐
                   │  Liquipedia (per game)   │
                   └────────────┬─────────────┘
                                │ polled every ≥90s
                                ▼
                       ┌─────────────────┐
                       │    gridwatch    │
                       │  (one binary)   │◀── your phone / desktop
                       └─────────────────┘
                                │
                                ├── web UI (EPG grid + card view)
                                ├── JSON API (for Homepage/Home Assistant)
                                ├── iCal feed (Google Calendar subscribe)
                                ├── XMLTV feed (Plex/Jellyfin Live TV guide)
                                └── optional ntfy / webhook alerts
```

## Quickstart

```bash
docker run -p 8080:8080 \
  -e GRIDWATCH_CONTACT=you@example.com \
  ghcr.io/jsabella/gridwatch:latest
```

Open http://localhost:8080 — that's it. The baked-in default config tracks Rocket League, League of Legends, and Counter-Strike 2.

> `GRIDWATCH_CONTACT` is **required** because Liquipedia's API Terms of Use require a contact in the User-Agent string. gridwatch refuses to start without it.

## Features

- **Multi-game EPG timeline** — horizontal scrollable grid across 7 games out of the box
- **Mobile card view** — auto-switches below 720 px with Live Now / Upcoming / Recent sections
- **Dark / light theme** — follows `prefers-color-scheme` or a manual toggle
- **Real-time updates** — Server-Sent Events push new matches into the view without page reloads
- **Shareable filters** — filter state is mirrored to the URL so you can bookmark "live RL + LoL only"
- **JSON API** at `/api/v1/matches` — for Homepage widgets, Home Assistant, etc.
- **iCal feed** at `/api/v1/matches.ics` — subscribe in Google Calendar / Fantastical
- **XMLTV feed** at `/api/v1/matches.xml` — Plex Live TV, Jellyfin Live TV, xTeVe, and Kodi can ingest it as a program guide
- **Optional push** — ntfy or generic webhook alerts for live matches, no UI to install
- **Demo mode** — `--demo` seeds fake data so you can screenshot without waiting for Liquipedia
- **Respects Liquipedia's ToU** — gzip, descriptive User-Agent, 90s page cache, 10-min 429 backoff (enforced in code, not just docs)

## Configuration

The minimum viable config is two lines:

```yaml
contact: "you@example.com"
games:
  - rocketleague
  - leagueoflegends
  - counterstrike
```

The full schema lives in [`configs/gridwatch.example.yaml`](configs/gridwatch.example.yaml) with every knob documented inline. Highlights:

- `poll.liquipedia_interval` — per-game polling floor. Hard minimum 90 s.
- `view.window_past` / `view.window_future` — how much of the timeline to show
- `notifications.sinks` — optional ntfy / webhook backends for live-match alerts
- `${VAR}` env var interpolation in strings (for secrets)
- Any `GRIDWATCH_*` env var overrides the corresponding config field

## Supported games

gridwatch ships with metadata for:

| slug              | game                | default Bo | default duration |
|-------------------|---------------------|:---------:|:----------------:|
| `rocketleague`    | Rocket League       | 5         | 90 min           |
| `leagueoflegends` | League of Legends   | 5         | 60 min           |
| `counterstrike`   | Counter-Strike 2    | 3         | 90 min           |
| `dota2`           | Dota 2              | 3         | 75 min           |
| `valorant`        | Valorant            | 3         | 90 min           |
| `starcraft2`      | StarCraft II        | 5         | 60 min           |
| `overwatch`       | Overwatch           | 5         | 45 min           |

Any other Liquipedia wiki slug also works — gridwatch will generate reasonable defaults for unknown games. You can override any field in the YAML.

## Reverse proxy

Sample configs live in `deploy/`:

- **`deploy/nginx-proxy-manager/`** — step-by-step for NPM, including the SSE buffering gotcha
- **`deploy/traefik/`** — labels fragment
- **`deploy/caddy/`** — Caddyfile snippet

> **SSE gotcha:** nginx (and therefore NPM and openresty) buffers Server-Sent Events by default. gridwatch sends `X-Accel-Buffering: no` on the `/events` endpoint, which nginx honors. If you're seeing stale pages under NPM, double-check proxy-buffering is off on the proxy host.

## Notifier

Off by default. To enable push alerts:

```yaml
notifications:
  enabled: true
  rules:
    - games: [rocketleague, leagueoflegends]
      stages: [live, result]
      min_tier: "A-Tier"
  sinks:
    - kind: ntfy
      url: http://ntfy.lan:8555
      topic: esports
      user: you
      password: "${NTFY_PASSWORD}"
    - kind: webhook
      url: https://hooks.example.com/esports
```

Fired-state is tracked per match, and dedupe is marked **only after a 2xx response** — a failed webhook doesn't cost you the notification on the retry.

## Plex / Jellyfin Live TV integration

gridwatch can't actually play Twitch streams (Plex doesn't know how), but the **schedule** can show up as a native TV guide. Point xTeVe or TVHeadend at `http://gridwatch.lan:8080/api/v1/matches.xml` as an XMLTV guide source, and Plex Live TV renders the schedule as "channels" with programme info.

## Architecture

```
internal/
├── config/         YAML + env overlay + validation
├── model/          Match, Team, Stream, Tournament, Status
├── store/          in-memory, revision-tracked, JSON snapshot
├── httpx/          shared HTTP client (gzip + UA + timeout)
├── ratelimit/      per-host buckets + global envelope + 429 cooldown
├── source/
│   ├── source.go   Source interface + Registry
│   └── liquipedia/ Liquipedia implementation + parser + golden tests
├── poller/         per-game goroutines, jittered schedules
├── notifier/       ntfy + webhook sinks, rule engine, 2xx-gated dedupe
├── server/         HTTP routes, HTMX detection, SSE, iCal, XMLTV, metrics
├── ui/             embedded templates + static assets
├── timeutil/       EPG slot math
└── buildinfo/      -ldflags version injection
```

One process. No database. No message broker. Tiny single binary (14 MB) on distroless/static.

## Development

```bash
make test        # unit tests + race detector
make run         # run against the example config (hits Liquipedia)
make demo        # run with canned fixtures, no upstream calls
make docker      # build a local Docker image
```

Parser golden files regenerate with `go test ./internal/source/liquipedia/ -run TestParseRocketLeagueGolden -update`. Add a new game by dropping its `Liquipedia:Matches` parse API response into `internal/source/liquipedia/testdata/<slug>_matches.json` and adding the corresponding test.

## License

MIT. See [LICENSE](LICENSE).

## Acknowledgments

- [Liquipedia](https://liquipedia.net) for curating the most comprehensive free esports database on the internet
- [htmx](https://htmx.org) for making server-rendered apps feel alive without a framework
- [ntfy](https://ntfy.sh) for the push-notification pattern this project inherits
