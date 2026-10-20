import { useId, useState, type RefObject } from 'react';
import { Check, Copy, Download, ExternalLink } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CHOICE_ACTIVATION,
  TwButton,
  TwPopover,
  TwSegmented,
  TwSwitch,
  type ITwChoice,
} from '@/components/agents/ui';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import SharePublish from '@/components/agents/share/share-publish';
import type { IShareDraftView } from '@/types/server/hl-share-drafts';
import { REDACTION_RULE_LABELS, type TRedactionRule } from '@/lib/hl-share-redact';
import { plural, t } from '@/lib/i18n';
import {
  DEFAULT_SHARE_OPTIONS,
  SHARE_TOOLS,
  type IShareOptions,
  type IShareReport,
  type TShareTools,
} from '@/lib/hl-share-snapshot';

const TOOLS_CHOICES: readonly ITwChoice<TShareTools>[] = [
  { value: SHARE_TOOLS.summary, label: t('share.sharePopover.toolsSummary') },
  { value: SHARE_TOOLS.none, label: t('share.sharePopover.toolsNone') },
] as const;

type TDraftState =
  | { status: 'idle' }
  | { status: 'pending' }
  | { status: 'ready'; draft: IShareDraftView }
  | { status: 'error'; message: string };

type TCopyState =
  { status: 'idle' } | { status: 'pending' } | { status: 'done' } | { status: 'error'; message: string };

const errorMessage = async (response: Response) => {
  const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
  return typeof body?.error === 'string'
    ? body.error
    : t('share.sharePopover.failedStatus', { status: response.status });
};

const redactionSummary = (report: IShareReport) => {
  const found = (Object.entries(report.redactions) as [TRedactionRule, number][]).filter(
    ([, count]) => count > 0,
  );
  if (found.length === 0) return t('share.sharePopover.noSecrets');
  return t('share.sharePopover.hidden', {
    list: found.map(([rule, count]) => `${REDACTION_RULE_LABELS[rule]} × ${count}`).join(', '),
  });
};

const reportDetails = (report: IShareReport) => {
  const parts = [plural(report.entries, 'share.sharePopover.entries')];
  if (report.pathsShortened > 0)
    parts.push(t('share.sharePopover.pathsShortened', { count: report.pathsShortened }));
  if (report.truncated) parts.push(t('share.sharePopover.truncated'));
  return parts.join(' · ');
};

interface ISharePopoverProps {
  open: boolean;
  onClose: () => void;
  anchorRef: RefObject<HTMLElement | null>;
  id: string;
  /** Conversation id is its tab id on the owning runtime. */
  tabId: string;
  /** Runtime that owns the conversation; undefined for this machine. */
  machineId: string | undefined;
}

