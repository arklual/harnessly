import { MoreHorizontal } from 'lucide-react';
import { LAYER_ALIGN, TW_MENU_KIND, TwKbd, TwMenuButton, cx } from '@/components/agents/ui';
import { AddressField, BrowserBar } from '@/components/agents/pane/address-bar';
import { PANE_ARIA_SHORTCUTS, PANE_KINDS, paneShortcutLabel } from '@/components/agents/pane/pane-registry';
import type { TPaneTabKind } from '@/lib/hl-pane-tabs';
import { t } from '@/lib/i18n';

export interface IToolCount {
  value: number;
  /** Something is running right now (subagents, a pipeline). */
  live?: boolean;
}

interface IToolsPageProps {
  tools: readonly TPaneTabKind[];
  counts: Partial<Record<TPaneTabKind, IToolCount>>;
  /** Null in a plain chat: there is no machine to open a page on. */
  onAddress: ((address: string) => void) | null;
  /** In place of this new tab, or beside it. */
  onOpen: (kind: TPaneTabKind, beside: boolean) => void;
}

/** Codex's new tab: the address field, then «Tools» as tiles with their shortcut and «…». */
const ToolsPage = ({ tools, counts, onAddress, onOpen }: IToolsPageProps) => (
  <div className="tw-rpane-tools-page">
    {onAddress && (
      <BrowserBar>
        <AddressField value="" onSubmit={onAddress} />
      </BrowserBar>
    )}
    <section className="tw-rpane-tools" aria-labelledby="tw-rpane-tools-title">
      <h3 id="tw-rpane-tools-title" className="tw-rpane-tools-title">
        {t('pane.toolsPage.title')}
      </h3>
      <ul className="tw-rpane-tool-grid">
        {tools.map((kind) => {
          const { icon: Icon, title } = PANE_KINDS[kind];
          const shortcut = paneShortcutLabel(kind);
          const count = counts[kind];
          return (
            <li key={kind} className="tw-rpane-tool">
              <button
                type="button"
                className="tw-rpane-tool-open"
                onClick={() => onOpen(kind, false)}
                aria-keyshortcuts={PANE_ARIA_SHORTCUTS[kind]}
              >
                <Icon />
                <span className="tw-rpane-tool-title">{title}</span>
                {!!count?.value && (
                  <span className={cx('tw-rpane-count', count.live && 'tw-rpane-count-live')}>
                    {count.value}
                  </span>
                )}
                {shortcut && <TwKbd className="tw-rpane-tool-kbd">{shortcut}</TwKbd>}
              </button>
              <TwMenuButton
                items={[
                  {
                    kind: TW_MENU_KIND.action,
                    id: 'here',
                    label: t('pane.toolsPage.openHere'),
                    onSelect: () => onOpen(kind, false),
                  },
                  {
                    kind: TW_MENU_KIND.action,
                    id: 'beside',
                    label: t('pane.toolsPage.openNewTab'),
                    onSelect: () => onOpen(kind, true),
                  },
                ]}
                menuLabel={title}
                align={LAYER_ALIGN.end}
                className="tw-icon-button tw-rpane-tool-more"
                aria-label={t('pane.toolsPage.moreFor', { title })}
                title={t('pane.toolsPage.more')}
              >
                <MoreHorizontal />
              </TwMenuButton>
            </li>
          );
        })}
      </ul>
    </section>
  </div>
);

export default ToolsPage;
