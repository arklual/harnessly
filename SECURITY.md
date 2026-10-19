# Security

## Supported versions

harnessly is in `0.x`: fixes land on `main` and in the next release. Use the latest release.

## Reporting a vulnerability

Please report security problems privately, through GitHub's private vulnerability reporting: open the repository's
**Security** tab and choose **Report a vulnerability**. Do not open a public issue or pull request for them.

Include what you found, the commit or release it applies to, how to reproduce it, and what an attacker gains. We will
acknowledge the report within a few working days, keep you informed while we fix it, and credit you in the release
notes unless you ask us not to.

## Scope

Both programs run on a person's machine with that person's rights, so the boundaries that matter most are:

- who may call the runtime (`cmd/runtime`): page sessions signed with the pair's secret, the `cli-token`, the
  network access rule for the hook routes;
- what the machine agent (`cmd/agent`) accepts over its link to the cluster, and its loopback routes guarded by the
  local token;
- which files the runtime's file routes may read or write (the viewer refuses credential folders and files);
- the preview proxy: claims checked against the pair's secret, gateway-only headers never passed upstream;
- conversation snapshots: secrets redacted before anything is shared.

The secrets, tokens and keys under `testdata` and in tests are fixtures and grant nothing; reports about them are not
needed.
