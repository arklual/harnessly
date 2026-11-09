package apps

import (
	"html"
	"strings"
)

// The files of the view templates of `harnessly app new`. A view is a page on an opaque origin: it loads the
// host's hl-ui.css (the platform's tokens and primitives), shows a state for loading and one for an error, and talks
// protocol 2 (a refused call answers a Refusal, it does not throw).

const paneChatHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{name}}</title>
<link rel="stylesheet" href="/__hl/hl-ui.css">
<link rel="stylesheet" href="style.css">
</head>
<body class="hl-body">
<main id="app" class="hl-stack" aria-live="polite"></main>
<!-- Classic scripts: they load from any frame. A bundler's module scripts and fetch of the view's own files work too (the
     files are served with the session in their path), as long as every address in this page is relative: no leading "/". -->
<script src="/__hl/app-sdk.js"></script>
<script src="main.js"></script>
</body>
</html>
`

const paneChatCSS = `/* The tokens (--t-*) and the primitives (.hl-*) come from hl-ui.css; the theme follows the pane. Put only what is yours here. */
#app { max-width: 640px; }
`

const paneChatJS = `// window.harnessly is the app SDK (harnessly app docs). Protocol 2: a call that is refused answers { error, code, message }
// instead of throwing; hl.refused(answer) tells it. connect() rejects only when there is no host (the page opened by itself).
var NAME = '{{js}}';
var root = document.getElementById('app');

function el(tag, className, text) {
  var node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function show() {
  root.replaceChildren.apply(root, arguments);
}

function showLoading() {
  show(el('span', 'hl-skeleton'), el('span', 'hl-skeleton'));
}

// An error stays on the page: the person sees what happened, and so does anyone who reads the page's console.
function showError(title, error) {
  var detail = error && error.code ? error.code + ': ' + error.message : String((error && error.message) || error || '');
  if (error && error.code === 'host_unavailable') detail += ' - open this view inside Harnessly.';
  console.error(NAME, title, error);
  show(el('h1', 'hl-title', title), el('p', 'hl-text hl-danger', detail));
}

showLoading();
window.harnessly
  .connect({ protocol: 2 })
  .then(function (hl) {
    var status = el('p', 'hl-hint');
    var button = el('button', 'hl-btn hl-btn--primary', 'Say hello in the chat');
    button.type = 'button';
    button.addEventListener('click', function () {
      button.disabled = true;
      status.textContent = '';
      hl.chat
        .send('Hello from ' + NAME)
        .then(function (answer) {
          if (hl.refused(answer)) status.textContent = answer.message;
        })
        .catch(function (error) {
          status.textContent = String(error && error.message || error);
        })
        .finally(function () {
          button.disabled = false;
        });
    });
    show(el('h1', 'hl-title', NAME), el('p', 'hl-text hl-muted', 'Shown in: ' + hl.context.target), button, status);
  })
  .catch(function (error) {
    showError('Could not connect to Harnessly', error);
  });
`

// The Vite + React + TypeScript template: the source is in src/, the build goes into ui/ (the folder the view is served
// from). base "./" keeps every address in the build relative, which the session in the view's path needs.
const viteIndexHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{name}}</title>
<link rel="stylesheet" href="/__hl/hl-ui.css">
</head>
<body class="hl-body">
<div id="root"></div>
<script src="/__hl/app-sdk.js"></script>
<script type="module" src="/src/main.tsx"></script>
</body>
</html>
`

const viteBuiltPlaceholder = `<!doctype html>
<html lang="ru">
<head><meta charset="utf-8"><title>{{name}}</title><link rel="stylesheet" href="/__hl/hl-ui.css"></head>
<body class="hl-body"><p class="hl-text">Not built yet: run <code>npm install</code> and <code>npm run build</code> in the app's folder.</p></body>
</html>
`

const vitePackageJSON = `{
  "name": "{{slug}}",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "build": "tsc --noEmit && vite build",
    "dev": "vite build --watch --mode development"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "typescript": "^5.6.3",
    "vite": "^5.4.11"
  }
}
`

const viteConfig = `import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The view is served from ui/ with the session in its address: "./" keeps every address of the build relative to the
// page (an absolute "/assets/…" would leave the session out and come back 401).
export default defineConfig({
  base: './',
  plugins: [react()],
  build: { outDir: 'ui', emptyOutDir: true, sourcemap: true, target: 'es2020' },
});
`

const viteTSConfig = `{
  "compilerOptions": {
    "target": "ES2020",
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "noEmit": true,
    "skipLibCheck": true,
    "isolatedModules": true
  },
  "include": ["src", "vite.config.ts"]
}
`

const viteMainTSX = `import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
`

const viteAppTSX = `import { useEffect, useState } from 'react';
import type { Harnessly } from './app-sdk';

const NAME = '{{js}}';

type State = { kind: 'loading' } | { kind: 'error'; message: string } | { kind: 'ready'; hl: Harnessly };

export function App() {
  const [state, setState] = useState<State>({ kind: 'loading' });
  const [status, setStatus] = useState('');

  useEffect(() => {
    window.harnessly
      .connect({ protocol: 2 })
      .then((hl) => setState({ kind: 'ready', hl }))
      .catch((error: Error & { code?: string }) => {
        console.error(NAME, error);
        setState({ kind: 'error', message: error.code ? error.code + ': ' + error.message : error.message });
      });
  }, []);

  if (state.kind === 'loading') return <span className="hl-skeleton" />;
  if (state.kind === 'error') {
    return (
      <>
        <h1 className="hl-title">Could not connect to Harnessly</h1>
        <p className="hl-text hl-danger">{state.message}</p>
      </>
    );
  }
  const { hl } = state;
  const hello = async () => {
    setStatus('');
    const answer = await hl.chat.send('Hello from ' + NAME);
    if (hl.refused(answer)) setStatus(answer.message);
  };
  return (
    <div className="hl-stack">
      <h1 className="hl-title">{NAME}</h1>
      <p className="hl-text hl-muted">Shown in: {hl.context.target}</p>
      <div>
        <button type="button" className="hl-btn hl-btn--primary" onClick={hello}>
          Say hello in the chat
        </button>
      </div>
      <p className="hl-hint">{status}</p>
    </div>
  );
}
`

// viteGlobalDTS types window.harnessly from the SDK's own types (src/app-sdk.d.ts, a copy of the host's).
const viteGlobalDTS = `import type { connect, HarnesslyError } from './app-sdk';

declare global {
  interface Window {
    /** The app SDK, put there by <script src="/__hl/app-sdk.js"> in index.html. */
    harnessly: { connect: typeof connect; HarnesslyError: typeof HarnesslyError };
  }
}

export {};
`

const viteGitignore = "node_modules\n"

// fillTemplate puts the app's name and folder name into a template.
// {{name}} is HTML-escaped, {{js}} fits inside a single-quoted JS string, {{slug}} is the folder's name.
func fillTemplate(text, name, slug string) string {
	js := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", " ", "\r", " ", "<", `\x3c`).Replace(name)
	return strings.NewReplacer("{{name}}", html.EscapeString(name), "{{js}}", js, "{{slug}}", slug).Replace(text)
}
