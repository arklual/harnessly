// Package realtime is the voice mode: a spoken conversation in real time, with interruptions, whose requests can go to
// the agent of the chat it was opened from as tasks.
//
// The browser holds the WebRTC media itself: its microphone goes out and the voice comes back over the peer
// connection. The runtime only makes the call and keeps the control socket beside it, so the token never reaches the
// page:
//
//   - The call: the page's SDP offer goes to the route's call endpoint with the token; the answer SDP and the call's id
//     come back to the page.
//   - The control socket (the "sideband"): a websocket joined to the call by its id. It brings the transcripts, the
//     turns and the delegations (what the voice hands over as a task), and takes back what the task came to.
//
// Routes. A Route says where the call goes and with which token:
//
//   - Subscription (unofficial): ChatGPT's own voice route of Codex signed in with ChatGPT
//     (chatgpt.com/backend-api/codex/realtime/calls), with the token of the machine's Codex sign-in, read and never
//     refreshed here (Codex refreshes it itself). OpenAI documents no such use for other apps; the person turns it on
//     in Settings knowing that, and it is off by default.
//   - API key: the public Realtime API (api.openai.com/v1/realtime/calls) with the machine's OpenAI key, when the
//     subscription route is off or Codex is not signed in. A task there is a call of the hand_over tool.
//
// Nothing of the token is logged or answered to the page.
package realtime
