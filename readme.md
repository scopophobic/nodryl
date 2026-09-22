# Nodryl

Nodryl is a terminal-first system map for a codebase. It inventories files, directories, languages, and common dependency manifests in any repository, then adds deeper code relationships where parsers are available. An optional live view overlays observed OpenTelemetry spans for supported Node.js apps.

## Try it locally

Requirements: Go 1.26+, Node.js 20.6+, and npm.

```sh
npm ci
go build -o dist/nodryl ./cmd/nodryl
./dist/nodryl explore
./dist/nodryl map
./dist/nodryl map --json
```

Or build the npm launcher with `npm run build` and run `node bin/nodryl.js explore`. To try live tracing with the bundled Express fixture:

```sh
npm run build:fixture
cd test/fixture
node ../../bin/nodryl.js dev -- node dist/server.js
# In another terminal:
curl http://127.0.0.1:3000/checkout
```

Run Nodryl from the project directory, or pass a directory to `explore` or `map`. The release package will provide the `nodryl` command after one npm install.

## Commands

- `nodryl` or `nodryl explore [path]` opens the interactive system map. Without a terminal, bare `nodryl` prints a text summary.
- `nodryl map [path]` prints a text summary; `nodryl map --json [path]` emits the graph.
- `nodryl dev -- <command>` launches a supported Node.js app and opens the map and live activity tabs.

In the TUI, use ↑/↓ to browse, Enter to open a directory, ← to go back, `/` to search, `s` to open source, `r` to rescan, `?` for help, and `q` to quit. Press Tab to switch map/activity while running `dev`. Cyan links are inferred from code; amber activity is runtime evidence.

The universal scan is a baseline, not full semantic understanding of every language: unknown files still appear in the map, common manifests contribute dependency links, and JavaScript/TypeScript, Go, and Python receive deeper parsing. Framework behavior, generated wiring, and dynamic calls may be missing. The live tracing path currently supports CommonJS Express apps, not arbitrary runtimes. It traces HTTP, Express, PostgreSQL, node-redis, and ioredis calls; it does not automatically trace internal function calls. No payloads, headers, or SQL text are retained by the local receiver.

Nodryl currently ships native binaries for macOS and Linux on x64 and arm64.

## Development

```sh
npm test
```

The integration test opens loopback ports. `npm test` compiles the TypeScript Express fixture before running Go tests.

Publishing is prepared as a manual GitHub Actions workflow. It builds native binaries, publishes each platform package, then publishes the `nodryl` npm package. No package has been published from this repository.
