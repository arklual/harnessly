import './reader.css';
import { owner, ownerKey, shelf, type Copy } from './storage';
import type { CachedFeed } from '../stores/conversation';

const base = new URL('./', location.href).pathname;
const person = owner(base);
const ru = navigator.language.startsWith('ru');
const msg = (en: string, rus: string) => (ru ? rus : en);
document.documentElement.lang = ru ? 'ru' : 'en';
const nav = document.querySelector('nav')!;
const view = document.querySelector('section')!;
document.querySelector('h1')!.textContent = msg('Offline library', 'Офлайн-библиотека');
document.querySelector('a')!.textContent = msg('Open workspace', 'Открыть рабочее пространство');
const check = () => {
  if (owner(base) !== person) location.reload();
};
window.addEventListener('storage', (e) => {
  if (!e.key || e.key === ownerKey(base)) check();
});
window.addEventListener('pageshow', check);
let selection = new AbortController();
function reset() {
  selection.abort();
  selection = new AbortController();
  view.replaceChildren();
  return selection.signal;
}
function button(parent: HTMLElement, title: string, click: () => void) {
  const el = document.createElement('button');
  el.textContent = title;
  el.onclick = click;
  parent.append(el);
}
function request<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}
function error(e: unknown) {
  view.textContent = String(e);
}
async function editChatDraft(db: IDBDatabase, id: string, signal: AbortSignal) {
  if (!db.objectStoreNames.contains('drafts')) return;
  const load = db.transaction('drafts').objectStore('drafts').get(id);
  const previous = await request(load);
  if (signal.aborted) return;
  let baseText = previous?.text ?? '';
  const editor = document.createElement('textarea');
  editor.ariaLabel = msg('Message draft', 'Черновик сообщения');
  editor.value = baseText;
  view.append(editor);
  const status = document.createElement('p');
  button(view, msg('Save message draft', 'Сохранить черновик сообщения'), () => {
    if (owner(base) !== person) return;
    const tx = db.transaction('drafts', 'readwrite');
    const store = tx.objectStore('drafts');
    const get = store.get(id);
    const text = editor.value;
    get.onsuccess = () => {
      if ((get.result?.text ?? '') !== baseText) {
        tx.abort();
        return;
      }
      store.put({ ...get.result, attachments: get.result?.attachments ?? [], text }, id);
    };
    tx.oncomplete = () => {
      baseText = text;
      status.textContent = msg('Draft saved', 'Черновик сохранён');
    };
    tx.onabort = () => {
      status.textContent = msg(
        'Could not save. Reopen the draft to see the latest version.',
        'Не удалось сохранить. Открой черновик повторно, чтобы увидеть последнюю версию.',
      );
    };
  });
  view.append(status);
}
async function init() {
  if (!person) {
    view.textContent = msg(
      'Open your workspace online first.',
      'Сначала открой рабочее пространство с интернетом.',
    );
    return;
  }
  const db = await shelf(base, person);
  const copies = () => db.transaction('copies').objectStore('copies');
  async function open(id: string) {
    const signal = reset();
    const copy = await request<Copy>(copies().get(id));
    if (signal.aborted || !copy) return;
    const h = document.createElement('h2');
    h.textContent = copy.title;
    view.append(h);
    button(view, msg('Remove offline copy', 'Удалить офлайн-копию'), () => {
      const tx = db.transaction(['copies', 'drafts'], 'readwrite');
      tx.objectStore('copies').delete(id);
      tx.objectStore('drafts').delete(id);
      tx.oncomplete = () => location.reload();
      tx.onabort = () => error(tx.error);
    });
    if (copy.kind === 'text') {
      const saved = await request<string | undefined>(db.transaction('drafts').objectStore('drafts').get(id));
      const text = saved ?? (await copy.body.text());
      if (signal.aborted) return;
      const editor = document.createElement('textarea');
      editor.ariaLabel = copy.title;
      editor.value = text;
      view.append(editor);
      const status = document.createElement('p');
      button(view, msg('Save local draft', 'Сохранить локальный черновик'), () => {
        const tx = db.transaction('drafts', 'readwrite');
        tx.objectStore('drafts').put(editor.value, id);
        tx.oncomplete = () => {
          status.textContent = msg('Draft saved on this device', 'Черновик сохранён на этом устройстве');
        };
        tx.onabort = () => error(tx.error);
      });
      view.append(status);
    } else if (copy.kind === 'html') {
      const html = await copy.body.text();
      if (signal.aborted) return;
      const frame = document.createElement('iframe');
      frame.title = copy.title;
      frame.sandbox.add('allow-scripts');
      frame.srcdoc =
        "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; frame-src about: data:; form-action 'none'; base-uri 'none'\">" +
        html;
      view.append(frame);
    } else if (copy.kind === 'docx' || copy.kind === 'pptx') {
      const { showOffice } = await import('./office');
      if (!signal.aborted) await showOffice(copy, view, signal);
    } else if (copy.kind === 'model') {
      const canvas = document.createElement('canvas');
      view.append(canvas);
      const { showModel } = await import('./model');
      if (!signal.aborted) await showModel(copy.body, canvas, signal);
    } else {
      const mime = copy.body.type;
      const url = URL.createObjectURL(copy.body);
      signal.addEventListener('abort', () => URL.revokeObjectURL(url), { once: true });
      const el = document.createElement(
        mime.startsWith('image/')
          ? 'img'
          : mime.startsWith('video/')
            ? 'video'
            : mime.startsWith('audio/')
              ? 'audio'
              : 'iframe',
      );
      if (el instanceof HTMLMediaElement) el.controls = true;
      if (el instanceof HTMLIFrameElement && mime !== 'application/pdf') return;
      el.title = copy.title;
      el.src = url;
      view.append(el);
    }
  }
  const cursor = copies().index('at').openCursor(null, 'prev');
  cursor.onsuccess = () => {
    const row = cursor.result;
    if (row) {
      const { id, title } = row.value as Copy;
      button(nav, title, () => void open(id).catch(error));
      row.continue();
    }
  };
  const chat = indexedDB.open('hx-next:' + person);
  chat.onupgradeneeded = () => chat.transaction?.abort();
  chat.onsuccess = async () => {
    const chatDb = chat.result;
    try {
      if (!chatDb.objectStoreNames.contains('feeds')) return;
      for (const feed of await request<CachedFeed[]>(
        chatDb.transaction('feeds').objectStore('feeds').getAll(),
      )) {
        const entries = feed.entries.filter((e) => e.t === 'say' || e.t === 'user');
        if (!entries.length) continue;
        const title = entries.find((e) => e.t === 'user')?.text.slice(0, 80) || feed.convId;
        button(nav, title, () => {
          const signal = reset();
          for (const e of entries) {
            const p = document.createElement('pre');
            p.textContent = e.t === 'say' ? e.md : e.text;
            view.append(p);
          }
          void editChatDraft(chatDb, feed.convId, signal).catch(error);
        });
      }
    } catch (e) {
      error(e);
    }
  };
  view.textContent = msg(
    'Choose a saved item. Open documents online or save a design from its download menu.',
    'Выбери сохранённый материал. Открой документ онлайн или сохрани макет через меню скачивания.',
  );
}
void init().catch(error);
