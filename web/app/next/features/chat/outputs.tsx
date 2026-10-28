// What a turn made, at the bottom of its answer: the things the person opens — a published page,
// a design, a document, a merge request, a stand — not «I created a file». Each card is the thing's name and what it
// is; a tap opens it in the right pane. Which kinds get a card is the person's choice (Settings → General); the plain
// files the turn wrote are one of them, off by default.
import { createContext, useContext } from 'react';
import type { FileOp, OutputKind, TurnItem, TurnOutput } from '@/next/features/chat/types';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import DeliveryCard from '@/next/features/chat/delivery-card';

export type TShownKind = OutputKind | 'file';
export type TShownKinds = Readonly<Record<TShownKind, boolean>>;

/** Everything but the plain files: what the feed shows until the host says otherwise. */
export const SHOWN_BY_DEFAULT: TShownKinds = {
  artifact: true,
  design: true,
  doc: true,
  mr: true,
  stand: true,
  delivery: true,
  file: false,
} as const;

export interface IOutputsContext {
  shown: TShownKinds;
  /** A thing the turn made, in the pane; without it the cards are not something to tap. */
  onOpen?: (output: TurnOutput) => void;
  /** A plain file of the turn, in the pane's Files. */
  onOpenFile?: (path: string) => void;
  /** Where a file the agent handed over is saved from; without it the card has only its viewer. */
  downloadUrl?: (path: string) => string;
  /** «Поделиться» of a document of the turn that is a file of the chat's folder. */
  onShareFile?: (path: string) => void;
}

export const OutputsContext = createContext<IOutputsContext>({ shown: SHOWN_BY_DEFAULT });

const KIND_ICON = {
  artifact: 'book-open',
  design: 'palette',
  doc: 'file-text',
  mr: 'git-pull-request',
  stand: 'rocket',
  delivery: 'file-text',
} as const satisfies Record<OutputKind, string>;

const FILE_ICON = { created: 'file-plus', edited: 'file-pen', read: 'file-search' } as const satisfies Record<
  FileOp,
  string
>;

interface ICard {
  key: string;
  icon: string;
  accent: boolean;
  title: string;
  meta: string;
  open: (() => void) | null;
  /** Makes the card's file a shared document; only a Markdown or HTML file of the folder has it. */
  share: (() => void) | null;
}

const SHAREABLE = /\.(md|markdown|html?)$/i;

const outputCard = (output: TurnOutput, context: IOutputsContext): ICard => {
  const { onOpen, onShareFile } = context;
  const kind = t(`next.outputs.kind.${output.kind}`);
  const shareable = output.kind === 'doc' && !/^https?:\/\//.test(output.ref) && SHAREABLE.test(output.ref);
  return {
    share: shareable && onShareFile ? () => onShareFile(output.ref) : null,
    key: `${output.kind}:${output.ref}`,
    icon: KIND_ICON[output.kind],
    accent: output.op === 'created',
    title: output.title,
    meta: output.op === 'changed' ? `${kind} · ${t('next.outputs.changed')}` : kind,
    open: onOpen ? () => onOpen(output) : null,
  };
};

const fileCard = (file: { path: string; op: FileOp }, onOpenFile: IOutputsContext['onOpenFile']): ICard => {
  const slash = file.path.lastIndexOf('/');
  const where = slash > 0 ? ` · ${file.path.slice(0, slash)}` : '';
  return {
    share: null,
    key: `file:${file.path}`,
    icon: FILE_ICON[file.op],
    accent: file.op === 'created',
    title: file.path.slice(slash + 1),
    meta: t(`next.files.${file.op}`) + where,
    open: onOpenFile ? () => onOpenFile(file.path) : null,
  };
};

/** The cards of a turn: its outputs of the kinds shown, then its plain files when they are. */
export const cardsOf = (item: TurnItem, context: IOutputsContext): ICard[] => {
  const { shown } = context;
  const outputs = (item.turn.outputs ?? []).filter((o) => shown[o.kind] && o.kind !== 'delivery');
  const files = shown.file ? item.turn.files.filter((f) => f.op !== 'read') : [];
  return [
    ...outputs.map((o) => outputCard(o, context)),
    ...files.map((f) => fileCard(f, context.onOpenFile)),
  ];
};

const Card = ({ card, index }: { card: ICard; index: number }) => {
  const { open, share } = card;
  const body = (
    <>
      <span className="hx-file__ico" data-tone={card.accent ? 'accent' : undefined}>
        <Icon name={card.icon} />
      </span>
      <span className="hx-file__text">
        <span className="hx-file__name">{card.title}</span>
        <span className="hx-file__meta">{card.meta}</span>
      </span>
    </>
  );
  if (!open)
    return (
      <div className="hx-file" data-j={index}>
        {body}
      </div>
    );
  // A card that can be shared is a row of two buttons: the card itself and «Поделиться» at its end.
  if (share)
    return (
      <div className="hx-file hx-file--row" data-j={index}>
        <button type="button" className="hx-file__open" data-open="true" onClick={open}>
          {body}
        </button>
        <button
          type="button"
          className="hx-head-btn hx-file__share"
          aria-label={t('next.share.button')}
          onClick={share}
        >
          <Icon name="share" size="sm" />
        </button>
      </div>
    );
  return (
    <button type="button" className="hx-file" data-j={index} data-open="true" onClick={open}>
      {body}
    </button>
  );
};

/** The files the turn handed to the person: each is its own card, with the way to save it. */
export const deliveriesOf = (item: TurnItem, context: IOutputsContext): TurnOutput[] =>
  context.shown.delivery ? (item.turn.outputs ?? []).filter((o) => o.kind === 'delivery') : [];

const Outputs = ({ item }: { item: TurnItem }) => {
  const context = useContext(OutputsContext);
  const cards = cardsOf(item, context);
  const deliveries = deliveriesOf(item, context);
  if (cards.length === 0 && deliveries.length === 0) return null;
  return (
    <div className="hx-files">
      {deliveries.map((output, index) => (
        <DeliveryCard
          key={output.ref}
          output={output}
          index={index}
          onOpen={context.onOpen}
          downloadUrl={context.downloadUrl}
        />
      ))}
      {cards.map((card, index) => (
        <Card key={card.key} card={card} index={index} />
      ))}
    </div>
  );
};

export default Outputs;
