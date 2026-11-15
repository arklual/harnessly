// The editor's «Пометить»: five tools of the person's own marks under the picture (frame, arrow, words, pen,
// crop), the colours, «Отменить», and the button that draws the marks into the picture and hands the new picture to
// the field (to-composer.ts). A hook, so the editor with the change tools (image-editor.tsx) and the one without them
// share the same viewer, its slots filled by whichever part is on.
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { CardReply } from '@/next/bindings/reply-field';
import useKeyboardInset from '@/next/features/composer/use-keyboard-inset';
import MarkupLayer from '@/next/features/image-edit/markup-layer';
import {
  DEFAULT_MARKUP_COLOR,
  MARKUP_TOOL,
  MARKUP_TOOLS,
  addItem,
  freshHistory,
  hasMarkup,
  markedName,
  pushMarkup,
  renderMarkup,
  undoMarkup,
  type IMarkupHistory,
  type TMarkupTool,
} from '@/next/features/image-edit/markup';
import { MARK_COLORS, type IPoint } from '@/next/features/image-edit/marks';
import type { IMarkupTarget } from '@/next/features/image-edit/to-composer';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import type { IImageViewEdit } from '@/next/ui/image-view';

const COLOR_NAME = { '#ff3b5c': 'red', '#ffd60a': 'yellow', '#ffffff': 'white' } as const;
/** Where words stand when the person types before tapping a place. */
const MIDDLE: IPoint = { x: 0.5, y: 0.5 };

interface IMarkupState {
  /** The picture this state belongs to; another one (a step, a version) starts afresh. */
  forSrc: string;
  tool: TMarkupTool | null;
  color: string;
  history: IMarkupHistory;
  /** Where the words being typed will stand. */
  place: IPoint | null;
  /** Their size: the share of the usual at the zoom the place was tapped at (markup.ts markScale). */
  placeScale?: number;
  text: string;
  saving: boolean;
  failed: boolean;
}

/**
 * The marks not handed over yet, by picture: stepping to another picture or closing the viewer keeps them quietly, and
 * the picture opens with them again. Only for this page's life.
 */
const drafts = new Map<string, IMarkupHistory>();

const startOf = (src: string, tool: TMarkupTool | null): IMarkupState => ({
  forSrc: src,
  tool,
  color: DEFAULT_MARKUP_COLOR,
  history: drafts.get(src) ?? freshHistory(),
  place: null,
  text: '',
  saving: false,
  failed: false,
});

interface IUseMarkupProps {
  src: string;
  name: string;
  target: IMarkupTarget;
  /** The picture's own size, once known: the stage takes its shape. */
  natural: { w: number; h: number } | null;
  onClose: () => void;
  /** Over the tools: the switch to the change tools, where there are some. */
  switcher?: ReactNode;
  /** The tool on when a picture opens: the first stroke marks it. */
  startTool?: TMarkupTool;
}

export type TMarkupSlots = Pick<IImageViewEdit, 'dock' | 'layer' | 'locked' | 'onBack' | 'ratio'>;

export const useMarkup = ({
  src,
  name,
  target,
  natural,
  onClose,
  switcher,
  startTool,
}: IUseMarkupProps): TMarkupSlots => {
  const first = startTool ?? null;
  const [state, setState] = useState<IMarkupState>(() => startOf(src, first));
  let s = state;
  if (s.forSrc !== src) {
    s = startOf(src, first);
    setState(s);
  }
  // The marks are the picture's draft until they are handed over.
  useEffect(() => {
    if (hasMarkup(s.history.now)) drafts.set(s.forSrc, s.history);
    else drafts.delete(s.forSrc);
  }, [s.forSrc, s.history]);
  const patch = (change: Partial<IMarkupState>) => setState((now) => ({ ...now, ...change }));
  const markup = s.history.now;
  const writing = s.tool === MARKUP_TOOL.text;

  const pickTool = (tool: TMarkupTool) => {
    if (s.saving) return;
    patch({ tool: s.tool === tool ? null : tool, place: null, placeScale: undefined, text: '' });
  };
  const placeText = () => {
    const words = s.text.trim();
    if (words === '') return;
    const item = { kind: 'text' as const, color: s.color, at: s.place ?? MIDDLE, text: words };
    const sized = s.placeScale === undefined ? item : { ...item, scale: s.placeScale };
    patch({
      history: pushMarkup(s.history, addItem(markup, sized)),
      place: null,
      placeScale: undefined,
      text: '',
    });
  };
  const finish = () => {
    if (s.saving || !hasMarkup(markup)) return;
    patch({ saving: true, failed: false });
    renderMarkup(src, markup, markedName(name, target.replacing)).then(
      (file) => {
        target.give(file);
        patch({ saving: false, history: freshHistory() });
        onClose();
      },
      () => patch({ saving: false, failed: true }),
    );
  };

  const layer = (
    <MarkupLayer
      tool={s.tool}
      markup={markup}
      color={s.color}
      pending={writing ? { at: s.place ?? MIDDLE, text: s.text, color: s.color, scale: s.placeScale } : null}
      onChange={(next) => patch({ history: pushMarkup(s.history, next), failed: false })}
      onPlace={(place, placeScale) => patch({ place, placeScale })}
      label={t('next.imageEdit.canvas')}
    />
  );
  const dock = (
    <MarkupDock
      switcher={switcher}
      tool={s.tool}
      onTool={pickTool}
      color={s.color}
      onColor={(color) => patch({ color })}
      canUndo={s.history.before.length > 0}
      onUndo={() => patch({ history: undoMarkup(s.history) })}
      text={s.text}
      onText={(text) => patch({ text })}
      focusField={s.place}
      onPlaceText={placeText}
      canFinish={hasMarkup(markup) && !s.saving}
      saving={s.saving}
      replacing={target.replacing}
      onFinish={finish}
      failed={s.failed}
    />
  );
  return {
    layer,
    dock,
    locked: s.tool !== null,
    ratio: natural === null ? undefined : natural.w / natural.h,
    onBack: () => {
      if (s.tool === null) return false;
      patch({ tool: null, place: null, placeScale: undefined, text: '' });
      return true;
    },
  };
};

