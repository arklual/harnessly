import { Text } from "@acme/ui";

// A number with its label: a block the kit does not have, made of its Text and its tokens.
export function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div style={{ display: "flex", alignItems: "baseline", gap: "var(--acme-space-2)", color: "var(--acme-accent)" }}>
      <Text size="l">{value}</Text>
      <Text tone="muted">{label}</Text>
    </div>
  );
}
