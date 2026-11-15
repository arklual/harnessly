// The small widgets a thing's big widget is made of: formatted text runs straight through it, and between its
// paragraphs stand a chart, figures, a table, a document's first page, a file, slides, design variants, a merge request
// as a list of files, review comments, a letter with its draft. A feed that sends no parts gets them made from the
// thing's plain fields (partsOf).
import { useState, type ReactNode } from 'react';
import { webUrl, type IDayCard, type TDayPart } from '@/next/features/day/feed';
import { feedIcon } from '@/next/features/day/icons';
import { cssVars } from '@/next/features/day/place';
import { inline, plain } from '@/next/features/day/rich';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

type TPartOf<K extends TDayPart['k']> = Extract<TDayPart, { k: K }>;

/** How many rows show before «Show the rest». */
const MR_SHOWN = 5;
const REVIEW_SHOWN = 2;
const BAR_PX = 56;

const PartHead = ({ icon, title, src }: { icon: string; title: string; src?: string }) => (
  <div className="hx-day-p__h">
    <Icon name={icon} size="sm" />
    <b>{title}</b>
    {src && <em>{src}</em>}
  </div>
);

/** A row that leads somewhere: a link when the feed gave an address, plain otherwise. */
const Target = ({
  url,
  className,
  children,
}: {
  url: string | undefined;
  className: string;
  children: ReactNode;
}) => {
  const href = webUrl(url);
  return href ? (
    <a className={className} href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  ) : (
    <div className={className}>{children}</div>
  );
};

const Flow = ({ rows }: { rows: TPartOf<'rich'>['v'] }) => (
  <>
    {rows.map((row, i) => {
      if (row[0] === 'h') return <h4 key={i}>{inline(row[1])}</h4>;
      if (row[0] === 'ul')
        return (
          <ul key={i}>
            {row[1].map((item, j) => (
              <li key={j}>{inline(item)}</li>
            ))}
          </ul>
        );
      return <p key={i}>{inline(row[1])}</p>;
    })}
  </>
);

/** Rows past the first few: they open with the height travelling from nothing, and fold back the same way. */
const Rest = ({ label, children }: { label: string; children: ReactNode }) => {
  const [open, setOpen] = useState(false);
  return (
    <>
      <div className="hx-day-rest" data-open={open ? '' : undefined}>
        <div inert={!open}>{children}</div>
      </div>
      <button
        type="button"
        className="hx-day-rest__b"
        data-open={open ? '' : undefined}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <span>{open ? t('next.day.part.fold') : label}</span>
        <Icon name="chevron-down" size="sm" />
      </button>
    </>
  );
};

const Checks = ({ ci }: { ci: TPartOf<'mr'>['ci'] }) =>
  ci && ci.length > 0 ? (
    <div className="hx-day-ci">
      {ci.map(([name, state], i) => (
        <span key={i}>
          <Icon name={state === 'ok' ? 'check' : 'x'} size="sm" />
          {name}
        </span>
      ))}
    </div>
  ) : null;

const Chart = ({ part }: { part: TPartOf<'chart'> }) => {
  const max = Math.max(...part.v.map((x) => x[1]), 1);
  return (
    <section className="hx-day-p" data-k="chart">
      <PartHead icon="chart-column" title={part.h} src={part.src} />
      <div className="hx-day-ch">
        {part.v.map(([label, value, hot], i) => (
          <div key={i} className="hx-day-ch__c" data-hot={hot ? '' : undefined}>
            <span>{value}</span>
            <i ref={cssVars({ '--h': `${Math.max(6, Math.round((value / max) * 100))}%` })} />
            <em>{label}</em>
          </div>
        ))}
      </div>
      {part.note && <p className="hx-day-p__n">{part.note}</p>}
    </section>
  );
};

