# The corpus of old apps

Apps as they were written for earlier versions of harnessly, kept unchanged: an update of the host must not break
them («Совместимость»). `compat-corpus.test.ts` runs each view against the real bridge and SDK — handshake,
the calls it makes on start, its page tools — and `internal/apps/compat_test.go` installs each package and checks that
an install made before consent was asked keeps its rights.

- `ptcheck/` — the page-tools check (0.1.3, harnessly >=0.16): registers `echo` and `wipe` through
  `navigator.modelContext` and publishes its context.
- The apps of `docs/apps/` (cashback card, deck-lint presentation validator and the rest) and the MCP Apps views of
  `fixtures/ext-apps/` are part of the corpus where they are.

Do not edit an app here to make a test pass: a red test means the host broke it.
