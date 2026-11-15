# The day feed of the start page

A view of the `home` slot that takes the stage (`"height": "stage"`) may name `feed`: the https address of a feed of the
person's day. The host then draws the page of the day itself (`web/app/next/features/day`), on its own components, in
place of the view's frame: a digest, the calendar strip by the real clock, the priorities, and a thing opened into its
chat with the agent. Without `feed` the view's frame is drawn as before; a host that cannot draw the day draws the frame
too, so the entry stays.

```json
{
  "network": { "connect": ["day.example.com"] },
  "permissions": ["secrets:token"],
  "secrets": [{ "name": "token", "description": "The token the feed knows this machine by" }],
  "views": [
    {
      "id": "home",
      "entry": "https://day.example.com/day?embed=1",
      "targets": ["home"],
      "height": "stage",
      "feed": "https://day.example.com/api/day",
      "feedSecret": "token"
    }
  ]
}
```

## What a feed answers

| Request                           | Answer                                                     |
| --------------------------------- | ---------------------------------------------------------- |
| `GET {feed}`                      | the day: `calendar` (required), digest, priorities, rows   |
| `GET {feed}/items/{id}`           | one thing of the day, opened as its chat                   |
| `POST {feed}/items/{id}/{action}` | `{}` or `{"chat": "<conversation id>"}`                    |

The actions are `take`, `ask` (`{say}`), `send` and `feedback` (`{verdict}`). Every field but `calendar` is optional: a
feed that says less draws less. The shapes are in `web/app/next/features/day/feed.ts`; a whole feed with neutral names is
`examples/ext-day-feed`, and `web/e2e/day-feed.spec.ts` walks the page on it.

## Who signs the feed in

- **The machine (`feedSecret`).** The page asks its own runtime at `<platform api>/apps/feed/<app>/<view>` (the list of
  installed apps gives it as `feedPath`), and the runtime calls the feed with `Authorization: Bearer <secret>` from the
  machine's safe. The page holds no secret, needs no CORS, and shows no sign-in: a feed on another site cannot keep a
  cookie in the host's page anyway. Only the three requests above are forwarded. A secret with no value, or one the feed
  refuses (`401`/`403`), answers `409` (`secret-missing`, `secret-refused`): the app's card asks for the secret again.
- **The person's cookie (no `feedSecret`).** The page asks the feed itself with `credentials: "include"`; the feed answers
  CORS for the host's origin, and a write carries `X-Requested-With: harnessly`. `401` shows «Sign in», which opens
  `{origin of the feed}/auth/login?popup=1` in a window. A hub whose page has a Content-Security-Policy must allow the
  feed's origin in `connect-src`; prefer `feedSecret`.
