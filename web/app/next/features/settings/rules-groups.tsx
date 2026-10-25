// Claude Code's permission rules and environment on its page: the rules by what they do, a builder that turns a tool
// and a pattern into the rule string, and the variables every session of the agent gets.
import { useState, type KeyboardEvent } from 'react';
import {
  buildRule,
  envError,
  RULE_KINDS,
  RULE_TOOLS,
  ruleAffixes,
  ruleError,
  type RuleKind,
  type RuleToolId,
} from '@/next/features/settings/rules';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { AgentEnvVar, AgentRules } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { useLeavingList } from '@/next/ui/presence';

/** A rule's or a variable's exit, as long as .hx-rule and .hx-env__row fade in settings.css. */
const GONE_MS = 260;

interface IRulesProps {
  rules: AgentRules;
  /** The MCP servers the machine has: their names are the suggestions of the MCP tool. */
  servers: readonly string[];
  /** The rule being added or removed waits for the machine. */
  busy: boolean;
  /** Resolves true when the machine took the change. */
  onAdd: (kind: RuleKind, rule: string) => Promise<boolean>;
  onRemove: (kind: RuleKind, rule: string) => Promise<boolean>;
}

const RuleChips = ({
  kind,
  rules,
  onRemove,
}: {
  kind: RuleKind;
  rules: AgentRules;
  onRemove: IRulesProps['onRemove'];
}) => {
  const items = useLeavingList(rules[kind], (rule) => rule, GONE_MS);
  return (
    <div className="hx-rules__chips">
      {items.map(({ item, leaving }) => (
        <span className="hx-rule" key={item} data-gone={leaving}>
          <code>{item}</code>
          <button
            type="button"
            aria-label={t('next.settings.rules.remove')}
            onClick={() => void onRemove(kind, item)}
          >
            <Icon name="x" size="sm" />
          </button>
        </span>
      ))}
      {items.length === 0 && <span className="hx-acct__muted">{t('next.settings.rules.none')}</span>}
    </div>
  );
};

export const RulesGroup = ({ rules, servers, busy, onAdd, onRemove }: IRulesProps) => {
  const [tool, setTool] = useState<RuleToolId>(RULE_TOOLS[0].id);
  const [kind, setKind] = useState<RuleKind>('allow');
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);
  const affixes = ruleAffixes(tool);
  const suggestions =
    tool === 'mcp' ? servers : (RULE_TOOLS.find((item) => item.id === tool)?.suggestions ?? []);

  const add = async () => {
    if (busy) return;
    const rule = buildRule(tool, draft);
    const problem = ruleError(rule, rules);
    setError(problem);
    if (problem) return;
    if (await onAdd(kind, rule)) setDraft('');
  };
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== 'Enter') return;
    event.preventDefault();
    void add();
  };

  return (
    <Group title={t('next.settings.rules.title')} card="hx-rules" foot={t('next.settings.rules.foot')}>
      {RULE_KINDS.map((item) => (
        <div className="hx-rules__row" key={item} data-kind={item}>
          <div className="hx-rules__head">
            <span className="hx-rules__label">{t(`next.settings.rules.kind.${item}`)}</span>
            <span className="hx-set__row-hint">{t(`next.settings.rules.kind.${item}.hint`)}</span>
          </div>
          <RuleChips kind={item} rules={rules} onRemove={onRemove} />
        </div>
      ))}
      <div className="hx-rb">
        <div className="hx-rb__top">
          <div className="hx-rb__tools" role="radiogroup" aria-label={t('next.settings.rules.tool')}>
            {RULE_TOOLS.map((item) => (
              <button
                key={item.id}
                type="button"
                className="hx-rb__tool"
                role="radio"
                aria-checked={item.id === tool}
                onClick={() => {
                  setTool(item.id);
                  setError(null);
                }}
              >
                <Icon name={item.icon} size="sm" />
                {t(`next.settings.rules.tool.${item.id}`)}
              </button>
            ))}
          </div>
          <div className="hx-set__seg" role="radiogroup" aria-label={t('next.settings.rules.where')}>
            {RULE_KINDS.map((item) => (
              <button
                key={item}
                type="button"
                className="hx-set__seg-item"
                role="radio"
                aria-checked={item === kind}
                onClick={() => setKind(item)}
              >
                {t(`next.settings.rules.kind.${item}`)}
              </button>
            ))}
          </div>
        </div>
        <div className="hx-rb__field">
          <span className="hx-rb__affix">{affixes.prefix}</span>
          <input
            className="hx-rb__in"
            value={draft}
            placeholder={t(`next.settings.rules.ph.${tool}`)}
            autoComplete="off"
            spellCheck={false}
            aria-label={t(`next.settings.rules.hint.${tool}`)}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onKey}
          />
          <span className="hx-rb__affix">{affixes.suffix}</span>
          <SetButton tone="primary" busy={busy} onClick={() => void add()}>
            {t('next.settings.add')}
          </SetButton>
        </div>
        <div className="hx-rb__hint">
          <span>{t(`next.settings.rules.hint.${tool}`)}</span>
          {suggestions.map((value) => (
            <button key={value} type="button" className="hx-rb__sugg" onClick={() => setDraft(value)}>
              {value}
            </button>
          ))}
        </div>
        {error && <FormError>{error}</FormError>}
      </div>
    </Group>
  );
};

interface IEnvProps {
  env: readonly AgentEnvVar[];
  busy: boolean;
  onAdd: (key: string, value: string) => Promise<boolean>;
  onRemove: (key: string) => Promise<boolean>;
}

export const EnvGroup = ({ env, busy, onAdd, onRemove }: IEnvProps) => {
  const [key, setKey] = useState('');
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | null>(null);
  const rows = useLeavingList(env, (item) => item.key, GONE_MS);

  const add = async () => {
    if (busy) return;
    const name = key.trim();
    const problem = envError(name, value.trim(), env);
    setError(problem);
    if (problem) return;
    if (await onAdd(name, value.trim())) {
      setKey('');
      setValue('');
    }
  };
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== 'Enter') return;
    event.preventDefault();
    void add();
  };

  return (
    <Group title={t('next.settings.env.title')} card="hx-env" foot={t('next.settings.env.foot')}>
      {rows.map(({ item, leaving }) => (
        <div className="hx-env__row" key={item.key} data-gone={leaving}>
          <code className="hx-env__k hx-trunc">{item.key}</code>
          <code className="hx-env__v hx-trunc">{item.value}</code>
          <button
            type="button"
            className="hx-head-btn hx-src__del"
            aria-label={t('next.settings.env.remove')}
            onClick={() => void onRemove(item.key)}
          >
            <Icon name="trash-2" size="sm" />
          </button>
        </div>
      ))}
      <div className="hx-env__add">
        <input
          className="hx-set__input hx-set__input--mono"
          value={key}
          placeholder={t('next.settings.env.name')}
          autoComplete="off"
          spellCheck={false}
          aria-label={t('next.settings.env.nameAria')}
          onChange={(event) => setKey(event.target.value)}
          onKeyDown={onKey}
        />
        <input
          className="hx-set__input hx-set__input--mono"
          value={value}
          placeholder={t('next.settings.env.value')}
          autoComplete="off"
          spellCheck={false}
          aria-label={t('next.settings.env.valueAria')}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={onKey}
        />
        <SetButton tone="ghost" busy={busy} onClick={() => void add()}>
          {t('next.settings.add')}
        </SetButton>
      </div>
      {error && <FormError className="hx-env__err">{error}</FormError>}
    </Group>
  );
};
