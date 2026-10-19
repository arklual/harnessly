# Contributing

## Build and test

You need the Go version named in `go.mod`. The binaries are static and need no cgo:

```sh
go build ./...
CGO_ENABLED=0 go build -trimpath -o agent ./cmd/agent
CGO_ENABLED=0 go build -trimpath -o runtime ./cmd/runtime
```

Before sending a change, run what CI runs:

```sh
test -z "$(gofmt -l cmd internal)"
go vet ./...
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null ./cmd/...
go test -race -count=1 ./...
```

Some tests drive real tools and skip themselves when the tool is missing: `tmux` (terminals, tabs, the core), `git`
(working-tree routes, checkpoints), `ffmpeg` (voice), `unzip` (design imports), `bash`, `python3`, `node` and a
headless Chrome. Install `tmux`, `git`, `ffmpeg` and `unzip` to run nearly everything. Tests that start tmux use private
servers named `hs-test-*` and stop them when they finish; they never touch your own tmux server, `~/.claude` or
`~/.codex` (they work in temporary folders). Some tests read `/proc` and skip elsewhere, so the full suite is meant
for Linux; macOS is covered by the cross build.

## Golden tests

Much of the runtime replaces an earlier Node.js implementation whose answers other programs already parse, so
compatibility is byte for byte: status codes, JSON bodies with their key order (`internal/nodejson` writes like
`JSON.stringify`), headers, ETags and 304s. That is pinned by golden files under the packages' `testdata`: the
previous implementation's own handlers were run under Node on generated inputs, and the files record what they
answered. The generators sit next to the goldens (`testdata/gen`, `*.mjs`); they need that implementation's source,
which is not published, so for most contributors a golden is a fixed contract:

- a change that alters a golden answer is a behaviour change — say so in the merge request and explain why;
- new behaviour that has no counterpart there gets ordinary Go tests;
- a few tests compare against large generated sets that are not committed; they skip unless their variable
  (`TS_GOLDEN`, `SHARE_RANDOM`, `SOURCES_RANDOM`, `ENTRIES_RANDOM`, `RUNTIME_COVERAGE`) points at one.

Goldens never contain a credential-shaped string. A test that needs a fake GitHub, Anthropic, AWS or similar token
builds it at run time (`"gh" + "p_" + …`) and the golden stores a placeholder such as `@FAKE_GITHUB_TOKEN@`. JWTs and
fixture secrets that are obviously fake are allowlisted in `.gitleaks.toml`, narrowly, each with a reason. Run
`gitleaks dir .` before you push; it must report nothing.

## Neutral names

The code is used by a deployment, but it does not know which one. Keep product names, company names, people's names,
internal host names and home folder paths out of code, tests, test data and comments. Whatever a deployment must name
(paths, header and cookie names, hosts, URL prefixes, task key patterns) is read from a `RUNTIME_*` or `AGENT_*`
variable with a neutral default, and documented in the README's tables. Tests use stand-ins: `acme`, `example.org`,
`example.com`, `/home/agent`.

## Style

- `gofmt`, standard library first, few dependencies.
- Comments say why, not what; neutral English names.
- One logical change per commit, with a message in the conventional form (`feat(scope): …`, `fix(scope): …`,
  `test(scope): …`, `chore(scope): …`).

## License

By contributing you agree that your contribution is licensed under the Apache License 2.0 (see `LICENSE`).
