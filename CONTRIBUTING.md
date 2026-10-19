# Contributing

Thanks for helping with harnessly. This page covers the development setup, the checks a change must pass, how the
golden tests work, and how commits and pull requests are written.

By taking part you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md). Security problems go through private
reporting, see [SECURITY.md](SECURITY.md).

## Development setup

You need:

- Go, the version named in [`go.mod`](go.mod);
- `make`;
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2 for `make lint`;
- for the tests that drive real tools: `tmux`, `git`, `ffmpeg` and `unzip`. Tests that need a missing tool skip
  themselves; `bash`, `python3`, `node` and a headless Chrome enable a few more.

The full suite is meant for Linux (some tests read `/proc` and skip elsewhere); macOS is covered by the cross build.

```sh
git clone https://github.com/ly-solutions/harnessly.git
cd harnessly
make build        # bin/runtime, bin/agent, bin/agentprobe
make test
```

## Make targets

`make help` lists them all.

| Target | What it does |
| --- | --- |
| `make build` | static binaries without cgo into `bin/`, version from `git describe` |
| `make test` | `go test ./...` |
| `make test-race` | the tests with the race detector, as CI runs them |
| `make lint` | golangci-lint with [`.golangci.yml`](.golangci.yml) |
| `make fmt` / `make fmt-check` | format with gofmt / fail on an unformatted file |
| `make vet` | `go vet ./...` |
| `make cover` | the tests with coverage into `coverage.out`, and the total |
| `make cross` | compile every package for linux and darwin, amd64 and arm64 |
| `make clean` | remove build outputs |

Before you push, run what CI runs:

```sh
make fmt-check vet lint test-race cross
```

and scan for secrets with [gitleaks](https://github.com/gitleaks/gitleaks): `gitleaks dir .` must report nothing.

## How the tests are safe to run

Tests that start tmux use private servers named `hs-test-*` and stop them when they finish. They never touch your own
tmux server, `~/.claude` or `~/.codex`: everything happens in temporary folders, and network access is limited to test
servers on localhost.

## Golden tests

Much of the runtime replaces an earlier Node.js implementation whose answers other programs already parse, so
compatibility is byte for byte: status codes, JSON bodies with their key order (`internal/nodejson` writes like
`JSON.stringify`), headers, ETags and 304s. That is pinned by golden files under the packages' `testdata`: the
previous implementation's own handlers were run under Node on generated inputs, and the files record what they
answered. The generators sit next to the goldens (`testdata/gen`, `*.mjs`); they need that implementation's source,
which is not published, so for most contributors a golden is a fixed contract:

- a change that alters a golden answer is a behaviour change: say so in the pull request and explain why;
- new behaviour that has no counterpart there gets ordinary Go tests;
- a few tests compare against large generated sets that are not committed; they skip unless their variable
  (`TS_GOLDEN`, `SHARE_RANDOM`, `SOURCES_RANDOM`, `ENTRIES_RANDOM`, `RUNTIME_COVERAGE`) points at one.

Goldens that record file modes set the umask they were taken with, so the modes match on every machine.

Goldens never contain a credential-shaped string. A test that needs a fake GitHub, Anthropic, AWS or similar token
builds it at run time (`"gh" + "p_" + …`) and the golden stores a placeholder such as `@FAKE_GITHUB_TOKEN@`. JWTs and
fixture secrets that are obviously fake are allowlisted in [`.gitleaks.toml`](.gitleaks.toml), narrowly, each with a
reason.

## Neutral names

The code is used by deployments it does not know about. Keep product names, company names, people's names, internal
host names and home folder paths out of code, tests, test data and comments. Whatever a deployment must name (paths,
header and cookie names, hosts, URL prefixes, task key patterns) is read from a `RUNTIME_*` or `AGENT_*` variable with a
neutral default, and documented in [docs/configuration.md](docs/configuration.md). Tests use stand-ins: `acme`,
`example.org`, `example.com`, `/home/agent`.

## Style

- `gofmt`, the standard library first, few dependencies.
- Comments say why, not what; neutral English names.
- Lint findings are fixed, not silenced. A `//nolint` needs the linter's name and a reason.

## Commits and pull requests

Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```text
<type>(<scope>): <summary>

<body: what changed and why, wrapped at 120 columns>
```

- `type` is one of `feat`, `fix`, `perf`, `refactor`, `test`, `docs`, `build`, `ci`, `chore`, `style`;
- `scope` is the package or area (`gate`, `status`, `agent`, `runtime`, `design`, ...);
- a breaking change adds `!` after the scope and a `BREAKING CHANGE:` footer.

The release changelog is built from these subjects, so `feat` and `fix` subjects should read well on their own.

One logical change per commit. Open the pull request against `main`, fill in the template, and keep it focused; CI
(tests, lint, CodeQL, the cross build and gitleaks) must be green before it is merged.

## Releases

A maintainer tags `vX.Y.Z` on `main`. The [release workflow](.github/workflows/release.yml) runs
[GoReleaser](.goreleaser.yaml): `agent` and `runtime` for linux and darwin, amd64 and arm64, with checksums and a
changelog from the commit subjects. [CHANGELOG.md](CHANGELOG.md) is updated in the same pull request that prepares the
release.

## License

By contributing you agree that your contribution is licensed under the [Apache License 2.0](LICENSE).
