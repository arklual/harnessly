# Кешбэки

Пример приложения с видом (шаблон `pane-chat`). Человек выбирает до трёх категорий банковского кешбэка на месяц;
выбор лежит в хранилище чата (`hl.storage.chat`, ключ `picks:<ГГГГ-ММ>`), агент читает и пишет его инструментами
хоста (`app_data_get`, `app_data_set`), показывает окно через `app_open`, а перед заменой выбора спрашивает
через `app_ask`. Выбор уходит в чат кнопкой (`hl.chat.send`).

Банка за приложением нет: категории лежат в `ui/cashback-data.js`. Виды работают на классических скриптах: фрейм вида
живёт на непрозрачном origin и несёт cookie слота только со скриптами, стилями и картинками, но не с модулями и
CORS-загрузками (`fetch` без учётных данных).

```sh
harnessly app validate docs/apps/cashback --strict
harnessly app dev docs/apps/cashback
harnessly app enable harnessly.cashback
```
