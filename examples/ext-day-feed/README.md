# ext-day-feed

A feed of the day for the start page: what a stage view of the `home` slot may name in its `feed` field. With a feed the
host draws the page of the day itself (`web/app/next/features/day`), in place of the view's frame: a digest, the
calendar strip by the real clock, the priorities, and a thing opened into its chat with the agent.

The files here are what such an address answers; the names are neutral (`example-corp`, `example.com`) and the dates
stand still on Thursday, 12 March 2026 (UTC), so a test can stand the clock there.

| Request                           | File                                            |
| --------------------------------- | ----------------------------------------------- |
| `GET {feed}`                      | `feed/day.json`                                 |
| `GET {feed}/items/{id}`           | `feed/items/{id}.json`                          |
| `POST {feed}/items/{id}/{action}` | answers `{}` or `{"chat": "<conversation id>"}` |

The page asks the feed itself, with the person's cookies (`credentials: "include"`), so the feed answers with CORS
headers for the hub's origin. A write carries `X-Requested-With: harnessly`; the actions are `take`, `ask` (`{say}`),
`send` and `feedback` (`{verdict}`). `401` shows «Sign in», which opens `{origin of the feed}/auth/login?popup=1` in a
window and asks for the day again when it closes.

A feed that knows the person by a token reads better through the machine: the view names `"feedSecret": "<secret>"` (a
secret of the app's `secrets`), and the page asks its own runtime at `<platform api>/apps/feed/<app>/<view>[/items/{id}[/{action}]]`
instead (the list of installed apps gives that path as `feedPath`). The runtime calls the feed with
`Authorization: Bearer <the secret>` from the machine's safe, so the page holds no secret, needs no CORS and shows no
sign-in: a missing or refused secret answers `409` (`secret-missing`, `secret-refused`) and the app's card asks for it.

Every field but `calendar` is optional: a feed that says less draws less. `web/e2e/day-feed.spec.ts` answers the
runtime's path with these files, first as a machine without the token does, and walks the page.
