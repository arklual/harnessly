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

The hub (`cmd/hub`, `internal/hub`, `internal/hubproto`) is in scope too, and what it holds is more than most:

- **Sign-in.** Email codes (stored hashed, ten minutes, five tries, rate-limited per address and per network), passkeys,
  GitHub, Yandex ID and OpenID Connect (`state`, PKCE and `nonce` checked), and the rules for joining an identity to an
  account: an identity joins an existing account by email only when its provider verified the email, and Yandex never does.
- **Sessions and CSRF.** Opaque random tokens stored hashed, cookies `HttpOnly` and `SameSite`, `Secure` and `__Host-`
  over https, and the refusal of cross-site and non-JSON requests that change something.
- **Spaces and invitations.** Only members see a space and its machines; invitations are bound to an address or are links
  with an expiry and a number of uses; the last owner cannot leave.
- **Machine registration and the link.** One-time registration codes (stored hashed, thirty minutes), an Ed25519 key per
  machine, the signed dial (machine, host, time window, a nonce used once), revocation that ends the link.
- **The runtime proxy.** Membership of the machine's space checked on every request and on every WebSocket, the one-time
  tickets and host-only cookies of subdomain mode, the hub's cookie never passed to a machine and a machine's `Set-Cookie`
  never passed to the browser.
- **What passes through.** API keys, environment values and proxy credentials are sealed to the machine's key and relayed;
  the hub does not store or log them, and a report that one of them reaches a log, a database row or an error answer is a
  vulnerability.
- **The SSH way.** A space's own SSH key (one per space, sealed in the database, shown only to the space's owners), the host key fingerprint the owner confirms before anything runs, and the refusal of non-public addresses.

### What the hub can do to a machine

Said plainly, because it is the point of the design: **the hub can run code on every machine connected to it.** It
installs agents, opens a login console that is a terminal, delivers environment variables that agents read, and carries
the runtime page, whose terminals and agents are the machine's own. Whoever controls a hub (its host, its database, its
administrators' accounts, or a flaw in it) can do on a machine what a person with that space's access can: run commands
as the user the agent runs as. Connect only machines you would give an SSH login for that user to the people of the space
and to whoever runs the hub.

What holds that back is on the machine and not in the hub: the **local policy** (a `policy.toml` on the machine,
edited there and never by the hub) switches terminals, agent logins, keys, environment delivery, installs and the runtime session off one by one, and
the **kill switch** (`harnessly-agent off`) ends the link at once; the **profiles** (`dir`, `container`, `full`) decide
what the agent's user can reach in the first place; and a machine can be revoked from the hub. The hub cannot change a
machine's local policy. In path mode (no `HUB_RUNTIME_DOMAIN`; the cloud edition refuses it) a machine's pages share the hub's origin and so are
trusted as much as the hub, whatever its software does: use [subdomain mode](docs/hub/deploy.md#where-machines-open-path-or-subdomain) when machines belong to
people who do not trust each other.

Not vulnerabilities: the sign-in code written to the log by a hub that has no mail server (marked `DEV`, and what that mode
is for); a provider missing from the sign-in page because its settings are incomplete; what an administrator of the
installation, or a member with the rights of their role, is meant to be able to do.

The secrets, tokens and keys under `testdata` and in tests are fixtures and grant nothing; reports about them are not
needed.
