// A kit as one screen, the way a design tool shows a library: the tree on the left, one canvas in the middle
// with the kit's sections laid out by themselves, the panel of what is selected on the right, the search on ⌘K. A
// template is a stack on the canvas; «Раскрыть» goes inside it to its grid of layouts by sections, a layout opens
// alone and large with ← →. The screen knows nothing of the machine: the binding gives it the canvas and the ways out.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type {
  IAtlas,
  IKitCanvas,
  IKitLayout,
  IKitLink,
  TPropValue,
} from '@/next/features/designly/kit/kit-api';
import { kitPlayPath, kitViewPath } from '@/next/features/designly/kit/kit-api';
import KitCanvas, { type IKitCanvasHandle } from '@/next/features/designly/kit/kit-canvas';
import { findNode, NODE } from '@/next/features/designly/kit/kit-node';
import KitPanel, { type IDeckChoice } from '@/next/features/designly/kit/kit-panel';
import KitSearchDialog from '@/next/features/designly/kit/kit-search-dialog';
import KitSingle from '@/next/features/designly/kit/kit-single';
import KitTree, { treeOf } from '@/next/features/designly/kit/kit-tree';
import {
  buildWorld,
  drawsLive,
  ITEM,
  liveProps,
  MAIN_VIEW,
  sameView,
  VIEW,
  viewOfNode,
  type IWorldItem,
  type TView,
} from '@/next/features/designly/kit/kit-world';
import { RoundButton } from '@/next/ui/button';
import Segmented from '@/next/ui/segmented';
import { t } from '@/next/i18n';

export interface IKitScreenProps {
  canvas: IKitCanvas;
  /** The node the camera starts on (a link's `?node=`). */
  startNode: string | null;
  fileUrl: (rel: string) => string;
  prepUrl: (file: string) => string;
  /** The preview slot's origin once it can show the kit's pages; null before (pictures stay). */
  liveOrigin: string | null;
  usage: IKitLink[] | null;
  decks: IDeckChoice[] | null;
  onBack: () => void;
  onTheme: (theme: string) => void;
  onNode: (node: string | null) => void;
  onCopy: (text: string) => void;
  onSendToChat: (node: string, extra: Record<string, TPropValue>) => void;
  onInsert: (deck: string, layout: IKitLayout) => Promise<void>;
  onOpenAsProject: (template: string) => Promise<void>;
  onOpenBoard: (project: string, board: string) => void;
  /** The head's tools of the editing mode: «Редактировать», «Опубликовать версию», «Только просмотр». */
  tools?: ReactNode;
  /** The draft's chat, docked on the left while the kit is edited. */
  chat?: ReactNode;
}

/** The kit's draft stands where a version goes: its live pages carry the canvas's moment, so an edit shows. */
const DRAFT_VERSION = 'draft';

/** The default names of the canvas's sections and foundation groups. */
export const sectionTitles = (): Record<string, string> => ({
  foundation: t('next.kit.section.foundation'),
  components: t('next.kit.section.components'),
  templates: t('next.kit.section.templates'),
  assets: t('next.kit.section.assets'),
  reference: t('next.kit.section.reference'),
  colors: t('next.kit.section.colors'),
  type: t('next.kit.section.type'),
  tokens: t('next.kit.section.tokens'),
  providers: t('next.kit.section.providers'),
});

const LIVE_W = { layout: 1280, component: 800 } as const;

