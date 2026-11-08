// SPDX-License-Identifier: Apache-2.0
import type { VizViewport } from '../model/viewport';
import { BAR_RENDERER } from './bar-paint';
import { VizTimeEngine } from './time-engine';

/** Столбчатый график: группой, стопкой и долями; при смене детализации столбики делятся и сливаются. */
export class ColumnEngine extends VizTimeEngine {
  constructor(canvas: HTMLCanvasElement, viewport: VizViewport) {
    super(canvas, viewport, BAR_RENDERER);
  }
}
