// The card the feed draws under a show_standup step: read-only. The tool result carries only the date
// (hl.context.data.date: the feed can read no more of a result than its first 100 characters), so the entries are read
// from the day's file, which also keeps the card current. It shares style.css with the window.
(function () {
  var list = document.getElementById('list');
  var FIELDS = [['yesterday', 'Вчера'], ['today', 'Сегодня'], ['blockers', 'Блокеры']];

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  }

  function draw(data) {
    document.getElementById('date').textContent = data.date;
    list.textContent = '';
    if (data.entries.length === 0) {
      list.append(el('li', 'su__empty', 'Записей за этот день нет'));
      return;
    }
    data.entries.forEach(function (e) {
      var li = el('li', 'su__item');
      li.append(el('h3', 'su__who', e.person));
      var dl = el('dl', 'su__fields');
      FIELDS.forEach(function (f) {
        var row = el('div', 'su__field su__field--' + f[0]);
        row.dataset.has = String(f[0] === 'blockers' && e.blockers !== '' && e.blockers.toLowerCase() !== 'нет');
        row.append(el('dt', '', f[1]), el('dd', '', e[f[0]] || '—'));
        dl.append(row);
      });
      li.append(dl);
      list.append(li);
    });
  }

  // A day with no file yet is an empty day: the host answers invalid_params for a file that is not there (it answers the same
  // for text it cannot read, and never says which). Any other refusal is shown, not hidden.
  function readDay(hl, date) {
    return hl.files.read('standup/' + date + '.json').then(
      function (text) { return { date: date, entries: JSON.parse(text).entries }; },
      function (e) {
        if (e && e.code === 'invalid_params') return { date: date, entries: [] };
        throw e;
      }
    );
  }

  window.harnessly.connect().then(function (hl) {
    var date = hl.context.data && hl.context.data.date;
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) throw new Error('no date');
    return readDay(hl, date);
  }).then(draw).catch(function (e) {
    list.append(el('li', 'su__empty', 'Не удалось показать карточку: ' + (e && (e.code || e.message))));
  });
})();
