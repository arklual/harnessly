const esc = (value: string): string =>
  value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

/** Shown when no local runtime answers and no server address is saved. */
export const buildConnectPage = (recent: readonly string[], isDark: boolean): string => {
  const bg = isDark ? '#09090b' : '#ffffff';
  const fg = isDark ? '#fafafa' : '#18181b';
  const items = recent.map((url) => `<button type="button" data-url="${esc(url)}">${esc(url)}</button>`).join('');
  return `<!doctype html><meta charset="utf-8"><title>Harnessly</title>
<style>body{margin:0;display:grid;place-items:center;height:100vh;background:${bg};color:${fg};font:14px system-ui}
main{width:360px}input,button{width:100%;box-sizing:border-box;padding:8px;margin-top:8px;font:inherit}</style>
<main><h3>Connect to Harnessly</h3><form id="f"><input id="u" placeholder="https://harnessly.example.com" autofocus>
<button>Connect</button></form>${items}</main>
<script>const go=u=>window.electronAPI.connect(u);
document.getElementById('f').onsubmit=e=>{e.preventDefault();go(document.getElementById('u').value)};
document.querySelectorAll('[data-url]').forEach(b=>b.onclick=()=>go(b.dataset.url));</script>`;
};
