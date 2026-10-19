## What and why

<!-- What this change does and the problem it solves. Link the issue: "Fixes #123". -->

## How it was tested

<!-- Commands you ran, new tests, a manual run of the runtime or the agent. -->

## Checklist

- [ ] `make fmt-check vet lint test-race` passes
- [ ] The title is a Conventional Commit (`feat(scope): …`, `fix(scope): …`)
- [ ] Golden answers are unchanged, or the change says which ones move and why
- [ ] New settings are `RUNTIME_*` / `AGENT_*` variables with neutral defaults, documented in `docs/configuration.md`
- [ ] No product names, hosts, personal paths or credential-shaped strings in code, tests or test data
