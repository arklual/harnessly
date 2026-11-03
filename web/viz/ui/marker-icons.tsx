// SPDX-License-Identifier: Apache-2.0
import type { ComponentType, ReactNode } from 'react';

import {
  BadgePercent,
  CalendarClock,
  Gift,
  Mail,
  Megaphone,
  MessageCircle,
  Pencil,
  Rocket,
  Send,
  Tag,
  TicketX,
} from 'lucide-react';

import type { VizMarker } from '../model/types';

/** Значки изменений по виду: запуск, анонс, цена, промокод, реклама, соцсети, рассылка, бот, распродано, розыгрыш, правка. */
export const MARKER_ICONS = {
  start: Rocket,
  announce: Megaphone,
  price: Tag,
  promo: BadgePercent,
  ads: Send,
  social: MessageCircle,
  mail: Mail,
  bot: CalendarClock,
  soldout: TicketX,
  gift: Gift,
  edit: Pencil,
} as const satisfies Record<string, ComponentType<{ 'aria-hidden'?: boolean | 'true' | 'false' }>>;

export type VizMarkerIconKind = keyof typeof MARKER_ICONS;

/** Иконка значка по его виду; для неизвестного вида — ничего. */
export function renderMarkerIcon(marker: Pick<VizMarker, 'kind'>): ReactNode {
  const Icon = (MARKER_ICONS as Record<string, ComponentType<{ 'aria-hidden': 'true' }>>)[marker.kind];
  return Icon ? <Icon aria-hidden="true" /> : null;
}
