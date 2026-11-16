// Start page of the native app: finds the server to show, or asks for its address.
(function () {
  'use strict';
  var invoke = function (cmd, args) { return window.__TAURI_INTERNALS__.invoke(cmd, args || {}); };
  var $ = function (id) { return document.getElementById(id); };
  var forceConnect = new URLSearchParams(location.search).has('connect');

  function showLoading(text) {
    $('loading-text').textContent = text || 'Connecting…';
    $('loading').hidden = false;
    $('connect').hidden = true;
  }

  function renderRecent(list) {
    var ul = $('recent');
    ul.textContent = '';
    (list || []).forEach(function (url) {
      var li = document.createElement('li');
      var open = document.createElement('button');
      open.type = 'button'; open.className = 'open'; open.textContent = url; open.title = url;
      open.onclick = function () { connect(url); };
      var forget = document.createElement('button');
      forget.type = 'button'; forget.className = 'forget'; forget.textContent = '×'; forget.title = 'Forget';
      forget.onclick = function () { invoke('forget_server', { url: url }).then(renderRecent); };
      li.append(open, forget);
      ul.append(li);
    });
    $('recent-box').hidden = !(list && list.length);
  }

  function showConnect(recent) {
    renderRecent(recent);
    $('loading').hidden = true;
    $('connect').hidden = false;
    $('url').focus();
  }

  function connect(url) {
    $('error').textContent = '';
    $('submit').disabled = true;
    invoke('connect', { url: url }).then(function (ok) {
      $('submit').disabled = false;
      if (!ok) $('error').textContent = 'Enter an http:// or https:// address.';
      else showLoading();
    }, function (err) {
      $('submit').disabled = false;
      $('error').textContent = String(err);
    });
  }

  function start() {
    showLoading();
    invoke('startup').then(function (recent) {
      if (recent) showConnect(recent);
    }, function (err) {
      showConnect([]);
      $('error').textContent = String(err);
    });
  }

  $('form').onsubmit = function (e) { e.preventDefault(); connect($('url').value.trim()); };
  $('local').onclick = function () {
    invoke('use_local').then(function () { history.replaceState(null, '', location.pathname); start(); });
  };

  if (forceConnect) invoke('recent_servers').then(showConnect);
  else start();
})();
