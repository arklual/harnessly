// A hook on the agent's calls (hooks.agent): a Bash command that deletes recursively or force-pushes is stopped before
// it runs, with the reason in words for the agent. The hook only stops or lets through; it cannot change the call.
(function () {
  var sub = document.getElementById('sub');
  var list = document.getElementById('list');
  // The command in words: quotes dropped and every separator (; && || | newline, a subshell) splitting it, so a word
  // is found wherever it stands, also under sudo, env, xargs or bash -c.
  function words(command) {
    return command
      .replace(/["'`\\]/g, ' ')
      .split(/[\s;&|()<>]+/)
      .filter(Boolean)
      .map(function (w) {
        return w.replace(/^.*\//, '');
      });
  }
  function shortFlags(w) {
    return /^-[a-zA-Z]+$/.test(w) ? w.slice(1) : '';
  }
  // rm with recursion and force, in any order, case and spelling.
  function deletesForce(ws) {
    var at = ws.indexOf('rm');
    if (at < 0) return false;
    var rest = ws.slice(at + 1);
    var flags = rest.map(shortFlags).join('');
    var recursive = /[rR]/.test(flags) || rest.indexOf('--recursive') >= 0;
    var force = /f/.test(flags) || rest.indexOf('--force') >= 0;
    return recursive && force;
  }
  // git push that rewrites history: -f in a group, --force, --force-with-lease, --mirror, a refspec with +.
  function pushesForce(ws) {
    var at = ws.indexOf('git');
    var push = at < 0 ? -1 : ws.indexOf('push', at + 1);
    if (push < 0) return false;
    return ws.slice(push + 1).some(function (w) {
      return (
        /f/.test(shortFlags(w)) || /^--force(-with-lease)?(=|$)/.test(w) || w === '--mirror' || /^\+/.test(w)
      );
    });
  }
  var RULES = [
    { test: deletesForce, why: 'удаляет файлы без возврата (rm -rf)' },
    { test: pushesForce, why: 'перезаписывает историю на сервере (git push --force)' },
  ];

  function stopped(command, why) {
    var li = document.createElement('li');
    li.className = 'gd__item';
    li.textContent = new Date().toLocaleTimeString() + ' — остановлено: ' + why;
    var code = document.createElement('code');
    code.className = 'gd__cmd';
    code.textContent = command;
    li.appendChild(code);
    list.insertBefore(li, list.firstChild);
  }

  window.harnessly
    .connect({ name: 'guard', protocol: 2 })
    .then(function (hl) {
      return hl.hooks.on('tool', function (call) {
        if (call.tool !== 'Bash') return {};
        var command = String((call.input && call.input.command) || '');
        // A call too big to show: its command is not seen, so it is stopped rather than guessed at.
        if (call.cut) return { decision: 'deny', reason: 'Стоп-кран не видит команду целиком; разбей её на части.' };
        var ws = words(command);
        for (var i = 0; i < RULES.length; i++) {
          if (RULES[i].test(ws)) {
            stopped(command, RULES[i].why);
            return {
              decision: 'deny',
              reason: 'Стоп-кран: команда ' + RULES[i].why + '. Спроси человека, нужно ли это, и предложи безопасный вариант.',
            };
          }
        }
        return {};
      }).then(function () {
        sub.textContent = hl.context.chat ? 'Слежу за командами агента в этом чате' : 'Включите приложение в чате';
      });
    })
    .catch(function (e) {
      sub.textContent = 'Нет связи с хостом: ' + (e && e.message ? e.message : e);
    });
})();