const KitScreen = (p: IKitScreenProps) => {
  const { canvas } = p;
  const titles = useMemo(() => sectionTitles(), []);
  const startView = p.startNode ? viewOfNode(p.startNode) : MAIN_VIEW;
  const [view, setView] = useState<TView>(startView);
  const [selected, setSelected] = useState<string | null>(p.startNode);
  const [searchOpen, setSearchOpen] = useState(false);
  const [single, setSingle] = useState<{ template: string; layout: string } | null>(null);
  const [pct, setPct] = useState(0);
  const [sideOpen, setSideOpen] = useState(true);
  const canvasRef = useRef<IKitCanvasHandle | null>(null);
  const pendingFly = useRef<{ node: string; smooth: boolean } | null>(
    p.startNode ? { node: p.startNode, smooth: false } : null,
  );
  const mode = canvas.modes?.[0] ?? null;

  const world = useMemo(() => buildWorld(canvas, view, titles, mode), [canvas, view, titles, mode]);
  const worldKey = `${canvas.kit}@${canvas.version}/${canvas.theme}/${view.kind === VIEW.template ? view.id : ''}`;
  const tree = useMemo(() => treeOf(canvas, titles), [canvas, titles]);

  const atlases = useMemo(() => {
    const m = new Map<string, { atlas: IAtlas; url: string }>();
    for (const g of canvas.assets ?? [])
      if (g.atlas) m.set(`a:${g.id}`, { atlas: g.atlas, url: p.prepUrl(g.atlas.file) });
    for (const tp of canvas.templates ?? [])
      if (tp.atlas) m.set(`t:${tp.id}`, { atlas: tp.atlas, url: p.prepUrl(tp.atlas.file) });
    for (const g of canvas.groups ?? [])
      if (g.atlas) m.set(`c:${g.id}`, { atlas: g.atlas, url: p.prepUrl(g.atlas.file) });
    return m;
  }, [canvas, p]);

  // After the world of another view is laid out, the camera flies to the node that asked for it.
  useEffect(() => {
    const fly = pendingFly.current;
    if (!fly) return;
    pendingFly.current = null;
    const id = requestAnimationFrame(() => canvasRef.current?.flyTo(fly.node, fly.smooth));
    return () => cancelAnimationFrame(id);
  }, [world]);

  const { onNode } = p;
  const select = useCallback(
    (node: string | null, fly: boolean) => {
      setSelected(node);
      onNode(node);
      if (!node || !fly) return;
      const v = viewOfNode(node);
      if (!sameView(v, view)) {
        pendingFly.current = { node, smooth: true };
        setView(v);
        return;
      }
      canvasRef.current?.flyTo(node);
    },
    [view, onNode],
  );

  const openTemplate = useCallback(
    (template: string) => {
      const v: TView = { kind: VIEW.template, id: template };
      setSingle(null);
      if (!sameView(v, view)) setView(v);
      const node = `template:${template}`;
      setSelected(node);
      onNode(node);
    },
    [view, onNode],
  );

  const backToMain = useCallback(() => {
    if (view.kind !== VIEW.template) return;
    const node = `template:${view.id}`;
    pendingFly.current = { node, smooth: false };
    setSingle(null);
    setView(MAIN_VIEW);
    setSelected(node);
    onNode(node);
  }, [view, onNode]);

  const onOpen = useCallback(
    (it: IWorldItem) => {
      if (it.kind === ITEM.stack) {
        openTemplate(it.node.replace(/^template:/, ''));
        return;
      }
      const m = /^layout:([^/]+)\/(.+)$/.exec(it.node);
      if (m?.[1] && m[2]) {
        setSingle({ template: m[1], layout: m[2] });
        select(it.node, false);
        return;
      }
      select(it.node, true);
    },
    [openTemplate, select],
  );

  // ⌘K opens the search wherever the focus is on the screen.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        e.stopPropagation();
        setSearchOpen(true);
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, []);

  const fresh = useCallback(
    (src: string) =>
      canvas.version === DRAFT_VERSION
        ? `${src}${src.includes('?') ? '&' : '?'}at=${canvas.preparedAt}`
        : src,
    [canvas],
  );
  const layoutLive = useCallback(
    (l: IKitLayout, component: string | undefined): string | null => {
      if (!p.liveOrigin) return null;
      const base = p.liveOrigin.replace(/\/+$/, '');
      if (l.board?.startsWith('_harnessly/layouts/') && l.board.endsWith('.dc.html'))
        return fresh(base + kitViewPath(canvas, l.board.slice('_harnessly/layouts/'.length)));
      if (component) return fresh(base + kitPlayPath(canvas, component, l.props ?? {}, l.scripts ?? []));
      return null;
    },
    [p.liveOrigin, canvas, fresh],
  );

  const liveSrc = useCallback(
    (node: string): string | null => {
      const n = findNode(canvas, node);
      if (!n || !p.liveOrigin) return null;
      if (n.kind === NODE.layout) return layoutLive(n.layout, n.template.component);
      if (n.kind === NODE.template) {
        const cover = n.template.sections.flatMap((s) => s.layouts).find((l) => l.id === n.template.cover);
        return cover ? layoutLive(cover, n.template.component) : null;
      }
      if (n.kind === NODE.component && drawsLive(n.component))
        return fresh(
          p.liveOrigin.replace(/\/+$/, '') + kitPlayPath(canvas, n.component.id, liveProps(n.component)),
        );
      return null;
    },
    [canvas, p.liveOrigin, layoutLive, fresh],
  );

  const liveWidth = useCallback(
    (node: string) => (node.startsWith('component:') ? LIVE_W.component : LIVE_W.layout),
    [],
  );

  const playSrc = useCallback(
    (component: string, props: Record<string, TPropValue>, scripts: readonly string[] = []) =>
      p.liveOrigin
        ? fresh(p.liveOrigin.replace(/\/+$/, '') + kitPlayPath(canvas, component, props, scripts))
        : null,
    [p.liveOrigin, canvas, fresh],
  );

  const node = selected ? findNode(canvas, selected) : null;
  const frame = !node && selected ? world.frames.find((f) => f.node === selected) : undefined;
  const singleTemplate = single
    ? ((canvas.templates ?? []).find((x) => x.id === single.template) ?? null)
    : null;
  const viewTemplate =
    view.kind === VIEW.template ? (canvas.templates ?? []).find((x) => x.id === view.id) : undefined;
  const themes = canvas.themes;

  return (
    <div className="hx-kit" data-side={sideOpen || undefined} data-chat={p.chat ? '' : undefined}>
      <header className="hx-kit__head">
        <RoundButton
          icon="arrow-left"
          label={t('next.kit.back')}
          variant="ghost"
          size="sm"
          onClick={p.onBack}
        />
        <RoundButton
          icon="panel-left"
          label={t('next.kit.tree')}
          variant="ghost"
          size="sm"
          className="hx-kit__side-toggle"
          onClick={() => setSideOpen((v) => !v)}
        />
        <nav className="hx-kit__crumbs" aria-label={t('next.kit.crumbs')}>
          <button type="button" className="hx-kit__crumb" data-root onClick={backToMain}>
            {canvas.title}
          </button>
          <span className="hx-kit__ver">
            {canvas.version === DRAFT_VERSION ? t('next.versions.kit.draft') : canvas.version}
          </span>
          {viewTemplate && (
            <>
              <span className="hx-kit__sep">›</span>
              <button type="button" className="hx-kit__crumb" onClick={backToMain}>
                {titles.templates}
              </button>
              <span className="hx-kit__sep">›</span>
              <span className="hx-kit__crumb" aria-current="page">
                {viewTemplate.title}
              </span>
            </>
          )}
        </nav>
        <span className="hx-spacer" />
        {themes.length > 1 && (
          <Segmented
            label={t('next.kit.theme')}
            value={canvas.theme}
            options={themes.map((x) => ({ value: x.id, label: x.title }))}
            onChange={p.onTheme}
          />
        )}
        {p.tools}
        <button type="button" className="hx-kit__search" onClick={() => setSearchOpen(true)}>
          <span>{t('next.kit.search.open')}</span>
          <kbd>⌘K</kbd>
        </button>
      </header>
      <div className="hx-kit__body">
        {p.chat}
        <nav className="hx-kit__side" aria-label={t('next.kit.tree')}>
          <KitTree
            tree={tree}
            selected={selected}
            onSelect={(n) => select(n, true)}
            label={t('next.kit.tree')}
          />
        </nav>
        <main className="hx-kit__main">
          <KitCanvas
            ref={canvasRef}
            world={world}
            worldKey={worldKey}
            selected={selected}
            fileUrl={p.fileUrl}
            atlases={atlases}
            onSelect={(n) => select(n, false)}
            onOpen={onOpen}
            liveSrc={liveSrc}
            liveWidth={liveWidth}
            onZoom={setPct}
          />
          <div className="hx-kit__zoom">
            <RoundButton
              icon="minus"
              label={t('next.kit.zoomOut')}
              keys={['⌘', '−']}
              size="sm"
              variant="ghost"
              onClick={() => canvasRef.current?.zoomBy(0.8)}
            />
            <button
              type="button"
              className="hx-kit__pct"
              onClick={() => canvasRef.current?.fitAll()}
              aria-label={t('next.kit.fit')}
            >
              {pct}%
            </button>
            <RoundButton
              icon="plus"
              label={t('next.kit.zoomIn')}
              keys={['⌘', '+']}
              size="sm"
              variant="ghost"
              onClick={() => canvasRef.current?.zoomBy(1.25)}
            />
          </div>
          <KitSingle
            template={singleTemplate}
            layout={single?.layout ?? null}
            fileUrl={p.fileUrl}
            liveSrc={(l) => layoutLive(l, singleTemplate?.component)}
            onMove={(n) => {
              const m = /^layout:([^/]+)\/(.+)$/.exec(n);
              if (m?.[1] && m[2]) setSingle({ template: m[1], layout: m[2] });
              setSelected(n);
              onNode(n);
            }}
            onClose={() => {
              setSingle(null);
              if (selected) canvasRef.current?.flyTo(selected, false);
            }}
          />
        </main>
        <KitPanel
          canvas={canvas}
          node={node}
          frame={
            frame
              ? { title: frame.title, ...(frame.count !== undefined ? { count: frame.count } : {}) }
              : null
          }
          titles={titles}
          fileUrl={p.fileUrl}
          playSrc={playSrc}
          usage={p.usage}
          decks={p.decks}
          onCopy={p.onCopy}
          onSendToChat={(extra) => selected && p.onSendToChat(selected, extra)}
          onInsert={async (deck) => {
            if (node?.kind === NODE.layout) await p.onInsert(deck, node.layout);
          }}
          onOpenTemplate={openTemplate}
          onOpenAsProject={p.onOpenAsProject}
          onOpenSingle={(n) => onOpen({ node: n, kind: ITEM.layout, label: '' })}
          onOpenBoard={p.onOpenBoard}
          onClose={() => select(null, false)}
        />
      </div>
      <KitSearchDialog
        open={searchOpen}
        index={canvas.index ?? []}
        onPick={(n) => select(n, true)}
        onClose={() => setSearchOpen(false)}
      />
    </div>
  );
};

export default KitScreen;