interface IMarkupDockProps {
  switcher?: ReactNode;
  tool: TMarkupTool | null;
  onTool: (tool: TMarkupTool) => void;
  color: string;
  onColor: (color: string) => void;
  canUndo: boolean;
  onUndo: () => void;
  text: string;
  onText: (text: string) => void;
  /** A new place for the words: the field takes the cursor. */
  focusField: IPoint | null;
  onPlaceText: () => void;
  canFinish: boolean;
  saving: boolean;
  replacing: boolean;
  onFinish: () => void;
  failed: boolean;
}

const MarkupDock = (props: IMarkupDockProps) => {
  const { tool, focusField } = props;
  const root = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (tool === MARKUP_TOOL.text) field.current?.focus();
  }, [tool, focusField]);
  useKeyboardInset(root, '.hx-imgview__dock');
  const colored = tool !== null && tool !== MARKUP_TOOL.crop;
  const finishLabel = props.replacing
    ? t('next.imageEdit.markup.replace')
    : t('next.imageEdit.markup.attach');
  return (
    <div ref={root} className="hx-ie">
      {props.switcher}
      {tool !== null && (
        <div className="hx-ie__opts" key={tool}>
          {colored &&
            MARK_COLORS.map((color) => (
              <button
                key={color}
                type="button"
                className="hx-ie__color"
                data-c={color}
                aria-pressed={props.color === color}
                aria-label={t(`next.imageEdit.color.${COLOR_NAME[color]}`)}
                onClick={() => props.onColor(color)}
              />
            ))}
          <button
            type="button"
            className="hx-ie__chip hx-ie__undo"
            aria-label={t('next.imageEdit.undo')}
            disabled={!props.canUndo}
            onClick={props.onUndo}
          >
            <Icon name="undo-2" />
          </button>
        </div>
      )}
      <div className="hx-ie__bar" role="toolbar" aria-label={t('next.imageEdit.tools')}>
        {MARKUP_TOOLS.map(({ id, icon }) => (
          <button
            key={id}
            type="button"
            className="hx-ie__tool"
            aria-pressed={tool === id}
            aria-label={t(`next.imageEdit.markup.tool.${id}`)}
            disabled={props.saving}
            onClick={() => props.onTool(id)}
          >
            <Icon name={icon} />
            <span className="hx-ie__long">{t(`next.imageEdit.markup.tool.${id}`)}</span>
            <span className="hx-ie__short" aria-hidden="true">
              {t(`next.imageEdit.markup.short.${id}`)}
            </span>
          </button>
        ))}
      </div>
      {tool === MARKUP_TOOL.text && (
        <div className="hx-ie__field hx-ie__words">
          <CardReply
            value={props.text}
            onChange={props.onText}
            busy={false}
            placeholder={t('next.imageEdit.markup.hint.text')}
            onSend={() => props.text.trim() !== '' && props.onPlaceText()}
            ready={props.text.trim() !== ''}
            inputRef={field}
          />
        </div>
      )}
      {props.failed && (
        <p className="hx-ie__fail" role="status">
          <span>{t('next.imageEdit.markup.failed')}</span>
          <button type="button" className="hx-ie__retry" onClick={props.onFinish}>
            {t('next.imageEdit.retry')}
          </button>
        </p>
      )}
      <button
        type="button"
        className="hx-btn hx-ie__finish"
        data-variant="primary"
        data-busy={props.saving || undefined}
        disabled={!props.canFinish}
        onClick={props.onFinish}
      >
        {finishLabel}
      </button>
    </div>
  );
};
