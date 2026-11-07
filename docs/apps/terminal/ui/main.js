// A terminal of the chat's folder built on the SDK (terminal.pty): xterm draws it, hl.terminal.open gives a shell in
// the agent's sandbox, and what is typed and printed goes through the host's bridge. «Замер» times keystroke to echo
// (the drawing left out, as the right pane's Terminal is timed).
(function () {
  var $ = function (id) {
    return document.getElementById(id);
  };
  var sub = $('sub');
  var again = $('again');
  var measureBtn = $('measure');
  var hl = null;
  var shell = null;
  var listen = null;
  var held = [];
  // The handshake first: it does not wait on the drawing, which a browser without a screen may not do.
  var connected = window.harnessly.connect({ name: 'terminal', protocol: 2 });
  var term;
  var fit;
  try {
    term = makeTerminal();
  } catch (e) {
    sub.textContent = 'Терминал не рисуется в этом окне';
    return;
  }

  function makeTerminal() {
    // The terminal takes the host's colors: the view's page has them from the host's theme.
    var colors = getComputedStyle(document.body);
    var t = new window.Terminal({
      cursorBlink: true,
      fontSize: 13,
      scrollback: 5000,
      theme: { background: colors.backgroundColor, foreground: colors.color, cursor: colors.color },
    });
    fit = new window.FitAddon.FitAddon();
    t.loadAddon(fit);
    t.open($('term'));
    return t;
  }

  function fitNow() {
    try {
      fit.fit();
    } catch (e) {
      // not drawn yet
    }
    if (shell) shell.resize(term.cols, term.rows);
  }

  function open() {
    again.hidden = true;
    sub.textContent = 'Открываю…';
    fitNow();
    return hl.terminal.open({ cols: term.cols, rows: term.rows }).then(function (t) {
      if (hl.refused(t)) {
        sub.textContent = t.error === 'denied' ? 'Нет права открывать терминал' : 'Терминал не открылся: ' + t.message;
        again.hidden = false;
        return;
      }
      shell = t;
      sub.textContent = 'Папка чата, песочница агента';
      measureBtn.disabled = false;
      t.onData(function (bytes) {
        if (listen) {
          // While timing, what comes is drawn after: the time is the way of the keys and the echo, not the drawing.
          listen(bytes);
          held.push(bytes);
          return;
        }
        term.write(bytes);
      });
      t.onExit(function (code) {
        shell = null;
        measureBtn.disabled = true;
        sub.textContent = code === null ? 'Терминал закрыт' : 'Оболочка вышла с кодом ' + code;
        again.hidden = false;
      });
      term.focus();
    });
  }

  // Keystroke to echo: the shell's line editor prints each letter back. The line is wiped after.
  function measure() {
    if (!shell) return;
    measureBtn.disabled = true;
    var times = [];
    var n = 0;
    var started = 0;
    var waiting = null;
    listen = function (bytes) {
      if (waiting && Array.prototype.indexOf.call(bytes, 120) >= 0) {
        var done = waiting;
        waiting = null;
        done();
      }
    };
    function one() {
      return new Promise(function (resolve) {
        waiting = function () {
          times.push(performance.now() - started);
          resolve();
        };
        started = performance.now();
        shell.write('x');
      });
    }
    function loop() {
      if (n++ >= 50) return Promise.resolve();
      return one().then(loop);
    }
    return loop().then(function () {
      listen = null;
      held.splice(0).forEach(function (b) {
        term.write(b);
      });
      shell.write('\u0015');
      times.sort(function (a, b) {
        return a - b;
      });
      var median = times[Math.floor(times.length / 2)];
      var p95 = times[Math.floor(times.length * 0.95)];
      sub.textContent = 'Эхо: медиана ' + median.toFixed(1) + ' мс, 95% — ' + p95.toFixed(1) + ' мс';
      window.__hlMeasure = { median: median, p95: p95, n: times.length };
      measureBtn.disabled = false;
    });
  }

  term.onData(function (text) {
    if (shell) shell.write(text);
  });
  new ResizeObserver(fitNow).observe($('term'));
  again.addEventListener('click', open);
  measureBtn.addEventListener('click', measure);

  connected.then(function (api) {
    hl = api;
    return open();
  });
})();
