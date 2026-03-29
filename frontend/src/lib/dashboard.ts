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

export function buildSparkPath(
  values: Array<number | null | undefined>,
  width: number,
  height: number,
  minValue: number,
  maxValue: number
): string {
  if (values.length === 0) {
    return "";
  }

  const span = Math.max(maxValue - minValue, 1);
  const points = values
    .map((value, index) => {
      if (value == null) {
        return null;
      }
      const x = values.length === 1 ? width / 2 : (index / (values.length - 1)) * width;
      const normalized = Math.min(Math.max((value - minValue) / span, 0), 1);
      const y = height - normalized * height;
      return { x, y };
    });

  let path = "";
  for (const point of points) {
    if (!point) {
      continue;
    }
    path += path ? ` L ${point.x} ${point.y}` : `M ${point.x} ${point.y}`;
  }
  return path;
}

export function activitySegments(day: DashboardDay) {
  const activity = day.activity;
  const raw = [
    {
      label: "High",
      className: "high",
      minutes: activity?.high_activity_minutes ?? 0
    },
    {
      label: "Medium",
      className: "medium",
      minutes: activity?.medium_activity_minutes ?? 0
    },
    {
      label: "Low",
      className: "low",
      minutes: activity?.low_activity_minutes ?? 0
    },
    {
      label: "Rest",
      className: "rest",
      minutes: activity?.resting_minutes ?? 0
    },
    {
      label: "Off-body",
      className: "off",
      minutes: activity?.non_wear_minutes ?? 0
    }
  ];

  const total = raw.reduce((sum, item) => sum + item.minutes, 0);
  if (total === 0) {
    return [];
  }

  return raw
    .filter((item) => item.minutes > 0)
    .map((item) => ({
      ...item,
      percent: (item.minutes / total) * 100
    }));
}

export function minutesToHoursLabel(value?: number): string {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
    return "--";
  }

  const hours = Math.floor(value / 60);
  const minutes = value % 60;
  return `${hours}h ${String(minutes).padStart(2, "0")}m`;
}

export function clockTimeLabel(value?: string): string {
  if (!value) {
    return "--";
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "--";
  }

  return new Intl.DateTimeFormat(undefined, {
    hour: "numeric",
    minute: "2-digit"
  }).format(date);
}

export function sleepPosition(sleep?: DashboardSleep): { top: number; height: number } | null {
  if (!sleep?.start_time || !sleep.end_time) {
    return null;
  }

  const start = wrapNightMinutes(sleep.start_time);
  const end = wrapNightMinutes(sleep.end_time);
  if (start == null || end == null) {
    return null;
  }

  const safeEnd = Math.max(end, start + 10);
  const top = clampPercent((start / NIGHT_AXIS_SPAN_MINUTES) * 100);
  const height = clampPercent(((safeEnd - start) / NIGHT_AXIS_SPAN_MINUTES) * 100);

  return {
    top,
    height: Math.max(height, 2.5)
  };
}

export function sleepOpacity(sleep?: DashboardSleep): number {
  const efficiency = sleep?.efficiency_percent;
  if (typeof efficiency !== "number") {
    return 0.45;
  }
  return Math.min(Math.max((efficiency - 50) / 45, 0.35), 1);
}

function wrapNightMinutes(value: string): number | null {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return null;
  }

  let minutes = date.getHours() * 60 + date.getMinutes();
  if (minutes < NIGHT_AXIS_START_MINUTES) {
    minutes += 24 * 60;
  }
  return Math.min(Math.max(minutes - NIGHT_AXIS_START_MINUTES, 0), NIGHT_AXIS_SPAN_MINUTES);
}

function clampPercent(value: number): number {
  return Math.min(Math.max(value, 0), 100);
}