const MergeRequest = ({ part }: { part: TPartOf<'mr'> }) => {
  const max = Math.max(...part.files.map((f) => f[1] + f[2]), 1);
  const row = ([path, added, removed, note]: TPartOf<'mr'>['files'][number], i: number) => {
    const cut = path.lastIndexOf('/') + 1;
    return (
      <div key={i} className="hx-day-mr__r">
        <span className="hx-day-mr__p">
          <em>{path.slice(0, cut)}</em>
          <b>{path.slice(cut)}</b>
        </span>
        {note && <span className="hx-day-mr__n">{note}</span>}
        <span className="hx-day-mr__d" aria-hidden="true">
          <i ref={cssVars({ '--w': `${Math.max(3, Math.round((added / max) * BAR_PX))}px` })} />
          {removed > 0 && (
            <i
              data-del=""
              ref={cssVars({ '--w': `${Math.max(3, Math.round((removed / max) * BAR_PX))}px` })}
            />
          )}
        </span>
        <span className="hx-day-mr__c">
          +{added} −{removed}
        </span>
      </div>
    );
  };
  return (
    <section className="hx-day-p" data-k="mr">
      <div className="hx-day-p__h">
        <Icon name="git-pull-request" size="sm" />
        <b>{part.h}</b>
        {part.src && <em>{part.src}</em>}
        {webUrl(part.url) && (
          <a className="hx-day-p__a" href={webUrl(part.url)} target="_blank" rel="noopener noreferrer">
            {t('next.day.part.openMr')}
            <Icon name="arrow-up-right" size="sm" />
          </a>
        )}
      </div>
      {part.sum && <p className="hx-day-mr__s">{part.sum}</p>}
      <div className="hx-day-mr">
        {part.files.slice(0, MR_SHOWN).map(row)}
        {part.files.length > MR_SHOWN && (
          <Rest label={t('next.day.part.restFiles')}>
            {part.files.slice(MR_SHOWN).map((file, i) => row(file, i + MR_SHOWN))}
          </Rest>
        )}
      </div>
      <Checks ci={part.ci} />
    </section>
  );
};

const Review = ({ part }: { part: TPartOf<'review'> }) => {
  const comment = ([file, code, text, instead]: TPartOf<'review'>['items'][number], i: number) => (
    <article key={i} className="hx-day-rv__c">
      <div className="hx-day-rv__f">
        <Icon name="file-code" size="sm" />
        <b>{file}</b>
      </div>
      <pre>{code}</pre>
      <p>{text}</p>
      {instead && (
        <div className="hx-day-rv__s">
          <em>{t('next.day.part.suggests')}</em>
          <pre>{instead}</pre>
        </div>
      )}
    </article>
  );
  return (
    <section className="hx-day-p" data-k="review">
      <PartHead icon="message-square" title={part.h} src={part.src} />
      <div className="hx-day-rv">
        {part.items.slice(0, REVIEW_SHOWN).map(comment)}
        {part.items.length > REVIEW_SHOWN && (
          <Rest label={t('next.day.part.rest')}>
            {part.items.slice(REVIEW_SHOWN).map((item, i) => comment(item, i + REVIEW_SHOWN))}
          </Rest>
        )}
      </div>
    </section>
  );
};

const SLIDE_LINES = [80, 60, 70] as const;
const DESIGN_BLOCKS = { table: 4, cards: 6, wizard: 3 } as const;

