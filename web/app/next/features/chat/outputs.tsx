// What a turn made, at the bottom of its answer: the things the person opens — a published page,
// a design, a document, a merge request, a stand — not «I created a file». Each card is the thing's name and what it
// is; a tap opens it in the right pane. Which kinds get a card is the person's choice (Settings → General); the plain
// files the turn wrote are one of them, off by default.
import { createContext, useContext, useMemo } from 'react';
import type { FileOp, OutputKind, TurnItem, TurnOutput } from '@/next/features/chat/types';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import DeliveryCard from '@/next/features/chat/delivery-card';
import { PictureCards } from '@/next/features/chat/pictures';
import { LINK_ICON, linkOf, viewOf, type ILinkOutput } from '@/next/features/chat/link-output';
import { usePortUp } from '@/next/features/chat/port-up';
import OutputRow from '@/next/features/chat/output-row';
import Made from '@/next/features/chat/made';
import { useArtifactMetas } from '@/next/features/artifacts/meta-store';
import type { IArtifactItem } from '@/next/features/artifacts/model';
import type { MachineId } from '@/next/protocol/v2';
import { appConfig } from '@/platform/config';

/** `mrDone`: the block of the chat's merge requests under a finished task (mr-done.tsx). `made`, `preview` and
 *  `bigShots` are how the cards look (stores/feed-kinds.ts). */
export type TShownKind = OutputKind | 'file' | 'mrDone' | 'made' | 'preview' | 'bigShots';
export type TShownKinds = Readonly<Record<TShownKind, boolean>>;

/** Everything but the plain files and the merge request card: what the feed shows until the host says otherwise. */
export const SHOWN_BY_DEFAULT: TShownKinds = {
  artifact: true,
  design: true,
  doc: true,
  mr: true,
  stand: true,
  delivery: true,
  task: true,
  commit: true,
  file: false,
  mrDone: false,
  made: true,
  preview: true,
  bigShots: true,
} as const;

export interface IOutputsContext {
  shown: TShownKinds;
  /** A thing the turn made, in the pane; without it the cards are not something to tap. */
  onOpen?: (output: TurnOutput) => void;
  /** A plain file of the turn, in the pane's Files. */
  onOpenFile?: (path: string) => void;
  /** Where a file the agent handed over is saved from; without it the card has only its viewer. */
  downloadUrl?: (path: string, output?: TurnOutput) => string;
  /** Where the browser shows a handed-over file in a tab of its own (⌘/Ctrl-click on its card). */
  viewUrl?: (path: string, output?: TurnOutput) => string;
  /** «Поделиться» of a document of the turn that is a file of the chat's folder. */
  onShareFile?: (path: string) => void;
  /** The chat's machine: the cards of links read their live title and state from it; without it they show the turn's. */
  machine?: MachineId;
}

export const OutputsContext = createContext<IOutputsContext>({ shown: SHOWN_BY_DEFAULT });

