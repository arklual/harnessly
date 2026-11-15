// The talk's small wave (harnessly design project, «Голос — в другом чате»): four bars at the end of the chat's row in
// the sidebar and in the bar away from the chat. Accent — the agent speaks, red — it listens, grey — it thinks.
import type { TVoiceLook } from '@/next/features/voice-live/field';
import '@/next/styles/voice-layer.css';

/** `idle`: held still and dimmed (a paused reading). */
const MiniWave = ({ look }: { look: TVoiceLook | 'idle' }) => (
  <span className="hx-mini-wave" data-s={look} aria-hidden="true">
    <i />
    <i />
    <i />
    <i />
  </span>
);

export default MiniWave;
