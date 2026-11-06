// «Добавить»: what to add, then the kind's page (from the organization's catalog or a new one), or a pasted link.
import { useRef, useState } from 'react';

import type { CatalogItem } from '../../../../hub-api/env';
import { Btn, Icon } from '../../ui';
import { Search, Seg } from './bits';
import type { SheetCtx, SheetMode } from './ctx';
import { BAD, detect, detWord } from './detect';
import {
  blankDraft,
  type Draft,
  type Fields,
  ADD_KINDS,
  ADMIN_KINDS,
  KIND,
  lastSeg,
  valid,
  viewOf,
} from './model';
import { fitsLayer } from './actions';
import { ItemForm } from './itemform';
import { useTry } from './sandbox';
import { haveItem, metaOf, same } from './scope';
import { Foot, Title } from './sheet';
import { ScopePicker } from './picker';
import { Slot } from './sheet';

const To = ({ ctx }: { ctx: SheetCtx }) => (
  <Slot name="to">
    <div className="ev-to">
      <span>Куда</span>
      <ScopePicker
        view={ctx.view}
        role={ctx.role}
        target={ctx.target}
        onPick={ctx.setTarget}
        variant="sheet"
        label="Куда добавить"
      />
    </div>
  </Slot>
);

export const AddHome = ({ ctx }: { ctx: SheetCtx }) => {
  const ways = [
    ...(ctx.canCopy
      ? [
          {
            label: 'Скопировать из другой команды',
            about: 'Возьмём то, что уже настроено у соседей: всё или только нужное',
            icon: 'copy',
            go: () => ctx.go({ mode: 'copy' }),
          },
        ]
      : []),
    {
      label: 'Ссылкой, командой или файлом',
      about: 'Вставьте ссылку, команду установки, конфиг MCP или файл — сами поймём, что это',
      icon: 'link',
      go: () => ctx.go({ mode: 'add', step: 'link' }),
    },
  ];
  return (
    <>
      <Title text="Что добавить" />
      <To ctx={ctx} />
      <div className="ev-pick">
        {ADD_KINDS.filter((k) => !ADMIN_KINDS.includes(k) || ctx.orgAdmin).map((k) => (
          <button
            type="button"
            className="ev-pick__i"
            key={k}
            onClick={() => ctx.go({ mode: 'add', step: 'kind', kind: k })}
          >
            <span className="ad-ic">
              <Icon i={KIND[k].icon} size="sm" />
            </span>
            <span className="ad-row__t">
              <b>{KIND[k].one}</b>
              <small>{KIND[k].about}</small>
            </span>
            <Icon i="chevron-right" size="sm" />
          </button>
        ))}
      </div>
      <div className="ev-or">или</div>
      <div className="ev-pick">
        {ways.map((w) => (
          <button type="button" className="ev-pick__i" key={w.label} onClick={w.go}>
            <span className="ad-ic">
              <Icon i={w.icon} size="sm" />
            </span>
            <span className="ad-row__t">
              <b>{w.label}</b>
              <small>{w.about}</small>
            </span>
            <Icon i="chevron-right" size="sm" />
          </button>
        ))}
      </div>
    </>
  );
};

const fixRepo = (d: Draft, ctx: SheetCtx): Draft =>
  d.k === 'script' && !d.f.repoOther && !ctx.repos.some((r) => r.id === d.f.repo)
    ? { ...d, f: { ...d.f, repo: ctx.repos[0]?.id ?? '' } }
    : d;

