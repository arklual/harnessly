// An agent CLI is installed unless __golden.missing names its panel type.
const PROVIDERS = { 'claude-code': ['claude', 'Claude Code'], 'codex-cli': ['codex', 'Codex'] };
export const checkAgentAvailabilityForPanelType = async (panelType) => {
  const p = PROVIDERS[panelType];
  if (!p || !globalThis.__golden.missing.includes(panelType)) return { ok: true, provider: null };
  return { ok: false, status: 409, code: 'agent-not-installed', providerId: p[0], providerDisplayName: p[1], panelType,
    suggestedCommand: p[0] === 'claude' ? 'claude' : null };
};
export const toAgentAvailabilityError = (a) => ({ error: a.code, providerId: a.providerId, providerDisplayName: a.providerDisplayName,
  panelType: a.panelType, suggestedCommand: a.suggestedCommand });
