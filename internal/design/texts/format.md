## The board format in full

A board is one HTML file: the page's `<head>` with `support.js`, a template in `<x-dc>` and, usually, a logic script. The
runtime turns the template into React. Everything below is what the runtime really does; where it is stricter than it
looks, the section says so.

```html
<!doctype html>
<html><head>
<meta charset="utf-8">
<script src="./support.js"></script>
</head><body>
<x-dc>
  <helmet>
    <style>.row:hover { background: var(--t-surface-raised); }</style>
  </helmet>
  <main style="padding: 24px; display: flex; flex-direction: column; gap: 12px">
    <h1 style="font-size: 24px; margin: 0">{{ title }}</h1>
    <sc-for list="{{ rows }}" as="row">
      <div class="row" onClick="{{ row.pick }}">{{ $index }}. {{ row.name }}</div>
    </sc-for>
    <sc-if value="{{ empty }}"><p>Nothing yet</p></sc-if>
  </main>
</x-dc>
<script type="text/x-dc" data-dc-script data-props='{"title":{"editor":"text","default":"Guests"}}'>
class Component extends DCLogic {
  state = { picked: null };
  renderVals() {
    const names = ['Anna', 'Boris'];
    return {
      rows: names.map((name) => ({ name, pick: () => this.setState({ picked: name }) })),
      empty: names.length === 0,
    };
  }
}
</script>
</body></html>
```

### Substitutions

`{{ … }}` is not JavaScript. It takes a path and nothing more: `a.b`, `a[1]`, `a.0`, `a["b"]`, a literal (`{{ 12 }}`,
`{{ "x" }}`, `{{ true }}`, `{{ null }}`), each with an optional `!` in front (`{{ !open }}`).

- Anything else does not resolve and renders as empty: `{{ n + 1 }}`, `{{ a ? b : c }}`, `{{ this.x }}`, `{{ state.q }}`.
  In text the console then says `{{ x }} never resolved`. Compute it in `renderVals()` and give it a name.
- Names are looked up in this order: the variables of the enclosing `sc-for` (and `$index`), then what `renderVals()`
  returned, then the props.
- In text: `null`, `undefined`, `true` and `false` show nothing; an array or a React element is put in as it is; any other
  value becomes `<span class="sc-interp">`. The span is an element: keep that in mind for `:first-child` and flex gaps.
- A whole attribute (`width="{{ w }}"`, `onClick="{{ pick }}"`) keeps the value's type: a number, a function, an object. A
  mixed one (`href="/e/{{ id }}"`) is always a string, with `null` turned into nothing.

### Attributes

- Write them as in HTML: `class` and `style` are fine. A `style` may hold substitutions, whole declarations included:
  `style="display: flex; {{ extra }}"` with `extra: 'display: none'` gives `display: none`; a property written twice keeps
  its first place and last value. `style="{{ obj }}"` takes a React style object.
- An attribute with no value on an HTML element (`<input disabled>`, `<video loop>`) means true.
- Events are written in React's spelling and take a function: `onClick="{{ pick }}"`. Anything but a function is dropped.
  `on-click` works on `x-import` and `dc-import`, not on plain elements.
- `ref="{{ boxRef }}"` takes a function or a `React.createRef()`.
- `style-hover`, `style-focus`, `style-active` and the like hold declarations for that state:
  `<a style="color: gray" style-hover="color: white">`. Their text is taken literally: a `{{ }}` inside does not work. For
  a dynamic state, use a class and a rule in `<helmet><style>`.
- `hint-size="320,48"` and `hint-placeholder-…` only describe a placeholder while the board is being written; they never
  reach the page. Keep them when you see them and add them on large imports.

### sc-if and sc-for

```html
<sc-if value="{{ open }}" hint-placeholder-val="{{ false }}"> … </sc-if>
<sc-for list="{{ items }}" as="item" hint-placeholder-count="3"> … {{ item.name }} {{ $index }} … </sc-for>
```

`value` is checked as in JavaScript: the string `"false"` is true, so pass real booleans from `renderVals()`, not
strings. There is no else: write a second `sc-if` with `{{ !open }}`. `list` must be an array, otherwise the
console warns and nothing is drawn. Children of `sc-for` are keyed by position: to keep the state of a row while the
list reorders, keep that state in the logic, by id.

### x-import: a component from a script

```html
<x-import component-from-global-scope="{{kit}}.Button" variant="primary" onClick="{{ save }}" hint-size="120,40">Save</x-import>
<x-import component-from-global-scope="IOSDevice" from="./ios-frame.jsx" dark="{{ true }}"> … </x-import>
```

- `component-from-global-scope` is a dotted path on `window`, and may be a substitution (`"{{ deviceName }}"`). The runtime
  waits for it to appear and shows a placeholder until then.
- `from` is a space-separated list of files to load first; `.jsx` goes through Babel. They load as soon as the board is
  read, even inside an `sc-if` that is false. A file that fails is logged as `FAILED to load`.
- Props are `kebab-case` turned into `camelCase`, except `data-*` and `aria-*`; children are passed as children. A value-less
  prop arrives as an empty string: write `dark="{{ true }}"`.
- The element is wrapped in `<div class="sc-host-x">`. Of its `style`, the wrapper takes only width, height, position, top,
  left, right, bottom, inset, z-index and transform; without any of those it has `display: contents`. Any other style has
  to be a prop the component understands.

### dc-import: another board

```html
<dc-import name="SiteNav" active="events" style="position: relative; z-index: 1" hint-size="100%,64px"></dc-import>
```

