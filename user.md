# Nodryl user guide

Nodryl helps you show what is in a code project and what its backend does when a request runs. The **Map** tab comes from files and source code. The **Activity** tab comes from real OpenTelemetry traces. A line on the map is an inference; a step in Activity was observed.

## Install and start

From this repository, install the development dependencies and build the command:

```sh
npm ci
npm run build
./dist/nodryl help
```

Requirements: Go 1.26+, Node.js 20.6+, and npm. After the npm package is published and installed, use `nodryl` instead of `./dist/nodryl`. For the bundled Node tracing helper, run `node /path/to/nodryl/bin/nodryl.js` from this checkout or use the installed npm command.

## Commands

| Command | What it does |
| --- | --- |
| `nodryl` | Open the current project's map in a terminal. When output is redirected, print a text summary. |
| `nodryl explore [path]` | Open the interactive map for a project directory. |
| `nodryl map [path]` | Print a quick text summary. |
| `nodryl map --json [path]` | Export nodes, links, source locations, and scan warnings as JSON. |
| `nodryl observe [--listen address] [path]` | Open the map and receive OTLP/HTTP protobuf traces from an instrumented backend. Default address: `127.0.0.1:4318`. |
| `nodryl dev -- <command>` | Start a Node.js app with the bundled tracing helper and open Map and Activity. Run this from the app's project directory. |
| `nodryl help` | Show command help. |

Paths are optional and default to the current directory. `observe` can accept the path and `--listen` in either order. `dev` runs the given command in the current directory; the command and its arguments follow `--`.

## Demonstrate a backend flow

### Bundled Express example

In this repository:

```sh
npm run build:fixture
cd test/fixture
node ../../bin/nodryl.js dev -- node dist/server.js
```

In another terminal:

```sh
curl http://127.0.0.1:3000/checkout
```

Press **Tab** to open Activity. Select the checkout request. The right pane shows the HTTP and Express steps, the outbound call to `/payment`, their durations, and any reported failure. Press **Tab** to inspect the project map again.

### Any instrumented backend

Start Nodryl in the backend repository:

```sh
nodryl observe
```

Configure the backend's OpenTelemetry exporter for **OTLP over HTTP with protobuf**, then start the backend and make a request:

```sh
export OTEL_SERVICE_NAME=my-backend
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
# Start your instrumented backend with its usual command.
```

The exporter sends traces to `http://127.0.0.1:4318/v1/traces`. If your SDK needs a trace-specific URL, set `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:4318/v1/traces`. An exporter may need an instrumentation package or agent before it emits spans. See the [OpenTelemetry language guides](https://opentelemetry.io/docs/languages/) for your backend and the [OTLP exporter settings](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/) for its exact configuration. Nodryl currently receives traces only; disable metrics and logs exporters if your SDK sends those by default.

To listen on another local port, use `nodryl observe --listen 127.0.0.1:4320` and point the exporter to that port. A remote backend requires an address reachable from that backend, for example `--listen 0.0.0.0:4318`, and appropriate network protection. The default binds only to the local machine.

`observe` works with a backend written in any language **when that backend exports compatible traces**. The map inventories any repository, with deeper code parsing for JavaScript, TypeScript, Go, and Python. Nodryl does not automatically instrument every language or infer every internal function call.

## Terminal keys

| Key | Action |
| --- | --- |
| `↑` / `↓` or `k` / `j` | Move through components or requests. |
| `Enter` / `→` / `l` | Open a selected directory. |
| `←` / `Backspace` / `h` | Go to the parent directory. |
| `g` | Return to the project root. |
| `/` | Search file names, source paths, or request names. `Enter` keeps the filter; `Esc` clears it. |
| `Tab` | Switch Map and Activity when tracing is active. |
| `s` | Open a selected source location in `$VISUAL`, `$EDITOR`, or `vi`. |
| `r` | Rescan the project map. |
| `?` | Show or hide the key guide. |
| `q` / `Ctrl+C` | Quit. In `dev`, this also stops the launched app. |

## Reading the display

- **Map** shows files, folders, dependency manifests, imports, detected routes, and source links. Cyan marks code evidence.
- **Activity** groups observed spans by trace ID, labels the reporting service, nests child spans beneath parents, shows duration bars, and marks reported failures in red. Amber marks runtime evidence.
- The Activity list keeps the most recent 100 traces in memory. A trace appears when the exporter sends it, which may happen a few seconds after a request ends.
- Source links from Activity are available when a request name matches a route detected in the project.

## Current limits

The scanner recognizes common manifests and languages, but dynamic routing, generated wiring, and framework conventions can be missed. `dev` currently supplies automatic tracing for supported CommonJS Node.js and Express apps, including HTTP, PostgreSQL, node-redis, and ioredis instrumentation. Other backends use `observe` plus their own OpenTelemetry setup. The receiver accepts OTLP/HTTP protobuf traces at `/v1/traces`; it does not receive gRPC, JSON, metrics, or logs. Traces stay in memory and disappear when Nodryl closes. The receiver drops payloads, headers, and SQL statement attributes rather than retaining them for display.

## AI analysis direction

An optional AI explanation layer could summarize a selected route or trace, explain a failure, and suggest which source file to inspect. It should receive only a compact graph and sanitized trace summary; every factual claim should link to a code location or observed span. AI output should be labeled as an interpretation, with a clear way to inspect the underlying evidence. Provider credentials, data sharing, and cost controls should be explicit opt-in settings. This layer is a proposed addition; the current commands do not call an AI service.
