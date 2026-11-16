package kitadd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// An Angular library has no layout and no data-driven components a board can use without a template: a table is
// `<table mat-table>` with column definitions, a field is a form field around an input. An agent drawing on the kit
// would fall back to plain HTML. So the kit adds a small set of components of its own, made of the library's
// components and its tokens:
//
//	Stack, Grid, Text, Glyph   layout, type and icons in numbers and names, not CSS
//	DataTable                  columns + rows
//	TextField, SelectField     label, placeholder, icon, value, onChange in one component
//
// They are React components (the first four) or Angular templates built from the props (the last three).

// ngComposite is one such component.
type ngComposite struct {
	ID          string
	Description string
	Props       []TypeProp
	Sample      string
	Dyn         bool // an Angular template built from the props (the library's own components)
}

var colTypeNote = "[{ key, header, align: \"left\" | \"right\" | \"center\", cell?: \"Tag\", severityBy?: { <value>: \"success\" | \"info\" | \"warn\" | \"danger\" | \"secondary\" } }]: the columns; key names a field of a row; cell \"Tag\" draws the value as a coloured tag (chip), coloured by severityBy"

func compositeSet(p ngPreset) []ngComposite {
	var out []ngComposite
	out = append(out,
		ngComposite{ID: "Stack", Description: "Layout box: puts children in a row or a column with a gap. A card (`card`) is the library's surface with its border and radius. Use it for every block that would be a <div>; sizes are numbers (1 = 4 px).",
			Props: []TypeProp{
				{Name: "direction", Type: "enum", Values: []string{"column", "row"}, Description: "default column"},
				{Name: "gap", Type: "number", Description: "space between children, in steps of 4 px (2 = 8 px)"},
				{Name: "padding", Type: "number", Description: "inner space, in steps of 4 px"},
				{Name: "align", Type: "enum", Values: []string{"start", "center", "end", "stretch"}},
				{Name: "justify", Type: "enum", Values: []string{"start", "center", "end", "between"}},
				{Name: "wrap", Type: "boolean", Description: "children wrap to the next line"},
				{Name: "grow", Type: "boolean", Description: "takes the free space of its parent row or column"},
				{Name: "card", Type: "boolean", Description: "the surface of a card: background, border, radius"},
				{Name: "fill", Type: "boolean", Description: "the page: at least the height of the screen, the page background"},
				{Name: "width", Type: "number", Description: "width in px; leave out to fill the parent"},
				{Name: "maxWidth", Type: "number", Description: "largest width in px, centered"},
				{Name: "children", Type: "node"},
			},
			Sample: `{"card":true,"padding":4,"gap":2,"children":[{"$c":"Text","props":{"variant":"heading"},"children":"Title"},{"$c":"Text","props":{"muted":true},"children":"Text under the title"}]}`},
		ngComposite{ID: "Grid", Description: "Layout grid of equal columns with a gap (cards, metrics).",
			Props: []TypeProp{
				{Name: "columns", Type: "number", Description: "number of columns (default 2); one column under 640 px"},
				{Name: "gap", Type: "number", Description: "space between cells, in steps of 4 px"},
				{Name: "children", Type: "node"},
			},
			Sample: `{"columns":3,"gap":3,"children":[{"$c":"Stack","props":{"card":true,"padding":3},"children":"One"},{"$c":"Stack","props":{"card":true,"padding":3},"children":"Two"},{"$c":"Stack","props":{"card":true,"padding":3},"children":"Three"}]}`},
		ngComposite{ID: "Text", Description: "Text in the library's type: a heading, a body, a caption. Use it in place of <h1>, <p>, <span>.",
			Props: []TypeProp{
				{Name: "variant", Type: "enum", Values: []string{"display", "title", "heading", "body", "label", "caption"}, Description: "default body"},
				{Name: "tone", Type: "enum", Values: []string{"default", "muted", "primary", "danger"}},
				{Name: "muted", Type: "boolean", Description: "same as tone muted"},
				{Name: "align", Type: "enum", Values: []string{"left", "center", "right"}},
				{Name: "children", Type: "node"},
			},
			Sample: `{"variant":"title","children":"Orders"}`},
		ngComposite{ID: "Glyph", Description: "An icon by name (search, plus, filter, download, user, calendar, check, close, chevron-down, settings, edit, trash, mail, bell, home).",
			Props: []TypeProp{
				{Name: "name", Type: "string", Required: true, Description: "icon name from the list in the description"},
				{Name: "size", Type: "number", Description: "size in px, default 20"},
			},
			Sample: `{"name":"search"}`},
	)
	if p.Dyn["DataTable"] != "" {
		out = append(out, ngComposite{ID: "DataTable", Dyn: true,
			Description: "A table from data: `columns` (key, header, align) and `rows` (objects). Use it in place of <table> and the library's Table.",
			Props: []TypeProp{
				{Name: "columns", Type: "array", Required: true, Description: colTypeNote},
				{Name: "rows", Type: "array", Required: true, Description: "[{ <key>: value }]: one object per row, values are text or numbers"},
			},
			Sample: `{"columns":[{"key":"id","header":"Order"},{"key":"customer","header":"Customer"},{"key":"status","header":"Status","cell":"Tag","severityBy":{"Paid":"success","Pending":"warn"}},{"key":"total","header":"Total","align":"right"}],"rows":[{"id":"#1042","customer":"Ada Lovelace","status":"Paid","total":"4 200"},{"id":"#1043","customer":"Grace Hopper","status":"Pending","total":"1 750"}]}`})
	}
	if p.Dyn["TextField"] != "" {
		out = append(out, ngComposite{ID: "TextField", Dyn: true,
			Description: "A text input with its label, placeholder and an icon in one component. Use it in place of <input> and the library's Input and FormField.",
			Props: []TypeProp{
				{Name: "label", Type: "string", Description: "the label of the field"},
				{Name: "placeholder", Type: "string"},
				{Name: "icon", Type: "string", Description: "a Glyph name drawn at the start of the field (search)"},
				{Name: "value", Type: "string"},
				{Name: "type", Type: "string", Description: "text (default), email, password, number"},
				{Name: "width", Type: "number", Description: "width in px; left out, the field takes the width of its container (at least 200px)"},
				{Name: "onChange", Type: "function", Description: "called with the new text"},
			},
			Sample: `{"label":"Search","placeholder":"Order or customer","icon":"search"}`})
	}
	if p.Dyn["SelectField"] != "" {
		out = append(out, ngComposite{ID: "SelectField", Dyn: true,
			Description: "A dropdown with its label in one component: `options` are strings or { value, label }. Use it in place of <select> and the library's Select.",
			Props: []TypeProp{
				{Name: "label", Type: "string"},
				{Name: "placeholder", Type: "string"},
				{Name: "options", Type: "array", Required: true, Description: "[\"A\", \"B\"] or [{ value, label }]"},
				{Name: "value", Type: "string", Description: "the selected value"},
				{Name: "width", Type: "number", Description: "width in px; left out, the field takes the width of its container (at least 200px)"},
				{Name: "onChange", Type: "function", Description: "called with the selected value"},
			},
			Sample: `{"label":"Status","options":["All","Paid","Pending","Refunded"],"value":"All"}`})
	}
	return out
}

