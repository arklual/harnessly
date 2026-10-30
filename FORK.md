# arklual/harnessly: свои патчи поверх ly-solutions/harnessly

Ветка `klodik`: релизный тег апстрима и сверху наши коммиты. Старые базы лежат ветками `klodik-<версия>`, а то, что
реально стояло на машине, отмечено тегами `klodik-v<версия>-fish.<n>`.

Патчи (на v0.16.25):
- `fix(tmux)`: `exec` перед login shell в `shellLaunchCommand`. Под fish без него агенты не стартуют
  («agent launch did not come up»).
- `fix(voice)`: `RUNTIME_VOICE_LANG` важнее языка, который присылает страница (иначе русская речь переводится на
  английский), и быстрые промежуточные проходы с `audio_ctx`.
- `feat(brand)`: название klodik.tech (по умолчанию `RUNTIME_APP_NAME`, заголовок, manifest, вход, боковая панель) и
  значок Claude (оранжевая «искра» на тёмной плитке) вместо «h.». Команда `harnessly`, пути `~/.harnessly` и API
  не переименованы.
- `ci(klodik-web)`: страницу собирает GitHub Actions (на push в любую ветку, кроме main), артефакт `web-dist`.
  На машине для `node_modules` нет места.

Уже в апстриме, поэтому из наших патчей выброшено:
- экранирование промпта для fish (`ForAnyShell`, v0.16.16);
- `-nc` у whisper-server (v0.16.16);
- папка проекта, `project.update` и «Настройки проекта» (наш PR #15, вошёл в v0.16.24).

## Обновление

    git fetch upstream --tags
    git checkout -b klodik-new vX.Y.Z && git cherry-pick <наши коммиты>
    git push origin klodik-new                       # Actions собирает страницу
    gh run watch -R arklual/harnessly <id>
    rm -rf web/dist && gh run download <id> -R arklual/harnessly -n web-dist -D web/dist
    systemd-run --user --scope -p MemoryMax=1800M -- \
      env CGO_ENABLED=0 go build -p 2 -trimpath -tags webui \
      -ldflags "-s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=X.Y.Z+fish.1" \
      -o bin/runtime ./cmd/runtime
    install -m755 bin/runtime ~/.local/bin/harnessly-runtime        # CLI harnessly берём из релиза как есть
    systemctl --user restart harnessly-runtime                      # KillMode=process: вкладки живут

Без Actions можно вынуть страницу из релизного бинаря (`extract_embed.py <релиз>/runtime dist web/dist`) и прогнать
`python3 -I scripts/klodik-dist.py web/dist`: скрипт ставит наше название и значок прямо в собранные файлы.