/** «Share»: owner-side snapshot with redaction report, preview, HTML download and Markdown copy. */
const SharePopover = ({ open, onClose, anchorRef, id, tabId, machineId }: ISharePopoverProps) => {
  const [options, setOptions] = useState<IShareOptions>(DEFAULT_SHARE_OPTIONS);
  const [draftState, setDraftState] = useState<TDraftState>({ status: 'idle' });
  const [copyState, setCopyState] = useState<TCopyState>({ status: 'idle' });
  const reportId = useId();

  const draftPath = (draftId: string, tail: string) =>
    scopedAppPath(`/api/platform/snapshots/${encodeURIComponent(draftId)}/${tail}`, machineId);

  const changeOptions = (patch: Partial<IShareOptions>) => {
    setOptions((current) => ({ ...current, ...patch }));
    setDraftState({ status: 'idle' });
    setCopyState({ status: 'idle' });
  };

  const prepare = async () => {
    setDraftState({ status: 'pending' });
    setCopyState({ status: 'idle' });
    try {
      const response = await appFetch(scopedAppPath('/api/platform/snapshots', machineId), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tabId, options }),
      });
      if (!response.ok) {
        setDraftState({ status: 'error', message: await errorMessage(response) });
        return;
      }
      const { draft } = (await response.json()) as { draft: IShareDraftView };
      setDraftState({ status: 'ready', draft });
    } catch {
      setDraftState({
        status: 'error',
        message: t('share.sharePopover.machineNoAnswer'),
      });
    }
  };

  const fetchMarkdown = async (draftId: string) => {
    const response = await appFetch(draftPath(draftId, 'download?format=md'));
    if (!response.ok) throw new Error(await errorMessage(response));
    return response.blob();
  };

  // Safari drops clipboard access after an await, so the write starts synchronously with a pending blob where possible.
  const copyMarkdown = async (draftId: string) => {
    setCopyState({ status: 'pending' });
    try {
      if (typeof ClipboardItem !== 'undefined' && navigator.clipboard?.write) {
        const blob = fetchMarkdown(draftId).then((markdown) => new Blob([markdown], { type: 'text/plain' }));
        await navigator.clipboard.write([new ClipboardItem({ 'text/plain': blob })]);
      } else {
        await navigator.clipboard.writeText(await (await fetchMarkdown(draftId)).text());
      }
      setCopyState({ status: 'done' });
    } catch (error) {
      setCopyState({
        status: 'error',
        message: error instanceof Error && error.message ? error.message : t('share.sharePopover.copyFailed'),
      });
    }
  };

  return (
    <TwPopover
      open={open}
      onClose={onClose}
      anchorRef={anchorRef}
      id={id}
      align="end"
      title={t('share.sharePopover.title')}
      className="tw-share-pop"
    >
      <p className="tw-share-pop-lead">{t('share.sharePopover.lead')}</p>
      <div className="tw-share-pop-field">
        <span className="tw-share-pop-label">{t('share.sharePopover.agentActions')}</span>
        <TwSegmented
          options={TOOLS_CHOICES}
          value={options.tools}
          onChange={(tools) => changeOptions({ tools })}
          aria-label={t('share.sharePopover.agentActions')}
          activation={CHOICE_ACTIVATION.auto}
        />
      </div>
      <div className="tw-share-pop-switches">
        <TwSwitch
          checked={options.thinking}
          onCheckedChange={(thinking) => changeOptions({ thinking })}
          label={t('share.sharePopover.thinking')}
        />
        <TwSwitch
          checked={options.diffs}
          onCheckedChange={(diffs) => changeOptions({ diffs })}
          label={t('share.sharePopover.diffs')}
          description={t('share.sharePopover.diffsNote')}
        />
        <TwSwitch
          checked={options.economics}
          onCheckedChange={(economics) => changeOptions({ economics })}
          label={t('share.sharePopover.economics')}
        />
      </div>
      {draftState.status !== 'ready' && (
        <TwButton
          variant={BUTTON_VARIANT.primary}
          pending={draftState.status === 'pending'}
          onClick={prepare}
          className="tw-share-pop-primary"
        >
          {t('share.sharePopover.prepare')}
        </TwButton>
      )}
      {draftState.status === 'error' && (
        <p className="tw-share-pop-error" role="alert">
          {draftState.message}
        </p>
      )}
      {draftState.status === 'ready' && (
        <section className="tw-share-pop-draft" aria-labelledby={reportId}>
          <div className="tw-share-pop-report" role="status">
            <p id={reportId} className="tw-share-pop-report-title">
              {redactionSummary(draftState.draft.report)}
            </p>
            <p className="tw-share-pop-report-meta">{reportDetails(draftState.draft.report)}</p>
          </div>
          <div className="tw-share-pop-actions">
            <a
              className="tw-ui-button tw-share-pop-link"
              data-variant={BUTTON_VARIANT.secondary}
              data-size={BUTTON_SIZE.sm}
              href={draftPath(draftState.draft.id, 'preview')}
              target="_blank"
              rel="noopener noreferrer"
            >
              <ExternalLink aria-hidden="true" />
              <span className="tw-ui-button-label">{t('share.sharePopover.preview')}</span>
            </a>
            <a
              className="tw-ui-button tw-share-pop-link"
              data-variant={BUTTON_VARIANT.secondary}
              data-size={BUTTON_SIZE.sm}
              href={draftPath(draftState.draft.id, 'download?format=html')}
              download
            >
              <Download aria-hidden="true" />
              <span className="tw-ui-button-label">{t('share.sharePopover.downloadHtml')}</span>
            </a>
            <TwButton
              size={BUTTON_SIZE.sm}
              pending={copyState.status === 'pending'}
              icon={copyState.status === 'done' ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
              onClick={() => copyMarkdown(draftState.draft.id)}
            >
              {copyState.status === 'done'
                ? t('share.sharePopover.copied')
                : t('share.sharePopover.copyMarkdown')}
            </TwButton>
          </div>
          {copyState.status === 'error' && (
            <p className="tw-share-pop-error" role="alert">
              {copyState.message}
            </p>
          )}
          <TwButton
            variant={BUTTON_VARIANT.ghost}
            size={BUTTON_SIZE.sm}
            onClick={prepare}
            className="tw-share-pop-again"
          >
            {t('share.sharePopover.rebuild')}
          </TwButton>
        </section>
      )}
      <SharePublish
        tabId={tabId}
        machineId={machineId}
        draft={draftState.status === 'ready' ? draftState.draft : null}
        onDraftUsed={() => setDraftState({ status: 'idle' })}
      />
      <p className="tw-share-pop-caption">{t('share.sharePopover.checkNote')}</p>
    </TwPopover>
  );
};

export default SharePopover;
