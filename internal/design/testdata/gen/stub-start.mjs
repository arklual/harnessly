export const validateLaunchChoice = async () => ({ ok: true, launch: undefined });
export const resolveStartCwd = async (requested) => ({ ok: true, value: requested });
export const startAgentConversation = async (input) => { globalThis.__golden.started.push(input); return { ok: true, conversation: { id: 'tab-new' } }; };
