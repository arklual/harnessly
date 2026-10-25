// «Модели»: Kimi, GLM and Grok, each with the form of its key. A model of a connected vendor shows up in the new chat's
// picker as a group of its own; the chats of Claude and Codex are not touched by anything here.
import { useState } from 'react';
import VendorCard from '@/next/features/providers/vendor-card';
import {
  vendorsOf,
  type IPresetActions,
  type IPresetState,
  type Vendor,
} from '@/next/features/providers/model';
import { Group } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';

export interface IModelsSectionProps extends IPresetActions {
  states: readonly IPresetState[];
}

const ModelsSection = ({ states, ...actions }: IModelsSectionProps) => {
  const [open, setOpen] = useState<Vendor | null>(null);
  return (
    <Group foot={`${t('next.providers.foot')} ${t('next.providers.note')}`}>
      {vendorsOf(states).map((vendor) => (
        <VendorCard
          key={vendor}
          vendor={vendor}
          states={states}
          open={open === vendor}
          onToggle={(next) => setOpen((now) => (now === next ? null : next))}
          {...actions}
        />
      ))}
    </Group>
  );
};

export default ModelsSection;
