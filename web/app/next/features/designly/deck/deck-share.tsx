// «Поделиться» of a deck: who opens it (the team, everyone by link, only the person), what the team may do with it
// (look, comment, edit: an editor opens the same editor), the people invited by address with their roles, how a link
// opens, what guests may do (a link without sign-in is never more than comments), the link and «Посмотреть как гость». It sits on the project's access (the team's level, the deck's guest link) and
// changes nothing on screen until the gateway has answered. The deck's link is the first live link whose `view` carries
// a deck mode; the flags are in that `view` (deck/deck-link-view.ts). The link never waits for the PDF: it is made or
// changed at once, and a PDF it offers is kept by the runtime (POST /design/deck/pdf), which builds it in the background,
// again whenever the deck changes, and publishes each build for the guests. One quiet line says how the PDF is doing.
// A video's share (`video`) is the same popover with the video's link (video/share-view.ts): no «Как откроется» and no
// PDF, the format the link opens in instead.
import { useEffect, useRef, useState } from 'react';
import {
  GUEST_VERSION,
  changeGuestLink,
  createGuestLink,
  putTeam,
  revokeGuestLink,
  type TTeamLevel,
  type IGuestLink,
  type IProjectAccess,
  type TGuestRights,
} from '@/next/features/designly/access/api';
import AccessAdd from '@/next/features/designly/access/access-add';
import AccessPeople from '@/next/features/designly/access/access-people';
import { copyText } from '@/next/features/designly/access/copy-link';
import { useAction } from '@/next/features/designly/access/use-action';
import { useGuestLinks } from '@/next/features/designly/access/use-guest-links';
import { ACCESS, useProjectAccess } from '@/next/features/designly/access/use-access';
import {
  DECK_LINK_DEFAULT,
  deckViewOf,
  readDeckLinkView,
  type IDeckLinkFlags,
} from '@/next/features/designly/deck/deck-link-view';
import {
  deckLinkOf,
  liveLinks,
  otherLinks,
  pdfLineOf,
  revokeAll,
  type TPdfLine,
} from '@/next/features/designly/deck/share-links';
import { putDeckPdfAuto } from '@/next/features/designly/deck/api';
import {
  DECK_AUDIENCE,
  DECK_OPENS,
  type TDeckAudience,
  type TDeckOpens,
} from '@/next/features/designly/deck/types';
import Icon from '@/next/ui/icon';
import Switch from '@/next/ui/switch';
import { Button } from '@/next/ui/button';
import { toast } from '@/next/features/designly/sdk/toast';
import { t } from '@/next/i18n';
import { formatAgoFull } from '@/next/i18n/format';
import type { MachineId } from '@/next/protocol/v2';
import { changeDeck, useDeck } from '@/next/stores/deck';
import { useVideo } from '@/next/stores/video';
import { readVideoLinkView, videoLinkOf, videoViewOf } from '@/next/features/designly/video/share-view';
import type { TVideoFormat } from '@/next/features/designly/video/types';
import { useMe } from '@/next/stores/me';
import '@/next/styles/design-access.css';

export interface IDeckShareProps {
  /** The machine the deck lives on: the PDF for guests is built there. */
  machine: MachineId;
  /** The project's id: the gateway's `project` and the deck routes' `artifact`. */
  project: string;
  title: string;
  /** The popover is leaving (its closing animation runs). */
  closing: boolean;
  /** «Посмотреть как гость»: the host turns to the link scene with the flags as they are. */
  onAsGuest: (flags: IDeckLinkFlags) => void;
  /** A video's share: the formats it is made in (one or two), the first is the link's when it says none. */
  video?: { formats: readonly [TVideoFormat, ...TVideoFormat[]] };
}

const AUDIENCES: readonly TDeckAudience[] = [DECK_AUDIENCE.team, DECK_AUDIENCE.link, DECK_AUDIENCE.me];
const OPENS: readonly TDeckOpens[] = [DECK_OPENS.slides, DECK_OPENS.full, DECK_OPENS.video];

const AUDIENCE_KEY = {
  team: 'next.deck.share.team',
  link: 'next.deck.share.link',
  me: 'next.deck.share.me',
} as const satisfies Record<TDeckAudience, string>;
const OPENS_KEY = {
  slides: 'next.deck.share.slides',
  full: 'next.deck.share.full',
  video: 'next.deck.share.video',
} as const satisfies Record<TDeckOpens, string>;

