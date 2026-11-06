---
name: task-pulse
description: Show a live card of a tracker task in the chat — its status, assignee, the «Готово когда» checklist, the merge requests that name it and their pipelines — and change it in place on the next look. Use when the person names a task key and asks how it is going, whether it is done or its CI passed, or writes /pulse KEY.
---

# Карточка задачи

Скрипты лежат в папке этого скилла (её путь написан выше, «Base directory for this skill»). Обозначим её `$SKILL`.

1. Собери данные и покажи карточку **одной командой** (так лента рисует виджет под этим шагом):

   ```sh
   K=HRN-784; F="${TMPDIR:-/tmp}/pulse-$K.json"
   harnessly secret run --env GITLAB_TOKEN=gitlab-token -- python3 -I "$SKILL/pulse.py" "$K" > "$F" \
     || python3 -I "$SKILL/pulse.py" "$K" > "$F"; \
   harnessly widget render --title "$K" --id "task-$(printf %s "$K" | tr A-Z a-z)" \
     --html @"$SKILL/widget/card.html" --css @"$SKILL/widget/card.css" --js @"$SKILL/widget/card.js" --data @"$F"
   ```

   Первая половина берёт токен GitLab из сейфа; если его там нет, скрипт идёт без него и карточка покажет только задачу.
   Ответ последней строкой: `widget task-hrn-784 shown` (новая карточка) или `... changed` (та же, обновлена на месте).

2. Скрипт печатает JSON. Поле `error` — задача не прочиталась (нет подключения трекера, нет такой задачи): скажи это
   человеку словами, карточку с ошибкой всё равно покажи — она объяснит, что не так.

3. Следить: повторяй ту же команду с тем же `--id` через `sleep 60`, пока у всех `mrs[].pipeline.status` не будет
   `success`, `failed`, `canceled` или `skipped`, но не дольше 15 повторов.

Карточка только читает. В трекер и GitLab ничего не пиши.
