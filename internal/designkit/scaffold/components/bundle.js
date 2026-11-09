// __TITLE__: components written with React.createElement — no build, no imports, React from the page.
(function () {
  var h = window.React.createElement;
  function cls(base, extra) { return extra ? base + ' ' + extra : base; }
  function Root(p) {
    return h('div', { className: '__ID__-root', 'data-theme': p.theme || 'light' }, p.children);
  }
  function Button(p) {
    return h('button', { type: 'button', className: cls('__ID__-btn __ID__-btn--' + (p.variant || 'primary'), p.className), disabled: p.disabled, onClick: p.onClick }, p.children);
  }
  function Card(p) { return h('section', { className: cls('__ID__-card', p.className) }, p.children); }
  function CardHeader(p) { return h('header', { className: '__ID__-card__header' }, h('h3', null, p.title)); }
  function CardBody(p) { return h('div', { className: '__ID__-card__body' }, p.children); }
  window.__NS__ = { Root: Root, Button: Button, Card: Card, CardHeader: CardHeader, CardBody: CardBody };
})();
