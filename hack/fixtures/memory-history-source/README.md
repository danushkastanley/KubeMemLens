# Controlled history source

This qualification fixture serves synthetic memory evidence over TLS. It does
not measure workload or Node memory and must never be labelled as a real memory
source in a qualification result.

`POST /api/v1/query_range` provides the existing controlled range-response and
failure modes. `GET /metrics` exposes working-set and RSS gauges for a real
Prometheus evaluator to scrape. Both require the fixture-only bearer credential.
The scrape endpoint accepts only `normal` mode and rejects incomplete identities,
duplicate series, unknown labels and control characters before writing any data.
It retains zero values and emits no timestamps: the evaluator supplies the actual
scrape time, rather than the fixture inventing historical samples.

For Kubernetes checks, prepare the configured identity labels from the exact
current Node UID, Pod UID and runtime container ID. Retain that acquisition as
evidence, check the identities again after the read, and label all configured
values as controlled fixtures. Do not rewrite earlier series to a new lifetime.
The configuration file is bounded to 64 KiB and 32 series.

Use `--listen=127.0.0.1:PORT` for host checks or a same-Pod scraper. The default
remains `:9443` for the existing isolated Kubernetes range fixture. Supply
`--config`, `--cert`, `--key` and `--token` as private files. The fixture does not
create credentials, Kubernetes permissions or an evaluator. Keep it outside
performance measurement windows and remove its owned resources afterwards.
