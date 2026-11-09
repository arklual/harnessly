# Card

A raised surface that groups one record or one topic: a header with its title, then the content.

## When to use

- One event, one order, one setting group in a grid or a column of cards.

## When not to use

- A long list of records: a table. A message about the page: an alert.

## Props

| Prop | Type | Default | What |
| --- | --- | --- | --- |
| `children` | node | — | `CardHeader`, then `CardBody` |

## Example

```html
<x-import component-from-global-scope="__NS__.Card">
  <x-import component-from-global-scope="__NS__.CardHeader" title="Spring concert"></x-import>
  <x-import component-from-global-scope="__NS__.CardBody">May 14, 8 pm.</x-import>
</x-import>
```
