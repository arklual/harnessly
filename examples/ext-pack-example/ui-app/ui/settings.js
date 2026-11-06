// The settings page of the package: one field kept in the app's own storage (permission `storage`).
harnessly.connect().then(async (hl) => {
  const root = document.getElementById('root');
  const saved = (await hl.storage.install.get('workspace')) || '';
  root.innerHTML =
    '<h1>Example Corp</h1><p>The workspace your agents report to.</p>' +
    '<label for="ws">Workspace address</label><input id="ws" placeholder="team.example.com" />' +
    '<button id="save" type="button">Save</button><div class="ok" id="ok"></div>';
  const input = document.getElementById('ws');
  input.value = saved;
  document.getElementById('save').addEventListener('click', async () => {
    await hl.storage.install.set('workspace', input.value.trim());
    document.getElementById('ok').textContent = 'Saved';
  });
});
