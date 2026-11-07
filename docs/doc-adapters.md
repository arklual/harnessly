# Documents and boards provider

The page's document reader (the list of documents, versions, diff, comments, access, boards) talks to a provider. The
default one is the built-in artifacts and boards service named by `RUNTIME_ARTIFACTS_URL` and `RUNTIME_BOARDS_URL`; its
behaviour does not change. A corporate document service plugs in as an adapter of the extension frame, with no fork.

## Contract

Go: `internal/docprovider.Provider`. Read a document, publish a new version, access (public link and grants), comments
(list, create, reply, close), an embed address for an iframe or widget, and the list of boards.

## Declaring an adapter

`RUNTIME_DOC_ADAPTERS_FILE` points at a JSON array, the same form as the connection and memory adapters:

```json
[
  {
    "name": "example-docs",
    "kind": "docs",
    "url": "http://127.0.0.1:8080",
    "tokenFile": "/etc/harnessly/example-docs.token",
    "protocol": "harnessly.docs/v1",
    "maxAnswerBytes": 4194304
  }
]
```

Unset means off. The file is read once at start; a bad entry is reported on stderr and skipped. The first adapter
replaces the built-in provider as a whole (documents are not layered, unlike memory).

`maxAnswerBytes` is the biggest answer taken from the adapter, 1 MiB when unset, at most 64 MiB. A bigger answer is
never cut and parsed: the call fails as `too_large`, and the page's routes answer 413 `{"code":"too_large"}`.

## Protocol `harnessly.docs/v1`

The adapter answers `GET /v1/describe` with its `capabilities`; `read` is required. A call of a capability the adapter
did not list is not sent: publishing then answers 501, comments are an empty list, access can only be read.

| capability      | calls (all `POST` with a JSON body)                                                                |
| --------------- | -------------------------------------------------------------------------------------------------- |
| `read`          | `/v1/list`, `/v1/get`, `/v1/read`, `/v1/versions`, `/v1/diff`, `/v1/embed`                         |
| `publish`       | `/v1/publish` (title, format, content, slug?, note?) -> slug, version, url                         |
| `access`        | `/v1/access`, `/v1/access/set` (public?, grants?)                                                  |
| `comments`      | `/v1/threads`, `/v1/threads/create`, `/v1/threads/reply`, `/v1/threads/status`, `/v1/threads/read` |
| `boards`        | `/v1/boards`                                                                                       |
| `boards-search` | `/v1/boards/search` (query, limit) -> boards                                                       |

Answers: 404 not found, 501 not supported, 401/403 refusal, 409 conflict, 413 or `{"error":"too_large"}` an answer
too big to give. Shapes match the page's own document types (`DocSummary`, `DocVersion`, `DocDiff`, `DocThread`,
`Board`). An adapter that keeps no history answers 501 on `/v1/versions`; the reader then shows no versions, not an
error. A read-only adapter (no `publish`, no `access`) answers 501 `{"code":"unsupported"}` on publish and on access
changes.

The list of documents carries `capabilities` (publish, access, comments, boards, boardsSearch, providerEmbed,
maxAnswerBytes), so the page offers only what the adapter does.

### External IDs

An adapter keeps its own document and board IDs: case-sensitive, base64url, anything up to 72 bytes. The runtime's
routes take lowercase slugs, so it shows each ID as an alias and turns the alias back on the way to the adapter, with
no table to keep. An ID that is already lowercase letters, digits and dashes (not starting with `x-`) is its own
alias; any other one is `x-` and its bytes in lowercase base32. The answers carry both: `slug` (the alias) and
`externalId` (the adapter's ID). An ID longer than 72 bytes is left out of lists, and a publish that names one fails
with 502.

The viewer address is the one the adapter gives (`url`), whatever its path (`/app/board/<id>` is fine): nothing is
made up from the alias. Documents and boards of an adapter are framed through the adapter (`/v1/embed`, the summary's
`providerEmbed`), not through the built-in `/b/` sign-in.

### Boards that only search

An adapter that cannot list every board declares `boards-search` without `boards`. `GET /api/integrations/boards`
then answers `{"boards":[],"searchRequired":true}`, and the page asks `GET /api/integrations/boards?q=<text>` as the
person types. An adapter with both lists and searches; one with only `boards` is searched by filtering its list.

## Documents of a conversation

A document a conversation publishes is bound to that conversation, so its list survives a reload and a runtime
restart. The built-in catalog keeps that link itself; for an adapter the runtime keeps it in its data directory
(`doc-bindings.json`), one record per document of a conversation: the runtime's own id, the alias, the external ID,
the adapter's viewer address and the provider's name. Another conversation, or another adapter, does not see it.

```
POST /api/integrations/artifacts/documents
{"session": "<conversation>", "title": "Plan", "format": "md", "content": "# Plan", "slug": "<alias>?", "note": "?", "requestId": "<8-64 of A-Za-z0-9_->"}
```

| answer                          | meaning                                                                     |
| ------------------------------- | --------------------------------------------------------------------------- |
| 201 `{document}`                | published and bound                                                         |
| 200 `{document}`                | a repeat of `requestId`: the binding made before, nothing published again   |
| 501 `{"code":"unsupported"}`    | the adapter does not publish                                                |
| 502 `{"code":"publish_failed"}` | nothing was published                                                       |
| 500 `{"code":"bind_failed"}`    | published (`published` names it) but not bound; repeat the same `requestId` |
| 413 `{"code":"too_large"}`      | the document, or the adapter's answer, is too large                         |

`requestId` reaches the adapter as the `Idempotency-Key` header, so a repeat after a runtime restart is one operation
there too. `GET /api/integrations/artifacts/documents?session=<conversation>` lists the bound documents, newest first,
with titles and addresses refreshed from the adapter; a document the adapter no longer has is left out.

A file the agent shares into the conversation (the doc-share route) goes the same way when an adapter is declared: the
runtime publishes it through the adapter and binds it, keyed by the file, the document and its content.

## Example

[examples/docs-adapter-ts](../examples/docs-adapter-ts/README.md): a dependency-free TypeScript adapter on example.com.
