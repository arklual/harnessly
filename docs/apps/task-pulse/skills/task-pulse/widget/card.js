// The card of a task (pulse.py gives window.widgetData). Built with textContent only: the tracker's and GitLab's words
// are data, never markup.
(function () {
  var d = window.widgetData || {};
  var $ = function (id) { return document.getElementById(id); };
  var el = function (tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  };
  var PIPELINE = { success: 'CI прошёл', failed: 'CI упал', running: 'CI идёт', pending: 'CI ждёт', created: 'CI ждёт',
    canceled: 'CI отменён', skipped: 'CI пропущен', manual: 'CI ждёт запуска' };
  var MERGE = { need_rebase: 'ждёт ребейза в очереди', conflict: 'конфликт', mergeable: 'можно вливать',
    ci_still_running: 'ждёт CI', not_approved: 'ждёт одобрения', discussions_not_resolved: 'есть обсуждения' };
  var STATE = { merged: 'влит', closed: 'закрыт' };

  $('key').textContent = d.key || '';
  if (d.error) {
    $('title').textContent = d.error === 'not-a-key' ? 'Это не ключ задачи' : 'Задача не прочиталась';
    $('problem').textContent = d.reason || 'Проверьте ключ и подключение трекера в «Настройки → Подключения».';
  } else {
    $('title').textContent = d.title || '';
    var status = $('status');
    status.textContent = d.status || '';
    status.setAttribute('data-cat', d.statusCategory || '');
    var meta = [];
    if (d.assignee) meta.push(d.assignee);
    if (d.parent && d.parent.key) meta.push(d.parent.key + ' · ' + (d.parent.title || ''));
    $('meta').textContent = meta.join(' — ');

    var dod = d.dod || { total: 0, items: [] };
    if (dod.total > 0) {
      var box = $('dod');
      box.appendChild(el('p', 'tp-sub', 'Готово когда: ' + dod.done + ' из ' + dod.total));
      var bar = el('div', 'tp-bar');
      var fill = el('i');
      fill.style.width = Math.round((dod.done / dod.total) * 100) + '%';
      bar.appendChild(fill);
      box.appendChild(bar);
      var list = el('ul', 'tp-items');
      dod.items.forEach(function (it) {
        var li = el('li', '', (it.done ? '✓ ' : '○ ') + it.text);
        li.setAttribute('data-done', String(!!it.done));
        list.appendChild(li);
      });
      box.appendChild(list);
    }

    var mrs = $('mrs');
    if (Array.isArray(d.mrs) && d.mrs.length) {
      d.mrs.forEach(function (mr) {
        var row = el('div', 'tp-mr');
        var s = mr.pipeline ? mr.pipeline.status : '';
        var dot = el('span', 'tp-dot');
        dot.setAttribute('data-s', s || '');
        row.appendChild(dot);
        var text = el('div', 'tp-mr-text');
        text.appendChild(el('div', 'tp-mr-title', (mr.project ? mr.project.split('/').pop() + ' !' : '!') + mr.iid + ' ' + mr.title));
        var words = [];
        if (STATE[mr.state]) words.push(STATE[mr.state]);
        else {
          if (s) words.push(PIPELINE[s] || s);
          if (mr.mergeStatus && MERGE[mr.mergeStatus]) words.push(MERGE[mr.mergeStatus]);
        }
        text.appendChild(el('div', 'muted', words.join(', ')));
        row.appendChild(text);
        mrs.appendChild(row);
      });
    } else if (d.gitlab === 'no-token') {
      $('problem').textContent = 'MR и пайплайны появятся, когда в сейфе будет gitlab-token.';
    } else if (Array.isArray(d.mrs)) {
      $('problem').textContent = 'MR с этим ключом нет.';
    }
  }
  if (d.checkedAt) {
    var t = new Date(d.checkedAt);
    $('checked').textContent = 'Проверено в ' + t.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });
  }

  var button = $('refresh');
  if (!d.key || !window.harnessly) {
    button.hidden = true;
    return;
  }
  window.harnessly.connect().then(function (hl) {
    button.addEventListener('click', function () {
      button.disabled = true;
      hl.chat.send('Обнови карточку ' + d.key).catch(function () { button.disabled = false; });
    });
  }, function () { button.hidden = true; });
})();
