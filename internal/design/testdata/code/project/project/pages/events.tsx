import { Root, Stack, Card, Text, PrimaryButton } from "@acme/ui";
import { Stat } from "../components/stat";
import { summary, events } from "./events.fixtures";

export default function Events() {
  return (
    <Root>
      <Stack gap="space-4">
        <Stat label="Tickets sold" value={summary.sold} />
        {events.map((e) => (
          <Card key={e.id} title={e.name}>
            <Text tone="muted">{e.date}</Text>
          </Card>
        ))}
        <PrimaryButton>Create event</PrimaryButton>
      </Stack>
    </Root>
  );
}
