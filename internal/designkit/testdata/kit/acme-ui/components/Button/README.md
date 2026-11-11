# Button

The main way to act on a page: submits a form or starts a flow.

## When to use
- One primary action per area.
- A second action takes `variant="secondary"`.

## When not to use
- To go to another page: use a link.

## Props
| Prop | Type | Default | What |
| `variant` | `primary` · `secondary` | `primary` | visual weight |

## Example
<x-import component-from-global-scope="Acme.Button" variant="primary">Save</x-import>

## In code
```tsx
import { Button } from '@acme/ui';
```

## Accessibility
- A label is required; an icon alone needs `aria-label`.

## States
- disabled greys it out and ignores clicks.

## Content
- Verb first: «Save», not «Saving».

## Status
Stable since 1.0.
