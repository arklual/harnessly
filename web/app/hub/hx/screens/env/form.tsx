// The form of an item: one set of fields per kind, as the board draws them. The draft is text; `model.ts` turns it into
// the catalog's source. Nothing secret is typed here: a token is chosen from the safe by its name.
import { type ReactNode, useRef, useState } from 'react';

import { Icon } from '../../ui';
import { Help } from './bits';
import { Seg } from './bits';
import {
  type Draft,
  type Fields,
  hasBrew,
  runOf,
  secretVar,
  SETTINGS_EXAMPLE,
  SOURCES,
  type RepoRef,
} from './model';
import { lastSeg } from './model';

export type Patch = (p: Partial<Fields>) => void;

const MAX_FILE = 48 * 1024;
const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

export const Field = ({
  label,
  tip,
  hint,
  warn,
  tight,
  children,
}: {
  label?: string;
  tip?: string;
  hint?: string;
  warn?: boolean;
  tight?: boolean;
  children?: ReactNode;
}) => (
  <div className="ev-f" data-tight={tight ? 'true' : undefined}>
    {label && (
      <div className="ev-f__l">
        <span>{label}</span>
        {tip && <Help text={tip} />}
      </div>
    )}
    {children}
    {hint && (
      <div className="ev-f__h" data-warn={warn ? 'true' : undefined}>
        {hint}
      </div>
    )}
  </div>
);

const TextIn = ({
  value,
  onChange,
  placeholder,
  label,
  mono,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  label: string;
  mono?: boolean;
}) => (
  <input
    className={mono ? 'hx-set__input hx-set__input--mono' : 'hx-set__input'}
    placeholder={placeholder}
    value={value}
    aria-label={label}
    onChange={(e) => onChange(e.target.value)}
  />
);

const AreaIn = ({
  value,
  onChange,
  placeholder,
  label,
  rows,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  label: string;
  rows: number;
}) => (
  <textarea
    className={`hx-set__input hx-set__input--mono ev-ta${rows <= 2 ? ' ev-ta--sm' : ''}`}
    rows={rows}
    placeholder={placeholder}
    value={value}
    aria-label={label}
    onChange={(e) => onChange(e.target.value)}
  />
);

const Opts = ({
  items,
  isOn,
  onPick,
  label,
}: {
  items: readonly (readonly [string, string])[];
  isOn: (v: string) => boolean;
  onPick: (v: string) => void;
  label: string;
}) => (
  <div className="ev-opts" role="group" aria-label={label}>
    {items.map(([v, text]) => (
      <button
        key={v}
        type="button"
        className="ev-opt"
        aria-pressed={isOn(v) ? 'true' : 'false'}
        onClick={() => onPick(v)}
      >
        {text}
      </button>
    ))}
  </div>
);

/** A file from the person's disk, read as text on the spot: it is kept in the environment, not uploaded elsewhere. */
const FileDrop = ({
  name,
  title,
  sub,
  accept,
  onText,
}: {
  name: string;
  title: string;
  sub: string;
  accept: string;
  onText: (fileName: string, text: string) => void;
}) => {
  const input = useRef<HTMLInputElement>(null);
  const [err, setErr] = useState<string | null>(null);
  const take = (file: File | undefined) => {
    if (!file) return;
    if (file.size > MAX_FILE) {
      setErr('Файл больше 48 КБ — в окружение такой не положить');
      return;
    }
    file.text().then(
      (text) => {
        setErr(null);
        onText(file.name, text);
      },
      () => setErr('Не получилось прочитать файл'),
    );
  };
  return (
    <>
      <button
        type="button"
        className="ev-drop"
        data-has={name !== '' ? 'true' : undefined}
        onClick={() => input.current?.click()}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault();
          take(e.dataTransfer.files[0]);
        }}
      >
        <Icon i="upload" />
        <b>{name !== '' ? lastSeg(name) : title}</b>
        <small>{name !== '' ? 'нажмите, чтобы заменить' : sub}</small>
      </button>
      <input
        ref={input}
        type="file"
        accept={accept}
        hidden
        onChange={(e) => {
          take(e.target.files?.[0]);
          e.target.value = '';
        }}
      />
      {err && (
        <div className="ev-f__h" data-warn="true">
          {err}
        </div>
      )}
    </>
  );
};