`name` is the file `./SiteNav.dc.html` next to this board. It is wrapped in `<div class="sc-host">`, which takes the `style`.
Its props are the attributes (`kebab-case` to `camelCase`); the defaults in its own `data-props` do **not** apply here, so
guard for missing props in its logic (`this.props.active ?? 'home'`). Its `<helmet>` goes into the page once. A missing
file renders as a placeholder, and the console says which file.

### helmet

`<helmet>` holds what goes to `<head>`: `<link>`, `<style>`, `<script>`, `<meta>`. It is copied as written, so `{{ }}` does
not work inside it. Elements go in the order written, but a `<script src>` loads asynchronously: do not count on
it being there in the next script (an `x-import` waits for its component by itself). A `<link>` with an `href`, or a `<script>` with a `src`, that a helmet has already put there is skipped, so boards can
share a font or a kit; `./a.css` and `a.css` count as different. Two special forms:

- `<meta name="design_doc_mode" content="canvas">` makes the board a canvas, with the neutral canvas background behind
  it.
- `<helmet data-dc-atomics>` adds the short utility classes (`.ac`, `.fs12`, `.col`…) that some generated boards use.

### The logic: DCLogic

The script is a class `Component extends DCLogic`, compiled once per edit. `React` and `ReactDOM` are in scope.

| Member | What it does |
| --- | --- |
| `this.props` | the `data-props` defaults overlaid with the current tweaks, or the attributes of a `dc-import` |
| `this.state`, `setState(objectOrFn, done)` | as in React; declare the first state as a class field |
| `forceUpdate()` | redraw (no callback) |
| `renderVals()` | runs on every draw, returns the names for `{{ }}`: values, objects, functions |
| `componentDidMount()` | after the first draw: timers, listeners, measuring |
| `componentDidUpdate(prevProps)` | after each later draw; it has no second argument, so compare state yourself |
| `componentWillUnmount()` | clear what `componentDidMount` started |

Rules that follow from this:

- `renderVals()` runs often: no `setState` in it, and nothing heavy. Build the handlers there as arrow functions.
- An exception in `renderVals()` does not break the page: the board draws a banner `<name>.renderVals(): <message>`, and
  there is no console error, so look at the screenshot.
- A logic edit restarts the board: the state is reset and `componentDidMount` runs again. A template edit keeps the state.
- A board with no script is valid; `{{ }}` then has only its props.

### data-props: tweaks and preview size

`data-props` on the logic script is JSON, one entry per prop. The person sees the entries in the panel and changes them live.

```json
{
  "variant": { "editor": "enum", "options": ["Desktop", "Mobile"], "default": "Desktop", "section": "Screen" },
  "loading": { "editor": "boolean", "default": false, "section": "States" },
  "gap": { "editor": "range", "default": 12, "min": 0, "max": 48, "step": 4, "unit": "px" },
  "accent": { "editor": "color", "options": ["#3b82f6", "#2fbf6c"], "default": "#3b82f6" },
  "title": { "editor": "text", "default": "" },
  "$preview": { "width": 438, "height": 900 }
}
```

`$preview` is not a prop: it fixes the board's frame. Without it the board fills the window. Inside an HTML attribute the
quotes become `&quot;`, or wrap the attribute in single quotes.

### Drawing a board well

- Say what the board is about first: the real scenario and its states (loading, empty, error) as `data-props` switches, not
  extra copies of the board. The content is plausible and specific to this product, not lorem ipsum.
- Layout goes in `style`, so the properties panel can edit it. `<helmet><style>` is for what `style` cannot say: `:hover`
  chains, media and container queries, keyframes, and classes repeated many times.
- Colours, spacing and fonts come from the design system's tokens (the README in `_ds/` names them). A hard-coded value means the board will not follow the theme. Theme switches go on `<html>` from
  `componentDidMount` and `componentDidUpdate`, as the neighbouring boards do.
- Check two sizes if the screen has two: one board with a `variant` tweak or `@container` rules, not two near-copies.
- Long lists come from `renderVals()` with `sc-for`, not from pasted markup. A board that takes more than a few seconds to draw
  is too heavy: reduce the list.
- Logic stays small. Business rules of the real product do not belong here; what matters is what the person sees.

### Working with the design system's components

- Before drawing anything, look for it in the kit: `{{kit}}` on `window`, documented in `project/_ds/*/README.md` (a newer
  project: `project/ds/*/README.md`). A real component looks right in every theme and shows what the product will really get.
- Choose from the README's index, then read the card of each component you consider, `components/<Comp>/README.md` next
  to the README: its first sentence says what it is for, «When not to use» names the one to take instead, its example is
  the markup to copy. For a kit installed on the machine, `harnessly design kit search <kit> <words>` finds components,
  templates and icons by purpose, and `harnessly design kit show <kit> <node>` prints one with its props.
- Connect the kit in `<helmet>`: its stylesheet, then its script, as in a neighbouring board. A kit that needs React expects
  the runtime's copy, so do not add your own.
- Pass the component's documented props and nothing else. Copy prop names from the README: a wrong name fails silently. Booleans
  and numbers are `{{ true }}` and `{{ 12 }}`, because `size="12"` is a string.
- If the kit has no such component, compose it from smaller ones and tokens, and say so in your answer: it may need to
  go into the design system. Do not restyle a kit component's internals with selectors.
- A component that renders a placeholder forever has a wrong path: check it in the console with `window.{{kit}}`.
