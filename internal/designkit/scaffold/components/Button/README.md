# Button

The main way to act on a page: submits a form or starts a flow; one primary button per area.

## When to use

- An action on this page: save, create, send. `variant="secondary"` for the second action of an area.

## When not to use

- Going to another page: a link. Opening a menu of actions: a menu button.

## Props

| Prop | Type | Default | What |
| --- | --- | --- | --- |
| `variant` | `primary` · `secondary` · `ghost` | `primary` | visual weight |
| `disabled` | boolean | `false` | greys it out and ignores clicks |
| `children` | node | — | the label, a verb |

## Example

```html
<x-import component-from-global-scope="__NS__.Button" variant="primary">Save</x-import>
```
