export const submitPromptText = async (session, text) => { globalThis.__golden.typed.push({ session, text, via: 'editor' }); };
