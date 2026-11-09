# The hub's central password safe

The hub keeps people's site passwords for the workspace's «Sign in as …» buttons and for agents that log in on a site
for them. It is the central side of the safe; the machine's own safe (`internal/vault`, `internal/secrets`) only carries
an agent's request to the person's page.

## Switching it on

Off by default. Set `HUB_VAULT_KEYS` to turn it on:

| Variable | Meaning |
| --- | --- |
| `HUB_VAULT_KEYS` | `v2:<base64 of 32 bytes>,v1:<base64>`: the master keys, the first one seals new entries. Empty: the safe is off and the page's `/vault` calls go to a machine as before. Generate one with `openssl rand -base64 32`. |
| `HUB_VAULT_SHARED_MEMBERS` | emails (comma-separated) of people who see the shared entries; none means there are none. |
| `HUB_VAULT_SHARED_EDITORS` | emails who create, change and delete shared entries; each must be a member too. |

Keep the keys in the hub's secret store (the environment of the hub's process), not in the database. A key that is
dropped from the list makes the entries sealed with it unreadable; rotate by putting the new key first and keeping the
old one until nothing is sealed with it.

## What is stored

Migration `0039_vault.sql` adds `vault_entries` (AES-256-GCM ciphertext as base64 text, the key id, the owner,
`shared`), `vault_grants` (an agent tab the person allowed to use an entry, for 8 hours) and `vault_audit` (who, which
entry and site, what and how it ended; never a value). The associated data binds a secret to its id, scope (owner or
shared), site and login, so a row moved or edited by hand no longer opens. The migration only adds tables: a revision
without it never reads them.

## API

Under the workspace's platform API, `<platform api>/vault/…`; every call carries `X-Hl-Vault: 1` and is refused when it
comes from another site (`Sec-Fetch-Site`). The person is always the signed-in account, never a field of a request.

| Call | Answer |
| --- | --- |
| `GET status` | `{enabled, canShare, grantHours}` or `{enabled: false, reason}` |
| `GET entries`, `POST entries` | the person's entries (and the shared ones they see); create `{origin, login, password, label?, shared?}` |
| `PATCH entries/<id>`, `DELETE entries/<id>` | change or delete |
| `POST entries/<id>/approve` | the person allows an agent tab (`{agent: "<machine>/<tab>", origin}`): a 30-second approval |
| `POST entries/<id>/fill` | a one-time fill token for one document of one preview slot (`{for: person\|agent, origin, slot, nonce, approval?}`) |
| `POST entries/<id>/deny`, `DELETE entries/<id>/grants` | record a refusal; revoke an agent's grant |
| `GET audit` | the last 50 events of the person |

No route answers a password. The value leaves the hub only through `vaultsvc.Service.Redeem`, which the preview side of a
slot calls for `POST <slot>/fill endpoint of the preview bridge` with what the slot's signed cookie says; a token is spent by its first
attempt and opens only for its person, slot, document and site.

Limits: 6 fills a minute and 40 an hour, 30 writes an hour, 120 reads a minute, 200 entries per person.
