(function () {
  var h = window.React.createElement;
  function Root(p) { return h('div', { 'data-theme': p.theme }, p.children); }
  function Button(p) { return h('button', { className: 'acme-btn acme-btn--' + (p.variant || 'primary'), disabled: p.disabled }, p.children); }
  function Card(p) { return h('section', { className: 'acme-card' }, p.children); }
  function CardHeader(p) { return h('header', null, p.children); }
  function CardBody(p) { return h('div', null, p.children); }
  function Tag(p) { return h('span', { className: 'acme-tag' }, p.children); }
  window.Acme = { Root: Root, Button: Button, PrimaryButton: Button, Card: Card, CardHeader: CardHeader, CardBody: CardBody, Tag: Tag };
})();
