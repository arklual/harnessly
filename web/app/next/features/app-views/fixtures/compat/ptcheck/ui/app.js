const state = document.getElementById('state');
const step = (t) => { state.textContent = t; };
window.addEventListener('error', (e) => step('ошибка: ' + e.message));
window.addEventListener('unhandledrejection', (e) => step('ошибка: ' + (e.reason?.code ?? '') + ' ' + (e.reason?.message ?? e.reason)));
step('подключаюсь');
(async () => {
  const hl = await window.harnessly.connect();
  step('подключено');
  navigator.modelContext.registerTool({
    name: 'echo',
    description: 'Возвращает переданный текст и время страницы',
    inputSchema: { type: 'object', properties: { text: { type: 'string' } }, required: ['text'] },
    execute: async ({ text }) => {
      step('вызван echo: ' + text);
      return { echoed: text, at: new Date().toISOString() };
    },
  });
  navigator.modelContext.registerTool({
    name: 'wipe',
    description: 'Очищает проверочный список',
    annotations: { destructiveHint: true },
    inputSchema: { type: 'object', properties: { list: { type: 'string' } } },
    execute: async ({ list }) => {
      step('очищено: ' + list);
      return { wiped: list };
    },
  });
  await hl.publish({ shown: 'проверочная страница', step: 1 });
  step('тулы зарегистрированы: echo, wipe');
})().catch((e) => step('ошибка: ' + (e?.code ?? '') + ' ' + (e?.message ?? e)));