/** «Токены и пароли»: chosen from the safe by name; the value stays in the safe. */
const Keys = ({
  chosen,
  names,
  onChange,
}: {
  chosen: string[];
  names: string[];
  onChange: (v: string[]) => void;
}) => {
  const [pick, setPick] = useState(false);
  const [other, setOther] = useState<string | null>(null);
  const free = names.filter((n) => !chosen.includes(n));
  const add = (n: string) => {
    setPick(false);
    setOther(null);
    onChange([...chosen, n]);
  };
  return (
    <div className="ev-keys">
      {chosen.map((n) => (
        <div className="ev-key" key={n}>
          <Icon i="key-round" size="sm" />
          <span className="ev-key__t">
            <b>${secretVar(n)}</b>
            <small>{n} · из сейфа</small>
          </span>
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            data-shape="icon"
            aria-label="Убрать"
            onClick={() => onChange(chosen.filter((x) => x !== n))}
          >
            <Icon i="x" size="sm" />
          </button>
        </div>
      ))}
      {pick && other === null && (
        <div className="ev-keypick">
          {free.map((n) => (
            <button type="button" className="ev-keypick__i" key={n} onClick={() => add(n)}>
              <b>{n}</b>
              <small>уже используется в окружении</small>
            </button>
          ))}
          <button type="button" className="ev-keypick__i ev-keypick__new" onClick={() => setOther('')}>
            <Icon i="plus" size="sm" />
            Указать другое имя
          </button>
        </div>
      )}
      {other !== null && (
        <div className="ev-keynew">
          <input
            className="hx-set__input hx-set__input--mono"
            placeholder="Имя ключа в сейфе, например figma-token"
            value={other}
            aria-label="Имя ключа"
            autoFocus
            onChange={(e) => setOther(e.target.value.trim())}
          />
          <div className="ev-keynew__f">
            <span>Значение положите в Настройки → Сейф: здесь только имя</span>
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              onClick={() => setOther(null)}
            >
              Отмена
            </button>
            <button
              type="button"
              className="hx-btn"
              data-variant="primary"
              data-size="sm"
              disabled={!NAME_RE.test(other) || chosen.includes(other)}
              onClick={() => add(other)}
            >
              Добавить
            </button>
          </div>
        </div>
      )}
      {other === null && (
        <button
          type="button"
          className="ev-keyadd"
          aria-expanded={pick ? 'true' : 'false'}
          onClick={() => setPick((v) => !v)}
        >
          <Icon i="plus" size="sm" />
          {chosen.length ? 'Ещё ключ' : 'Добавить из сейфа'}
        </button>
      )}
    </div>
  );
};

const SRC_TIP =
  'Скрипт запустится на Linux-машине: bash, apt, curl. Файл в репозитории правят как обычный код, через MR. Команды здесь — для пары строк. Файл и ссылка — если скрипт уже есть готовый';