// icon paths: 24x24 viewBox, stroke icons.
var hkIcons = map[string]string{
	"search":       `<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`,
	"plus":         `<path d="M12 5v14M5 12h14"/>`,
	"filter":       `<path d="M3 5h18l-7 8v6l-4-2v-4z"/>`,
	"download":     `<path d="M12 4v11m-5-4 5 5 5-5M5 20h14"/>`,
	"user":         `<circle cx="12" cy="8" r="4"/><path d="M4 21c0-4 4-6 8-6s8 2 8 6"/>`,
	"calendar":     `<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/>`,
	"check":        `<path d="m5 12 5 5 9-10"/>`,
	"close":        `<path d="m6 6 12 12M18 6 6 18"/>`,
	"chevron-down": `<path d="m6 9 6 6 6-6"/>`,
	"settings":     `<circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M19 5l-2 2M7 17l-2 2"/>`,
	"edit":         `<path d="M4 20h4L19 9l-4-4L4 16z"/>`,
	"trash":        `<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>`,
	"mail":         `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 7 9 6 9-6"/>`,
	"bell":         `<path d="M6 17V11a6 6 0 0 1 12 0v6l2 2H4zM10 21h4"/>`,
	"home":         `<path d="m3 11 9-8 9 8M5 10v10h5v-6h4v6h5V10"/>`,
}

