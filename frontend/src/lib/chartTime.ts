import { scaleUtc } from "d3-scale";
import { utcDay, utcMonday, utcMonth } from "d3-time";
import { utcFormat } from "d3-time-format";
import type { PeriodId } from "./types";

export type ChartTick = {
  date: string;
  label: string;
  x: number;
};

type ChartTimeAxisOptions = {
  startDate: string;
  endDate: string;
  periodId: PeriodId;
  width: number;
  padLeft: number;
  padRight: number;
  minTickGap?: number;
};

const DEFAULT_MIN_TICK_GAP = 56;
const formatMonthDayTick = utcFormat("%b %-d");
const formatMonthTick = utcFormat("%b");

export function buildChartTimeAxis(options: ChartTimeAxisOptions) {
  const domainStart = parseUTCDate(options.startDate);
  const domainEndExclusive = addUtcDays(parseUTCDate(options.endDate), 1);
  const scale = scaleUtc()
    .domain([domainStart, domainEndExclusive])
    .range([options.padLeft, options.width - options.padRight]);

  const rawTicks = buildRawTickDates(options.periodId, domainStart, domainEndExclusive);
  const ticks = filterTicksBySpacing(
    rawTicks.map((date) => ({
      date: formatISODate(date),
      label: formatTick(date, options.periodId),
      x: scale(date)
    })),
    options.minTickGap ?? DEFAULT_MIN_TICK_GAP
  );

  return {
    ticks,
    xForRangeCenter(startDate: string, endDate: string, offsetUnits = 0): number {
      const rangeStart = parseUTCDate(startDate);
      const rangeEndExclusive = addUtcDays(parseUTCDate(endDate), 1);
      const widthMs = rangeEndExclusive.getTime() - rangeStart.getTime();
      const center = new Date(rangeStart.getTime() + widthMs / 2 + offsetUnits * widthMs);
      return scale(center);
    },
    bandForRange(startDate: string, endDate: string): { x: number; width: number } {
      const rangeStart = parseUTCDate(startDate);
      const rangeEndExclusive = addUtcDays(parseUTCDate(endDate), 1);
      return {
        x: scale(rangeStart),
        width: Math.max(scale(rangeEndExclusive) - scale(rangeStart), 0)
      };
    }
  };
}

function buildRawTickDates(periodId: PeriodId, start: Date, endExclusive: Date): Date[] {
  if (periodId === "1w") {
    return utcDay.range(start, endExclusive);
  }

  if (periodId === "1m") {
    return utcMonday.range(start, endExclusive);
  }

  return utcMonth.range(start, endExclusive);
}

function formatTick(date: Date, periodId: PeriodId): string {
  if (periodId === "3m" || periodId === "1y") {
    return formatMonthTick(date);
  }

  return formatMonthDayTick(date);
}

function filterTicksBySpacing(ticks: ChartTick[], minGap: number): ChartTick[] {
  const filtered: ChartTick[] = [];

  for (const tick of ticks) {
    const previous = filtered[filtered.length - 1];
    if (!previous || tick.x - previous.x >= minGap) {
      filtered.push(tick);
    }
  }

  return filtered;
}

function parseUTCDate(date: string): Date {
  return new Date(`${date}T00:00:00Z`);
}

function addUtcDays(date: Date, amount: number): Date {
  const next = new Date(date.getTime());
  next.setUTCDate(next.getUTCDate() + amount);
  return next;
}

function formatISODate(date: Date): string {
  return date.toISOString().slice(0, 10);
}
