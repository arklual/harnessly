# Deploying in a closed network

For a network with no internet, a company CA, a corporate proxy and internal domains. This page lists every address
harnessly may open, and what to do about each one: serve it from inside, give it through the proxy, or switch it off.
Example names below are `example.corp`.

## What leaves the machine

| Who | Address | What for | In a closed network |
| --- | --- | --- | --- |
| `install.sh` | `raw.githubusercontent.com`, `github.com/ly-solutions/harnessly/releases` | the installer and the release archives | Install from the hub: `curl <hub>/install \| sh -s -- --token …`, the programs come from the hub's `/dl`. Without a hub: [offline bundle](../hub/machine.md) (`--offline`) or `--mirror` |
| installer | `claude.ai/install.sh` | Claude Code | Put Claude Code on the machine yourself (your package source); the check «Claude Code / Codex» in Settings tells when it is missing |
| runtime | `<hub>/dl/…` | updates of the machine's programs | Inside: the hub. `RUNTIME_UPDATE_CHECK_URL` for a mirror of the GitHub API |
| runtime | `huggingface.co`, `cdn.jsdelivr.net` | the built-in voice input: model files, browser engine | Mirror the same paths inside and set `RUNTIME_VOICE_MODEL_HOST` and `RUNTIME_VOICE_RUNTIME_HOST`, or leave voice off (no address is opened until someone turns it on) |
| runtime | `storage.googleapis.com/chrome-for-testing-public` | Chrome of the agent's browser | `RUNTIME_BROWSER=off`, or install Chrome and point `RUNTIME_BROWSER` at it |
| agent | `api.anthropic.com`, `api.openai.com` | the model of Claude Code / Codex | Your own model gateway: see «Model provider» below |
| design | `unpkg.com` (React, Babel) | the design engine's scripts | Nothing: React and Babel are served from the binary; only a release engine of other versions asks unpkg (see [design engine](../design-engine.md)) |
| demo projects | `fonts.googleapis.com` | the sample decks and sites | Cosmetic: the page falls back to system fonts. Delete the demo or ignore it |
| Connections | the hosts of the services you connect (`github.com`, a GitLab, …) | boards, repositories, documents | Only what you connect; internal services are listed in the network policy below |
| apps | what an app's `.mcp.json` names | the app's own backend | Your service; listed in the network policy below |
| hub | your OIDC provider, your SMTP | sign-in | Inside: see «Hub sign-in» |

`harnessly selftest` and Settings → General → «Проверка машины» show what of this the machine can reach.

## The network policy

One policy says what apps and Connections may reach. The hub holds it, machines take it by themselves.

Put `network.json` into the hub's dist folder (`HUB_DIST_DIR`, `/opt/harnessly/dist`):

```json
{
  "internalHosts": ["*.example.corp", "crm.example.corp"],
  "caPem": "-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----\n",
  "proxy": "http://proxy.example.corp:3128",
  "noProxy": ["localhost", ".example.corp"]
}
```

| Key | Meaning |
| --- | --- |
| `internalHosts` | names (`crm.example.corp`) and suffixes (`*.example.corp`) that may sit on private addresses; plain `http` is allowed to them; link-local addresses (cloud metadata) stay closed |
| `caPem` | your root certificates, added to the system's |
| `proxy` | the proxy for the calls of apps and Connections |
| `noProxy` | hosts that go direct: names, `.suffix`, `*.suffix`, IPs, CIDRs |

The hub serves the file at `/dl/network.json` (no login, like the programs: no secrets in it) and refuses a file that is
not valid. A machine reads it at start and every five minutes, keeps the last one in its data folder (`network-policy.json`),
so a restart without the hub keeps the policy.

**The machine's environment overrides it.** What the environment names replaces the hub's value of that setting; what it
leaves out stays from the hub:

