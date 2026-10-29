# arklual/harnessly: свои патчи поверх ly-solutions/harnessly

Ветка `klodik`: релизный тег апстрима и сверху наши коммиты. Ветки называются `klodik-<версия>` для старых баз,
теги `klodik-v<версия>-fish.<n>` для того, что реально стояло на машине.

Патчи (на v0.16.16):
- `fix(tmux)`: `exec` перед login shell в `shellLaunchCommand`. Под fish без него агенты не стартуют
  («agent launch did not come up»).
- `fix(voice)`: `RUNTIME_VOICE_LANG` важнее языка, который присылает страница (иначе русская речь переводится на
  английский), и быстрые промежуточные проходы с `audio_ctx`.
- `feat(brand)`: название klodik.tech (по умолчанию `RUNTIME_APP_NAME`, заголовок, manifest, вход, боковая панель) и
  значок Claude (оранжевая «искра» на тёмной плитке) вместо «h.». Команда `harnessly`, пути `~/.harnessly` и API
  не переименованы.
- `feat(brand)`: название klodik (по умолчанию `RUNTIME_APP_NAME`, заголовок, manifest) и значок Claude (оранжевая
  «искра» на тёмной плитке) вместо «h.». Команда `harnessly`, пути `~/.harnessly` и API не переименованы.

Уже в апстриме, из наших патчей выброшено: экранирование промпта для fish (`ForAnyShell`, v0.16.16) и `-nc` у
whisper-server (v0.16.16).

## Обновление

    git fetch upstream --tags
    git checkout -b klodik-new vX.Y.Z && git cherry-pick <наши коммиты>   # или git rebase --onto vX.Y.Z <старый тег> klodik
    # web/dist не собираем, вынимаем из релизного бинаря:
    gh release download vX.Y.Z -R ly-solutions/harnessly -p 'harnessly_X.Y.Z_linux_amd64.tar.gz' -p checksums.txt
    python3 -I ~/.local/share/harnessly-hotfix/extract_embed.py <релиз>/runtime dist web/dist
    python3 -I scripts/klodik-dist.py web/dist    # наше название и значок в собранную страницу
    python3 -I scripts/klodik-dist.py web/dist    # наше название и значок в собранную страницу
    systemd-run --user --scope -p MemoryMax=1800M -- \
      env CGO_ENABLED=0 go build -p 2 -trimpath -tags webui \
      -ldflags "-s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=X.Y.Z+fish.1" \
      -o bin/runtime ./cmd/runtime
    install -m755 bin/runtime ~/.local/bin/harnessly-runtime        # CLI harnessly берём из релиза как есть
    systemctl --user restart harnessly-runtime                      # KillMode=process: вкладки живут
