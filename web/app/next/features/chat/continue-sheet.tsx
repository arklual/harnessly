// «Продолжить в другой модели»: the models a chat can be carried over to, grouped by who runs them (Claude, Codex and
// each connected vendor). Choosing one closes the list and opens the sheet with the text the new chat starts with.
import Modal from '@/next/features/shell/modal';
import type { IPickerAgent } from '@/next/features/composer/agents';
import { agentKey } from '@/next/features/composer/agents';
import type { AgentModel } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import '@/next/styles/handoff.css';

export interface IContinueSheetProps {
  open: boolean;
  onClose: () => void;
  /** The groups a handoff goes to: the connected ones, the chat's own left out. */
  agents: readonly IPickerAgent[];
  onPick: (agent: IPickerAgent, model: AgentModel) => void;
  /** «Подключить модели» leads to the page of models and keys. */
  onConnect: () => void;
}

const ContinueSheet = ({ open, onClose, agents, onPick, onConnect }: IContinueSheetProps) => (
  <Modal
    open={open}
    onClose={onClose}
    title={t('next.handoff.continue.title')}
    lead={t('next.handoff.continue.lead')}
    boxClass="hx-create hx-continue"
  >
    <div className="hx-continue__list">
      {agents.map((agent) => (
        <section key={agentKey(agent)} className="hx-continue__group" aria-label={agent.name}>
          <h3 className="hx-continue__name">{agent.name}</h3>
          {agent.models.map((model) => (
            <button
              key={model.id}
              type="button"
              className="hx-continue__model"
              onClick={() => onPick(agent, model)}
            >
              {model.label}
            </button>
          ))}
        </section>
      ))}
    </div>
    <button type="button" className="hx-continue__connect" onClick={onConnect}>
      {t('next.handoff.continue.connect')}
    </button>
  </Modal>
);

export default ContinueSheet;