func hkIconCSS() string {
	var names []string
	for n := range hkIcons {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(".hk-ico{display:inline-block;flex:none;width:1.25em;height:1.25em;background:currentColor;vertical-align:middle;-webkit-mask:var(--hk-i) center/contain no-repeat;mask:var(--hk-i) center/contain no-repeat}")
	for _, n := range names {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="black" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + hkIcons[n] + `</svg>`
		uri := strings.NewReplacer("<", "%3C", ">", "%3E", `"`, "'", "#", "%23").Replace(svg)
		fmt.Fprintf(&b, ".hk-ico-%s{--hk-i:url(\"data:image/svg+xml,%s\")}", n, uri)
	}
	return b.String()
}

// composeJS is the part of the runtime that is the same for every library: the React components and the icon sheet,
// with the library's variables in HKV and the Angular templates of its data components in DYN.
func composeJS(p ngPreset) string {
	vars, _ := json.Marshal(p.HK)
	css, _ := json.Marshal(hkIconCSS())
	var dyn strings.Builder
	var ids []string
	for id := range p.Dyn {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(&dyn, "DYN.%s = %s;\n", id, p.Dyn[id])
	}
	iconMode := "mask"
	if p.IconClass != "" {
		iconMode = p.IconClass
	}
	s := strings.NewReplacer("/*HKV*/", string(vars), "/*ICONCSS*/", string(css), "/*DYNFNS*/", dyn.String(), "/*ICONMODE*/", fmt.Sprintf("%q", iconMode)).Replace(composeRuntime)
	return s
}

const composeRuntime = `
var HKV = /*HKV*/;
var ICONMODE = /*ICONMODE*/;
(function () {
  var st = document.createElement('style');
  st.setAttribute('data-hk-kit', NS);
  st.textContent = /*ICONCSS*/;
  (document.head || document.documentElement).appendChild(st);
})();
/*DYNFNS*/
function fnIdx(st, f) { st.fns.push(typeof f === 'function' ? f : function () {}); return st.fns.length - 1; }
function valIdx(st, v) { st.vals.push(v); return st.vals.length - 1; }
function arr(v) { if (typeof v === 'string') { try { v = JSON.parse(v); } catch (e) { v = v.split(',').map(function (x) { return x.trim(); }).filter(Boolean); } } return Array.isArray(v) ? v : []; }
function optsOf(p) { return arr(p.options).map(function (o) { return typeof o === 'object' && o ? { value: o.value, label: o.label != null ? o.label : o.value } : { value: o, label: o }; }); }
var SEV = { success: ['#e6f4ea', '#1e6b34'], info: ['#e3f0fc', '#0b57a4'], warn: ['#fff4e0', '#8a5300'], danger: ['#fdeaea', '#a62a2a'], secondary: ['#eceff3', '#3f4a5a'] };
var SEVNAME = { warning: 'warn', error: 'danger', neutral: 'secondary', ok: 'success' };
function sevOf(c) {
  var by = c.severityBy;
  if (typeof by === 'string') { try { by = JSON.parse(by); } catch (e) { by = null; } }
  var out = {};
  if (by && typeof by === 'object') Object.keys(by).forEach(function (k) {
    var n = String(by[k]); n = SEVNAME[n] || n; if (!SEV[n]) n = 'secondary';
    out[k] = ICONMODE === 'pi' ? n : { bg: SEV[n][0], fg: SEV[n][1] };
  });
  return out;
}
function colsOf(p) { return arr(p.columns).map(function (c, i) { c = typeof c === 'object' && c ? c : { key: c }; return { i: i, key: String(c.key).replace(/[^\w$ -]/g, ''), header: c.header != null ? c.header : c.key, right: c.align === 'right', center: c.align === 'center', tag: c.cell === 'Tag' || c.cell === 'Chip' || c.cell === 'tag', sev: sevOf(c) }; }); }
function fieldStyle(p) { var w = num(p.width); return ' style="display:flex;flex-direction:column;gap:6px;box-sizing:border-box;' + (w != null ? 'width:' + w + 'px;flex:none' : 'width:100%;min-width:200px') + '"'; }
function alignStyle(c) { return c.right ? ' style="text-align:right"' : c.center ? ' style="text-align:center"' : ''; }
function wStyle(p) { var w = num(p.width); return ' style="' + (w != null ? 'width:' + w + 'px' : 'width:100%;min-width:200px') + '"'; }
function hv(n, fb) { return 'var(' + HKV[n] + (fb ? ', ' + fb : '') + ')'; }
function num(v) { if (typeof v === 'string' && v.trim() !== '' && isFinite(Number(v))) return Number(v); return typeof v === 'number' ? v : undefined; }
function on(v) { return v === true || v === '' || v === 'true'; }
function u(n) { n = num(n); return n === undefined ? undefined : (n * 4) + 'px'; }
function h(tag, props) { return React.createElement.apply(null, [tag, props].concat([].slice.call(arguments, 2))); }
ns.Stack = function Stack(p) {
  var s = { display: 'flex', flexDirection: p.direction === 'row' ? 'row' : 'column', boxSizing: 'border-box', color: hv('text') };
  if (p.gap != null) s.gap = u(p.gap);
  if (p.padding != null) s.padding = u(p.padding);
  if (p.align) s.alignItems = { start: 'flex-start', end: 'flex-end', center: 'center', stretch: 'stretch' }[p.align];
  if (p.justify) s.justifyContent = { start: 'flex-start', end: 'flex-end', center: 'center', between: 'space-between' }[p.justify];
  if (on(p.wrap)) s.flexWrap = 'wrap';
  if (on(p.grow)) { s.flex = '1 1 0'; s.minWidth = 0; }
  if (on(p.card)) { s.background = hv('card'); s.border = '1px solid ' + hv('border'); s.borderRadius = hv('radius'); }
  if (on(p.fill)) { s.minHeight = '100vh'; s.background = hv('page'); }
  if (num(p.width) != null) { s.width = num(p.width) + 'px'; s.flex = 'none'; }
  if (num(p.maxWidth) != null) { s.maxWidth = num(p.maxWidth) + 'px'; s.width = '100%'; s.marginLeft = 'auto'; s.marginRight = 'auto'; }
  return h('div', { style: s, 'data-hk': 'Stack' }, p.children);
};
ns.Stack.displayName = 'Stack';
ns.Grid = function Grid(p) {
  var n = num(p.columns) || 2;
  var s = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(max(0px, calc((100% - ' + (n - 1) + ' * ' + (u(p.gap) || '0px') + ') / ' + n + ')), 1fr))', boxSizing: 'border-box' };
  if (p.gap != null) s.gap = u(p.gap);
  return h('div', { style: s, 'data-hk': 'Grid' }, p.children);
};
ns.Grid.displayName = 'Grid';
var TEXT = {
  display: ['h1', '2rem', 700, 1.2], title: ['h2', '1.5rem', 600, 1.25], heading: ['h3', '1.125rem', 600, 1.3],
  body: ['p', '0.875rem', 400, 1.5], label: ['span', '0.8125rem', 500, 1.4], caption: ['span', '0.75rem', 400, 1.4]
};
ns.Text = function Text(p) {
  var t = TEXT[p.variant] || TEXT.body, tone = p.tone || (p.muted ? 'muted' : 'default');
  var col = { default: 'text', muted: 'muted', primary: 'primary', danger: 'danger' }[tone] || 'text';
  var s = { margin: 0, fontSize: t[1], fontWeight: t[2], lineHeight: t[3], color: hv(col) };
  if (p.align) s.textAlign = p.align;
  return h(t[0], { style: s, 'data-hk': 'Text' }, p.children);
};
ns.Text.displayName = 'Text';
ns.Glyph = function Glyph(p) {
  var size = (num(p.size) || 20) + 'px', name = String(p.name || '').replace(/[^a-z0-9-]/g, '');
  if (ICONMODE === 'pi') return h('i', { className: 'pi pi-' + name, style: { fontSize: size }, 'data-hk': 'Glyph' });
  return h('span', { className: 'hk-ico hk-ico-' + name, style: { width: size, height: size }, 'data-hk': 'Glyph' });
};
ns.Glyph.displayName = 'Glyph';
`
