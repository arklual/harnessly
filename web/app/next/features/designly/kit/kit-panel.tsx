// The panel of what is selected on the kit's canvas: Обзор (name, what it is for, where it lies), Параметры (variants
// and props of a component, the slots of a layout, with a live preview that follows them), Как вызвать (the call, to
// copy), Где используется (the projects and boards, a click opens the board), and the actions: «Вставить в доску»,
// «Отправить в чат», for a template «Раскрыть» and «Открыть как проект».
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import type { IKitCanvas, IKitLink, TPropValue } from '@/next/features/designly/kit/kit-api';
import { callOf, nameOf, NODE, placeOf, type TNode } from '@/next/features/designly/kit/kit-node';
import { layoutCount } from '@/next/features/designly/kit/kit-world';
import { Button } from '@/next/ui/button';
import Popover from '@/next/ui/popover';
import Segmented from '@/next/ui/segmented';
import { plural, t } from '@/next/i18n';

export interface IDeckChoice {
  id: string;
  title: string;
}

interface IKitPanelProps {
  canvas: IKitCanvas;
  node: TNode | null;
  /** A section or a group selected (no node of its own): its name and how many it holds. */
  frame: { title: string; count?: number } | null;
  titles: Record<string, string>;
  fileUrl: (rel: string) => string;
  /** A component drawn live with props, null while the preview slot opens. */
  playSrc: (
    component: string,
    props: Record<string, TPropValue>,
    scripts?: readonly string[],
  ) => string | null;
  usage: IKitLink[] | null;
  decks: IDeckChoice[] | null;
  onCopy: (text: string) => void;
  onSendToChat: (extra: Record<string, TPropValue>) => void;
  onInsert: (deck: string) => Promise<void>;
  onOpenTemplate: (template: string) => void;
  onOpenAsProject: (template: string) => Promise<void>;
  onOpenSingle: (node: string) => void;
  onOpenBoard: (project: string, board: string) => void;
  onClose: () => void;
}

const DEBOUNCE_MS = 400;

const useDebounced = <T,>(value: T, ms: number): T => {
  const [v, setV] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setV(value), ms);
    return () => window.clearTimeout(timer);
  }, [value, ms]);
  return v;
};

const Section = ({ title, children }: { title: string; children: ReactNode }) => (
  <section className="hx-kit-panel__sec">
    <h3 className="hx-kit-panel__h">{title}</h3>
    {children}
  </section>
);

