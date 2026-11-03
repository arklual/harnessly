// The standup window. Today's entries are standup/<date>.json in the chat's folder, the same file the agent's
// MCP server (backend/server.mjs) writes. The person edits and deletes here; the agent has no delete tool.
(function () {
  var FIELDS = [['yesterday', 'Вчера'], ['today', 'Сегодня'], ['blockers', 'Блокеры']];
  var POLL_MS = 4000;
  var list = document.getElementById('list');
  var note = document.getElementById('note');
  var hl = null;
  // null is "not loaded yet", an empty array is "loaded, nobody wrote".
  var entries = null;
  var editing = null;
  var busy = false;

  function pad(n) { return String(n).padStart(2, '0'); }
  var now = new Date();
  var date = now.getFullYear() + '-' + pad(now.getMonth() + 1) + '-' + pad(now.getDate());
  var path = 'standup/' + date + '.json';
  document.getElementById('date').textContent = date;

  function say(text, isError) {
    note.textContent = text;
    note.dataset.kind = isError ? 'error' : '';
  }
  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  }
  function button(label, cls, onClick) {
    var b = el('button', 'su__btn' + (cls ? ' su__btn--' + cls : ''), label);
    b.type = 'button';
    b.addEventListener('click', onClick);
    return b;
  }

  // A day with no file yet is an empty day: the host answers invalid_params for a file that is not there (it answers the same
  // for text it cannot read, and never says which). Any other refusal is shown, not hidden.
  function readDay() {
    return hl.files.read(path).then(
      function (text) {
        var doc = JSON.parse(text);
        return Array.isArray(doc.entries) ? doc.entries : [];
      },
      function (e) {
        if (e && e.code === 'invalid_params') return [];
        throw e;
      }
    );
  }
  function writeDay(next) {
    return hl.files.write(path, JSON.stringify({ date: date, entries: next }, null, 2) + '\n');
  }
  // Every change starts from a fresh read, so what the agent saved a moment ago is not overwritten.
  function change(fn) {
    busy = true;
    return readDay().then(function (fresh) {
      var next = fn(fresh);
      return writeDay(next).then(function () { entries = next; });
    }).finally(function () { busy = false; });
  }

  function viewItem(e) {
    var li = el('li', 'su__item');
    var who = el('h2', 'su__who', e.person);
    who.append(el('span', 'su__by', e.updatedBy === 'person' ? 'правили вы' : 'записал агент'));
    li.append(who);
    var dl = el('dl', 'su__fields');
    FIELDS.forEach(function (f) {
      var row = el('div', 'su__field su__field--' + f[0]);
      row.dataset.has = String(f[0] === 'blockers' && e.blockers !== '' && e.blockers.toLowerCase() !== 'нет');
      row.append(el('dt', '', f[1]), el('dd', '', e[f[0]] || '—'));
      dl.append(row);
    });
    li.append(dl);
    var row = el('div', 'su__row');
    row.append(
      button('Изменить', '', function () { editing = e.id; draw(); }),
      button('Удалить', 'danger', function () { remove(e); })
    );
    li.append(row);
    return li;
  }

  function editItem(e) {
    var li = el('li', 'su__item');
    li.append(el('h2', 'su__who', e.person));
    var inputs = {};
    FIELDS.forEach(function (f) {
      var label = el('label', 'su__label', f[1]);
      var ta = el('textarea', 'su__input');
      ta.value = e[f[0]];
      inputs[f[0]] = ta;
      label.append(ta);
      li.append(label);
    });
    var row = el('div', 'su__row');
    row.append(
      button('Сохранить', 'primary', function () { save(e, inputs); }),
      button('Отмена', '', function () { editing = null; draw(); })
    );
    li.append(row);
    return li;
  }

  function draw() {
    list.textContent = '';
    if (entries === null) return;
    if (entries.length === 0) {
      list.append(el('li', 'su__empty', 'Сегодня никто не написал. Начните встречу командой /standup.'));
      return;
    }
    entries.forEach(function (e) { list.append(e.id === editing ? editItem(e) : viewItem(e)); });
  }

  function save(e, inputs) {
    var texts = {};
    FIELDS.forEach(function (f) { texts[f[0]] = inputs[f[0]].value.trim(); });
    return change(function (fresh) {
      return fresh.map(function (x) {
        return x.id === e.id ? Object.assign({}, x, texts, { updatedAt: new Date().toISOString(), updatedBy: 'person' }) : x;
      });
    }).then(function () {
      editing = null;
      draw();
      say('Сохранено');
      // The file already says updatedBy: person; the message makes the agent look at it now. The text is fixed on
      // purpose: what reaches the agent this way reads as the person's words, and a name from a file could be anything.
      return hl.chat.send('Я поправил запись в стендапе. Прочитай записи заново через list_entries, прежде чем менять их.')
        .catch(function (err) { say('Запись сохранена, но агент не получил сообщение: ' + err.code, true); });
    }).catch(function (err) { say('Не сохранилось: ' + (err.code || err.message), true); });
  }

  function remove(e) {
    return hl.ui.confirm('Удалить запись «' + e.person + '» за ' + date + '? Вернуть её не получится.').then(function (ok) {
      if (ok !== true) return;
      return change(function (fresh) { return fresh.filter(function (x) { return x.id !== e.id; }); }).then(function () {
        draw();
        say('Запись удалена');
      });
    }).catch(function (err) { say('Не удалилось: ' + (err.code || err.message), true); });
  }

  function refresh() {
    // Not while the person types or a write is under way: the list would jump under the hands.
    if (busy || editing !== null || document.hidden) return Promise.resolve();
    return readDay().then(function (next) {
      if (entries === null || JSON.stringify(next) !== JSON.stringify(entries)) {
        entries = next;
        draw();
      }
      if (note.dataset.kind === 'error') say('');
    }).catch(function (err) { say('Не прочиталось: ' + (err.code || err.message), true); });
  }

  window.harnessly.connect().then(function (api) {
    hl = api;
    if (!api.context.chat) {
      say('Окно работает внутри чата с включённым приложением.', true);
      return null;
    }
    return refresh().then(function () { setInterval(refresh, POLL_MS); });
  }).catch(function (err) {
    say(err && err.code === 'host_unavailable' ? 'Откройте приложение в Harnessly' : 'Ошибка: ' + (err && (err.code || err.message)), true);
  });
})();
