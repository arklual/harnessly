# Sign-in: who calls your backend

An app's remote MCP server (`.mcp.json`, a `url`) often has to know who calls it: to show the person's own data, to
check their rights, to write an audit line. The host signs the call in for you. The view never holds a token of any
kind, and the agent's MCP client does not either: the host adds the token to the calls it makes for both.

## Pick a way

| Way | `.mcp.json` | Your backend gets | Fits when |
| --- | --- | --- | --- |
| Platform token | `"auth": {"type": "platform"}` | `Authorization: Bearer <JWT>` signed by the hub: the person, their organization and team, the app | Your backend is yours; you want «who is it» without a sign-in of its own. Most cases |
| Corporate SSO token | `"auth": {"type": "oidc"}` | `Authorization: Bearer <token of your identity provider>` for your backend, got by token exchange | The backend already trusts the company's SSO (Keycloak, ADFS…) and must not trust anything else |
| OAuth sign-in | `"auth": {"type": "oauth"}` | The token the person got signing in to your server (MCP authorization: discovery, registration, PKCE) | The server has its own OAuth, like a SaaS one |
| Secret | `"headers": {"X-Api-Key": "${secret:key}"}` (or `"auth": {"type": "secret"}`) | The install's key, the same for every person | A service key, no person behind it |
| Connection | `"connection": "<catalog id>"` | The person's own connection (Settings → Connections) | A well-known server from the catalog |

`auth` sits next to `url` in the server's entry:

```json
{
  "mcpServers": {
    "crm": {
      "type": "http",
      "url": "https://crm.internal.example/mcp",
      "auth": { "type": "platform" }
    }
  }
}
```

A platform token's `aud` is always the server's own origin (`https://host[:port]`): an app cannot ask a token for
another backend and have it sent to its own address. For `oidc`, `audience` names the identity provider's audience of
your backend (the origin when left out), and the hub exchanges only the audiences its admin listed for that origin.
`scope` is passed to the identity provider for `oidc` and `oauth`. A server with `auth` (other than `secret`) carries no `Authorization` header of its own;
`harnessly app check` says so.

## The platform token

A JWT, signed with the hub's Ed25519 key (`alg: EdDSA`, `typ: harnessly-app+jwt`), alive 5 minutes. The host keeps it
for that time and asks a new one a minute before it runs out; a backend that answers 401 gets one fresh token and the
call once more.

| Claim | Means |
| --- | --- |
| `iss` | The hub's address (`https://harnessly.example`). A machine without a hub: `urn:harnessly:machine:<host>` |
| `sub` | The person's stable id at the hub. Key your data by it, not by the email |
| `aud` | Your backend (the `audience`). Refuse any other |
| `exp`, `iat`, `nbf`, `jti` | Times and the token's id |
| `email`, `name` | For display and logs |
| `org`, `space` | The person's organization and team on the hub |
| `app`, `server` | The app and its server the call is for |
| `machine` | The machine that asked |

**Where the keys are.** The hub publishes them at `https://<hub>/.well-known/jwks.json` (cached for 5 minutes; after a
rotation the old key stays there). A machine without a hub signs with its own key and publishes nothing on the network:
it writes the set to `app-token.jwks.json` in the runtime's data folder for the backend's admin to copy.

**Check it on your backend**: the signature by a key of the set, `iss`, your own `aud` and `exp`, always; `typ` where the library lets you.

Go (with `github.com/coreos/go-oidc/v3`):

```go
keys := oidc.NewRemoteKeySet(ctx, "https://harnessly.example/.well-known/jwks.json")
verifier := oidc.NewVerifier("https://harnessly.example", keys, &oidc.Config{
	ClientID:             "https://crm.internal.example", // your audience
	SupportedSigningAlgs: []string{oidc.EdDSA},
})

func whoCalls(r *http.Request) (string, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return "", errors.New("no token")
	}
	tok, err := verifier.Verify(r.Context(), raw) // signature, iss, aud, exp
	if err != nil {
		return "", err
	}
	var c struct {
		Email string `json:"email"`
		App   string `json:"app"`
	}
	_ = tok.Claims(&c)
	return tok.Subject, nil
}
```

Node (with `jose`):

```js
import { createRemoteJWKSet, jwtVerify } from 'jose';

const keys = createRemoteJWKSet(new URL('https://harnessly.example/.well-known/jwks.json'));

export async function whoCalls(req) {
  const raw = (req.headers.authorization ?? '').replace(/^Bearer /, '');
  const { payload } = await jwtVerify(raw, keys, {
    issuer: 'https://harnessly.example',
    audience: 'https://crm.internal.example',
    typ: 'harnessly-app+jwt',
    algorithms: ['EdDSA'],
  });
  return { id: payload.sub, email: payload.email, org: payload.org, app: payload.app };
}
```

Answer `401` with `WWW-Authenticate: Bearer error="invalid_token"` to a token you do not take, and `403` to a person you
do not let in: the host turns them into the codes below.