export const DraftForm = ({
  d,
  set,
  repos,
  secretNames,
  canSecrets,
}: {
  d: Draft;
  set: Patch;
  repos: readonly RepoRef[];
  secretNames: string[];
  /** Only an organization admin connects safe keys to scripts. */
  canSecrets: boolean;
}) => {
  const f = d.f;
  const sources = d.k === 'skill' || d.k === 'mcp' || d.k === 'script' ? SOURCES[d.k] : null;
  const src = sources && (
    <Field label="Откуда" tip={d.k === 'script' ? SRC_TIP : undefined}>
      <Opts items={sources} isOn={(v) => f.src === v} onPick={(v) => set({ src: v })} label="Откуда" />
    </Field>
  );

  if (d.k === 'repo')
    return (
      <>
        <Field label="Адрес репозитория">
          <TextIn
            mono
            value={f.url}
            onChange={(v) => set({ url: v })}
            placeholder="gitlab.example.ru/frontend/web"
            label="Адрес репозитория"
          />
        </Field>
        <Field label="Ветка" hint="Пусто — main">
          <TextIn
            mono
            value={f.branch}
            onChange={(v) => set({ branch: v })}
            placeholder="main"
            label="Ветка"
          />
        </Field>
        <Field label="Папка на машине" hint="Пусто — ~/code/<имя репозитория>">
          <TextIn
            mono
            value={f.dir}
            onChange={(v) => set({ dir: v })}
            placeholder="~/code/web"
            label="Папка на машине"
          />
        </Field>
      </>
    );

  if (d.k === 'access')
    return (
      <>
        <Field label="Сервис">
          <Seg
            items={[
              ['gitlab', 'GitLab'],
              ['github', 'GitHub'],
            ]}
            value={f.svc}
            onChange={(v) => set({ svc: v })}
            label="Сервис"
          />
        </Field>
        <Field
          label="Адрес"
          hint="Каждый войдёт своим аккаунтом — токен ляжет в его личный сейф. Пусто — gitlab.com или github.com"
        >
          <TextIn
            mono
            value={f.host}
            onChange={(v) => set({ host: v })}
            placeholder={f.svc === 'github' ? 'github.com' : 'gitlab.example.ru'}
            label="Адрес"
          />
        </Field>
      </>
    );

  if (d.k === 'skill')
    return (
      <>
        {src}
        {f.src === 'link' && (
          <Field
            label="Ссылка на папку со скиллом"
            hint="Папка, где лежит SKILL.md: в GitLab, GitHub или по любой ссылке. Поменяете файл там — у всех обновится само"
          >
            <TextIn
              mono
              value={f.url}
              onChange={(v) => set({ url: v })}
              placeholder="gitlab.example.ru/org/skills/how-we-work"
              label="Ссылка на папку со скиллом"
            />
          </Field>
        )}
        {f.src === 'file' && (
          <Field label="Файл">
            <FileDrop
              name={f.fileName}
              title="Перетащите SKILL.md"
              sub="Текстовый файл скилла до 48 КБ"
              accept=".md,.txt,text/markdown,text/plain"
              onText={(fileName, body) => set({ fileName, body })}
            />
          </Field>
        )}
        {f.src === 'text' && (
          <Field label="Текст скилла" hint="Это и есть SKILL.md. Поменяете здесь — у всех обновится само">
            <AreaIn
              rows={7}
              value={f.body}
              onChange={(v) => set({ body: v })}
              placeholder={
                '---\nname: ui-copy\ndescription: Когда агенту это нужно\n---\n\nКак у нас принято…'
              }
              label="Текст скилла"
            />
          </Field>
        )}
        <Field label="Название" hint={f.src === 'text' ? undefined : 'Пусто — возьмём из источника'}>
          <TextIn
            value={f.name}
            onChange={(v) => set({ name: v })}
            placeholder={f.src === 'text' ? 'Тексты интерфейса' : 'Как мы работаем'}
            label="Название"
          />
        </Field>
        <Field label="Коротко, о чём">
          <TextIn
            value={f.about}
            onChange={(v) => set({ about: v })}
            placeholder="задачи, ветки, ревью"
            label="Коротко, о чём"
          />
        </Field>
      </>
    );

  if (d.k === 'mcp') {
    const remote = f.src === 'url';
    return (
      <>
        {src}
        {remote && (
          <Field label="Адрес" hint="Удалённый MCP-сервер: машина подключится к нему по сети">
            <TextIn
              mono
              value={f.url}
              onChange={(v) => set({ url: v })}
              placeholder="https://mcp.figma.com/mcp"
              label="Адрес"
            />
          </Field>
        )}
        {f.src === 'cmd' && (
          <Field
            label="Команда запуска"
            hint="Как в инструкции сервиса: npx …, uvx …, docker run … Сервер запустится на машине"
          >
            <TextIn
              mono
              value={f.cmd}
              onChange={(v) => set({ cmd: v })}
              placeholder="npx @playwright/mcp@latest"
              label="Команда запуска"
            />
          </Field>
        )}
        {f.src === 'json' && (
          <Field
            label="Конфиг"
            hint="Кусок mcp.json из документации сервиса. Токены сюда не пишите — выберите ключ из сейфа ниже"
          >
            <AreaIn
              rows={6}
              value={f.json}
              onChange={(v) => set({ json: v })}
              placeholder={
                '{\n  "mcpServers": {\n    "linear": { "url": "https://mcp.linear.app/mcp" }\n  }\n}'
              }
              label="Конфиг"
            />
          </Field>
        )}
        <Field label="Название" hint="Пусто — возьмём из адреса">
          <TextIn value={f.name} onChange={(v) => set({ name: v })} placeholder="Figma" label="Название" />
        </Field>
        <Field label={remote ? 'Вход' : 'Ключ'}>
          <Seg
            items={
              remote
                ? ([
                    ['own', 'Своим аккаунтом'],
                    ['shared', 'Общий ключ'],
                    ['none', 'Без входа'],
                  ] as const)
                : ([
                    ['none', 'Не нужен'],
                    ['shared', 'Общий из сейфа'],
                    ['own', 'Свой у каждого'],
                  ] as const)
            }
            value={f.auth}
            onChange={(v) => set({ auth: v })}
            label={remote ? 'Вход' : 'Ключ'}
          />
        </Field>
        {f.auth === 'shared' && (
          <Field label="Ключ из сейфа" hint="В окружении только имя ключа, сам ключ остаётся в сейфе">
            <Opts
              items={secretNames.map((n) => [n, n] as const)}
              isOn={(v) => f.secret === v}
              onPick={(v) => set({ secret: v })}
              label="Ключ из сейфа"
            />
            <TextIn
              mono
              value={f.secret}
              onChange={(v) => set({ secret: v.trim() })}
              placeholder="postgres-test"
              label="Имя ключа"
            />
          </Field>
        )}
      </>
    );
  }

  if (d.k === 'app') {
    const otherRepo = f.repoOther || repos.length === 0;
    return (
      <>
        {repos.length > 0 && (
          <Field
            label="Репозиторий"
            tip="Где лежит приложение. Машина склонирует репозиторий и поставит приложение из его папки. Репозитория нет в окружении — выберите «Другой» и вставьте ссылку"
          >
            <Opts
              items={[...repos.map((r) => [r.id, r.name] as const), ['__other', 'Другой'] as const]}
              isOn={(v) => (v === '__other' ? otherRepo : !otherRepo && f.repo === v)}
              onPick={(v) =>
                v === '__other' ? set({ repo: '', repoOther: true }) : set({ repo: v, repoOther: false })
              }
              label="Репозиторий"
            />
          </Field>
        )}
        {otherRepo && (
          <Field
            label={repos.length === 0 ? 'Ссылка на репозиторий' : undefined}
            tight={repos.length > 0}
            hint="Склонируем только чтобы взять приложение — в окружение репозиторий не добавится"
          >
            <TextIn
              mono
              value={f.repoUrl}
              onChange={(v) => set({ repoUrl: v })}
              placeholder="gitlab.example.ru/design/kit"
              label="Ссылка на репозиторий"
            />
          </Field>
        )}
        {otherRepo && (
          <Field label="Ветка" hint="Пусто — ветка по умолчанию">
            <TextIn
              mono
              value={f.branch}
              onChange={(v) => set({ branch: v })}
              placeholder="main"
              label="Ветка"
            />
          </Field>
        )}
        <Field
          label="Папка приложения"
          tip="Папка, в которой лежит harnessly/app.json"
          hint="Путь внутри репозитория"
        >
          <TextIn
            mono
            value={f.path}
            onChange={(v) => set({ path: v })}
            placeholder="apps/design-kit"
            label="Папка приложения"
          />
        </Field>
        <Field label="Название" hint="Пусто — из имени папки">
          <TextIn
            value={f.name}
            onChange={(v) => set({ name: v })}
            placeholder="Дизайн-система"
            label="Название"
          />
        </Field>
        <Field
          label="В организации"
          hint="Выключенное пропадает из меню и у агента у всех; доски, документы и ссылки на них остаются"
        >
          <Seg
            items={[
              ['on', 'Включено'],
              ['off', 'Выключено у всех'],
            ]}
            value={f.appOff ? 'off' : 'on'}
            onChange={(v) => set({ appOff: v === 'off' })}
            label="В организации"
          />
        </Field>
      </>
    );
  }

  if (d.k === 'plugin')
    return (
      <>
        <Field label="Плагин" hint="Имя и маркетплейс через @">
          <TextIn
            mono
            value={f.plugin}
            onChange={(v) => set({ plugin: v.trim() })}
            placeholder="review-kit@team-plugins"
            label="Плагин"
          />
        </Field>
        <Field label="Маркетплейс" hint="Адрес репозитория маркетплейса или owner/repo на GitHub">
          <TextIn
            mono
            value={f.marketplace}
            onChange={(v) => set({ marketplace: v })}
            placeholder="gitlab.example.ru/ai/team-plugins"
            label="Маркетплейс"
          />
        </Field>
        <Field label="Название" hint="Пусто — из имени плагина">
          <TextIn
            value={f.name}
            onChange={(v) => set({ name: v })}
            placeholder="Ревью кода"
            label="Название"
          />
        </Field>
      </>
    );

  if (d.k === 'claude-settings')
    return (
      <>
        <Field
          label="Настройки"
          tip="always — ключи settings.json, которые ставятся всегда; ifAbsent — только если у человека их нет; claudeMd — правила, которые допишем в ~/.claude/CLAUDE.md"
        >
          <AreaIn
            rows={8}
            value={f.json}
            onChange={(v) => set({ json: v })}
            placeholder={SETTINGS_EXAMPLE}
            label="Настройки"
          />
        </Field>
        <Field label="Название" hint="Пусто — «Настройки Claude»">
          <TextIn
            value={f.name}
            onChange={(v) => set({ name: v })}
            placeholder="Экономия Claude"
            label="Название"
          />
        </Field>
      </>
    );

  // a script
  const brew = hasBrew(f.body);
  const brewRun = hasBrew(f.run);
  const other = f.repoOther || repos.length === 0;
  const run = runOf(f);
  return (
    <>
      {src}
      {f.src === 'repo' && (
        <>
          {repos.length > 0 && (
            <Field
              label="Репозиторий"
              tip="Где лежит файл скрипта. Машина склонирует репозиторий и запустит скрипт из его папки. Его нет в окружении — выберите «Другой» и вставьте ссылку"
            >
              <Opts
                items={[...repos.map((r) => [r.id, r.name] as const), ['__other', 'Другой'] as const]}
                isOn={(v) => (v === '__other' ? other : !other && f.repo === v)}
                onPick={(v) =>
                  v === '__other' ? set({ repo: '', repoOther: true }) : set({ repo: v, repoOther: false })
                }
                label="Репозиторий"
              />
            </Field>
          )}
          {other && (
            <Field
              label={repos.length === 0 ? 'Ссылка на репозиторий' : undefined}
              tip={
                repos.length === 0
                  ? 'Где лежит файл скрипта. Машина склонирует репозиторий и запустит скрипт из его папки'
                  : undefined
              }
              tight={repos.length > 0}
              hint="Склонируем только чтобы запустить скрипт — в окружение он не добавится"
            >
              <TextIn
                mono
                value={f.repoUrl}
                onChange={(v) => set({ repoUrl: v })}
                placeholder="gitlab.example.ru/frontend/dev-scripts"
                label="Ссылка на репозиторий"
              />
            </Field>
          )}
          <Field label="Путь к файлу в репозитории">
            <TextIn
              mono
              value={f.path}
              onChange={(v) => set({ path: v })}
              placeholder="scripts/setup-dev.sh"
              label="Путь к файлу в репозитории"
            />
          </Field>
        </>
      )}
      {f.src === 'text' && (
        <Field
          label="Команды"
          tip="Выполнятся по порядку в bash, каждая с новой строки. Упала одна — дальше не идём, человек увидит ошибку у себя на главной"
          hint={
            brew
              ? 'brew на Linux-машинах нет — поставьте через apt или curl'
              : 'Только для Linux. Токены и пароли сюда не пишите — их выберите ниже'
          }
          warn={brew}
        >
          <AreaIn
            rows={5}
            value={f.body}
            onChange={(v) => set({ body: v })}
            placeholder={
              'curl -fsSL https://fnm.vercel.app/install | bash\nfnm install 22\ncorepack enable pnpm'
            }
            label="Команды"
          />
        </Field>
      )}
      {f.src === 'file' && (
        <Field
          label="Файл"
          tip="Файл хранится в окружении. Новая версия — загрузите заново, машины подтянут сами"
          hint={brew ? 'brew на Linux-машинах нет — поставьте через apt или curl' : undefined}
          warn={brew}
        >
          <FileDrop
            name={f.fileName}
            title="Перетащите файл скрипта или нажмите"
            sub=".sh, .py, .js — для Linux"
            accept=".sh,.bash,.py,.js,.mjs,text/*"
            onText={(fileName, body) => set({ fileName, body })}
          />
        </Field>
      )}
      {f.src === 'link' && (
        <Field
          label="Ссылка на скрипт"
          tip="Машина скачает файл по ссылке перед запуском. Поменяете файл там — в следующий раз скачает новый"
        >
          <TextIn
            mono
            value={f.url}
            onChange={(v) => set({ url: v })}
            placeholder="https://gitlab.example.ru/-/snippets/12/raw"
            label="Ссылка на скрипт"
          />
        </Field>
      )}
      {f.src !== 'text' && (
        <Field
          label="Как запустить"
          tip={`Команда, которой машина запустит файл${f.src === 'repo' ? ', из папки репозитория' : ''}. Допишите аргументы (--fast), переменные (ENV=dev) или замените bash на zsh, python3, node. Можно несколько строк — выполнятся по порядку`}
          hint={
            brewRun
              ? 'brew на Linux-машинах нет — поставьте через apt или curl'
              : run
                ? `Пусто — ${run}`
                : undefined
          }
          warn={brewRun}
        >
          <AreaIn
            rows={2}
            value={f.run}
            onChange={(v) => set({ run: v })}
            placeholder={run || 'bash setup.sh'}
            label="Как запустить"
          />
        </Field>
      )}
      <Field
        label="Токены и пароли"
        tip="Если скрипту нужен токен или пароль, его не пишут в сам скрипт. Он лежит в сейфе (Настройки → Сейф), а здесь выбирают его по имени. Перед запуском машина подставит значение в переменную окружения, в логах оно скрыто"
        hint={f.secrets.length || !canSecrets ? undefined : 'Не нужны — пропустите'}
      >
        {canSecrets ? (
          <Keys chosen={f.secrets} names={secretNames} onChange={(v) => set({ secrets: v })} />
        ) : (
          <span className="ev-f__h">Токены из сейфа подключает админ организации</span>
        )}
      </Field>
      <Field
        label="Запускать"
        tip="Один раз — на каждой машине запустим один раз и больше не трогаем. Если не готово — сначала проверим: готово — пропустим, нет — запустим снова, например на новой машине или если что-то сломалось"
      >
        <Seg
          items={[
            ['once', 'Один раз'],
            ['check', 'Если не готово'],
          ]}
          value={f.rerun}
          onChange={(v) => set({ rerun: v })}
          label="Запускать"
        />
      </Field>
      {f.rerun === 'check' && (
        <Field tight hint="Команда-проверка: проходит — уже готово, скрипт не запускаем">
          <TextIn
            mono
            value={f.check}
            onChange={(v) => set({ check: v })}
            placeholder="test -f ~/code/web/.env.local"
            label="Проверка"
          />
        </Field>
      )}
      <Field label="Название" hint={f.src === 'text' ? undefined : 'Пусто — из имени файла'}>
        <TextIn
          value={f.name}
          onChange={(v) => set({ name: v })}
          placeholder={f.src === 'text' ? 'Node и pnpm' : 'Настройка web'}
          label="Название"
        />
      </Field>
    </>
  );
};
