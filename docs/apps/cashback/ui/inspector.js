// The inspector of Cashback in a Sinopia document: it opens when a bound element of a board is clicked (data-hl-field),
// shows that field's value and writes a new one back into the board through the Document API.
(function () {
  var NAMES = {
    title: 'Название категории',
    percent: 'Процент кешбэка',
    limit: 'Лимит в месяц, ₽',
    note: 'Пояснение',
    link: 'Адрес условий',
  };
  var NUMBERS = { percent: true, limit: true };
  var form = document.getElementById('form');
  var input = document.getElementById('value');
  var title = document.getElementById('field');
  var hint = document.getElementById('hint');
  var note = document.getElementById('note');
  var field = null;

  function show(hl, name) {
    field = name;
    title.textContent = NAMES[name] || name;
    hint.textContent = '';
    note.textContent = '';
    form.hidden = false;
    hl.sinopia.fields().then(function (r) {
      input.value = r.fields[name] == null ? '' : String(r.fields[name]);
      input.focus();
    }).catch(function (e) {
      note.textContent = 'Не удалось прочитать поле: ' + e.message;
    });
  }

  window.harnessly.connect().then(function (hl) {
    // Opened by a click: the host has put the clicked element into the selection before this view starts.
    hl.sinopia.selection().then(function (r) {
      var picked = r.nodes.find(function (n) { return n.field; });
      if (picked) show(hl, picked.field);
    });
    // Already open: the host says which element was clicked next.
    hl.on('selection', function (p) {
      if (p && p.field) show(hl, p.field);
    });
    form.addEventListener('submit', function (event) {
      event.preventDefault();
      if (!field) return;
      var patch = {};
      // schema/cashback.json keeps percent and limit as integers: a number goes as a number, anything else as typed,
      // so the runtime's refusal names the real problem.
      var raw = input.value.trim();
      patch[field] = NUMBERS[field] && /^-?\d+$/.test(raw) ? Number(raw) : input.value;
      // A value outside schema/cashback.json is refused with its reason; the board stays as it was.
      hl.sinopia.setFields(patch).then(function () {
        note.textContent = 'Записано на доску.';
      }).catch(function (e) {
        note.textContent = 'Не записано: ' + e.message;
      });
    });
  }).catch(function () {
    hint.textContent = 'Откройте документ в Harnessly.';
  });
})();
