# Rollout switches

A rollout switch turns a feature on or off for a **person on a machine** without a release (the wave that moves
the gateway's switches into the hub). It is finer than an organization's flag (`internal/hub/features`): the same feature
can be on for one person on one machine, for a person everywhere, for a machine, or for everyone.

## Table

`rollout_switches` (migration `0042`, additive only):

| column | meaning |
|---|---|
| `feature` | the name of the path or feature |
| `person_email` | a lower-case email, or `*` for anyone |
| `machine_id` | a machine id, or `*` for any |
| `enabled` | on or off |
| `note`, `updated_by`, `updated_at` | who and why (Unix ms) |

Primary key `(feature, person_email, machine_id)`.

## Which row decides

For a feature, a person and a machine, the rows that match are the exact pair and the `*` ones. The most specific wins:
the pair, then the person (`email, *`), then the machine (`*, id`), then `(*, *)`. **No row means off**, so a new feature
ships dark and is turned on pair by pair, then with one `(*, *)` row. A row with `enabled = false` turns it off again for
its scope, whatever the wider rows say.

## In code

`internal/hub/switches`: `Gate.On(ctx, feature, person, machine)`; `Store.ByPrefix(ctx, prefix, person, machine)` is the
sorted names after `prefix` of the features that are on (several variants of one thing, `<prefix><name>`); `Set`,
`Remove` and `List` change and read the rows. Answers are cached for 5 seconds (`switches.CacheTTL`): a change made by
this process is seen at once, one made by another replica within that time. A table that cannot be read means off.
`switches.Static` is a fixed gate for tests of other packages.

The instance's own domains and keys never belong in the rows or the code: they come from the configuration.

## API

Only an administrator of the installation (a switch belongs to the installation, not to an organization):

- `GET /api/admin/switches[?prefix=…]` → `{"switches": [{feature, person, machine, enabled, note, updatedBy, updatedAt}]}`
- `PUT /api/admin/switches` with `{feature, person?, machine?, enabled, note?}` → 204 (empty person or machine is `*`)
- `DELETE /api/admin/switches?feature=…&person=…&machine=…` → 204
