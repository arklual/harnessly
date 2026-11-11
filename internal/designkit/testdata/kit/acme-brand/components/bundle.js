(function () {
  var h = window.React.createElement;
  function Button(p) { return h('button', { className: 'acme-btn acme-btn--' + (p.variant || 'primary'), disabled: p.disabled }, p.children); }
  window.AcmeBrand = { Button: Button };
  // the replacement takes the base's place, so boards written on the base draw it
  window.Acme.Button = Button;
})();
