// A question the agent asked with options: the question, the options and what was picked. Answering happens in the
// card above the composer, so a question still open only says where.
import { memo } from 'react';
import { Check, MessageCircleQuestion } from 'lucide-react';
import { answeredLabels } from '@/lib/timeline-items';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITimelineAskUserQuestion } from '@/types/timeline';

const AskQuestionItem = ({ entry }: { entry: ITimelineAskUserQuestion }) => {
  const answered = entry.status !== 'pending' || !!entry.answers;
  return (
    <div className="tw-question-card" data-status={entry.status}>
      {entry.questions.map((question, index) => {
        const answer = answeredLabels(question, entry.answers);
        return (
          <section key={index} className="tw-question">
            <div className="tw-question-head">
              <MessageCircleQuestion size={14} aria-hidden />
              <span>{question.header || t('timeline.timelineEntry.question')}</span>
              {question.multiSelect && question.options.length > 0 && (
                <span className="tw-question-hint">{t('timeline.askQuestionItem.multi')}</span>
              )}
            </div>
            <p className="tw-question-text">{question.question}</p>
            {question.options.length > 0 && (
              <ul className="tw-question-options">
                {question.options.map((option, i) => {
                  const picked = answer?.picked.includes(i) ?? false;
                  return (
                    <li
                      key={option.label}
                      className={cn('tw-question-option', picked && 'tw-question-picked')}
                      data-picked={picked || undefined}
                    >
                      <span className="tw-question-mark" aria-hidden>
                        {picked ? <Check size={12} /> : i + 1}
                      </span>
                      <span>
                        <strong>{option.label}</strong>
                        {option.description && <span className="tw-question-desc">{option.description}</span>}
                      </span>
                    </li>
                  );
                })}
              </ul>
            )}
            {answer?.other && (
              <p className="tw-question-other">
                <Check size={12} aria-hidden /> {answer.other}
              </p>
            )}
          </section>
        );
      })}
      {!answered && <p className="tw-question-waiting">{t('timeline.askQuestionItem.waiting')}</p>}
    </div>
  );
};

export default memo(AskQuestionItem);