export const Part = ({ part }: { part: TDayPart }) => {
  switch (part.k) {
    case 'rich':
      return (
        <section className="hx-day-p" data-k="rich">
          <Flow rows={part.v} />
        </section>
      );
    case 'text':
      return (
        <section className="hx-day-p" data-k="text">
          {part.h && <PartHead icon={feedIcon(part.icon, 'file-text')} title={part.h} src={part.src} />}
          {part.ps?.map((text, i) => (
            <p key={i}>{inline(text)}</p>
          ))}
          {part.items && (
            <ul>
              {part.items.map((item, i) => {
                const [head, ...rest] = plain(item).split(' — ');
                return (
                  <li key={i}>
                    <b>{head}</b>
                    {rest.length > 0 && ` — ${rest.join(' — ')}`}
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      );
    case 'chart':
      return <Chart part={part} />;
    case 'kpi':
      return (
        <section className="hx-day-p" data-k="kpi">
          <PartHead icon="gauge" title={part.h} src={part.src} />
          <div className="hx-day-kpi">
            {part.v.map(([label, value, note], i) => (
              <div key={i}>
                <span>{label}</span>
                <b>{value}</b>
                {note && <em>{note}</em>}
              </div>
            ))}
          </div>
        </section>
      );
    case 'data':
      return (
        <section className="hx-day-p" data-k="data">
          <PartHead icon="layout-grid" title={part.h} src={part.src} />
          <div className="hx-day-tb" role="table">
            <div className="hx-day-tb__r" data-head="" role="row">
              {part.cols.map((col, i) => (
                <span key={i} role="columnheader">
                  {col}
                </span>
              ))}
            </div>
            {part.rows.map((row, i) => (
              <div key={i} className="hx-day-tb__r" data-hot={row.hot ? '' : undefined} role="row">
                {row.c.map((cell, j) => (
                  <span key={j} role="cell">
                    {cell}
                  </span>
                ))}
              </div>
            ))}
          </div>
          {part.note && <p className="hx-day-p__n">{part.note}</p>}
        </section>
      );
    case 'doc':
      return (
        <section className="hx-day-p" data-k="doc">
          <Target url={part.url} className="hx-day-doc">
            <div className="hx-day-doc__pg">
              <b>{part.title}</b>
              <Flow rows={part.body} />
            </div>
            <div className="hx-day-doc__f">
              <Icon name="file-text" size="sm" />
              <span>{part.meta ?? t('next.day.part.doc')}</span>
              {webUrl(part.url) && (
                <em>
                  {part.act ?? t('next.day.open')}
                  <Icon name="arrow-up-right" size="sm" />
                </em>
              )}
            </div>
          </Target>
        </section>
      );
    case 'file':
      return (
        <section className="hx-day-p" data-k="file">
          <Target url={part.url} className="hx-day-fl">
            <span className="hx-day-fl__ic">
              <Icon name={feedIcon(part.icon, 'file-text')} />
            </span>
            <span className="hx-day-fl__m">
              <b>{part.name}</b>
              {part.meta && <span>{part.meta}</span>}
            </span>
            {webUrl(part.url) && (
              <span className="hx-day-fl__b">
                {part.act ?? t('next.day.open')}
                <Icon name="arrow-up-right" size="sm" />
              </span>
            )}
          </Target>
        </section>
      );
    case 'slides':
      return (
        <section className="hx-day-p" data-k="slides">
          <PartHead icon="presentation" title={part.h} src={part.src} />
          <div className="hx-day-sl">
            {part.v.map(([title, kind, warn], i) => (
              <figure key={i} data-warn={warn ? '' : undefined}>
                <div className="hx-day-sl__s" data-k={kind}>
                  <b>{title}</b>
                  {kind === 'chart' && (
                    <span>
                      <i />
                      <i />
                      <i />
                      <i />
                    </span>
                  )}
                  {kind === 'text' &&
                    SLIDE_LINES.map((width) => <i key={width} ref={cssVars({ '--w': `${width}%` })} />)}
                </div>
                <figcaption>{warn ? t('next.day.part.slideCheck', { n: i + 1 }) : i + 1}</figcaption>
              </figure>
            ))}
          </div>
        </section>
      );
    case 'design':
      return (
        <section className="hx-day-p" data-k="design">
          <PartHead icon="layout-dashboard" title={part.h} src={part.src} />
          <div className="hx-day-ds">
            {part.v.map(([title, kind, note], i) => (
              <figure key={i}>
                <div className="hx-day-ds__s" data-k={kind}>
                  {Array.from({ length: DESIGN_BLOCKS[kind] }, (_, j) => (
                    <i key={j} />
                  ))}
                </div>
                <figcaption>
                  <b>{title}</b>
                  <span>{note}</span>
                </figcaption>
              </figure>
            ))}
          </div>
        </section>
      );
    case 'mr':
      return <MergeRequest part={part} />;
    case 'review':
      return <Review part={part} />;
    case 'message':
      return (
        <section className="hx-day-p" data-k="message">
          <PartHead icon={feedIcon(part.icon, 'messages-square')} title={part.h} src={part.src} />
          {part.quote && (
            <blockquote>
              {part.from && <b>{part.from}</b>}
              {part.quote}
            </blockquote>
          )}
          <div className="hx-day-ms">
            <em>{t('next.day.part.draft')}</em>
            <p>{part.draft}</p>
          </div>
        </section>
      );
  }
};

/** The parts of a thing: the feed's own, or made of its plain fields: the columns as text, the draft as a letter. */
export const partsOf = (card: IDayCard): TDayPart[] => {
  if (card.parts && card.parts.length > 0) return card.parts;
  const parts: TDayPart[] = (card.cols ?? [])
    .filter((col) => col.lines.length > 0)
    .map((col) => ({ k: 'text', h: col.title, items: col.lines }));
  if (card.draft)
    parts.push({
      k: 'message',
      h: card.where ?? card.whenText ?? t('next.day.part.letter'),
      from: card.people?.[0],
      quote: card.quote ? card.quote.replace(/[«»]/g, '') : undefined,
      draft: card.draft,
    });
  return parts;
};
