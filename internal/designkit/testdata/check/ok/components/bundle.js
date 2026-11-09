(function () {
  var h = window.React.createElement;
  function Button(p) { return h('button', { className: 'acme-btn acme-btn--' + (p.variant || 'primary'), disabled: p.disabled }, p.children); }
  function Link(p) { return h('a', { className: 'acme-link', href: p.href }, p.children); }
  window.Acme = { Button: Button, Link: Link };
})();