const KitPanel = (p: IKitPanelProps) => {
  const { canvas, node } = p;
  // The person's changes to the props (a component's) or the texts (a layout's slots); a new node starts clean.
  const [extra, setExtra] = useState<Record<string, TPropValue>>({});
  const [texts, setTexts] = useState<Record<string, string>>({});
  const [mode, setMode] = useState<string>('light');
  const [insertOpen, setInsertOpen] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const key = node ? `${node.kind}:${nameOf(node)}` : '';
  const [lastKey, setLastKey] = useState(key);
  if (lastKey !== key) {
    setLastKey(key);
    setExtra({});
    setTexts({});
  }

  const props = useMemo<Record<string, TPropValue>>(() => {
    if (!node) return {};
    if (node.kind === NODE.layout) {
      const changed = Object.fromEntries(Object.entries(texts).filter(([, v]) => v !== ''));
      return Object.keys(changed).length > 0 ? { ...extra, texts: changed } : extra;
    }
    if (node.kind === NODE.asset && node.asset.dark && mode === 'dark') return { mode: 'dark' };
    return extra;
  }, [node, texts, extra, mode]);
  const shownProps = useDebounced(props, DEBOUNCE_MS);

  if (!node) {
    return (
      <aside className="hx-kit-panel" aria-label={t('next.kit.panel.label')}>
        {p.frame ? (
          <header className="hx-kit-panel__head">
            <h2 className="hx-kit-panel__name">{p.frame.title}</h2>
            {p.frame.count !== undefined && <p className="hx-kit-panel__place">{p.frame.count}</p>}
          </header>
        ) : (
          <p className="hx-kit-panel__empty">{t('next.kit.panel.empty')}</p>
        )}
      </aside>
    );
  }

  const call = callOf(canvas, node, props);
  const uses = (p.usage ?? []).flatMap((link) =>
    link.boards
      .filter((b) => b.nodes.includes(nodeId(node)))
      .map((b) => ({ project: link.project, projectTitle: link.title, board: b.path, title: b.title })),
  );

  let preview: string | null = null;
  if (node.kind === NODE.component && node.component.preview !== undefined)
    preview = p.playSrc(node.component.id, { ...(node.component.sample ?? {}), ...shownProps });
  if (node.kind === NODE.layout && node.template.component)
    preview = p.playSrc(
      node.template.component,
      { ...(node.layout.props ?? {}), ...shownProps },
      node.layout.scripts ?? [],
    );

  const run = (what: string, fn: () => Promise<void>) => {
    setBusy(what);
    void fn().finally(() => setBusy(null));
  };

  return (
    <aside className="hx-kit-panel" aria-label={t('next.kit.panel.label')}>
      <header className="hx-kit-panel__head">
        <p className="hx-kit-panel__place">{placeOf(node, p.titles).filter(Boolean).join(' › ')}</p>
        <h2 className="hx-kit-panel__name">{nameOf(node)}</h2>
      </header>

      <Section title={t('next.kit.panel.overview')}>
        {node.kind === NODE.component && node.component.description && (
          <p className="hx-kit-panel__text">{node.component.description}</p>
        )}
        {node.kind === NODE.layout && (
          <>
            {node.layout.purpose && <p className="hx-kit-panel__text">{node.layout.purpose}</p>}
            {node.layout.thumb && (
              <button
                type="button"
                className="hx-kit-panel__shot"
                onClick={() => p.onOpenSingle(node.layout.node)}
              >
                <img src={p.fileUrl(node.layout.thumb)} alt={node.layout.name} loading="lazy" />
              </button>
            )}
          </>
        )}
        {node.kind === NODE.template && (
          <p className="hx-kit-panel__text">
            {node.template.description ? `${node.template.description} ` : ''}
            {plural(layoutCount(node.template), 'next.kit.panel.layouts', { n: layoutCount(node.template) })}
          </p>
        )}
        {node.kind === NODE.tsection && (
          <p className="hx-kit-panel__text">
            {plural(node.section.layouts.length, 'next.kit.panel.layouts', {
              n: node.section.layouts.length,
            })}
          </p>
        )}
        {node.kind === NODE.color && (
          <ul className="hx-kit-panel__swatches">
            {Object.entries(node.color.values).map(([m, v]) => (
              <li key={m}>
                <button type="button" className="hx-kit-swatch" onClick={() => p.onCopy(v)}>
                  <span className="hx-kit-swatch__chip" ref={(el) => el?.style.setProperty('--chip', v)} />
                  <span className="hx-kit-swatch__mode">{m || t('next.kit.panel.value')}</span>
                  <code>{v}</code>
                </button>
              </li>
            ))}
            {node.color.usage && <li className="hx-kit-panel__text">{node.color.usage}</li>}
          </ul>
        )}
        {node.kind === NODE.type && (
          <p
            className="hx-kit-panel__type"
            ref={(el) => {
              el?.style.setProperty('--ff', node.style.family);
              el?.style.setProperty('--fs', node.style.size);
              el?.style.setProperty('--fw', node.style.weight ?? '400');
            }}
          >
            {t('next.kit.panel.typeSample')}
            <small>{[node.style.size, node.style.line, node.style.weight].filter(Boolean).join(' / ')}</small>
          </p>
        )}
        {node.kind === NODE.token && (
          <p className="hx-kit-panel__text">
            <code>{node.token.value}</code> {node.token.usage}
          </p>
        )}
        {node.kind === NODE.asset && (
          <div
            className="hx-kit-panel__asset"
            data-dark={(mode === 'dark' && node.asset.dark !== undefined) || undefined}
          >
            <img
              src={p.fileUrl(mode === 'dark' && node.asset.dark ? node.asset.dark : node.asset.file)}
              alt={node.asset.name}
            />
          </div>
        )}
      </Section>

      {(node.kind === NODE.component ||
        node.kind === NODE.layout ||
        (node.kind === NODE.asset && node.asset.dark)) && (
        <Section title={t('next.kit.panel.params')}>
          {node.kind === NODE.asset && (
            <Segmented
              label={t('next.kit.panel.mode')}
              value={mode}
              options={[
                { value: 'light', label: t('next.kit.panel.light') },
                { value: 'dark', label: t('next.kit.panel.dark') },
              ]}
              onChange={setMode}
            />
          )}
          {node.kind === NODE.component && node.component.variants.length > 0 && (
            <Segmented
              label={t('next.kit.panel.variant')}
              value={
                node.component.variants.find(
                  (v) => JSON.stringify(v.props ?? {}) === JSON.stringify(pick(extra, v.props)),
                )?.id ?? ''
              }
              options={node.component.variants.map((v) => ({ value: v.id, label: v.name }))}
              onChange={(id) => {
                const v = node.component.variants.find((x) => x.id === id);
                setExtra((prev) => ({ ...prev, ...(v?.props ?? {}) }));
              }}
            />
          )}
          {node.kind === NODE.component && (
            <dl className="hx-kit-props">
              {node.component.props.map((prop) => (
                <div key={prop.name} className="hx-kit-props__row">
                  <dt>
                    <code>{prop.name}</code>
                    {prop.required && <span aria-label={t('next.kit.panel.required')}>*</span>}
                  </dt>
                  <dd>
                    {prop.type === 'string' ? (
                      <input
                        className="hx-kit-input"
                        value={String(extra[prop.name] ?? node.component.sample?.[prop.name] ?? '')}
                        onChange={(e) => setExtra((prev) => ({ ...prev, [prop.name]: e.target.value }))}
                        aria-label={prop.name}
                      />
                    ) : (
                      <span className="hx-kit-props__type">
                        {prop.values ? prop.values.join(' | ') : prop.type}
                      </span>
                    )}
                    {prop.description && <small>{prop.description}</small>}
                  </dd>
                </div>
              ))}
            </dl>
          )}
          {node.kind === NODE.layout && node.layout.slots.length > 0 && (
            <dl className="hx-kit-props">
              {node.layout.slots.map((slot) => (
                <div key={slot.key} className="hx-kit-props__row">
                  <dt>
                    <code>{slot.key}</code>
                  </dt>
                  <dd>
                    {slot.kind === 'text' ? (
                      <input
                        className="hx-kit-input"
                        placeholder={slot.sample ?? ''}
                        value={texts[slot.key] ?? ''}
                        onChange={(e) => setTexts((prev) => ({ ...prev, [slot.key]: e.target.value }))}
                        aria-label={slot.key}
                      />
                    ) : (
                      <span className="hx-kit-props__type">
                        {t(`next.kit.slot.${slot.kind === 'image' ? 'image' : 'chart'}`)}
                      </span>
                    )}
                  </dd>
                </div>
              ))}
            </dl>
          )}
          {preview && (
            <div className="hx-kit-preview" data-wide={node.kind === NODE.layout || undefined}>
              <iframe key={preview} src={preview} title={nameOf(node)} tabIndex={-1} />
            </div>
          )}
        </Section>
      )}

      {call && (
        <Section title={t('next.kit.panel.call')}>
          <pre className="hx-kit-code">
            <code>{call}</code>
          </pre>
          <Button size="sm" icon="copy" onClick={() => p.onCopy(call)}>
            {t('next.kit.panel.copy')}
          </Button>
        </Section>
      )}

      {(node.kind === NODE.layout || node.kind === NODE.component || node.kind === NODE.asset) && (
        <Section title={t('next.kit.panel.used')}>
          {p.usage === null && <p className="hx-kit-panel__muted">{t('next.kit.panel.usedLoading')}</p>}
          {p.usage !== null && uses.length === 0 && (
            <p className="hx-kit-panel__muted">{t('next.kit.panel.usedNone')}</p>
          )}
          {uses.length > 0 && (
            <ul className="hx-kit-uses">
              {uses.map((u) => (
                <li key={`${u.project}/${u.board}`}>
                  <button type="button" onClick={() => p.onOpenBoard(u.project, u.board)}>
                    <span>{u.title}</span>
                    <small>{u.projectTitle}</small>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Section>
      )}

      <footer className="hx-kit-panel__actions">
        {node.kind === NODE.template && (
          <>
            <Button
              variant="primary"
              size="sm"
              icon="layout-grid"
              onClick={() => p.onOpenTemplate(node.template.id)}
            >
              {t('next.kit.panel.expand')}
            </Button>
            <Button
              size="sm"
              icon="folder-open"
              busy={busy === 'project'}
              onClick={() => run('project', () => p.onOpenAsProject(node.template.id))}
            >
              {t('next.kit.panel.asProject')}
            </Button>
          </>
        )}
        {node.kind === NODE.layout && node.layout.address && (
          <Popover
            open={insertOpen}
            onOpenChange={setInsertOpen}
            label={t('next.kit.panel.insert')}
            className="hx-kit-insert"
            side="top"
            trigger={
              <Button size="sm" icon="plus" busy={busy === 'insert'}>
                {t('next.kit.panel.insert')}
              </Button>
            }
          >
            <p className="hx-kit-insert__title">{t('next.kit.panel.insertWhere')}</p>
            {p.decks === null && <p className="hx-kit-panel__muted">{t('next.kit.panel.usedLoading')}</p>}
            {p.decks !== null && p.decks.length === 0 && (
              <p className="hx-kit-panel__muted">{t('next.kit.panel.noDecks')}</p>
            )}
            <ul className="hx-kit-insert__list">
              {(p.decks ?? []).map((d) => (
                <li key={d.id}>
                  <button
                    type="button"
                    onClick={() => {
                      setInsertOpen(false);
                      run('insert', () => p.onInsert(d.id));
                    }}
                  >
                    {d.title}
                  </button>
                </li>
              ))}
            </ul>
          </Popover>
        )}
        <Button size="sm" icon="message-square" onClick={() => p.onSendToChat(props)}>
          {t('next.kit.panel.toChat')}
        </Button>
      </footer>
    </aside>
  );
};

/** The values of `of`'s keys in `from`. */
const pick = (from: Record<string, TPropValue>, of: Record<string, TPropValue> | undefined) =>
  Object.fromEntries(Object.keys(of ?? {}).map((k) => [k, from[k]]));

const nodeId = (n: TNode): string => {
  switch (n.kind) {
    case NODE.component:
      return n.component.node;
    case NODE.layout:
      return n.layout.node;
    case NODE.template:
      return n.template.node;
    case NODE.tsection:
      return n.section.node;
    case NODE.asset:
      return n.asset.node;
    case NODE.color:
      return n.color.id;
    case NODE.type:
      return n.style.id;
    case NODE.token:
      return n.token.id;
  }
};

export { nodeId };
export default KitPanel;
