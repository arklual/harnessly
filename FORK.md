# arklual/harnessly: свои патчи поверх ly-solutions/harnessly

Ветка `klodik`: релизный тег апстрима и сверху наши коммиты. Старые базы лежат ветками `klodik-<версия>`, а то, что
реально стояло на машине, отмечено тегами `klodik-v<версия>-fish.<n>`.

База: `upstream/main` f8bd105e (11 коммитов после v0.16.200, 2026-11-10; тега ещё нет), версия
`0.16.200+fish.<n>.main.f8bd105e`. Без дефиса: `X.Y.Z-что-то` по semver предрелиз, меньше `X.Y.Z`, и страница
предлагает «обновиться» до той же базы. Прошлые базы: v0.16.200 — тег `klodik-v0.16.200-fish.1` и ветка
`klodik-keep-views`; v0.16.187 — `klodik-v0.16.187-fish.1`; `upstream/main` 3aba1cc — ветка `klodik-0.16.124-main`;
v0.16.154 (откачен из-за багов) — `klodik-0.16.154`; v0.16.86 — `klodik-0.16.86-browser`.

Патчи:
- `fix(tmux)`: `exec` перед login shell в `shellLaunchCommand`. Под fish без него агенты не стартуют
  («agent launch did not come up»).
- `fix(voice)`: `RUNTIME_VOICE_LANG` важнее языка, который присылает страница (иначе русская речь переводится на
  английский), и быстрые промежуточные проходы с `audio_ctx`.
- `feat(brand)`: название klodik.tech (по умолчанию `RUNTIME_APP_NAME`, заголовок, manifest, вход, боковая панель) и
  значок Claude (оранжевая «искра» на тёмной плитке) вместо «h.». Команда `harnessly`, пути `~/.harnessly` и API
  не переименованы.
- `ci(klodik-web)`: страницу собирает GitHub Actions (на push в любую ветку, кроме main), артефакт `web-dist`.
  Можно и локально: `~/.local/opt/node24`, `corepack pnpm install && corepack pnpm build` в `web/`.
- `feat(projects)`: удаление проекта (`project.delete {id, chats: keep|delete}`, событие `project_removed`): чаты
  уходят в общий список или удаляются вместе с ним. Кнопка «Удалить проект» в настройках проекта.
- `feat(tracker)`: вкладки агентов получают `YOUTRACK_TOKEN`, когда задан `RUNTIME_TRACKER_URL` (`services.AgentEnv`,
  токен читается при каждом запуске вкладки). Так Codex ходит в MCP-сервер трекера через `bearer_token_env_var`.
- `feat(app-views)` (для приложения Коворкинг, `~/apps/klodik.coworking`): `chat-handoff.ts`. `pane.open("chat:<id>")`
  со страницы приложения меняет чат рядом с видом или отправляет его через BroadcastChannel на страницу, где был фокус;
  `resume:`/`new:`/`stop:` запускают и останавливают работу; вид знает чат рядом с собой (`context.shown`). С v0.16.187
  `chat:` есть и в апстриме (`agents.ts`, право `agents.read`): проверку и запасной `openConversation` берём оттуда.

Отправлены в апстрим, пока открыты (PR ly-solutions/harnessly #62–64):
- `feat(app-views)`: вид приложения во вкладке панели и на своей странице приложения продолжает работать, пока его не
  видно (другой экран, другая вкладка).
- `fix(limits)`: карточка лимитов в узком поле открывается целиком, у поля внизу страницы приложения есть чип лимитов.
- `feat(app-main)`: окно чата над приложением растягивается за любой край и угол.

Заменено браузером агента:
- `feat(chrome)` (вкладка «Chrome агента», `internal/chromeview`): её заменила вкладка браузера со своими профилями.
  `kaplya-chrome.service` и `chrome` в `~/.claude.json` пока работают, но в панели этот Chrome больше не виден.

Уже в апстриме, поэтому из наших патчей выброшено:
- экранирование промпта для fish (`ForAnyShell`, v0.16.16);
- `-nc` у whisper-server (v0.16.16);
- папка проекта, `project.update` и «Настройки проекта» (наш PR #15, вошёл в v0.16.24);
- файлы, которые не картинки, доходят до агента (`attach.UploadItem`, v0.16.86);
- браузер агента (наш PR #22, вошёл в main 2026-11-06): инструменты агента теперь `chrome_*`, а `browser_*`
  принадлежат браузеру панели (`internal/browseract`); вместе с ним пришли ответ на вопрос о доверии папке по одному
  шагу (`fix(status)`), CLI в чате своей tmux-сессии и привязки проектов при загрузке каталога;
- куки preview-слота без Partitioned, когда слоты одного сайта со страницей (p1…p8.klodik.tech рядом с
  harnessly.klodik.tech): наш `sameSite()`, в апстриме `sameSiteOrigins` (v0.16.187);
- модель диктовки «Подписка ChatGPT» (`internal/voice/cloud/chatgpt.go`, наш PR #20, вошёл в main 2026-11-06).

## Обновление

    git fetch upstream --tags
    git fetch upstream main:refs/remotes/upstream/main   # refspec клона берёт только теги: main забирать явно
    git checkout -b klodik-new vX.Y.Z && git cherry-pick <наши коммиты>   # или upstream/main, если тега ещё нет
    git push origin klodik-new                       # Actions собирает страницу
    gh run watch -R arklual/harnessly <id>
    rm -rf web/dist && gh run download <id> -R arklual/harnessly -n web-dist -D web/dist
    systemd-run --user --scope -p MemoryMax=1800M -- \
      env CGO_ENABLED=0 go build -p 2 -trimpath -tags webui \
      -ldflags "-s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=X.Y.Z+fish.1" \
      -o bin/runtime ./cmd/runtime
    # без тега версия X.Y.Z-main.<sha>+fish.1: она меньше X.Y.Z, и выход релиза проверка обновлений покажет
    install -m755 bin/runtime ~/.local/bin/harnessly-runtime
    go build -trimpath -ldflags "-s -w -X github.com/ly-solutions/harnessly/internal/hlcli.Version=X.Y.Z+fish.1" \
      -o bin/harnessly ./cmd/harnessly                               # браузеру нужна команда `harnessly browser`
    install -m755 bin/harnessly ~/.local/bin/harnessly
    systemctl --user restart harnessly-runtime                      # KillMode=process: вкладки живут

Без Actions можно вынуть страницу из релизного бинаря (`extract_embed.py <релиз>/runtime dist web/dist`) и прогнать
`python3 -I scripts/klodik-dist.py web/dist`: скрипт ставит наше название и значок прямо в собранные файлы.
