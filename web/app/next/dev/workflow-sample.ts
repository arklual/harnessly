// A made-up dynamic workflow for the galleries: review, fix and verify phases, agents at work, done and failed. Never
// part of a production path.
import type { TWorkflowTask } from '@/next/features/workflows/run';
import type { BackgroundStatus, ConvId, WorkflowAgent } from '@/next/protocol/v2';

const MIN = 60_000;

const agent = (
  id: string,
  label: string,
  status: BackgroundStatus,
  startedAt: number,
  took: number | null,
  said: Partial<Pick<WorkflowAgent, 'last' | 'result' | 'model'>> = {},
): WorkflowAgent => ({
  id,
  label,
  status,
  startedAt,
  endedAt: took === null ? null : startedAt + took,
  model: 'claude-sonnet-5-5',
  last: null,
  result: null,
  ...said,
});

/** The workflow a gallery chat runs; `toolUseId` is its Workflow call's step in the feed. */
export const sampleWorkflow = (conv: ConvId, now: number, toolUseId: string): TWorkflowTask => {
  const start = now - 14 * MIN;
  return {
    id: 'wq7m2kx4d',
    conversationId: conv,
    kind: 'workflow',
    title: 'Проверить оплату и поправить найденное',
    startedAt: start,
    endedAt: null,
    status: 'running',
    devServer: false,
    stoppable: false,
    outputPath: null,
    workflow: {
      name: 'checkout-review',
      description: 'Проверить оплату и поправить найденное',
      toolUseId,
      phases: [
        {
          title: 'Review',
          agents: [
            agent('a1c0ffee00000001a', 'review:payments', 'done', start + 4_000, 4 * MIN + 12_000, {
              result: 'Два места читают ответ виджета, не проверяя status.',
            }),
            agent('a1c0ffee00000002b', 'review:forms', 'done', start + 4_000, 3 * MIN, {
              result: 'Форма шлёт orderId, а виджет ждёт order_id.',
            }),
            agent('a1c0ffee00000003c', 'review:copy', 'failed', start + 4_000, 50_000),
          ],
        },
        {
          title: 'Fix',
          agents: [
            agent('a1c0ffee00000004d', 'fix:payments', 'done', start + 6 * MIN, 5 * MIN, {
              model: 'claude-opus-5-5',
              result: 'Маппинг поправлен, тест добавлен.',
            }),
            agent('a1c0ffee00000005e', 'fix:forms', 'running', start + 6 * MIN, null, {
              model: 'claude-opus-5-5',
              last: 'Edit · web/app/pay/form.ts',
            }),
          ],
        },
        { title: 'Verify', agents: [] },
      ],
    },
  };
};
