import type { DashboardDay, DashboardSleep } from "./types";
import { buildDateRange } from "./time";

export const SLEEP_AXIS_LABELS = [
  { label: "6p", percent: 0 },
  { label: "9p", percent: 16.6667 },
  { label: "12a", percent: 33.3333 },
  { label: "3a", percent: 50 },
  { label: "6a", percent: 66.6667 },
  { label: "9a", percent: 83.3333 },
  { label: "12p", percent: 100 }
];

const NIGHT_AXIS_START_MINUTES = 18 * 60;
const NIGHT_AXIS_SPAN_MINUTES = 18 * 60;

export type ChartResolution = "daily" | "weekly";

export type DashboardBucket = {
  start_date: string;
  end_date: string;
  label_date: string;
  activity_steps: number | null;
  readiness_score: number | null;
  sleep_start_minutes: number | null;
  sleep_end_minutes: number | null;
  sleep_duration_minutes: number | null;
};

export function fillWindow(daily: DashboardDay[], startDate: string, endDate: string): DashboardDay[] {
  const daysByDate = new Map(daily.map((day) => [day.date, day]));
  return buildDateRange(startDate, endDate).map((date) => daysByDate.get(date) ?? { date });
}

export function averageDefined(values: Array<number | null | undefined>): number | null {
  const filtered = values.filter((value): value is number => typeof value === "number" && Number.isFinite(value));
  if (filtered.length === 0) {
    return null;
  }
  return filtered.reduce((sum, value) => sum + value, 0) / filtered.length;
}

export function sumDefined(values: Array<number | null | undefined>): number {
  return values.reduce((sum, value) => sum + (typeof value === "number" && Number.isFinite(value) ? value : 0), 0);
}

export function movingAverage(values: Array<number | null | undefined>, windowSize: number): Array<number | null> {
  return values.map((_, index) => {
    const start = Math.max(0, index - windowSize + 1);
    return averageDefined(values.slice(start, index + 1));
  });
}

export function centeredMovingAverage(values: Array<number | null | undefined>, windowSize: number): Array<number | null> {
  const before = Math.floor((windowSize - 1) / 2);
  const after = Math.ceil((windowSize - 1) / 2);

  return values.map((_, index) => {
    const start = index - before;
    const end = index + after;
    if (start < 0 || end >= values.length) {
      return null;
    }

    return averageDefined(values.slice(start, end + 1));
  });
}

export function chartResolutionForDays(dayCount: number): ChartResolution {
  return dayCount > 180 ? "weekly" : "daily";
}

export function buildDashboardBuckets(days: DashboardDay[], resolution: ChartResolution): DashboardBucket[] {
  const bucketSize = resolution === "weekly" ? 7 : 1;
  const buckets: DashboardBucket[] = [];

  for (let start = 0; start < days.length; start += bucketSize) {
    const slice = days.slice(start, start + bucketSize);
    if (!slice.length) {
      continue;
    }

    const sleepRanges = slice
      .map((day) => sleepRangeMinutes(day.sleep))
      .filter((range): range is { start: number; end: number } => range != null);

    buckets.push({
      start_date: slice[0].date,
      end_date: slice[slice.length - 1].date,
      label_date: slice[slice.length - 1].date,
      activity_steps: averageDefined(slice.map((day) => day.activity?.steps)),
      readiness_score: averageDefined(slice.map((day) => day.readiness?.score)),
      sleep_start_minutes: medianDefined(sleepRanges.map((range) => range.start)),
      sleep_end_minutes: medianDefined(sleepRanges.map((range) => range.end)),
      sleep_duration_minutes: averageDefined(slice.map((day) => day.sleep?.duration_minutes))
    });
  }

  return buckets;
}

export function minutesToHoursLabel(value?: number): string {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
    return "--";
  }

  const hours = Math.floor(value / 60);
  const minutes = value % 60;
  return `${hours}h ${String(minutes).padStart(2, "0")}m`;
}

export function sleepRangeMinutes(sleep?: DashboardSleep): { start: number; end: number } | null {
  if (!sleep?.start_time || !sleep.end_time) {
    return null;
  }

  const start = wrapNightMinutes(sleep.start_time);
  const end = wrapNightMinutes(sleep.end_time);
  if (start == null || end == null) {
    return null;
  }

  return {
    start,
    end
  };
}

function wrapNightMinutes(value: string): number | null {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return null;
  }

  let minutes = date.getUTCHours() * 60 + date.getUTCMinutes();
  if (minutes < NIGHT_AXIS_START_MINUTES) {
    minutes += 24 * 60;
  }
  return Math.min(Math.max(minutes - NIGHT_AXIS_START_MINUTES, 0), NIGHT_AXIS_SPAN_MINUTES);
}

function medianDefined(values: Array<number | null | undefined>): number | null {
  const filtered = values
    .filter((value): value is number => typeof value === "number" && Number.isFinite(value))
    .sort((left, right) => left - right);

  if (!filtered.length) {
    return null;
  }

  const middle = Math.floor(filtered.length / 2);
  if (filtered.length % 2 === 1) {
    return filtered[middle];
  }

  return (filtered[middle - 1] + filtered[middle]) / 2;
}
