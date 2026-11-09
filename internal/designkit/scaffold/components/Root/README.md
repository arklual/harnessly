# Root

Wraps every board on the kit: sets the font, the colours and light or dark mode.

## When to use

- Once, around all of a board's content.

## When not to use

- Inside the content: one Root per board is enough.

## Props

| Prop | Type | Default | What |
| --- | --- | --- | --- |
| `theme` | `light` · `dark` | `light` | mode of the tokens |
| `children` | node | — | the board's content |

## Example

```html
<x-import component-from-global-scope="__NS__.Root" theme="dark">…</x-import>
```
