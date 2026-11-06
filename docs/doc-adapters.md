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
    "protocol": "harnessly.docs/v1"
  }
]
```

Unset means off. The file is read once at start; a bad entry is reported on stderr and skipped. The first adapter
replaces the built-in provider as a whole (documents are not layered, unlike memory).

## Protocol `harnessly.docs/v1`

The adapter answers `GET /v1/describe` with its `capabilities`; `read` is required. A call of a capability the adapter
did not list is not sent: publishing then answers 501, comments are an empty list, access can only be read.

| capability | calls (all `POST` with a JSON body)                                                                |
| ---------- | -------------------------------------------------------------------------------------------------- |
| `read`     | `/v1/list`, `/v1/get`, `/v1/read`, `/v1/versions`, `/v1/diff`, `/v1/embed`                         |
| `publish`  | `/v1/publish` (title, format, content, slug?, note?) -> slug, version, url                         |
| `access`   | `/v1/access`, `/v1/access/set` (public?, grants?)                                                  |
| `comments` | `/v1/threads`, `/v1/threads/create`, `/v1/threads/reply`, `/v1/threads/status`, `/v1/threads/read` |
| `boards`   | `/v1/boards`                                                                                       |

Answers: 404 not found, 501 not supported, 401/403 refusal, 409 conflict. Shapes match the page's own document types
(`DocSummary`, `DocVersion`, `DocDiff`, `DocThread`, `Board`).

## Example

[examples/docs-adapter-ts](../examples/docs-adapter-ts/README.md): a dependency-free TypeScript adapter on example.com.
