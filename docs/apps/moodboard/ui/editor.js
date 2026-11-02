// The editor of a moodboard: the cards of the document, kept in its own storage (hl.storage.doc) under the key "board".
// The format is schema/moodboard.json; the host's side of the document (history, links, rights) is not this file's.
(function () {
  var list = document.getElementById('cards');
  var note = document.getElementById('note');
  var form = document.getElementById('add');
  var input = document.getElementById('text');

  window.harnessly.connect().then(function (hl) {
    var board = { cards: [] };
    // A document open for reading only is drawn without the way to change it.
    var readOnly = hl.context.readOnly === true;
    form.hidden = readOnly;

    function save() {
      return hl.storage.doc.set('board', board).catch(function (e) {
        note.textContent = 'Не удалось сохранить: ' + e.message;
      });
    }

    function render() {
      list.textContent = '';
      board.cards.forEach(function (card, i) {
        var li = document.createElement('li');
        li.className = 'mb__card';
        var p = document.createElement('p');
        p.className = 'mb__text';
        p.textContent = card.text;
        var rm = document.createElement('button');
        rm.type = 'button';
        rm.className = 'mb__remove';
        rm.textContent = 'Убрать';
        rm.addEventListener('click', function () {
          board.cards.splice(i, 1);
          render();
          save();
        });
        if (readOnly) li.append(p);
        else li.append(p, rm);
        list.append(li);
      });
      note.textContent = '';
      if (board.cards.length === 0) note.textContent = readOnly ? 'Пока пусто.' : 'Пока пусто: добавьте первую идею.';
    }

    form.addEventListener('submit', function (e) {
      e.preventDefault();
      var text = input.value.trim();
      if (!text) return;
      board.cards.push({ text: text });
      input.value = '';
      render();
      save();
    });

    hl.storage.doc.get('board').then(function (saved) {
      if (saved && Array.isArray(saved.cards)) board = saved;
      render();
    }).catch(function (e) {
      note.textContent = 'Не удалось открыть: ' + e.message;
    });
  }).catch(function () {});
})();
