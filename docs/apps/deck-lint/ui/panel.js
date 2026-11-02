// The panel beside a presentation: the longest texts of its slides; a tap puts a mark on that text on the board.
// The check itself (rules in harnessly/app.json) runs in the host on save and on share; this only helps to look.
(function () {
  var LIMIT = 40;
  var list = document.getElementById('list');
  var note = document.getElementById('note');
  document.getElementById('limit').textContent = String(LIMIT);

  // The host counts the words of the element's own text whole (`words`): its `text` is clipped and would undercount.
  function words(node) {
    return typeof node.words === 'number' ? node.words : 0;
  }

  function row(hl, node, n) {
    var li = document.createElement('li');
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'dl__row';
    b.dataset.over = String(n > LIMIT);
    var text = document.createElement('span');
    text.className = 'dl__text';
    text.textContent = node.text || node.id;
    var count = document.createElement('span');
    count.className = 'dl__n';
    count.textContent = String(n);
    b.append(text, count);
    b.addEventListener('click', function () {
      hl.sinopia
        .mark([node.id], { note: n + ' слов', level: n > LIMIT ? 'warning' : 'info', replace: true })
        .catch(function (e) {
          note.textContent = 'Не удалось отметить: ' + e.message;
        });
    });
    li.append(b);
    return li;
  }

  window.harnessly.connect().then(function (hl) {
    function refresh() {
      hl.sinopia.query('h1, h2, h3, p, li').then(function (r) {
        var rows = r.nodes
          .map(function (node) {
            return { node: node, n: words(node) };
          })
          .sort(function (a, b) {
            return b.n - a.n;
          })
          .slice(0, 8);
        list.textContent = '';
        rows.forEach(function (x) {
          list.append(row(hl, x.node, x.n));
        });
        note.textContent = rows.length ? '' : 'В презентации пока нет текста.';
      }).catch(function (e) {
        note.textContent = 'Не удалось прочитать слайды: ' + e.message;
      });
    }
    refresh();
    hl.on('selection', refresh);
  }).catch(function () {
    note.textContent = 'Откройте презентацию в Harnessly.';
  });
})();