**Why the hub signs, not the machine.** The hub knows who the person is (the machine's owner, signed in by the
organization's SSO) and their organization and team; one key per deployment means one JWKS for every backend, not one
per machine; and the hub holds the client at the identity provider that token exchange needs. The machine never holds
the key: it asks the hub with a request signed by its own key, and the token comes back sealed to it.

## Corporate SSO: token exchange

With `"auth": {"type": "oidc"}` the hub hands your identity provider a platform token meant for it and gets back the
provider's own access token for your backend (RFC 8693, `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`).
The backend sees the bank's token, as if the person had signed in to it, and checks it with the provider's keys as it
already does. The hub keeps no token of the person's own sign-in.

The hub's admin sets it up:

| Variable | Means |
| --- | --- |
| `HUB_APP_TOKEN_EXCHANGE_URL` | The provider's token endpoint |
| `HUB_APP_TOKEN_EXCHANGE_CLIENT_ID`, `HUB_APP_TOKEN_EXCHANGE_CLIENT_SECRET` | The hub's client there (HTTP Basic) |
| `HUB_APP_TOKEN_EXCHANGE_AUDIENCE` | The `aud` of the platform token the provider is handed; the client id when empty |
| `HUB_APP_TOKEN_EXCHANGE_AUDIENCES` | Required. `audience=https://backend[:port]`, comma-separated: which provider audience may be sent to which backend. Anything else is refused (`auth_denied`) before the provider is asked |
| `HUB_APP_TOKEN_EXCHANGE_SCOPE` | A scope asked by default |
| `HUB_APP_TOKEN_EXCHANGE_SUBJECT_TOKEN_TYPE` | `urn:ietf:params:oauth:token-type:jwt` when empty |

On the provider's side, the hub is a trusted external issuer: its JWKS (`/.well-known/jwks.json`), its `iss`, and the
claim that names the person (`email` or `sub`). In Keycloak that is an identity provider of type «OpenID Connect v1.0»
or «JWT» with token exchange allowed for the hub's client and the backend's audience.

## The platform's keys

| Variable | Means |
| --- | --- |
| `HUB_APP_TOKEN` | `on` (default) or `off`: the hub mints no tokens and publishes no JWKS |
| `HUB_APP_TOKEN_KEY_FILE` | The Ed25519 key, PKCS#8 PEM; made there on first start (`<HUB_DATA_DIR>/app-token.pem`). Every replica reads the same file |
| `HUB_APP_TOKEN_PREVIOUS_KEYS` | Comma-separated public keys (PKIX PEM) of earlier rotations, still published |

To rotate: put the new key in `HUB_APP_TOKEN_KEY_FILE`, the old public key in `HUB_APP_TOKEN_PREVIOUS_KEYS`, restart;
remove the old one after 10 minutes.

## OAuth sign-in to your server

With `"auth": {"type": "oauth"}` the person signs in to your server itself, the way a connection does: the host finds
the server's authorization server (MCP authorization), registers itself as a client, runs the code flow with PKCE in a
top-level popup, and keeps the token in the safe. The sign-in is the app's own: a connection the person made to the same
address is never used for it, and it is used for nothing but this app's server. It shows in Settings → Connections, where the person can
sign out; it is off for the person's other agents, which reach the server only through the app. Until the person signs
in, calls answer `auth_required` with `error.signIn = {app, server}`, and the host offers «Войти».

## When a sign-in fails

| Code | When | The person sees | The agent reads |
| --- | --- | --- | --- |
| `auth_required` | No sign-in: the server answered 401 to a call without a credential; the connection is not made or not signed in; nobody signed in to an `oauth` server | «Войти» on the app's card | `the server wants a sign-in: sign in, then repeat the call` and `error.signIn` |
| `auth_expired` | A sign-in the server takes no more: 401 to a call with a token (after one fresh token), a token that ran out and did not refresh | «Войти снова» | `the server no longer takes the sign-in: sign in again, then repeat the call` |
| `auth_denied` | Signed in, and refused: 403, `insufficient_scope`, the provider refused the exchange | That access is the server admin's to grant | `the server refuses this to the signed-in person: ask its admin for access` |
| `idp_unreachable` | The identity provider (its token endpoint) did not answer | Try again later | `the sign-in service did not answer: try again later` |
| `host_unavailable` | The hub did not answer for the token, or this deployment gives no token of that kind (`HUB_APP_TOKEN=off`, no exchange) | The admin sets it up | The message names `HUB_APP_TOKEN_*` |

`error.signIn` names where a sign-in mends it: `{connection}` for a connection, `{app, server}` for an `oauth` server.
The server's own words and its `WWW-Authenticate` stay in the machine's log, never in the view.

## Rules the host keeps

- No token reaches a view, the agent's context or a log: the host adds it to the request it makes.
- A platform token lives 5 minutes (at most 10) and names in `aud` the server it is sent to; a backend must refuse any
  other. A provider's token goes only to the backend the hub's admin listed for its audience.
- A call that is refused is not repeated more than once with a fresh token.
- The keys and the exchange client are the hub's; a machine holds none of them.
