// The changes of the chat's folder: the branch and the changed files by git, the diff of the file picked. Read again
// when files under the masks change and when the agent ends its turn; no terminal.
(function () {
  var sub = document.getElementById('sub');
  var list = document.getElementById('files');
  var diffBox = document.getElementById('diff');
  var hl = null;
  var shown = '';
  var wait = 0;

  var LETTERS = { M: 'изменён', A: 'добавлен', D: 'удалён', R: 'переименован', '?': 'новый' };

  function stateOf(f) {
    var l = f.staged || f.unstaged || '';
    return LETTERS[l] || l;
  }

  function line(text, cls) {
    var span = document.createElement('span');
    span.textContent = text;
    if (cls) span.className = cls;
    return span;
  }

  function drawDiff(text) {
    diffBox.textContent = '';
    text.split('\n').forEach(function (l) {
      var cls = '';
      if (l.charAt(0) === '+' && l.indexOf('+++') !== 0) cls = 'ch__add';
      else if (l.charAt(0) === '-' && l.indexOf('---') !== 0) cls = 'ch__del';
      else if (l.indexOf('@@') === 0) cls = 'ch__hunk';
      diffBox.appendChild(line(l + '\n', cls));
    });
    diffBox.hidden = text === '';
  }

  function openDiff(path) {
    shown = path;
    hl.git.diff({ path: path }).then(function (r) {
      if (hl.refused(r)) return drawDiff('');
      drawDiff(r.diff || 'Новый файл: git ещё не знает его содержимого.');
    });
    Array.prototype.forEach.call(list.children, function (li) {
      li.firstChild.setAttribute('aria-pressed', String(li.dataset.path === path));
    });
  }

  function refresh() {
    hl.git.status().then(function (st) {
      if (hl.refused(st)) {
        sub.textContent = st.message || 'Папка чата не в git-репозитории.';
        list.textContent = '';
        drawDiff('');
        return;
      }
      var files = st.files || [];
      var n = files.length;
      sub.textContent =
        'Ветка ' + (st.branch || '—') + ' · ' + (n === 0 ? 'изменений нет' : n + ' ' + (n === 1 ? 'файл' : n < 5 ? 'файла' : 'файлов'));
      list.textContent = '';
      files.forEach(function (f) {
        var li = document.createElement('li');
        li.dataset.path = f.path;
        var b = document.createElement('button');
        b.type = 'button';
        b.className = 'ch__file';
        b.setAttribute('aria-pressed', String(f.path === shown));
        b.appendChild(line(f.path, 'ch__path'));
        b.appendChild(line(stateOf(f), 'ch__state'));
        b.addEventListener('click', function () {
          openDiff(f.path);
        });
        li.appendChild(b);
        list.appendChild(li);
      });
      var still = files.some(function (f) {
        return f.path === shown;
      });
      if (still) openDiff(shown);
      else if (n > 0) openDiff(files[0].path);
      else drawDiff('');
    });
  }

  // A burst of saves reads the status once.
  function soon() {
    clearTimeout(wait);
    wait = setTimeout(refresh, 400);
  }

  window.harnessly.connect({ name: 'changes', protocol: 2 }).then(function (api) {
    hl = api;
    refresh();
    if (hl.has('files.watch')) hl.files.watch(soon);
    if (hl.has('events.on')) hl.events.on('turn', soon);
  });
})();