const KIND_ICON = {
  artifact: 'book-open',
  design: 'palette',
  doc: 'file-text',
  mr: 'git-pull-request',
  stand: 'rocket',
  delivery: 'file-text',
  task: 'square-check-big',
  commit: 'git-commit-horizontal',
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

/** The things the turn made that are links: each is a live card of its service (link-output.ts). */
export const linksOf = (
  item: TurnItem,
  context: IOutputsContext,
): { output: TurnOutput; link: ILinkOutput }[] => linksAmong(item.turn.outputs ?? [], context);

const linksAmong = (
  outputs: readonly TurnOutput[],
  context: IOutputsContext,
): { output: TurnOutput; link: ILinkOutput }[] =>
  outputs.flatMap((output) => {
    if (!context.shown[output.kind] || output.kind === 'delivery') return [];
    const link = linkOf(output, appConfig());
    return link ? [{ output, link }] : [];
  });

/** The cards of a turn that are no links: its outputs of the kinds shown, then its plain files when they are. */
export const cardsOf = (item: TurnItem, context: IOutputsContext): ICard[] =>
  cardsAmong(
    item.turn.outputs ?? [],
    context.shown.file ? item.turn.files.filter((f) => f.op !== 'read') : [],
    context,
  );

const cardsAmong = (
  all: readonly TurnOutput[],
  files: readonly { path: string; op: FileOp }[],
  context: IOutputsContext,
): ICard[] => {
  const { shown } = context;
  const outputs = all.filter(
    (o) => shown[o.kind] && o.kind !== 'delivery' && linkOf(o, appConfig()) === null,
  );
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

const NO_ITEMS: readonly IArtifactItem[] = [];
const NO_MACHINE = '' as MachineId;

/**
 * A link the turn made, as an attachment of Linear: the service's icon, the live title, what it is and its state (a
 * merge request open or merged and its pipeline, a task's state and who has it, a stand up or not). A tap opens it
 * in the pane, where the runtime puts it (the browser, GitLab, the task, the documents); ⌘/Ctrl-click and the arrow at
 * the end open it in a new tab of the browser.
 */
const LinkOutputCard = ({
  output,
  link,
  index,
}: {
  output: TurnOutput;
  link: ILinkOutput;
  index: number;
}) => {
  const { onOpen, machine } = useContext(OutputsContext);
  const items = useMemo(() => (machine && link.item ? [link.item] : NO_ITEMS), [machine, link.item]);
  const metas = useArtifactMetas(machine ?? NO_MACHINE, items);
  const up = usePortUp(machine, link.port);
  const read = link.item ? metas.of(link.item) : null;
  const view = viewOf(link, read?.meta ?? null, read?.look ?? 'none', up);
  return (
    <OutputRow
      index={index}
      icon={LINK_ICON[link.kind]}
      title={view.title ?? output.title}
      meta={
        <>
          {view.tone && <span className="hx-file__dot" data-tone={view.tone} aria-hidden="true" />}
          {view.meta}
        </>
      }
      pending={view.pending}
      href={link.url}
      newTab={link.newTab}
      onOpen={() => onOpen?.(output)}
      end={
        link.newTab && (
          <a
            className="hx-head-btn hx-file__share"
            href={link.url}
            target="_blank"
            rel="noopener noreferrer"
            aria-label={t('next.outputs.newTab')}
            title={t('next.outputs.newTab')}
          >
            <Icon name="external-link" size="sm" />
          </a>
        )
      }
    />
  );
};

const PICTURE_EXT = /\.(?:png|jpe?g|gif|webp|svg)$/i;

/** A handed-over file the feed shows as the picture it is (deliver_file of a PNG, a poster for print), not as a line. */
export const isPictureFile = (output: Pick<TurnOutput, 'ref' | 'name' | 'mime'>): boolean =>
  output.mime?.startsWith('image/') === true || PICTURE_EXT.test(output.name ?? output.ref);

/** The files the turn handed to the person: each is a card of one line, with the way to save it. */
export const deliveriesOf = (item: TurnItem, context: IOutputsContext): TurnOutput[] =>
  context.shown.delivery ? (item.turn.outputs ?? []).filter((o) => o.kind === 'delivery') : [];

/** The pictures a finished turn drew, shown under its answer: the folded work would hide their cards. */
export const drawnOf = (item: TurnItem): readonly string[] =>
  item.turn.status === 'live' ? [] : (item.turn.pictures ?? []);

const Outputs = ({ item }: { item: TurnItem }) => {
  const context = useContext(OutputsContext);
  const outputs = item.turn.outputs ?? [];
  const files = context.shown.file ? item.turn.files.filter((f) => f.op !== 'read') : [];
  return (
    <OutputCards
      outputs={outputs}
      files={files}
      cards={cardsOf(item, context)}
      links={linksOf(item, context)}
      deliveries={deliveriesOf(item, context)}
      drawn={drawnOf(item)}
    />
  );
};

/**
 * Things made elsewhere, as the cards of a turn: what a delegated chat made, under its card «Поручено».
 * The same live cards of links and files, by the person's choice of kinds.
 */
export const OutputList = ({ outputs }: { outputs: readonly TurnOutput[] }) => {
  const context = useContext(OutputsContext);
  const deliveries = context.shown.delivery ? outputs.filter((o) => o.kind === 'delivery') : [];
  return (
    <OutputCards
      outputs={outputs}
      files={[]}
      cards={cardsAmong(outputs, [], context)}
      links={linksAmong(outputs, context)}
      deliveries={deliveries}
      head={false}
    />
  );
};

/** The things of the kinds shown that are no links and no files handed over: a document of the folder, a design. */
const plainAmong = (all: readonly TurnOutput[], context: IOutputsContext): TurnOutput[] =>
  all.filter((o) => context.shown[o.kind] && o.kind !== 'delivery' && linkOf(o, appConfig()) === null);

const OutputCards = ({
  outputs,
  files,
  cards,
  links,
  deliveries,
  drawn = [],
  head = true,
}: {
  outputs: readonly TurnOutput[];
  files: readonly { path: string; op: FileOp }[];
  cards: ICard[];
  links: { output: TurnOutput; link: ILinkOutput }[];
  deliveries: TurnOutput[];
  drawn?: readonly string[];
  head?: boolean;
}) => {
  const context = useContext(OutputsContext);
  if (cards.length === 0 && deliveries.length === 0 && links.length === 0 && drawn.length === 0) return null;
  // A picture handed over shows as one: the same card, viewer and marks as a picture the agent drew; the
  // viewer keeps the way to save it. Without a way to show the file (a gallery) it stays a line.
  const view = context.viewUrl;
  const pictures = view ? deliveries.filter(isPictureFile) : [];
  const lines = deliveries.filter((output) => !pictures.includes(output));
  // What the turn drew and what it handed over, each once: a drawn picture it also delivered is one card.
  const shown = view ? [...new Set([...drawn, ...pictures.map((output) => output.ref)])] : [];
  const big = context.shown.made && context.shown.bigShots;
  const grid = shown.length > 0 && view && (
    <PictureCards
      pictures={shown.map((path) => ({ id: path, text: path }))}
      srcOf={(picture) =>
        view(
          picture.text,
          pictures.find((output) => output.ref === picture.text),
        )
      }
      big={big}
    />
  );
  // Several screenshots large are a block of their own, «Скриншоты · N», as the list of what the turn made is.
  const pictureCards =
    big && shown.length > 1 ? (
      <section className="hx-made hx-made--shots" aria-label={t('next.turnMade.shots', { n: shown.length })}>
        <div className="hx-made__head">
          <span>{t('next.turnMade.shots', { n: shown.length })}</span>
        </div>
        {grid}
      </section>
    ) : (
      grid
    );
  const deliveryCards = lines.map((output, index) => (
    <DeliveryCard
      key={output.ref}
      output={output}
      index={index}
      onOpen={context.onOpen}
      downloadUrl={context.downloadUrl}
      viewUrl={context.viewUrl}
    />
  ));
  // «Сделано за ход»: one list at the end of the answer (made.tsx).
  if (context.shown.made) {
    const plain = plainAmong(outputs, context);
    return (
      <div className="hx-outputs">
        {pictureCards}
        <Made
          context={context}
          links={links}
          outputs={plain}
          files={files}
          deliveries={deliveryCards}
          count={links.length + plain.length + files.length + lines.length}
          head={head}
        />
      </div>
    );
  }
  return (
    <div className="hx-files">
      {pictureCards}
      {deliveryCards}
      {links.map(({ output, link }, index) => (
        <LinkOutputCard key={`${output.kind}:${output.ref}`} output={output} link={link} index={index} />
      ))}
      {cards.map((card, index) => (
        <Card key={card.key} card={card} index={index} />
      ))}
    </div>
  );
};

export default Outputs;
