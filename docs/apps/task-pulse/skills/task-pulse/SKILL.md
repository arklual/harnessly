---
name: task-pulse
description: Show a live card of a tracker task in the chat — its status, assignee, the «Готово когда» checklist, the merge requests that name it and their pipelines — and change it in place on the next look. Use when the person names a task key and asks how it is going, whether it is done or its CI passed, or writes /pulse KEY.
---

# Карточка задачи

1. Покажи карточку **одной командой**, как она написана, без переменных оболочки и без `; echo`. Вместо `<папка скилла>`
   подставь путь из «Base directory for this skill» выше, вместо ключа — ключ задачи заглавными:

   ```sh
   python3 -I <папка скилла>/pulse.py --show HRN-784
   ```

   Скрипт сам читает задачу, берёт токен GitLab из сейфа (`gitlab-token`, если он там есть) и рисует виджет
   `task-<ключ строчными>` в ленте под этим шагом. Первая строка ответа — `widget task-hrn-784 shown` (новая карточка) или
   `... changed` (та же, обновлена на месте), вторая — сводка: статус, пункты «Готово когда», MR и их пайплайны.

2. Сводка начинается с `error:` — задача не прочиталась (нет подключения трекера, нет такой задачи): скажи это
   человеку словами; карточка в ленте объяснит, что не так. `MR: no gitlab-token` — токена в сейфе нет.

3. Следить: повторяй ту же команду раз в минуту (между запусками — `sleep 60` отдельной командой), пока во второй строке
   у всех MR не будет `success`, `failed`, `canceled` или `skipped`, но не дольше 15 повторов.

Карточка только читает. В трекер и GitLab ничего не пиши.
