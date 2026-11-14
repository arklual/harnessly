// Package chatgpt is the ChatGPT integration of a runtime: a plain conversation with ChatGPT on the person's own
// ChatGPT plan (Plus or Pro), without an agent CLI, a terminal or any access to the machine's files.
//
// It follows OpenAI's «Sign in with ChatGPT» flow for open-source and self-hosted apps (developers.openai.com/siwc):
//
//   - Sign-in registers a client dynamically (client_id=dynamic_agent_client, agent_name_hint=harnessly) for this
//     machine's stable host id, with a fresh state, nonce and PKCE (S256) per attempt and a loopback callback
//     http://127.0.0.1:<port>/auth/callback. On a remote machine the person's browser cannot reach that address, so the
//     page also takes the callback's address pasted back from the browser: the code in it is useless without the PKCE
//     verifier, which never leaves the runtime.
//   - The ID token is checked against OpenAI's published keys (issuer, audience = the issued client, expiry, nonce),
//     and plan use needs the granted scope chatgpt.tokens.use.direct.
//   - Tokens live only in the machine's safe (internal/secrets, under integration/chatgpt/…, out of the safe's list and
//     of the agents' CLI), are refreshed by this package under one lock per account, and the refresh token is revoked
//     on sign-out.
//   - Inference goes only to the public Responses API (POST https://api.openai.com/v1/responses, store:false,
//     stream:true, the whole history in input), never to ChatGPT's backend-api.
//
// This package has nothing to do with the Codex relay of the GPT profile (internal/providers): it neither reads nor
// writes Codex's sign-in.
package chatgpt
