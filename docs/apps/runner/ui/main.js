// Tests and a dev server of the chat's folder: the test command runs to its end and shows its code and output; the dev
// server runs until stopped, its output follows, and «Открыть в панели» shows its port in the pane's browser. The
// agent runs the tests with the view's tool run_tests, which the host asks the person about.
(function () {
  var $ = function (id) {
    return document.getElementById(id);
  };
  var sub = $('sub');
  var testCmd = $('test-cmd');
  var testRun = $('test-run');
  var testResult = $('test-result');
  var testOut = $('test-out');
  var devCmd = $('dev-cmd');
  var devPort = $('dev-port');
  var devStart = $('dev-start');
  var devStop = $('dev-stop');
  var devOpen = $('dev-open');
  var devResult = $('dev-result');
  var devOut = $('dev-out');
  var hl = null;
  var dev = null;
  var since = 0;
  var follow = 0;

  function say(el, text, ok) {
    el.textContent = text;
    el.className = 'rn__result' + (ok === true ? ' rn__ok' : ok === false ? ' rn__fail' : '');
  }

  function remember() {
    if (!hl.has('storage.set')) return;
    hl.storage.chat.set('commands', { test: testCmd.value, dev: devCmd.value, port: devPort.value });
  }

  function runTests() {
    var command = testCmd.value.trim();
    if (command === '') return Promise.resolve({ code: -1, output: '' });
    remember();
    testRun.disabled = true;
    say(testResult, 'Идут тесты…');
    return hl.terminal.run(command, { timeout: 600 }).then(function (r) {
      testRun.disabled = false;
      if (hl.refused(r)) {
        say(testResult, r.message || 'Не запустилось.', false);
        return { code: -1, output: r.message || '' };
      }
      var secs = (r.ms / 1000).toFixed(1);
      if (r.timedOut) say(testResult, 'Остановлены по времени через ' + secs + ' с', false);
      else if (r.code === 0) say(testResult, 'Прошли за ' + secs + ' с', true);
      else say(testResult, 'Упали с кодом ' + r.code + ' за ' + secs + ' с', false);
      testOut.textContent = r.output + (r.cut ? '\n… вывод обрезан' : '');
      testOut.hidden = r.output === '';
      return r;
    });
  }

  function drawDev(p) {
    var running = p !== null && p.running;
    devStart.hidden = running;
    devStop.hidden = !running;
    devOpen.hidden = !running;
    if (p === null) say(devResult, '');
    else if (running) say(devResult, 'Работает: ' + p.command);
    else say(devResult, 'Остановлен' + (p.code !== undefined ? ', код ' + p.code : ''), p.code === 0 ? undefined : false);
  }

  function pull() {
    if (dev === null) return;
    hl.process.output(dev.id, { since: since }).then(function (o) {
      if (hl.refused(o)) return;
      if (o.text !== '') {
        devOut.textContent = (devOut.textContent + o.text).slice(-20000);
        devOut.hidden = false;
        devOut.scrollTop = devOut.scrollHeight;
      }
      since = o.next;
      dev = o;
      drawDev(o);
      if (o.running) follow = setTimeout(pull, 1000);
    });
  }

  function startDev() {
    var command = devCmd.value.trim();
    if (command === '') return;
    remember();
    devStart.disabled = true;
    hl.process.start(command).then(function (p) {
      devStart.disabled = false;
      if (hl.refused(p)) return say(devResult, p.message || 'Не запустился.', false);
      dev = p;
      since = 0;
      devOut.textContent = '';
      drawDev(p);
      pull();
    });
  }

  function stopDev() {
    if (dev === null) return;
    clearTimeout(follow);
    hl.process.stop(dev.id).then(function (p) {
      if (hl.refused(p)) return say(devResult, p.message || 'Не остановился.', false);
      dev = p;
      drawDev(p);
    });
  }

  function openDev() {
    var port = parseInt(devPort.value, 10);
    if (!(port >= 1024 && port <= 65535)) return say(devResult, 'Порт — число от 1024 до 65535', false);
    remember();
    hl.preview.open(port).then(function (r) {
      if (hl.refused(r)) say(devResult, r.message || 'Не открылось.', false);
    });
  }

  testRun.addEventListener('click', runTests);
  devStart.addEventListener('click', startDev);
  devStop.addEventListener('click', stopDev);
  devOpen.addEventListener('click', openDev);

  window.harnessly.connect({ name: 'runner', protocol: 2 }).then(function (api) {
    hl = api;
    testCmd.value = 'npm test';
    devCmd.value = 'npm run dev';
    devPort.value = '5173';
    sub.textContent = 'Команды идут в папке чата, в песочнице агента';
    var saved = hl.has('storage.get') ? hl.storage.chat.get('commands') : Promise.resolve(null);
    saved.then(function (c) {
      if (c && !hl.refused(c)) {
        testCmd.value = c.test || testCmd.value;
        devCmd.value = c.dev || devCmd.value;
        devPort.value = c.port || devPort.value;
      }
    });
    // A dev server started before the view was opened again is still the app's.
    hl.process.list().then(function (list) {
      if (hl.refused(list) || !Array.isArray(list)) return;
      var running = list.filter(function (p) {
        return p.running;
      });
      if (running.length > 0) {
        dev = running[running.length - 1];
        drawDev(dev);
        pull();
      }
    });
    if (hl.has('tools.register'))
      hl.tools.register(
        { name: 'run_tests', description: 'Запустить тесты папки чата командой из окна «Запуск» и вернуть код и вывод' },
        function () {
          return runTests().then(function (r) {
            return { code: r.code, output: (r.output || '').slice(-8000) };
          });
        },
      );
  });
})();
