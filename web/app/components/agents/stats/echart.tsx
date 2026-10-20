import { useEffect, useRef } from 'react';
import * as echarts from 'echarts/core';
import { BarChart, HeatmapChart, LineChart } from 'echarts/charts';
import {
  CalendarComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  VisualMapComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import type { EChartsCoreOption } from 'echarts/core';

echarts.use([
  BarChart,
  LineChart,
  HeatmapChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  CalendarComponent,
  VisualMapComponent,
  CanvasRenderer,
]);

interface IEChartProps {
  option: EChartsCoreOption;
  height: number;
  ariaLabel: string;
}

/** One ECharts instance per mount; follows its box size and replaces the option on change. */
const EChart = ({ option, height, ariaLabel }: IEChartProps) => {
  const host = useRef<HTMLDivElement>(null);
  const chart = useRef<echarts.ECharts | null>(null);

  useEffect(() => {
    if (!host.current) return;
    const instance = echarts.init(host.current, undefined, { renderer: 'canvas' });
    chart.current = instance;
    const observer = new ResizeObserver(() => instance.resize());
    observer.observe(host.current);
    return () => {
      observer.disconnect();
      instance.dispose();
      chart.current = null;
    };
  }, []);

  useEffect(() => {
    chart.current?.setOption(option, { notMerge: true, lazyUpdate: true });
  }, [option]);

  return <div ref={host} className="ts-echart" style={{ height }} role="img" aria-label={ariaLabel} />;
};

export default EChart;
