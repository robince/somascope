import type { PeriodId, PeriodOption } from "./types";

export const PERIODS: PeriodOption[] = [
  { id: "1w", label: "1W", shortcut: "W", days: 7 },
  { id: "1m", label: "1M", shortcut: "M", days: 30 },
  { id: "3m", label: "3M", shortcut: "Q", days: 90 },
  { id: "1y", label: "1Y", shortcut: "Y", days: 365 }
];

const DATE_TIMEZONE = "UTC";

function parseISODate(date: string): Date {
  return new Date(`${date}T12:00:00Z`);
}

function formatDate(date: string, options: Intl.DateTimeFormatOptions): string {
  return new Intl.DateTimeFormat(undefined, {
    ...options,
    timeZone: DATE_TIMEZONE
  }).format(parseISODate(date));
}

export function getPeriod(periodId: PeriodId): PeriodOption {
  return PERIODS.find((period) => period.id === periodId) ?? PERIODS[1];
}

export function addDays(date: string, amount: number): string {
  const next = parseISODate(date);
  next.setUTCDate(next.getUTCDate() + amount);
  return next.toISOString().slice(0, 10);
}

export function clampDate(date: string, minDate?: string, maxDate?: string): string {
  let next = date;
  if (minDate && next < minDate) {
    next = minDate;
  }
  if (maxDate && next > maxDate) {
    next = maxDate;
  }
  return next;
}

export function getWindowStart(endDate: string, days: number): string {
  return addDays(endDate, -(days - 1));
}

export function buildDateRange(startDate: string, endDate: string): string[] {
  if (!startDate || !endDate || startDate > endDate) {
    return [];
  }

  const dates: string[] = [];
  for (let cursor = startDate; cursor <= endDate; cursor = addDays(cursor, 1)) {
    dates.push(cursor);
  }
  return dates;
}

export function formatMonthDay(date: string): string {
  return formatDate(date, { month: "short", day: "numeric" });
}

export function formatMonthDayYear(date: string): string {
  return formatDate(date, { month: "short", day: "numeric", year: "numeric" });
}

export function formatMonthDayCompact(date: string): string {
  return formatDate(date, { month: "short", day: "numeric" }).replace(" ", "\u00a0");
}

export function formatWeekday(date: string): string {
  return formatDate(date, { weekday: "short" });
}

export function formatLongDate(date: string): string {
  return formatDate(date, { weekday: "short", month: "short", day: "numeric" });
}

export function formatRangeLabel(startDate: string, endDate: string): string {
  if (!startDate || !endDate) {
    return "No visible range";
  }
  return `${formatMonthDayYear(startDate)} - ${formatMonthDayYear(endDate)}`;
}

export function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }

  const tagName = target.tagName.toLowerCase();
  return tagName === "input" || tagName === "textarea" || tagName === "select" || target.isContentEditable;
}
