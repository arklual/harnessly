// The picks of this month: shown here, kept in the chat's storage (the agent reads and writes the same key through
// app_data_get / app_data_set), sent to the chat on request.
(function () {
  var data = window.CASHBACK;
  var now = new Date();
  var month = now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2, '0');
  var key = 'picks:' + month;
  var monthName = now.toLocaleDateString('ru-RU', { month: 'long' });
  var list = document.getElementById('list');
  var send = document.getElementById('send');
  var note = document.getElementById('note');
  var picks = [];
  var hl = null;

  document.getElementById('sub').textContent =
    'Выберите до ' + data.max + ' категорий на ' + monthName + '. Данные демонстрационные.';

  function label(c) {
    return c.name + ' ' + c.percent + '%';
  }

  function draw() {
    list.textContent = '';
    data.categories.forEach(function (c) {
      var on = picks.indexOf(c.id) >= 0;
      var li = document.createElement('li');
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'cb__item';
      b.setAttribute('aria-pressed', String(on));
      b.disabled = !on && picks.length >= data.max;
      var name = document.createElement('span');
      name.className = 'cb__name';
      var title = document.createElement('b');
      title.textContent = c.name;
      var sub = document.createElement('span');
      sub.textContent = c.note + ', до ' + c.limit.toLocaleString('ru-RU') + ' ₽ в месяц';
      name.append(title, sub);
      var pct = document.createElement('span');
      pct.className = 'cb__pct';
      pct.textContent = c.percent + '%';
      var mark = document.createElement('span');
      mark.className = 'cb__mark';
      mark.textContent = '✓';
      mark.setAttribute('aria-hidden', 'true');
      b.append(name, pct, mark);
      b.addEventListener('click', function () {
        toggle(c.id);
      });
      li.append(b);
      list.append(li);
    });
    send.disabled = picks.length === 0 || hl === null;
  }

  function save() {
    return hl.storage.chat.set(key, { month: month, picks: picks });
  }

  function toggle(id) {
    if (hl === null) return;
    var before = picks;
    picks = picks.indexOf(id) >= 0 ? picks.filter(function (p) { return p !== id; }) : picks.concat(id);
    draw();
    // The choice is kept only when the host has taken it: the one on screen goes back otherwise.
    save().catch(function (e) {
      picks = before;
      draw();
      note.textContent = 'Не удалось сохранить выбор: ' + e.code;
    });
  }

  function load() {
    return hl.storage.chat.get(key).then(function (v) {
      var next = v && Array.isArray(v.picks) ? v.picks : [];
      if (JSON.stringify(next) !== JSON.stringify(picks)) {
        picks = next;
        draw();
      }
    });
  }

  send.addEventListener('click', function () {
    var names = data.categories.filter(function (c) { return picks.indexOf(c.id) >= 0; }).map(label);
    send.disabled = true;
    hl.chat
      .send('Мой выбор кешбэка на ' + monthName + ': ' + names.join(', ') + '.')
      .then(function () { note.textContent = 'Отправлено в чат'; })
      .catch(function (e) { note.textContent = 'Не отправилось: ' + e.code; })
      .then(draw);
  });

  draw();
  window.harnessly.connect().then(function (api) {
    hl = api;
    // Opened by a link (a guest) or outside a chat: there is no chat to send to, so the button is not offered.
    send.hidden = !(api.context && api.context.chat);
    return load();
  }).then(function () {
    draw();
    // The agent may write the picks while the window is open.
    setInterval(function () {
      if (!document.hidden) load().catch(function () {});
    }, 4000);
  }).catch(function (e) {
    note.textContent = e && e.code === 'host_unavailable' ? 'Откройте приложение в Harnessly' : 'Ошибка: ' + (e && e.code);
  });
})();
