// What is under the picture in the editor: the options of the tool (colours, brush, shapes, undo), the five tools, and the
// field with the round send button; and the versions as thumbnails. One layout for every screen: the dock is a
// container, and when it is narrow (a phone, a phone held sideways) the tools are a five-column grid with a short label
// under each icon.
import { useEffect, useRef, type ReactNode } from 'react';
import { CardReply } from '@/next/bindings/reply-field';
import useKeyboardInset from '@/next/features/composer/use-keyboard-inset';
import {
  EDIT_FORMATS,
  drawsTransparent,
  imageProviderName,
  type ImageProvider,
  type IImageVersion,
  type TEditFormat,
} from '@/next/features/image-edit/host';
import { PriceNote, ProviderPicker, type IProviderChoice } from '@/next/features/image-edit/provider-picker';
import type { IImageProviderInfo, TPrice } from '@/next/features/image-edit/providers';
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
  provider?: ImageProvider;
  why: string | null;
  onRetry: () => void;
}

interface IEditDockProps {
  /** Over the tools: the switch to the person's own marks, where the picture has them. */
  switcher?: ReactNode;
  /** The generators of the machine and the one picked; the price of a paid one. */
  providers: readonly IImageProviderInfo[];
  choice: IProviderChoice;
  onChoice: (choice: IProviderChoice) => void;
  price: TPrice;
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
  const provider = props.choice.provider;
  const root = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const { focusField } = props;
  useEffect(() => {
    if (focusField > 0) field.current?.focus();
  }, [focusField]);
  // Over the phone's keyboard: the dock is lifted by what the keyboard covers (as the composer's is).
  useKeyboardInset(root, '.hx-imgview__dock');
  const drawsLine = tool === TOOL.mark || tool === TOOL.erase;
  const hasOptions = drawsLine || tool === TOOL.size;
  return (
    <div ref={root} className="hx-ie">
      {props.switcher}
      <ProviderPicker
        providers={props.providers}
        choice={props.choice}
        editing
        disabled={running}
        onChoice={props.onChoice}
      />
      {!drawsTransparent(provider) && (
        <p role="note">{t('next.imageEdit.noTransparencyOf', { name: imageProviderName(provider) })}</p>
      )}
      <PriceNote price={props.price} name={imageProviderName(provider)} />
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
            disabled={running || (id === TOOL.cut && !drawsTransparent(provider))}
            title={
              id === TOOL.cut && !drawsTransparent(provider)
                ? t('next.imageEdit.noTransparencyOf', { name: imageProviderName(provider) })
                : undefined
            }
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
            {props.failure.provider ? ` · ${imageProviderName(props.failure.provider)}` : ''}
          </button>
        </p>
      )}
      {/* The chat's own field (HRN-1362), words to the picture model, with the voice where the host gives it. */}
      <div className="hx-ie__field">
        <CardReply
          value={props.text}
          onChange={props.onText}
          busy={running}
          placeholder={t(hintKey(tool, running))}
          onSend={() => props.canSend && props.onSend()}
          ready={props.canSend}
          inputRef={field}
        />
      </div>
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
          title={version.provider ? imageProviderName(version.provider) : undefined}
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
