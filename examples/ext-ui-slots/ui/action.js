// A button at a machine: the host says which one in the view's context.
harnessly.connect().then(async (hl) => {
  const root = document.getElementById('root');
  const obj = (hl.context.data && hl.context.data.object) || {};
  const ws = (await hl.storage.install.get('workspace')) || 'team.example.com';
  root.innerHTML = '<button id="go" type="button"></button>';
  const button = document.getElementById('go');
  button.textContent = 'Open ' + (obj.name || 'machine') + ' in Example Corp';
  button.addEventListener('click', () => {
    hl.ui.toast('Opened ' + ws + ' for the machine ' + (obj.id || ''));
  });
});
