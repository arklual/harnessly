// The «Elements» tab of the site's DevTools: the page's tree of tags (opened by node, lit on the page while the pointer
// is on a row) and, beside it, the taken node's box, place and styles. The tree and the node come from the board's own
// script through the bridge (use-dom-bridge); a page that does not answer is said to be silent, not drawn from memory.
import { DOM_STATUS, type IDomBridge } from '@/next/features/designly/use-dom-bridge';
import { markOf, pathKey, selectorOf } from '@/next/features/designly/dom-tree';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

export interface ISiteElementsProps {
  dom: IDomBridge;
  /** The board's file in the project: the inspector names it. */
  file: string;
  /** The taken node goes to the chat; without a chat there is nowhere to send it. */
  onSend?: (words: string) => void;
}

const SiteElements = ({ dom, file, onSend }: ISiteElementsProps) => {
  const { detail, selected } = dom;
  const selectedKey = selected ? pathKey(selected) : null;

  let tree = null;
  if (dom.status === DOM_STATUS.silent) {
    tree = (
      <div className="hx-devtools__empty">
        {t('next.site.dev.elements.silent')}
        <button type="button" className="hx-els__retry" onClick={dom.reload}>
          {t('next.site.dev.elements.retry')}
        </button>
      </div>
    );
  } else if (dom.status !== DOM_STATUS.ready) {
    tree = <div className="hx-devtools__empty">{t('next.site.dev.elements.loading')}</div>;
  }

  const sendLine = detail
    ? `${selectorOf(detail)}${detail.text ? ` «${detail.text}»` : ''} · ${detail.rect.w} × ${detail.rect.h}`
    : '';

  return (
    <div className="hx-els">
      <div className="hx-els__tree" onPointerLeave={() => dom.hover(null)}>
        {tree}
        {dom.status === DOM_STATUS.ready &&
          dom.rows.map((row) => {
            const note =
              row.open && row.node.more > 0
                ? t('next.site.dev.elements.more', { n: row.node.more })
                : row.node.text;
            return (
              <div
                key={row.key}
                className="hx-els__item"
                ref={(el) => el?.style.setProperty('--d', String(row.depth))}
                onPointerEnter={() => dom.hover(row.path)}
              >
                <button
                  type="button"
                  className="hx-els__row"
                  aria-selected={row.key === selectedKey}
                  onClick={() => dom.select(row.path)}
                >
                  <span className="hx-els__tag">&lt;{row.node.tag}</span>
                  <span className="hx-els__cls">{markOf(row.node)}</span>
                  <span className="hx-els__tag">&gt;</span>
                  <span className="hx-els__note">{note}</span>
                </button>
                {row.expandable && (
                  <button
                    type="button"
                    className="hx-els__twist"
                    aria-expanded={row.open}
                    aria-label={t(row.open ? 'next.site.dev.elements.fold' : 'next.site.dev.elements.unfold')}
                    onClick={() => dom.toggle(row.path)}
                  >
                    <Icon name={row.open ? 'chevron-down' : 'chevron-right'} size="sm" />
                  </button>
                )}
              </div>
            );
          })}
      </div>
      <div className="hx-els__side">
        {detail && (
          <>
            <div className="hx-els__sel">{selectorOf(detail)}</div>
            {detail.text && <div className="hx-els__label">{detail.text}</div>}
            <div className="hx-els__box">
              <span className="hx-els__dim">
                {detail.rect.w} × {detail.rect.h}
              </span>
            </div>
            <dl className="hx-els__kv">
              <dt>{t('next.site.dev.elements.place')}</dt>
              <dd>{`x ${detail.rect.x} · y ${detail.rect.y}`}</dd>
              <dt>{t('next.site.dev.elements.viewport')}</dt>
              <dd>{`${detail.viewport.w}×${detail.viewport.h}`}</dd>
              <dt>{t('next.site.dev.elements.file')}</dt>
              <dd title={file}>{file}</dd>
              {detail.styles.map((style) => (
                <StyleRow key={style.name} name={style.name} value={style.value} />
              ))}
            </dl>
            {onSend && (
              <button
                type="button"
                className="hx-els__send"
                onClick={() => onSend(`${t('next.site.dev.tab.elements')}: ${sendLine}`)}
              >
                <Icon name="message-square-plus" size="sm" />
                {t('next.site.dev.elements.toChat')}
              </button>
            )}
          </>
        )}
        {!detail && selected === null && (
          <div className="hx-els__empty">{t('next.site.dev.elements.pick')}</div>
        )}
      </div>
    </div>
  );
};

/** A style of the node: the name and the value it computes to. */
const StyleRow = ({ name, value }: { name: string; value: string }) => (
  <>
    <dt>{name}</dt>
    <dd title={value}>{value}</dd>
  </>
);

export default SiteElements;