export const AddKind = ({
  ctx,
  kind,
  start,
}: {
  ctx: SheetCtx;
  kind: (typeof ADD_KINDS)[number];
  start?: Draft;
}) => {
  const [tab, setTab] = useState<'cat' | 'new' | null>(null);
  const [catQ, setCatQ] = useState('');
  const [draft, setDraft] = useState<Draft>(start ?? blankDraft(kind, ctx.repos));
  const [pinned, setPinned] = useState(true);
  const [added, setAdded] = useState<ReadonlySet<string>>(new Set());
  const t = useTry();
  const d = fixRepo(draft, ctx);
  const set = (p: Partial<Fields>) => {
    t.reset();
    setDraft({ ...d, f: { ...d.f, ...p } });
  };
  const list: CatalogItem[] = (ctx.catalog ?? []).filter(
    (c) => c.kind === kind && !c.archived && fitsLayer(c, ctx.target),
  );
  const free = list.some((c) => haveItem(ctx.chain, c.id) === null);
  const hasCat = list.length > 0;
  const mode = hasCat ? (tab ?? (free && !start ? 'cat' : 'new')) : 'new';
  const q = catQ.trim().toLowerCase();
  const shown = list.filter((c) => q === '' || `${c.name} ${c.hint}`.toLowerCase().includes(q));
  const ok = valid(d);

  const addNew = async () => {
    const id = await ctx.actions.create(ctx.target, d, pinned, ctx.repos);
    if (id) {
      ctx.flash([id]);
      ctx.close();
    }
  };
  return (
    <>
      <Title text={KIND[kind].one} onBack={() => ctx.go({ mode: 'add', step: 'home' })} />
      <To ctx={ctx} />
      {hasCat && (
        <div className="ev-ksw">
          <Seg
            items={[
              ['cat', 'Есть в организации'],
              ['new', KIND[kind].newT],
            ]}
            value={mode}
            onChange={setTab}
            label="Откуда"
          />
        </div>
      )}
      {mode === 'cat' && (
        <>
          {list.length > 6 && (
            <Search
              value={catQ}
              onChange={setCatQ}
              placeholder={`Найти: ${KIND[kind].many.toLowerCase()}`}
              label={`Найти: ${KIND[kind].many.toLowerCase()}`}
            />
          )}
          {shown.map((c) => {
            const at = haveItem(ctx.chain, c.id);
            const justAdded = added.has(c.id);
            const here = at !== null && same(at, ctx.target);
            const where = here
              ? 'уже здесь'
              : at?.layer === 'org'
                ? 'есть у организации'
                : at
                  ? 'есть у тенанта'
                  : '';
            return (
              <div className="ad-row" key={c.id}>
                <span className="ad-ic">
                  <Icon i={KIND[c.kind].icon} size="sm" />
                </span>
                <span className="ad-row__t">
                  <b>{c.name}</b>
                  <small>{c.hint}</small>
                </span>
                {at && !justAdded && <span className="ev-has">{where}</span>}
                {(!at || justAdded) && (
                  <button
                    type="button"
                    className="ad-add"
                    data-on={justAdded ? 'true' : 'false'}
                    disabled={ctx.actions.busy}
                    onClick={() => {
                      if (justAdded) return;
                      void ctx.actions.add(ctx.target, [c], true).then((ids) => {
                        if (ids) {
                          setAdded(new Set([...added, c.id]));
                          ctx.flash(ids);
                        }
                      });
                    }}
                  >
                    {justAdded ? 'Добавлено' : 'Добавить'}
                  </button>
                )}
              </div>
            );
          })}
          {shown.length === 0 && <div className="ev-sh__empty">Ничего не нашлось — создайте новый</div>}
        </>
      )}
      {mode === 'new' && (
        <ItemForm
          ctx={ctx}
          d={d}
          set={set}
          pinned={pinned}
          setPinned={setPinned}
          t={t}
          itemId={null}
          usage=""
        />
      )}
      {mode === 'new' && (
        <Foot>
          <span className="ev-foot__sp" />
          {d.k === 'script' && (
            <Btn
              variant="ghost"
              size="sm"
              icon="shield-check"
              disabled={!ok || t.asking || (t.active && t.run?.status === 'running')}
              onClick={() => void t.start(ctx.target, d, null)}
            >
              Проверить
            </Btn>
          )}
          <Btn
            variant="primary"
            size="sm"
            busy={ctx.actions.busy}
            disabled={!ok}
            onClick={() => void addNew()}
          >
            Добавить
          </Btn>
        </Foot>
      )}
    </>
  );
};

