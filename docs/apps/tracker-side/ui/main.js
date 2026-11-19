// The widget asks the person's own Yandex Tracker connection (Settings → Connections) for their open issues. The app holds
// no key: hl.connections.api goes through the machine, which signs the request in as the person, and only after the
// person agreed to this app reading the connection. Every state a person can be in is shown in words, and each has the
// one thing to do about it: not connected → Settings → Connections; no agreement → the button asks for it in the
// host's card; waiting for a sign-in → the same page; an answer → the list.
(function () {
  var CONNECTION = 'yandex-tracker';
  var QUERY = 'Assignee: me() Resolution: empty()';
  var note = document.getElementById('note');
  var list = document.getElementById('list');
  var actions = document.getElementById('actions');
  var hl = null;
  var seq = 0;

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  }
  function say(text, isError) {
    note.textContent = text;
    note.dataset.kind = isError ? 'error' : '';
  }
  function button(label, onClick) {
    var b = el('button', 'primary', label);
    b.type = 'button';
    b.addEventListener('click', onClick);
    return b;
  }
  function clear() {
    list.textContent = '';
    actions.textContent = '';
  }
  function show(state) {
    clear();
    say(state.text, state.error === true);
    if (state.action) actions.appendChild(button(state.action.label, state.action.run));
    resize();
  }
  function resize() {
    if (hl) hl.resize(Math.ceil(document.documentElement.scrollHeight));
  }

  // Where the connection stands (hl.connections.list) says what to show; a refusal of a call is shown the same way, since
  // the person may take the agreement back or sign out between the list and the call.
  var STATES = {
    not_connected: { text: 'Яндекс Трекер не подключён или ждёт входа. Подключите его в «Настройки → Подключения».' },
    needs_signin: { text: 'Яндекс Трекер ждёт входа или новых ключей: «Настройки → Подключения».' },
    off: { text: 'Подключение к Яндекс Трекеру выключено: включите его в «Настройки → Подключения».' },
    unavailable: { text: 'Яндекс Трекер сейчас не отвечает. Попробуйте позже.', error: true },
  };
  function askConsent() {
    return hl.connections.request(CONNECTION).then(function (r) {
      if (hl.refused(r)) return show({ text: r.message, error: true });
      if (r.granted) return load();
      say('Без вашего согласия приложение не читает Трекер.');
    });
  }
  function noConsent() {
    show({ text: 'Приложение пока не может читать ваш Яндекс Трекер: нужно ваше согласие.', action: { label: 'Разрешить чтение', run: askConsent } });
  }
  function refusal(r) {
    if (r.code === 'auth_expired') return show(STATES.needs_signin);
    if (r.code === 'auth_required') return show(STATES.not_connected);
    if (r.code === 'permission_denied') return noConsent();
    if (r.code === 'rate_limited') return show({ text: 'Слишком часто. Подождите несколько секунд.', error: true });
    show({ text: 'Не получилось получить задачи: ' + r.message, error: true });
  }

  function render(issues) {
    clear();
    if (issues.length === 0) {
      say('Открытых задач на вас нет.');
      return resize();
    }
    say('Открытых задач: ' + issues.length);
    issues.forEach(function (issue) {
      var li = el('li', 'ts__item');
      li.appendChild(el('span', 'ts__key', String(issue.key || '')));
      li.appendChild(el('span', 'ts__summary', String(issue.summary || '')));
      var status = issue.status && issue.status.display ? String(issue.status.display) : '';
      if (status) li.appendChild(el('span', 'ts__status', status));
      list.appendChild(li);
    });
    resize();
  }

  function load() {
    var mine = ++seq;
    say('Загружаю…');
    return hl.connections.list().then(function (all) {
      if (mine !== seq) return;
      if (hl.refused(all)) return refusal(all);
      var found = all.filter(function (c) { return c.id === CONNECTION; })[0];
      if (!found || found.status === 'not_connected') return show(STATES.not_connected);
      if (found.status === 'needs_consent') return noConsent();
      if (STATES[found.status]) return show(STATES[found.status]);
      // The search is a POST, and it only reads: the catalog lists it as a read of this API, so a read agreement is enough.
      return hl
        .connections.api(CONNECTION, { method: 'POST', path: '/issues/_search', query: { perPage: 10 }, body: { query: QUERY } })
        .then(function (answer) {
          if (mine !== seq) return;
          if (hl.refused(answer)) return refusal(answer);
          if (!answer.ok || !Array.isArray(answer.body)) return show({ text: 'Трекер ответил ' + answer.status + '.', error: true });
          render(answer.body);
        });
    });
  }

  window.harnessly.connect({ protocol: 2 }).then(function (host) {
    hl = host;
    document.getElementById('refresh').addEventListener('click', load);
    // The person agreed, signed in, turned it off or took the agreement back: ask again.
    hl.on('connections', load);
    hl.on('visibility', function (v) { if (v.visible) load(); });
    return load();
  }, function () {
    say('Окно открыто вне harnessly.', true);
  });
})();
