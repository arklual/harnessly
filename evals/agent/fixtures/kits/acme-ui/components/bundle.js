// Acme UI: components written with React.createElement — no build, no imports, React from the page.
(function () {
  var h = window.React.createElement;
  function cls(base, extra) { return extra ? base + ' ' + extra : base; }
  function Root(p) {
    return h('div', { className: 'acme-ui-root', 'data-theme': p.theme || 'light' }, p.children);
  }
  function Button(p) {
    return h('button', { type: 'button', className: cls('acme-ui-btn acme-ui-btn--' + (p.variant || 'primary'), p.className), disabled: p.disabled, onClick: p.onClick }, p.children);
  }
  function Card(p) { return h('section', { className: cls('acme-ui-card', p.className) }, p.children); }
  function CardHeader(p) { return h('header', { className: 'acme-ui-card__header' }, h('h3', null, p.title)); }
  function CardBody(p) { return h('div', { className: 'acme-ui-card__body' }, p.children); }
  window.AcmeUI = { Root: Root, Button: Button, Card: Card, CardHeader: CardHeader, CardBody: CardBody };
})();
