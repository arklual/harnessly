// A step of the first run: the same field, asked once.
harnessly.connect().then(async (hl) => {
  const root = document.getElementById('root');
  root.innerHTML =
    '<h1>Connect your Example Corp account</h1><p>Agents need the address of your workspace.</p>' +
    '<label for="ws">Workspace address</label><input id="ws" placeholder="team.example.com" />' +
    '<button id="go" type="button">Continue</button>';
  document.getElementById('go').addEventListener('click', async () => {
    const v = document.getElementById('ws').value.trim();
    if (!v) return;
    await hl.storage.install.set('workspace', v);
    hl.slot.done();
  });
});
