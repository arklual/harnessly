# arklual/harnessly: свои патчи поверх ly-solutions/harnessly

Ветка `klodik`: релизный тег апстрима и сверху наши коммиты. Старые базы лежат ветками `klodik-<версия>`, а то, что
реально стояло на машине, отмечено тегами `klodik-v<версия>-fish.<n>`.

Патчи (на `v0.16.42` от 31.10):
- `fix(tmux)`: `exec` перед login shell в `shellLaunchCommand`. Под fish без него агенты не стартуют
  («agent launch did not come up»).
- `fix(voice)`: `RUNTIME_VOICE_LANG` важнее языка, который присылает страница (иначе русская речь переводится на
  английский), и быстрые промежуточные проходы с `audio_ctx`.
- `feat(brand)`: название klodik.tech (по умолчанию `RUNTIME_APP_NAME`, заголовок, manifest, вход, боковая панель) и
  значок Claude (оранжевая «искра» на тёмной плитке) вместо «h.». Команда `harnessly`, пути `~/.harnessly` и API
  не переименованы.
- `ci(klodik-web)`: страницу собирает GitHub Actions (на push в любую ветку, кроме main), артефакт `web-dist`.
  На машине для `node_modules` нет места.
- `feat(projects)`: удаление проекта (`project.delete {id, chats: keep|delete}`, событие `project_removed`): чаты
  уходят в общий список или удаляются вместе с ним. Кнопка «Удалить проект» в настройках проекта.
- `feat(chrome)`: Chrome, которым управляет chrome MCP, вживую во вкладке «Chrome агента» правой панели
  (`internal/chromeview`, сокет `/api/chrome`, фича `chrome`). Рантайм берёт CDP из `RUNTIME_CHROME_CDP`
  (по умолчанию `http://127.0.0.1:9222`, `off` выключает); вкладка открывается сама на первый вызов `mcp__*chrome*__`.

Уже в апстриме, поэтому из наших патчей выброшено:
- экранирование промпта для fish (`ForAnyShell`, v0.16.16);
- `-nc` у whisper-server (v0.16.16);
- папка проекта, `project.update` и «Настройки проекта» (наш PR #15, вошёл в v0.16.24).

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
    install -m755 bin/runtime ~/.local/bin/harnessly-runtime        # CLI harnessly берём из релиза как есть
    systemctl --user restart harnessly-runtime                      # KillMode=process: вкладки живут

Без Actions можно вынуть страницу из релизного бинаря (`extract_embed.py <релиз>/runtime dist web/dist`) и прогнать
`python3 -I scripts/klodik-dist.py web/dist`: скрипт ставит наше название и значок прямо в собранные файлы.