const EXAMPLES = [
  ['Ссылка на скилл', 'gitlab.example.ru/frontend/skills/release-notes'],
  ['Адрес MCP', 'https://mcp.linear.app/mcp'],
  ['Команда установки', 'npx skills add vercel-labs/agent-skills --skill web-design-guidelines'],
] as const;

export const AddLink = ({ ctx }: { ctx: SheetCtx }) => {
  const [link, setLink] = useState('');
  const [file, setFile] = useState<{ name: string; text: string } | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  let det: Draft | typeof BAD | null = detect(link, ctx.repos);
  if (file) {
    const d = blankDraft(/\.(sh|py|js|mjs|bash)$/i.test(file.name) ? 'script' : 'skill', ctx.repos);
    det = { ...d, f: { ...d.f, src: 'file', fileName: file.name, body: file.text } };
  }
  const found = det !== null && det !== BAD ? det : null;
  const okFound = found !== null && valid(found);
  const take = (f: File | undefined) => {
    if (!f) return;
    if (f.size > 48 * 1024) return setErr('Файл больше 48 КБ — в окружение такой не положить');
    void f.text().then((text) => {
      setErr(null);
      setLink('');
      setFile({ name: f.name, text });
    });
  };
  const add = async () => {
    if (!found || !okFound) return;
    const id = await ctx.actions.create(ctx.target, found, true, ctx.repos);
    if (id) {
      ctx.flash([id]);
      ctx.close();
    }
  };
  return (
    <>
      <Title text="Ссылкой, командой или файлом" onBack={() => ctx.go({ mode: 'add', step: 'home' })} />
      <To ctx={ctx} />
      <label className="ev-link">
        <Icon i="link" size="sm" />
        <input
          placeholder="Ссылка, команда или конфиг MCP"
          value={link}
          aria-label="Ссылка или команда"
          onChange={(e) => {
            setFile(null);
            setLink(e.target.value);
          }}
        />
      </label>
      {link.trim() === '' && !file && (
        <div className="ev-ex">
          <span>Например:</span>
          {EXAMPLES.map(([label, value]) => (
            <button
              type="button"
              className="ev-ex__b"
              key={label}
              title={value}
              onClick={() => setLink(value)}
            >
              {label}
            </button>
          ))}
        </div>
      )}
      {found && (
        <div className="ev-det">
          <span className="ad-ic">
            <Icon i={KIND[found.k].icon} size="sm" />
          </span>
          <span className="ad-row__t">
            <small>{`Узнали: ${detWord(found)}`}</small>
            <b>{viewOf(found, ctx.repos).name}</b>
            <small>{viewOf(found, ctx.repos).hint}</small>
          </span>
        </div>
      )}
      {det === BAD && (
        <div className="ev-det ev-det--bad">
          Не узнали. Подойдёт ссылка на репозиторий или папку со SKILL.md, адрес MCP, команда установки или
          конфиг MCP
        </div>
      )}
      <div className="ev-or">или</div>
      <button
        type="button"
        className="ev-drop"
        data-has={file ? 'true' : undefined}
        onClick={() => (file ? setFile(null) : input.current?.click())}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault();
          take(e.dataTransfer.files[0]);
        }}
      >
        <Icon i="upload" />
        <b>{file ? lastSeg(file.name) : 'Перетащите файл или нажмите'}</b>
        <small>{file ? 'нажмите, чтобы убрать' : 'SKILL.md или скрипт .sh, .py, .js'}</small>
      </button>
      <input
        ref={input}
        type="file"
        accept=".md,.sh,.bash,.py,.js,.mjs,text/*"
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
      <Foot>
        <span className="ev-foot__sp" />
        <Btn
          variant="ghost"
          size="sm"
          disabled={!found}
          onClick={() => found && ctx.go({ mode: 'add', step: 'kind', kind: found.k, draft: found })}
        >
          Поправить перед добавлением
        </Btn>
        <Btn
          variant="primary"
          size="sm"
          busy={ctx.actions.busy}
          disabled={!okFound}
          onClick={() => void add()}
        >
          Добавить
        </Btn>
      </Foot>
    </>
  );
};

export type { SheetMode };
export { metaOf };
