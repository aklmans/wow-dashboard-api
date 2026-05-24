# Observability

The API exposes Prometheus metrics, OpenTelemetry traces, and structured
JSON logs out of the box. This document covers the **bundled local stack** —
Prometheus, Grafana, and Jaeger — that wires those signals into dashboards
and a trace UI without any clicks. Production deployments swap the
containers for their managed equivalents (Grafana Cloud, AMP, Tempo,
Datadog, etc.); the dashboards and queries here are the reference.

## What the API exposes

| Surface             | Endpoint / Channel                    | Notes |
| ------------------- | ------------------------------------- | ----- |
| Liveness            | `GET /healthz`                        | Process-only check. |
| Readiness           | `GET /readyz`                         | Pings PostgreSQL with `DB_HEALTH_TIMEOUT_SECONDS`. |
| Prometheus metrics  | `GET /metrics`                        | See [Metric reference](#metric-reference). |
| Distributed traces  | OTLP/HTTP → `OTEL_EXPORTER_OTLP_ENDPOINT` | No-op when env var is empty. |
| Structured logs     | stdout (JSON in production)           | Every request log carries `request_id`. |

## Local stack

```sh
make observability-up            # start prometheus + grafana + jaeger
make observability-down          # stop and remove
make observability-logs          # tail logs
make observability-config        # validate compose.yaml
```

URLs:

| Service     | URL                          | Credentials  |
| ----------- | ---------------------------- | ------------ |
| Grafana     | <http://localhost:3001>      | admin/admin (anonymous viewer also enabled) |
| Prometheus  | <http://localhost:9090>      | —            |
| Jaeger      | <http://localhost:16686>     | —            |

The Grafana instance auto-loads the `API Overview` dashboard from
`observability/grafana/dashboards/` and the Prometheus + Jaeger datasources
from `observability/grafana/provisioning/`.

To send traces, set the env var on the API process and restart it:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run ./cmd/api
```

Jaeger v2 receives OTLP directly on 4317 (gRPC) and 4318 (HTTP), so no
separate `otel-collector` is needed for local work.

### How Prometheus reaches the API

`observability/prometheus/prometheus.yml` scrapes
`host.docker.internal:7272` so the API process can keep running on the host
(no container build round-trip while iterating). Linux Docker also honours
this hostname since Compose v20.10+ — the `extra_hosts` block in
`compose.yaml` makes it explicit.

## Metric reference

The API uses two HTTP metrics plus the standard Go runtime and process
collectors.

| Metric                                     | Type      | Labels                  | Notes |
| ------------------------------------------ | --------- | ----------------------- | ----- |
| `http_requests_total`                      | Counter   | `method`, `route`, `status` | `route` is the matched Chi pattern (e.g. `/api/users/{id}`). Excludes `/metrics`. |
| `http_request_duration_seconds`            | Histogram | `method`, `route`       | Default Prometheus buckets, seconds. |
| `go_goroutines`, `go_gc_duration_seconds`, `go_memstats_*` | varies | —     | Runtime collector. |
| `process_resident_memory_bytes`, `process_cpu_seconds_total`, … | varies | — | Process collector. |
| `up{job="wow-dashboard-api"}`              | Gauge     | `instance`              | Prometheus-internal; 1 when the scrape succeeded. |

There are intentionally no DB-pool, queue-depth, or rate-limiter metrics
yet — those are deferred until there's a real production need.

## Dashboard layout (API Overview)

Grafana opens on `API Overview` (`uid: wow-api-overview`). Four rows:

1. **Overview (last 5m)** — Request rate, 5xx ratio, p95 latency, and an
   `UP/DOWN` tile from Prometheus's own `up` series.
2. **HTTP** — Stacked request rate by status class, p50/p95/p99 latency,
   top routes by RPS, and p95 latency per route. The per-route panels use
   `topk(10, …)` so a misbehaving endpoint surfaces without flooding the
   legend.
3. **Go runtime** — Goroutine count, heap in use, process RSS at-a-glance.
4. **Goroutines over time + GC pauses** — Drill-in panels for diagnosing
   leaks or GC pressure.

Every panel has a 5m rate window so transient blips don't trigger false
positives in the at-a-glance tiles.

## Alert rules

`observability/prometheus/alerts.yml` ships three example alerts. They mirror
the dashboard panels so an operator sees the same signal whether they happen
to be watching Grafana or not. Tune the thresholds and `for:` durations to
each environment before pointing Alertmanager at them.

| Alert               | Trigger                                                  | Severity |
| ------------------- | -------------------------------------------------------- | -------- |
| `ApiDown`           | `up == 0` for 2m                                         | critical |
| `ApiHighErrorRate`  | 5xx ratio > 5% over 5m, sustained 10m                    | warning  |
| `ApiSlowP95Latency` | Any route p95 > 1s over 5m, sustained 10m                | warning  |
| `ApiGoroutineLeak`  | `go_goroutines > 5000` sustained 15m                     | warning  |

These rules are not wired to Alertmanager by default — the local stack only
runs Prometheus, Grafana, and Jaeger. To wire alerts, add Alertmanager to
`observability/compose.yaml` and point Prometheus at it via
`alerting.alertmanagers`.

## RED methodology

The dashboard follows the **RED** pattern for a request-driven service:

- **Rate** — `sum(rate(http_requests_total[5m]))`
- **Errors** — same metric, filtered by `status=~"5.."`, divided by total
- **Duration** — `histogram_quantile(p, sum by (le) (rate(http_request_duration_seconds_bucket[5m])))`

The Go runtime row is supplementary — it lets you correlate a latency spike
with goroutine growth, GC pauses, or memory pressure without leaving the
dashboard.

## Production deployment notes

The local stack is for dev and demos. In production:

- **Metrics** — Scrape `/metrics` from a managed Prometheus (AMP, Grafana
  Cloud Metrics, a self-hosted HA pair) and keep the dashboards under
  source control. Import `observability/grafana/dashboards/api-overview.json`
  unchanged.
- **Traces** — Set `OTEL_EXPORTER_OTLP_ENDPOINT` to the address of your
  collector or backend (Tempo, Jaeger Collector, Honeycomb, etc.). The API
  uses OTLP/HTTP so no extra exporter library is required.
- **Logs** — Production logs are JSON via `slog`; ship them to your log
  aggregator and use `request_id` to join with traces (the same value rides
  the `X-Request-ID` response header).
- **Datasource UIDs** — The dashboards reference `prometheus` and `jaeger`
  datasource UIDs. When importing into a different Grafana, either name your
  datasources to match those UIDs or edit the dashboard to point at the
  production UIDs.

## Future additions (non-blocking)

These are intentionally deferred until a real need shows up:

- DB pool gauges (`pgxpool.Stat()` exposes idle/in-use/total).
- River job throughput, success/failure counts, queue depth.
- Rate-limiter rejection counter.
- An Alertmanager service in the local compose.
- A separate `otel-collector` (only needed once we have multiple trace
  backends or want sampling / batching tuned per-pipeline).