| Variable | Setting |
| --- | --- |
| `RUNTIME_APPS_INTERNAL_HOSTS` | `internalHosts` |
| `RUNTIME_APPS_CA_FILE` | the roots (a PEM file) |
| `RUNTIME_APPS_PROXY`, `RUNTIME_APPS_NO_PROXY` | `proxy`, `noProxy` |
| `RUNTIME_APPS_CLIENT_CERT_FILE`, `RUNTIME_APPS_CLIENT_KEY_FILE` | a client certificate for mutual TLS, sent when a server asks for one |

**Edit it in the hub instead of a file.** An administrator of the installation opens **Account → Closed network**
(`/network`) and fills a form: internal domains, root certificates, the proxy and its exceptions, the signature rule. The
form checks what is typed and says which field is wrong and what to write; a policy with a mistake is not saved. Saving
writes `network.json` in the dist folder (the same file as above) and the machines take it by themselves: they ask the hub
every five minutes, no restart. The same is `GET`/`PUT /api/admin/network` for an administrator.

`install.sh --ca-file`, `--proxy` and `--no-proxy` write these for the runtime next to `SSL_CERT_FILE` and
`HTTPS_PROXY` for every component. Run again, the installer keeps the lines it does not write itself (your
`RUNTIME_PREVIEW_DOMAIN`, `RUNTIME_APPS_INTERNAL_HOSTS`).

An app's call through the proxy is judged as a direct one: the machine resolves the name itself, every address it gets
must pass the policy, and the tunnel (`CONNECT`) goes to the checked address, not to the name. An internal domain the
machine cannot resolve (known to the proxy alone) goes to the proxy by name. A host written as a number in any form but
the canonical one (`2130706433`, `0x7f.1`, `127.1`) is refused. The proxy gets `CONNECT` for `http://` calls too. A call
refused by the policy says so: «домен `crm.example.corp` не в политике контура — попросите
администратора добавить его во внутренние домены» (the bridge's answer has a `hint`).

## DNS for the preview

The preview of a dev server goes by path on the page's own address (one name, one certificate; nothing to add to DNS):
a runtime behind the hub does this by itself. For slots on their own names set a domain, and the runtime makes the eight
addresses `https://s1-<id>.<domain>` … `s8-<id>.<domain>` from it, the same after every restart:

```sh
RUNTIME_PREVIEW_DOMAIN=pv.example.corp
RUNTIME_PREVIEW_ID=m1               # optional: the machine's id by default
```

DNS: one wildcard record, `*.pv.example.corp` → the machine (or your reverse proxy in front of ports `8023`–`8030`),
and a wildcard certificate; details in [Deploy](../deploy.md). `harnessly doctor preview` and the machine check
«preview_dns» say when a slot's name does not resolve, with the record to add.

## Company CA

- `install.sh --ca-file /path/ca.pem`: downloads and every component trust it.
- the policy's `caPem` or `RUNTIME_APPS_CA_FILE`: the apps' and Connections' calls.
- a Claude Code or git that talks to your servers: `SSL_CERT_FILE` / `NODE_EXTRA_CA_CERTS` (the installer sets them).

## Hub sign-in

Use your own OpenID Connect provider (a corporate Keycloak, Authentik, …): `HUB_OIDC_PROVIDERS` and `HUB_OIDC_<ID>_*`
(see [Running a hub](../hub/deploy.md)); turn off the mailed code with `HUB_AUTH_EMAIL=off` if there is no mail. The
hub fetches the provider's keys: it needs the provider's address, your CA and, if you use one, the proxy.

## Model provider

An agent needs a model. Give the machines a model profile whose `base_url` is your own gateway (https, see
[model profiles](../model-profiles.md)). harnessly does not reach any model host on its own: the agent does, so the
gateway and its certificate must be reachable from the machine.

## Apps and the catalog

- **The team's catalog on the hub is the main way** in a closed network: `harnessly app publish ./app --channel stable`
  puts a package into the team's catalog, `harnessly app install --team <id>` and the environment's `app` items give it
  to machines. Nothing is downloaded from outside.
- **The public catalog needs the internet**; leave it unused.
- **Moving a package across a gateway:** pack it into a file, carry the file, install it on the other side.
  `harnessly app validate ./folder --strict` says before that what is wrong; `harnessly app pack ./folder` writes
  `<id>-<version>.hlapp`; `harnessly app install ./file.hlapp` installs it. Sign the package so the other side knows whose
  it is (below).
- **An app's backend** is your service (`.mcp.json` with `type: http`): list its host in `internalHosts`; your CA gives
  the trust, and the app's own client certificate makes the call mutual TLS (below).

### A client certificate for one app

A bank's backend often asks for a client certificate. Give it to the app that talks to that backend, not to the machine:

```json
{
  "permissions": ["secrets:bank-mtls"],
  "secrets": [{ "name": "bank-mtls", "description": "Client certificate and key for crm.example.corp, PEM" }],
  "network": { "clientCert": { "secret": "bank-mtls", "hosts": ["crm.example.corp"] } }
}
```

- The certificate and its key are one PEM in the app's secret `bank-mtls`: the person (or the administrator) pastes it at
  install, it lives in the machine's safe and never in the manifest or the package.
- It goes only to the hosts of this app's own MCP servers (`.mcp.json`); `hosts` narrows it to some of them. It is not sent
  to any other host, and no other app gets it. The machine-wide certificate (`RUNTIME_APPS_CLIENT_CERT_FILE`) stays as it
  was for the apps that name none.
- No value yet, or not a certificate with its key: the app's calls wait with a word about it, they do not go out without.

### Signing packages (`.hlapp`)

A `.hlapp` is the package as one file; `FILE.hlapp.sig` beside it is the publisher's signature (ed25519 over the file's
SHA-256).

