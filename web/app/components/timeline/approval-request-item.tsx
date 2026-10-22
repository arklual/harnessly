// Codex: a command, a patch or extra permissions the agent asks to be allowed. The decision is made in the menu
// above the composer; the feed keeps what was asked and how it ended.
import { memo } from 'react';
import { Check, ShieldQuestion, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITimelineApprovalRequest } from '@/types/timeline';

const KINDS = new Set(['exec', 'apply-patch', 'permissions']);

const ApprovalRequestItem = ({ entry }: { entry: ITimelineApprovalRequest }) => {
  const state = entry.status === 'success' ? 'approved' : entry.status === 'error' ? 'rejected' : 'pending';
  return (
    <div className="tw-approval" data-status={state}>
      <div className="tw-approval-head">
        <ShieldQuestion size={14} className={cn(state === 'pending' && 'tw-pulse')} aria-hidden />
        <strong>{t('timeline.approval.title')}</strong>
        {KINDS.has(entry.approvalKind) && <span>({t(`timeline.approval.kind.${entry.approvalKind}`)})</span>}
      </div>
      {entry.command && (
        <code className="tw-approval-command">
          <span aria-hidden>$ </span>
          {entry.command}
        </code>
      )}
      {entry.cwd && (
        <code className="tw-approval-meta">{t('timeline.approval.cwd', { cwd: entry.cwd })}</code>
      )}
      {entry.patches && entry.patches.length > 0 && (
        <ul className="tw-patch-files">
          {entry.patches.map((patch, index) => (
            <li key={`${patch.path}-${index}`}>
              <code>{patch.path}</code>
              {patch.status && <span>{patch.status}</span>}
            </li>
          ))}
        </ul>
      )}
      {entry.permissions && entry.permissions.length > 0 && (
        <div className="tw-approval-permissions">
          {entry.permissions.map((permission, index) => (
            <code key={`${permission}-${index}`}>{permission}</code>
          ))}
        </div>
      )}
      <div className="tw-approval-state">
        {state === 'approved' && <Check size={12} aria-hidden />}
        {state === 'rejected' && <X size={12} aria-hidden />}
        {t(`timeline.approval.${state}`)}
      </div>
    </div>
  );
};

export default memo(ApprovalRequestItem);
