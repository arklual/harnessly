// What is under the picture in the editor: the options of the tool (colours, brush, shapes, undo), the five tools, and the
// field with the round send button; and the versions as thumbnails. One layout for every screen: the dock is a
// container, and when it is narrow (a phone, a phone held sideways) the tools are a five-column grid with a short label
// under each icon.
import { useEffect, useRef, type FormEvent } from 'react';
import useKeyboardInset from '@/next/features/composer/use-keyboard-inset';
import { EDIT_FORMATS, type IImageVersion, type TEditFormat } from '@/next/features/image-edit/host';
import { BRUSH_IDS, MARK_COLORS, type TBrushId } from '@/next/features/image-edit/marks';
import { TOOL, TOOLS, type TTool } from '@/next/features/image-edit/tool-rules';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

const COLOR_NAME = { '#ff3b5c': 'red', '#ffd60a': 'yellow', '#ffffff': 'white' } as const;

/** The field's hint: by tool, or that a change is running. */
export const hintKey = (tool: TTool | null, running: boolean): string => {
  if (running) return 'next.imageEdit.hint.running';
  return `next.imageEdit.hint.${tool ?? 'text'}`;
};

export interface IFailure {
  why: string | null;
  onRetry: () => void;
}

interface IEditDockProps {
  tool: TTool | null;
  onTool: (tool: TTool) => void;
  running: boolean;
  color: string;
  onColor: (color: string) => void;
  brush: TBrushId;
  onBrush: (brush: TBrushId) => void;
  format: TEditFormat;
  onFormat: (format: TEditFormat) => void;
  canUndo: boolean;
  onUndo: () => void;
  text: string;
  onText: (text: string) => void;
  /** Counts up when the field should take the cursor (a frame was just dropped). */
  focusField: number;
  canSend: boolean;
  onSend: () => void;
  failure: IFailure | null;
}

export const EditDock = (props: IEditDockProps) => {
  const { tool, running } = props;
  const root = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const { focusField } = props;
  useEffect(() => {
    if (focusField > 0) field.current?.focus();
  }, [focusField]);
  // Over the phone's keyboard: the dock is lifted by what the keyboard covers (as the composer's is).
  useKeyboardInset(root, '.hx-imgview__dock');
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (props.canSend) props.onSend();
  };
  const drawsLine = tool === TOOL.mark || tool === TOOL.erase;
  const hasOptions = drawsLine || tool === TOOL.size;
  return (
    <div ref={root} className="hx-ie">
      {hasOptions && (
        <div className="hx-ie__opts" key={tool}>
          {tool === TOOL.mark &&
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
          {tool === TOOL.erase &&
            BRUSH_IDS.map((id) => (
              <button
                key={id}
                type="button"
                className="hx-ie__chip"
                aria-pressed={props.brush === id}
                onClick={() => props.onBrush(id)}
              >
                {t(`next.imageEdit.brush.${id}`)}
              </button>
            ))}
          {tool === TOOL.size &&
            EDIT_FORMATS.map((format) => (
              <button
                key={format}
                type="button"
                className="hx-ie__chip"
                aria-pressed={props.format === format}
                aria-label={t('next.imageEdit.format', { format })}
                onClick={() => props.onFormat(format)}
              >
                {format}
              </button>
            ))}
          {drawsLine && (
            <button
              type="button"
              className="hx-ie__chip hx-ie__undo"
              aria-label={t('next.imageEdit.undo')}
              disabled={!props.canUndo}
              onClick={props.onUndo}
            >
              <Icon name="undo-2" />
            </button>
          )}
        </div>
      )}
      <div className="hx-ie__bar" role="toolbar" aria-label={t('next.imageEdit.tools')}>
        {TOOLS.map(({ id, icon }) => (
          <button
            key={id}
            type="button"
            className="hx-ie__tool"
            aria-pressed={id === TOOL.cut ? undefined : tool === id}
            aria-label={t(`next.imageEdit.tool.${id}`)}
            disabled={running}
            onClick={() => props.onTool(id)}
          >
            <Icon name={icon} />
            <span className="hx-ie__long">{t(`next.imageEdit.tool.${id}`)}</span>
            <span className="hx-ie__short" aria-hidden="true">
              {t(`next.imageEdit.short.${id}`)}
            </span>
          </button>
        ))}
      </div>
      {props.failure && (
        <p className="hx-ie__fail" role="status">
          <span>
            {props.failure.why === null
              ? t('next.imageEdit.failedPlain')
              : t('next.imageEdit.failed', { why: props.failure.why })}
          </span>
          <button type="button" className="hx-ie__retry" onClick={props.failure.onRetry}>
            {t('next.imageEdit.retry')}
          </button>
        </p>
      )}
      <form className="hx-ie__field" onSubmit={submit}>
        <textarea
          ref={field}
          className="hx-ie__input"
          rows={1}
          enterKeyHint="send"
          autoComplete="off"
          value={props.text}
          disabled={running}
          placeholder={t(hintKey(tool, running))}
          aria-label={t('next.imageEdit.hint.text')}
          onChange={(event) => props.onText(event.target.value)}
          onKeyDown={(event) => {
            // Enter sends, Shift+Enter is a new line; a key of the IME is not Enter.
            if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
            event.preventDefault();
            if (props.canSend) props.onSend();
          }}
        />
        <button
          type="submit"
          className="hx-ie__send"
          disabled={!props.canSend}
          aria-label={t('next.imageEdit.send')}
        >
          <Icon name="arrow-up" />
        </button>
      </form>
    </div>
  );
};

interface IVersionsProps {
  versions: readonly IImageVersion[];
  at: string;
  disabled: boolean;
  onPick: (version: IImageVersion) => void;
}

/** The thumbnails of the versions; none to show for a picture that has only its original. */
export const VersionStrip = ({ versions, at, disabled, onPick }: IVersionsProps) => {
  if (versions.length < 2) return null;
  return (
    <div className="hx-ie__versions hx-imgview__keep" role="group" aria-label={t('next.imageEdit.versions')}>
      {versions.map((version, i) => (
        <button
          key={version.id}
          type="button"
          className="hx-ie__thumb"
          data-clear={version.clear || undefined}
          aria-pressed={version.id === at}
          aria-label={i === 0 ? t('next.imageEdit.original') : t('next.imageEdit.version', { n: i + 1 })}
          disabled={disabled}
          onClick={() => onPick(version)}
        >
          <img src={version.src} alt="" draggable={false} />
        </button>
      ))}
    </div>
  );
};