/** What the team may do once the deck is open to it; «none» is the audience «Только я». */
const TEAM_PICK = ['view', 'comment', 'edit'] as const satisfies readonly TTeamLevel[];
type TTeamPick = (typeof TEAM_PICK)[number];
const TEAM_KEY = {
  view: 'next.deck.share.teamView',
  comment: 'next.deck.share.teamComment',
  edit: 'next.deck.share.teamEdit',
} as const satisfies Record<TTeamPick, string>;
/** The level a team gets when the deck is opened to it and it had none. */
const TEAM_DEFAULT: TTeamPick = 'comment';

const rightsOf = (comments: boolean): TGuestRights => (comments ? 'comment' : 'view');

const pdfText = (line: TPdfLine, now: number): string => {
  switch (line.k) {
    case 'building':
      return t('next.deck.share.pdfBuilding');
    case 'updating':
      return t('next.deck.share.pdfUpdating');
    case 'ready':
      return t('next.deck.share.pdfReady', { when: formatAgoFull(line.at, now) });
    case 'failed':
      return t('next.deck.share.pdfFailed', { reason: line.reason });
  }
};

const DeckShare = ({ machine, project, title, closing, onAsGuest, video }: IDeckShareProps) => {
  const deck = useDeck(project);
  const videoState = useVideo(project);
  const access = useProjectAccess(project);
  const guests = useGuestLinks(project);
  const action = useAction();
  const me = useMe();
  // What the toggles say while there is no link yet: they become the link's when it is made.
  const [draft, setDraft] = useState<IDeckLinkFlags & { comments: boolean; format: TVideoFormat | null }>({
    ...DECK_LINK_DEFAULT,
    pdf: video === undefined,
    comments: true,
    format: null,
  });
  const [now] = useState(() => Date.now());

  const links = guests.state.k === 'ready' ? guests.state.links : [];
  const live = liveLinks(links, now);
  const link = video ? videoLinkOf(live) : deckLinkOf(live);
  const others = video ? live.filter((l) => l !== link) : otherLinks(live);
  const team = access.state.k === ACCESS.ready ? access.state.access.team : null;
  const loaded = video ? videoState !== undefined : deck !== undefined;
  const ready = guests.state.k === 'ready' && access.state.k === ACCESS.ready && loaded;
  // The PDF is built by a headless Chrome: without one there is nothing to publish, so a new link carries no PDF.
  const canPdf = video === undefined && deck?.chrome === true;

  const videoLink = video && link ? readVideoLinkView(link.view) : null;
  const deckFlags = !video && link ? readDeckLinkView(link.view) : null;
  const flags: IDeckLinkFlags = videoLink
    ? { opens: DECK_OPENS.video, pdf: false, stats: videoLink.stats }
    : deckFlags || { ...draft, pdf: draft.pdf && canPdf };
  const format: TVideoFormat | null = video ? (videoLink?.format ?? draft.format ?? video.formats[0]) : null;
  /** The link's `view` for these flags: a video's carries its format. */
  const viewOf = (next: IDeckLinkFlags, at: TVideoFormat | null) =>
    at === null ? deckViewOf(next) : videoViewOf({ stats: next.stats, format: at });
  // «Видео» opens the published recording; until there is one guests get the slides (a video's guests a placeholder),
  // and the popover says so.
  const noDeckVideo =
    !video && flags.opens === DECK_OPENS.video && deck !== undefined && !deck.published.video;
  const noVideoMp4 =
    video !== undefined && videoState !== undefined && !videoState.renders.some((r) => r.published);
  const comments = link ? link.rights === 'comment' : draft.comments;
  // Any live link opens the deck, so the audience is «Все по ссылке» while one is there, whatever it was made for.
  const audience: TDeckAudience =
    live.length > 0
      ? DECK_AUDIENCE.link
      : team !== null && team !== 'none'
        ? DECK_AUDIENCE.team
        : DECK_AUDIENCE.me;
  const idle = action.busy === null;
  const pdfLine = link && flags.pdf && deck !== undefined ? pdfLineOf(deck.published) : null;

  const takeAccess = (next: IProjectAccess) => access.apply(next);
  const putLink = (next: IGuestLink) =>
    guests.put(
      links.some((l) => l.id === next.id)
        ? links.map((l) => (l.id === next.id ? next : l))
        : [next, ...links],
    );
  /** Starts or stops the PDF the runtime keeps for guests; the deck takes its answer. It returns at once. */
  const keepPdf = async (on: boolean) => {
    const published = await putDeckPdfAuto(machine, project, on);
    changeDeck(project, (d) => ({ ...d, published }));
  };
  const dropLinks = (ids: readonly string[]) =>
    guests.put(links.map((l) => (ids.includes(l.id) ? { ...l, revokedAt: new Date().toISOString() } : l)));

  const pickAudience = (next: TDeckAudience) => {
    if (next === audience && (next !== DECK_AUDIENCE.link || link !== null)) return;
    void action.run(`who:${next}`, async () => {
      if (next === DECK_AUDIENCE.link) {
        // The link is made now; the team keeps what it has. A PDF it offers is built after it, in the background.
        putLink(
          await createGuestLink(project, {
            title,
            rights: rightsOf(draft.comments),
            ttl: null,
            version: GUEST_VERSION.latest,
            pages: null,
            view: viewOf(flags, format),
          }),
        );
        if (flags.pdf) await keepPdf(true);
        return;
      }
      // Narrowing closes every link of the project, not only the deck's; the team level changes once all are closed.
      const outcome = await revokeAll(live, (id) => revokeGuestLink(project, id));
      dropLinks(outcome.revoked);
      if (outcome.failed.length > 0) throw new Error(t('next.deck.share.revokeFailed'));
      // Without a link nobody downloads the PDF: the runtime stops building it.
      if (deck?.published.pdfAuto === true) await keepPdf(false);
      // The team keeps the level it has (an «edit» stays «edit»); one it had none of gets the default.
      const level = team !== null && team !== 'none' ? team : TEAM_DEFAULT;
      takeAccess(await putTeam(project, next === DECK_AUDIENCE.team ? level : 'none'));
    });
  };

  const pickTeam = (next: TTeamPick) => {
    if (next === team) return;
    void action.run(`team:${next}`, async () => takeAccess(await putTeam(project, next)));
  };

  const change = (
    key: string,
    next: Partial<IDeckLinkFlags & { comments: boolean; format: TVideoFormat }>,
  ) => {
    const merged = { ...flags, comments, format, ...next };
    if (!link) {
      setDraft(merged);
      return;
    }
    void action.run(key, async () => {
      putLink(
        await changeGuestLink(project, link.id, {
          rights: rightsOf(merged.comments),
          view: viewOf(merged, merged.format),
        }),
      );
      if (merged.pdf !== flags.pdf) await keepPdf(merged.pdf);
    });
  };

  // A link that offered the PDF before the runtime kept it up to date: it is kept from now on, once per opening.
  const adopted = useRef(false);
  const adopt = ready && link !== null && flags.pdf && canPdf && deck?.published.pdfAuto === false;
  useEffect(() => {
    if (!adopt || adopted.current) return;
    adopted.current = true;
    void action.run('pdf', () => keepPdf(true));
  });

  /** What the project's access blocks (the add field, the people) take: the popover's one action and the fresh state. */
  const section = (current: IProjectAccess) => ({ project, access: current, action, onNext: takeAccess });

  const copy = () => {
    if (!link) return;
    copyText(Promise.resolve(link.url)).then(
      () => toast(t('next.deck.toast.linkCopied')),
      (failure: Error) => toast(t('next.site.copyFailed'), { tone: 'error', description: failure.message }),
    );
  };

  return (
    <div
      className="hx-deck-share"
      data-closing={closing ? '1' : '0'}
      data-busy={idle ? undefined : '1'}
      role="dialog"
      aria-label={t('next.deck.share.label')}
    >
      <div className="hx-deck-share__h" data-first="1">
        {t('next.deck.share.who')}
      </div>
      <div className="hx-deck-share__seg" role="radiogroup" aria-label={t('next.deck.share.who')}>
        {AUDIENCES.map((a) => (
          <button
            key={a}
            type="button"
            role="radio"
            aria-checked={audience === a}
            data-on={audience === a ? '1' : '0'}
            disabled={!ready || !idle}
            onClick={() => pickAudience(a)}
          >
            {t(AUDIENCE_KEY[a])}
          </button>
        ))}
      </div>
      {team !== null && team !== 'none' && (
        <>
          <div className="hx-deck-share__h">{t('next.deck.share.teamCan')}</div>
          <div className="hx-deck-share__seg" role="radiogroup" aria-label={t('next.deck.share.teamCan')}>
            {TEAM_PICK.map((level) => (
              <button
                key={level}
                type="button"
                role="radio"
                aria-checked={team === level}
                data-on={team === level ? '1' : '0'}
                disabled={!ready || !idle}
                onClick={() => pickTeam(level)}
              >
                {t(TEAM_KEY[level])}
              </button>
            ))}
          </div>
        </>
      )}
      {access.state.k === ACCESS.ready && (
        <div className="hx-deck-share__people">
          <div className="hx-deck-share__h">{t('next.deck.share.people')}</div>
          <AccessAdd {...section(access.state.access)} compact />
          {(access.state.access.people ?? []).length > 0 && (
            <AccessPeople {...section(access.state.access)} me={me.k === 'ready' ? me.me.email : null} />
          )}
        </div>
      )}
      {!video && (
        <>
          <div className="hx-deck-share__h">{t('next.deck.share.how')}</div>
          <div className="hx-deck-share__seg" role="radiogroup" aria-label={t('next.deck.share.how')}>
            {OPENS.map((o) => (
              <button
                key={o}
                type="button"
                role="radio"
                aria-checked={flags.opens === o}
                data-on={flags.opens === o ? '1' : '0'}
                disabled={!ready || !idle}
                onClick={() => change('opens', { opens: o })}
              >
                {t(OPENS_KEY[o])}
              </button>
            ))}
          </div>
        </>
      )}
      {video && video.formats.length > 1 && (
        <>
          <div className="hx-deck-share__h">{t('next.video.shell.shareFormat')}</div>
          <div
            className="hx-deck-share__seg"
            role="radiogroup"
            aria-label={t('next.video.shell.shareFormat')}
          >
            {video.formats.map((f) => (
              <button
                key={f}
                type="button"
                role="radio"
                aria-checked={format === f}
                data-on={format === f ? '1' : '0'}
                disabled={!ready || !idle}
                onClick={() => change('format', { format: f })}
              >
                {f}
              </button>
            ))}
          </div>
        </>
      )}
      {noDeckVideo && <p className="hx-deck-share__note">{t('next.deck.share.noVideo')}</p>}
      {noVideoMp4 && <p className="hx-deck-share__note">{t('next.video.shell.shareNoMp4')}</p>}
      <div className="hx-deck-share__opts">
        <div className="hx-deck-share__opt">
          <span>{t('next.deck.share.guestComments')}</span>
          <Switch
            label={t('next.deck.share.guestCommentsLabel')}
            checked={comments}
            disabled={!ready || !idle}
            onChange={(on) => change('comments', { comments: on })}
          />
        </div>
        {!video && (
          <div className="hx-deck-share__opt">
            <span>{t('next.deck.share.pdf')}</span>
            <Switch
              label={t('next.deck.share.pdfLabel')}
              checked={flags.pdf}
              disabled={!ready || !idle || (!canPdf && !flags.pdf)}
              onChange={(on) => change('pdf', { pdf: on })}
            />
          </div>
        )}
        {pdfLine !== null && (
          <p className="hx-deck-share__note" data-pdf={pdfLine.k} role="status">
            {pdfText(pdfLine, now)}
          </p>
        )}
        <div className="hx-deck-share__opt">
          <span>{t('next.deck.share.stats')}</span>
          <Switch
            label={t('next.deck.share.statsLabel')}
            checked={flags.stats}
            disabled={!ready || !idle}
            onChange={(on) => change('stats', { stats: on })}
          />
        </div>
      </div>
      {link && (
        <div className="hx-deck-share__link">
          <Icon name="link" size="sm" />
          <span>{link.url.replace(/^https?:\/\//, '')}</span>
          <Button variant="primary" size="sm" onClick={copy}>
            {t('next.deck.share.copy')}
          </Button>
        </div>
      )}
      {others.length > 0 && <p className="hx-deck-share__note">{t('next.deck.share.otherLinks')}</p>}
      {action.error && (
        <p className="hx-deck-share__error" role="alert">
          {action.error}
        </p>
      )}
      <button type="button" className="hx-deck-share__item" onClick={() => onAsGuest(flags)}>
        <Icon name="eye" />
        <span>{t('next.deck.share.asGuest')}</span>
      </button>
    </div>
  );
};

export default DeckShare;
