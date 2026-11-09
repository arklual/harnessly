# __TITLE__

What this kit is for, in two sentences: the product, the look (palette, type, density, radius), who builds pages
with it. Replace this paragraph.

## Rules

- One primary Button per area; the rest are secondary or ghost.
- Spacing on the 8px grid (`space-2`, `space-4`); cards on `surface-1` over a `surface-0` page.
- Light and dark are modes of the same tokens: `Root theme="dark"` switches them.

## Consuming

Namespace `__NS__`. A board loads, in this order: `tokens.css`, `components/bundle.css`, `components/bundle.js`, and
wraps its content in `__NS__.Root`.

```html
<x-import component-from-global-scope="__NS__.Root" theme="light">
  <x-import component-from-global-scope="__NS__.Button" variant="primary">Save</x-import>
</x-import>
```

### Index

Actions
- Button — The main way to act on a page: submits a form or starts a flow; one primary button per area.

Layout
- Card — A raised surface that groups one record or one topic: a header with its title, then the content.
  - CardHeader — The top row of a Card: its title, set apart from the content.
  - CardBody — The content of a Card under its header, with the card's padding.
- Root — Wraps every board on the kit: sets the font, the colours and light or dark mode.