1. The publisher makes a key once: `harnessly app key --out key.txt` prints the **public key** and writes the private one
   into `key.txt` (0600). Put it into the safe (`harnessly secret ask hlapp-signing-key`, paste the file) and delete the file.
2. The publisher signs: `harnessly secret run --env HLAPP_SIGNING_KEY=hlapp-signing-key -- harnessly app pack ./app --sign
   --publisher "Example Bank"`. The key is in the process only while it runs.
3. The administrator adds the publisher's public key in **Closed network → App signing** (or `publishers` in
   `network.json`: `{"name": "Example Bank", "key": "<base64>"}`) and switches **Require a signature** on
   (`requireSigned`, or `RUNTIME_APPS_REQUIRE_SIGNED=1` on one machine).
4. `harnessly app install ./app.hlapp` checks the signature first. A refusal says why, in words:
   - unsigned: «пакет не подписан, а в этом контуре устанавливаются только подписанные приложения»;
   - another publisher: «подпись не от доверенного издателя: пакет подписан «X» (ключ …), а в контуре доверяют другим
     издателям — попросите администратора хаба добавить этот ключ»;
   - changed after signing: «подпись не сходится с пакетом: пакет изменили после подписи» — always refused, signature
     required or not.

A signed `.hlapp` can be published straight from the file: `harnessly app publish ./app.hlapp --channel stable --team`
checks the signature and keeps it with the version; the team's catalog and the public catalog carry it on, and the machines
that receive the app check it again. Only `stable` keeps it: a pre-release channel rewrites the version in the manifest,
which the signature does not cover, so such a version is published unsigned. Where signatures are not required, an
unsigned package installs as before.

Where signatures are required, **no** install path skips the check: a folder (`harnessly app install ./folder`), a
channel (`--channel`, following a channel), the team's catalog, the public catalog and the environment's apps are all
refused unless the package is a signed `.hlapp` of a trusted publisher. A folder installed for development (`harnessly app dev`) is the author's own work and is not checked. Where signatures are
required the command says so in words and writes `install.dev-unsigned` in the app's journal: the exception is conscious
and visible.
