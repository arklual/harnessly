# Guest host of apps

A guest link to an app (`/apps/guest/<token>/…`, docs/hub/apps.md) is served by the hub from its store, with no sign-in:
the token is the whole authority and the link's rights are what the machine wrote into it. By default it runs on the
hub's own origin.

`HUB_APPS_GUEST_URL=https://guest.example.com` (off when empty) gives the guests an origin of their own, so an app's view
never runs next to the hub's session:

- on that host the hub answers `/apps/guest/…` and nothing else (everything else is 404, no API, no page, no sign-in);
- on the hub's own origin `/apps/guest/…` is then 404;
- the host must differ from the hub's origin (`HUB_PUBLIC_URL`) and from `HUB_DESIGN_SHARE_URL`;
- the view sets its own headers (the app's CSP), the hub adds none of its own there.

The hub's reverse proxy must send the host to the hub.
