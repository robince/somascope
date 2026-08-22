<script lang="ts">
  import { tick } from "svelte";
  import { buildChartTimeAxis } from "./chartTime";
  import {
    SLEEP_AXIS_LABELS,
    averageDefined,
    buildDashboardBuckets,
    chartResolutionForDays,
    centeredMovingAverage,
    dayActivity,
    dayReadiness,
    daySleep,
    fillWindow,
    minutesToHoursLabel,
    providersForSource
  } from "./dashboard";
  import { PERIODS, formatRangeLabel, getPeriod, getWindowStart } from "./time";
  import type {
    DashboardOverview,
    DashboardSource,
    PeriodId,
    ProviderName,
    ProviderStatus,
    RawExportOptions
  } from "./types";
  import { PROVIDER_NAMES, providerLabel, providerSubtitle } from "./types";

  export let dashboard: DashboardOverview | null = null;
  export let activePeriod: PeriodId = "1m";
  export let windowEndDate = "";
  export let loading = false;
  export let busy = false;
  export let statuses: Partial<Record<ProviderName, ProviderStatus | null>> = {};
  export let error = "";
  export let onSelectPeriod: (period: PeriodId) => void = () => {};
  export let onShiftWindow: (direction: -1 | 1) => void = () => {};
  export let onSyncIncremental: () => void = () => {};

  let activeSource: DashboardSource = "all";

  const CHART_WIDTH = 720;
  const CHART_HEIGHT = 220;
  const CHART_PAD_LEFT = 42;
  const CHART_PAD_RIGHT = 12;
  const CHART_PAD_TOP = 16;
  const CHART_PAD_BOTTOM = 28;
  const SLEEP_CHART_HEIGHT = 240;
  const SHOW_DAY_BOUNDARY_TICKS = true;
  const SHOW_HORIZONTAL_GUIDES = false;
  const READINESS_MIN = 40;
  const READINESS_MAX = 100;
  const READINESS_TICKS = [100, 85, 70, 55, 40];
  const SLEEP_AXIS_MAX_MINUTES = 18 * 60;
  type SeriesPoint = { key: string; x: number; y: number };
  type SleepInterval = { key: string; x: number; width: number; y: number; height: number };
  type ProviderTrend = {
    provider: ProviderName;
    activityRawPoints: SeriesPoint[];
    activityPath: string;
    activitySmoothPath: string;
    readinessRawPoints: SeriesPoint[];
    readinessPath: string;
    readinessSmoothPath: string;
    hasReadiness: boolean;
    sleepStartPoints: SeriesPoint[];
    sleepEndPoints: SeriesPoint[];
    sleepIntervals: SleepInterval[];
    sleepBandPath: string;
    sleepStartPath: string;
    sleepEndPath: string;
    hasSleep: boolean;
  };
  let rawOuraStartDate = "";
  let rawOuraEndDate = "";
  let rawOuraSelectedKinds: string[] = [];
  let rawOuraOptionsSignature = "";
  let rawOuraTypePickerMenu: HTMLDivElement | null = null;

  $: period = getPeriod(activePeriod);
  $: resolvedEndDate = windowEndDate || dashboard?.latest_date || "";
  $: windowStartDate = resolvedEndDate ? getWindowStart(resolvedEndDate, period.days) : "";
  $: visibleDays = dashboard && windowStartDate ? fillWindow(dashboard.daily, windowStartDate, resolvedEndDate) : [];
  $: rangeLabel = visibleDays.length ? formatRangeLabel(visibleDays[0].date, visibleDays[visibleDays.length - 1].date) : "No visible range";
  $: chartResolution = chartResolutionForDays(visibleDays.length);
  $: availableSources = (dashboard?.available_sources ?? dashboard?.providers ?? []).filter(
    (provider): provider is ProviderName => provider === "oura" || provider === "google_health"
  );
  $: validatedActiveSource =
    activeSource === "all" || !availableSources.length || availableSources.includes(activeSource)
      ? activeSource
      : availableSources.length > 1 ? "all" : availableSources[0];
  $: selectedProviders = providersForSource(validatedActiveSource, availableSources.length ? availableSources : ["oura"]);
  $: overlayMode = selectedProviders.length > 1;
  $: providerBuckets = selectedProviders.map((provider) => ({
    provider,
    buckets: buildDashboardBuckets(visibleDays, chartResolution, provider)
  }));
  $: buckets = providerBuckets[0]?.buckets ?? [];
  $: chartTimeAxis =
    visibleDays.length && resolvedEndDate
      ? buildChartTimeAxis({
          startDate: visibleDays[0].date,
          endDate: visibleDays[visibleDays.length - 1].date,
          periodId: activePeriod,
          width: CHART_WIDTH,
          padLeft: CHART_PAD_LEFT,
          padRight: CHART_PAD_RIGHT
        })
      : null;
  $: xTicks = chartTimeAxis?.ticks ?? [];
  $: weekendBands =
    chartResolution === "daily" && chartTimeAxis
      ? buildWeekendBands(visibleDays, chartTimeAxis)
      : [];
  $: boundaryTicks =
    SHOW_DAY_BOUNDARY_TICKS && chartTimeAxis
      ? buildBoundaryTicks(buckets, chartTimeAxis)
      : [];
  $: smoothingWindow = getSmoothingWindow(activePeriod);
  $: smoothingOffset = smoothingWindow % 2 === 0 ? 0.5 : 0;
  $: activityMax = niceUpperBound(
    providerBuckets.flatMap((series) => series.buckets.map((bucket) => bucket.activity_steps)),
    1000
  );
  $: activityTicks = buildLinearTicks(activityMax, 4);
  $: providerTrends = providerBuckets.map(({ provider, buckets: seriesBuckets }): ProviderTrend => {
    const activityValues = seriesBuckets.map((bucket) => bucket.activity_steps);
    const activitySmoothed = centeredMovingAverage(activityValues, smoothingWindow);
    const activityRawPoints = prefixPointKeys(
      buildSeriesPoints(activityValues, seriesBuckets, chartTimeAxis, activityY, CHART_HEIGHT),
      provider
    );
    const activitySmoothPoints = prefixPointKeys(
      buildSeriesPoints(activitySmoothed, seriesBuckets, chartTimeAxis, activityY, CHART_HEIGHT, smoothingOffset),
      provider
    );
    const readinessValues = seriesBuckets.map((bucket) => bucket.readiness_score);
    const readinessSmoothed = centeredMovingAverage(readinessValues, smoothingWindow);
    const readinessRawPoints = prefixPointKeys(
      buildSeriesPoints(readinessValues, seriesBuckets, chartTimeAxis, readinessY, CHART_HEIGHT),
      provider
    );
    const readinessSmoothPoints = prefixPointKeys(
      buildSeriesPoints(readinessSmoothed, seriesBuckets, chartTimeAxis, readinessY, CHART_HEIGHT, smoothingOffset),
      provider
    );
    const sleepStartPoints: SeriesPoint[] = [];
    const sleepEndPoints: SeriesPoint[] = [];
    const sleepIntervals: SleepInterval[] = [];
    const seriesIndex = selectedProviders.indexOf(provider);
    const seriesCount = Math.max(selectedProviders.length, 1);
    if (chartTimeAxis) {
      for (const bucket of seriesBuckets) {
        if (bucket.sleep_start_minutes == null || bucket.sleep_end_minutes == null) {
          continue;
        }
        const x = scaledBucketCenterX(bucket, chartTimeAxis);
        const yStart = clamp(sleepY(bucket.sleep_start_minutes), CHART_PAD_TOP, SLEEP_CHART_HEIGHT - CHART_PAD_BOTTOM);
        const yEnd = clamp(sleepY(bucket.sleep_end_minutes), CHART_PAD_TOP, SLEEP_CHART_HEIGHT - CHART_PAD_BOTTOM);
        sleepStartPoints.push({
          key: `${provider}-${bucket.start_date}-start`,
          x,
          y: yStart
        });
        sleepEndPoints.push({
          key: `${provider}-${bucket.start_date}-end`,
          x,
          y: yEnd
        });
        const band = chartTimeAxis.bandForRange(bucket.start_date, bucket.end_date);
        const gap = overlayMode ? 0.8 : 0;
        const usable = Math.max(band.width - 2, 1.5);
        const barWidth = overlayMode
          ? Math.max((usable - gap * (seriesCount - 1)) / seriesCount, 1.2)
          : Math.max(usable * 0.72, 1.5);
        const barX = overlayMode ? band.x + 1 + seriesIndex * (barWidth + gap) : band.x + (band.width - barWidth) / 2;
        sleepIntervals.push({
          key: `${provider}-${bucket.start_date}-interval`,
          x: barX,
          width: barWidth,
          y: Math.min(yStart, yEnd),
          height: Math.max(Math.abs(yEnd - yStart), 2)
        });
      }
    }

    return {
      provider,
      activityRawPoints,
      activityPath: buildPathFromPoints(activityRawPoints),
      activitySmoothPath: buildPathFromPoints(activitySmoothPoints),
      readinessRawPoints,
      readinessPath: buildPathFromPoints(readinessRawPoints),
      readinessSmoothPath: buildPathFromPoints(readinessSmoothPoints),
      hasReadiness: readinessValues.some((value) => value != null),
      sleepStartPoints,
      sleepEndPoints,
      sleepIntervals,
      sleepBandPath: buildBandPathFromPoints(sleepStartPoints, sleepEndPoints),
      sleepStartPath: buildPathFromPoints(sleepStartPoints),
      sleepEndPath: buildPathFromPoints(sleepEndPoints),
      hasSleep: sleepStartPoints.length > 0
    };
  });
  $: readinessTrends = providerTrends.filter((series) => series.hasReadiness);
  $: sleepTrends = providerTrends.filter((series) => series.hasSleep);
  $: activityDotCount = providerTrends.reduce((count, series) => count + series.activityRawPoints.length, 0);
  $: readinessDotCount = readinessTrends.reduce((count, series) => count + series.readinessRawPoints.length, 0);
  $: averageReadiness = averageDefined(
    visibleDays.flatMap((day) => selectedProviders.map((provider) => dayReadiness(day, provider)?.score))
  );
  $: averageSleep = averageDefined(
    visibleDays.flatMap((day) => selectedProviders.map((provider) => daySleep(day, provider)?.duration_minutes))
  );
  $: averageDailySteps = averageDefined(
    visibleDays.flatMap((day) => selectedProviders.map((provider) => dayActivity(day, provider)?.steps))
  );
  $: anyConnected = Object.values(statuses).some((status) => status?.connected);
  $: lastSyncAt = Object.values(statuses)
    .map((status) => status?.last_sync_at)
    .filter((value): value is string => Boolean(value))
    .sort()
    .at(-1);
  $: syncStatusLabel = PROVIDER_NAMES
    .map((provider) => {
      const status = statuses[provider];
      if (!status?.connected && !status?.configured) {
        return "";
      }
      const run = status?.current_run?.status === "running" ? "updating" : "idle";
      return `${providerLabel(provider)} ${run}`;
    })
    .filter(Boolean)
    .join(" · ");
  $: rawOuraExportBaseURL = dashboard?.export_urls.raw_jsonl_by_provider?.oura ?? "";
  $: rawOuraExportOptions = dashboard?.export_urls.raw_options_by_provider?.oura ?? null;
  $: rawGoogleExportBaseURL = dashboard?.export_urls.raw_jsonl_by_provider?.google_health ?? "";
  $: rawOuraAvailableKinds = rawOuraExportOptions?.document_kinds ?? [];
  $: {
    const nextSignature = JSON.stringify(rawOuraExportOptions ?? null);
    if (nextSignature !== rawOuraOptionsSignature) {
      rawOuraOptionsSignature = nextSignature;
      rawOuraStartDate = rawOuraExportOptions?.start_date ?? "";
      rawOuraEndDate = rawOuraExportOptions?.end_date ?? "";
      rawOuraSelectedKinds = [...rawOuraAvailableKinds];
    }
  }
  $: rawOuraSelectedKinds = rawOuraSelectedKinds.filter((kind, index, kinds) => rawOuraAvailableKinds.includes(kind) && kinds.indexOf(kind) === index);
  $: rawOuraExportURL = buildRawExportURL(rawOuraExportBaseURL, rawOuraStartDate, rawOuraEndDate, rawOuraSelectedKinds);
  $: rawOuraCanExport = rawOuraAvailableKinds.length === 0 || rawOuraSelectedKinds.length > 0;

  function scaledBucketCenterX(
    bucket: (typeof buckets)[number] | undefined,
    timeAxis: typeof chartTimeAxis,
    offsetUnits = 0
  ): number {
    if (!bucket || !timeAxis) {
      return CHART_PAD_LEFT;
    }

    return timeAxis.xForRangeCenter(bucket.start_date, bucket.end_date, offsetUnits);
  }

  function activityY(value: number): number {
    const plotHeight = CHART_HEIGHT - CHART_PAD_TOP - CHART_PAD_BOTTOM;
    const normalized = Math.min(Math.max(value / activityMax, 0), 1);
    return CHART_HEIGHT - CHART_PAD_BOTTOM - normalized * plotHeight;
  }

  function readinessY(score: number): number {
    const plotHeight = CHART_HEIGHT - CHART_PAD_TOP - CHART_PAD_BOTTOM;
    const normalized = Math.min(Math.max((score - READINESS_MIN) / (READINESS_MAX - READINESS_MIN), 0), 1);
    return CHART_HEIGHT - CHART_PAD_BOTTOM - normalized * plotHeight;
  }

  function sleepY(minutes: number): number {
    const plotHeight = SLEEP_CHART_HEIGHT - CHART_PAD_TOP - CHART_PAD_BOTTOM;
    const normalized = Math.min(Math.max(minutes / SLEEP_AXIS_MAX_MINUTES, 0), 1);
    return CHART_PAD_TOP + normalized * plotHeight;
  }

  function buildSeriesPoints(
    values: Array<number | null | undefined>,
    seriesBuckets: typeof buckets,
    timeAxis: typeof chartTimeAxis,
    yForValue: (value: number) => number,
    chartHeight: number,
    xOffset = 0
  ): SeriesPoint[] {
    if (!timeAxis) {
      return [];
    }

    const points: SeriesPoint[] = [];

    for (const [index, value] of values.entries()) {
      if (value == null) {
        continue;
      }

      const bucket = seriesBuckets[index];
      if (!bucket) {
        continue;
      }

      points.push({
        key: bucket.start_date,
        x: scaledBucketCenterX(bucket, timeAxis, xOffset),
        y: clamp(yForValue(value), CHART_PAD_TOP, chartHeight - CHART_PAD_BOTTOM)
      });
    }

    return points;
  }

  function buildWeekendBands(
    days: typeof visibleDays,
    timeAxis: NonNullable<typeof chartTimeAxis>
  ): Array<{ x: number; width: number }> {
    return days.flatMap((day) => {
      if (!isWeekend(day.date)) {
        return [];
      }

      const { x, width } = timeAxis.bandForRange(day.date, day.date);
      return [{ x, width }];
    });
  }

  function buildBoundaryTicks(
    seriesBuckets: typeof buckets,
    timeAxis: NonNullable<typeof chartTimeAxis>
  ): Array<{ date: string; x: number }> {
    return seriesBuckets.slice(1).map((bucket) => ({
      date: bucket.start_date,
      x: timeAxis.bandForRange(bucket.start_date, bucket.start_date).x
    }));
  }

  function buildPathFromPoints(points: SeriesPoint[]): string {
    let path = "";
    for (const [index, point] of points.entries()) {
      path += index === 0 ? `M ${point.x} ${point.y}` : ` L ${point.x} ${point.y}`;
    }
    return path;
  }

  function buildBandPathFromPoints(startPoints: SeriesPoint[], endPoints: SeriesPoint[]): string {
    if (!startPoints.length || !endPoints.length) {
      return "";
    }

    let path = `M ${startPoints[0].x} ${startPoints[0].y}`;
    for (const point of startPoints.slice(1)) {
      path += ` L ${point.x} ${point.y}`;
    }
    for (const point of [...endPoints].reverse()) {
      path += ` L ${point.x} ${point.y}`;
    }
    path += " Z";
    return path;
  }

  function buildLinearTicks(maxValue: number, segments: number): number[] {
    return Array.from({ length: segments + 1 }, (_, index) => Math.round((maxValue / segments) * index));
  }

  function niceUpperBound(values: Array<number | null | undefined>, minimum: number): number {
    const defined = values.filter((value): value is number => typeof value === "number" && Number.isFinite(value));
    if (!defined.length) {
      return minimum;
    }

    const maxValue = Math.max(...defined, minimum);
    const magnitude = 10 ** Math.floor(Math.log10(maxValue));
    const normalized = maxValue / magnitude;

    if (normalized <= 1) {
      return magnitude;
    }
    if (normalized <= 2) {
      return 2 * magnitude;
    }
    if (normalized <= 5) {
      return 5 * magnitude;
    }
    return 10 * magnitude;
  }

  function prefixPointKeys(points: SeriesPoint[], provider: ProviderName): SeriesPoint[] {
    return points.map((point) => ({ ...point, key: `${provider}-${point.key}` }));
  }

  function seriesClass(provider: ProviderName): string {
    return provider === "google_health" ? "series-google-health" : "series-oura";
  }

  function rollingLabel(windowSize: number): string {
    return `${windowSize}-${chartResolution === "weekly" ? "week" : "day"} average`;
  }

  function getSmoothingWindow(periodId: PeriodId): number {
    if (periodId === "1w") {
      return 2;
    }
    if (periodId === "1m") {
      return 3;
    }
    if (periodId === "1y") {
      return 2;
    }
    return 7;
  }

  function isWeekend(date: string): boolean {
    const parsed = new Date(`${date}T12:00:00Z`);
    const day = parsed.getUTCDay();
    return day === 0 || day === 6;
  }

  function clamp(value: number, min: number, max: number): number {
    return Math.min(Math.max(value, min), max);
  }

  function buildRawExportURL(baseURL: string, startDate: string, endDate: string, documentKinds: string[]): string {
    if (!baseURL) {
      return "";
    }

    const url = new URL(baseURL, "http://localhost");
    if (startDate) {
      url.searchParams.set("start_date", startDate);
    } else {
      url.searchParams.delete("start_date");
    }
    if (endDate) {
      url.searchParams.set("end_date", endDate);
    } else {
      url.searchParams.delete("end_date");
    }

    url.searchParams.delete("document_kind");
    for (const kind of documentKinds) {
      url.searchParams.append("document_kind", kind);
    }

    return `${url.pathname}${url.search}`;
  }

  function rawExportTypeSummary(documentKinds: string[], selectedKinds: string[]): string {
    if (!documentKinds.length) {
      return "Types";
    }
    if (selectedKinds.length === documentKinds.length) {
      return "All types";
    }
    if (selectedKinds.length === 0) {
      return "No types selected";
    }
    return `${selectedKinds.length} of ${documentKinds.length} types`;
  }

  function documentKindLabel(kind: string): string {
    return kind.replaceAll("_", " ");
  }

  function toggleRawOuraDocumentKind(kind: string, checked: boolean) {
    if (checked) {
      rawOuraSelectedKinds = Array.from(new Set([...rawOuraSelectedKinds, kind]));
      return;
    }
    rawOuraSelectedKinds = rawOuraSelectedKinds.filter((value) => value !== kind);
  }

  function selectAllRawOuraDocumentKinds() {
    rawOuraSelectedKinds = [...rawOuraAvailableKinds];
  }

  function clearAllRawOuraDocumentKinds() {
    rawOuraSelectedKinds = [];
  }

  async function handleRawTypePickerToggle(event: Event) {
    const picker = event.currentTarget as HTMLDetailsElement | null;
    if (!picker?.open) {
      return;
    }

    await tick();

    const menu = rawOuraTypePickerMenu;
    if (!menu) {
      return;
    }

    const viewportPadding = 16;
    const menuBounds = menu.getBoundingClientRect();
    const overflowBottom = menuBounds.bottom - (window.innerHeight - viewportPadding);
    if (overflowBottom > 0) {
      window.scrollBy({
        top: overflowBottom,
        behavior: "smooth"
      });
    }
  }
</script>

<section class="dashboard-shell">
  <article class="panel intro-panel">
    <div class="eyebrow-row">
      <p class="eyebrow">Dashboard</p>
      <div class="hero-actions">
        <button
          class="text-link text-link-strong"
          type="button"
          onclick={() => onSyncIncremental()}
          disabled={busy || !anyConnected}
        >
          {busy ? "Updating..." : "Update data"}
        </button>
      </div>
    </div>

    <div class="hero-stats">
      <article>
        <strong>{minutesToHoursLabel(Math.round(averageSleep ?? 0) || undefined)}</strong>
        <span>Average sleep in range</span>
      </article>
      <article>
        <strong>{averageDailySteps ? new Intl.NumberFormat().format(Math.round(averageDailySteps)) : "--"}</strong>
        <span>Average daily steps</span>
      </article>
      <article>
        <strong>{lastSyncAt ? "Fresh" : "--"}</strong>
        <span>{syncStatusLabel || (lastSyncAt ? `Last sync ${new Date(lastSyncAt).toLocaleString()}` : "No sync yet")}</span>
      </article>
    </div>
  </article>

  <section class="controls-strip" aria-label="Navigation">
    <div class="toolbar">
      <div class="period-group">
        {#each PERIODS as option}
          <button
            class:active={activePeriod === option.id}
            class="period-button"
            type="button"
            onclick={() => onSelectPeriod(option.id)}
          >
            <span>{option.label}</span>
          </button>
        {/each}
      </div>

      <div class="nav-group">
        <button class="nav-button" type="button" onclick={() => onShiftWindow(-1)}>&larr; Prev</button>
        <button class="nav-button" type="button" onclick={() => onShiftWindow(1)}>Next &rarr;</button>
      </div>
    </div>

    {#if availableSources.length > 1}
      <div class="source-switch" aria-label="Data source">
        <button class:active={validatedActiveSource === "all"} type="button" onclick={() => (activeSource = "all")}>All</button>
        {#each availableSources as provider}
          <button class:active={validatedActiveSource === provider} type="button" onclick={() => (activeSource = provider)}>
            {providerLabel(provider)}
          </button>
        {/each}
      </div>
    {/if}

    <p class="window-copy">{rangeLabel}</p>
  </section>

  {#if loading}
    <article class="panel empty-panel">
      <h2>Loading dashboard...</h2>
      <p>Pulling local overview data, settings, and export metadata.</p>
    </article>
  {:else if error}
    <article class="panel empty-panel">
      <h2>Dashboard load failed.</h2>
      <p>{error}</p>
    </article>
  {:else if !dashboard?.daily.length}
    <article class="panel empty-panel">
      <h2>No synced records yet.</h2>
      <p>Once a provider sync lands, this page can fill the same period framework with activity, readiness, and sleep timing trends.</p>
    </article>
  {:else}
    <section class="visual-grid">
      <article class="panel activity-panel">
        <div class="section-head">
          <div>
            <p class="eyebrow">Trend</p>
            <h2>Steps</h2>
          </div>
        </div>

        <div class="trend-legend">
          {#each selectedProviders as provider}
            <span class="legend-line">
              <span
                class="line-swatch line-swatch-raw"
                class:series-oura={provider === "oura"}
                class:series-google-health={provider === "google_health"}
              ></span>
              {overlayMode ? providerLabel(provider) : `Per ${chartResolution === "weekly" ? "week" : "day"}`}
            </span>
          {/each}
          {#each selectedProviders as provider}
            <span class="legend-line">
              <span
                class="line-swatch line-swatch-smooth"
                class:series-oura={provider === "oura"}
                class:series-google-health={provider === "google_health"}
              ></span>
              {overlayMode ? `${providerLabel(provider)} ${rollingLabel(smoothingWindow)}` : rollingLabel(smoothingWindow)}
            </span>
          {/each}
        </div>

        <div class="trend-wrap">
          <div class="chart-stat-badge activity-stat-badge">
            <span>Average steps</span>
            <strong>{averageDailySteps ? new Intl.NumberFormat().format(Math.round(averageDailySteps)) : "--"}</strong>
          </div>
          <svg viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`} aria-label="Steps trend">
            {#each weekendBands as band}
              <rect
                x={band.x}
                y={CHART_PAD_TOP}
                width={band.width}
                height={CHART_HEIGHT - CHART_PAD_TOP - CHART_PAD_BOTTOM}
                class="weekend-band"
              />
            {/each}
            {#each boundaryTicks as tick (tick.date)}
              <line
                x1={tick.x}
                y1={CHART_PAD_TOP}
                x2={tick.x}
                y2={CHART_HEIGHT - CHART_PAD_BOTTOM}
                class="day-boundary-line"
              />
            {/each}
            {#each activityTicks as tick}
              {@const y = activityY(tick)}
              {#if SHOW_HORIZONTAL_GUIDES}
                <line
                  x1={CHART_PAD_LEFT}
                  y1={y}
                  x2={CHART_WIDTH - CHART_PAD_RIGHT}
                  y2={y}
                  class="guide-line"
                />
              {/if}
              <text x={CHART_PAD_LEFT - 8} y={y + 4} text-anchor="end" class="axis-label">{new Intl.NumberFormat().format(tick)}</text>
            {/each}

            {#each providerTrends as series (series.provider)}
              {#if series.activitySmoothPath}
                <path d={series.activitySmoothPath} class="trend-path trend-path-smooth {seriesClass(series.provider)}" />
              {/if}
            {/each}
            {#each providerTrends as series (series.provider)}
              {#if series.activityPath}
                <path d={series.activityPath} class="trend-path trend-path-raw {seriesClass(series.provider)}" />
              {/if}
            {/each}

            {#if activityDotCount <= 40}
              {#each providerTrends as series (series.provider)}
                {#each series.activityRawPoints as point (point.key)}
                  <circle cx={point.x} cy={point.y} r="2.6" class="trend-dot trend-dot-raw {seriesClass(series.provider)}" />
                {/each}
              {/each}
            {/if}

            <line
              x1={CHART_PAD_LEFT}
              y1={CHART_PAD_TOP}
              x2={CHART_PAD_LEFT}
              y2={CHART_HEIGHT - CHART_PAD_BOTTOM}
              class="axis-line"
            />
            <line
              x1={CHART_PAD_LEFT}
              y1={CHART_HEIGHT - CHART_PAD_BOTTOM}
              x2={CHART_WIDTH - CHART_PAD_RIGHT}
              y2={CHART_HEIGHT - CHART_PAD_BOTTOM}
              class="axis-line"
            />

            {#each xTicks as tick (tick.date)}
              <text
                x={tick.x}
                y={CHART_HEIGHT - 8}
                text-anchor="middle"
                class="axis-label"
              >
                {tick.label}
              </text>
            {/each}
          </svg>
        </div>

      </article>

      <article class="panel readiness-panel" class:hidden-panel={!readinessTrends.length}>
        <div class="section-head">
          <div>
            <p class="eyebrow">Trend</p>
            <h2>Readiness</h2>
          </div>
        </div>

        <div class="trend-legend">
          {#each readinessTrends as series (series.provider)}
            <span class="legend-line">
              <span
                class="line-swatch line-swatch-raw"
                class:series-oura={series.provider === "oura"}
                class:series-google-health={series.provider === "google_health"}
              ></span>
              {overlayMode ? providerLabel(series.provider) : `Per ${chartResolution === "weekly" ? "week" : "day"}`}
            </span>
          {/each}
          {#each readinessTrends as series (series.provider)}
            <span class="legend-line">
              <span
                class="line-swatch line-swatch-smooth"
                class:series-oura={series.provider === "oura"}
                class:series-google-health={series.provider === "google_health"}
              ></span>
              {overlayMode ? `${providerLabel(series.provider)} ${rollingLabel(smoothingWindow)}` : rollingLabel(smoothingWindow)}
            </span>
          {/each}
        </div>

        <div class="trend-wrap">
          <div class="chart-stat-badge readiness-stat-badge">
            <span>Average readiness</span>
            <strong>{averageReadiness ? Math.round(averageReadiness) : "--"}</strong>
          </div>
          <svg viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`} aria-label="Readiness trend">
            {#each weekendBands as band}
              <rect
                x={band.x}
                y={CHART_PAD_TOP}
                width={band.width}
                height={CHART_HEIGHT - CHART_PAD_TOP - CHART_PAD_BOTTOM}
                class="weekend-band"
              />
            {/each}
            {#each boundaryTicks as tick (tick.date)}
              <line
                x1={tick.x}
                y1={CHART_PAD_TOP}
                x2={tick.x}
                y2={CHART_HEIGHT - CHART_PAD_BOTTOM}
                class="day-boundary-line"
              />
            {/each}
            {#each READINESS_TICKS as tick}
              {@const y = readinessY(tick)}
              {#if SHOW_HORIZONTAL_GUIDES}
                <line
                  x1={CHART_PAD_LEFT}
                  y1={y}
                  x2={CHART_WIDTH - CHART_PAD_RIGHT}
                  y2={y}
                  class="guide-line"
                />
              {/if}
              <text x={CHART_PAD_LEFT - 8} y={y + 4} text-anchor="end" class="axis-label">{tick}</text>
            {/each}

            {#each readinessTrends as series (series.provider)}
              {#if series.readinessSmoothPath}
                <path d={series.readinessSmoothPath} class="trend-path trend-path-smooth {seriesClass(series.provider)}" />
              {/if}
            {/each}
            {#each readinessTrends as series (series.provider)}
              {#if series.readinessPath}
                <path d={series.readinessPath} class="trend-path trend-path-raw {seriesClass(series.provider)}" />
              {/if}
            {/each}

            {#if readinessDotCount <= 40}
              {#each readinessTrends as series (series.provider)}
                {#each series.readinessRawPoints as point (point.key)}
                  <circle cx={point.x} cy={point.y} r="2.6" class="trend-dot trend-dot-raw {seriesClass(series.provider)}" />
                {/each}
              {/each}
            {/if}

            <line
              x1={CHART_PAD_LEFT}
              y1={CHART_PAD_TOP}
              x2={CHART_PAD_LEFT}
              y2={CHART_HEIGHT - CHART_PAD_BOTTOM}
              class="axis-line"
            />
            <line
              x1={CHART_PAD_LEFT}
              y1={CHART_HEIGHT - CHART_PAD_BOTTOM}
              x2={CHART_WIDTH - CHART_PAD_RIGHT}
              y2={CHART_HEIGHT - CHART_PAD_BOTTOM}
              class="axis-line"
            />

            {#each xTicks as tick (tick.date)}
              <text
                x={tick.x}
                y={CHART_HEIGHT - 8}
                text-anchor="middle"
                class="axis-label"
              >
                {tick.label}
              </text>
            {/each}
          </svg>
        </div>

      </article>

      <article class="panel sleep-panel">
        <div class="section-head">
          <div>
            <p class="eyebrow">Timing</p>
            <h2>Sleep</h2>
          </div>
        </div>

        <div class="trend-wrap sleep-wrap">
          <div class="chart-stat-badge sleep-stat-badge">
            <span>Average sleep</span>
            <strong>{minutesToHoursLabel(Math.round(averageSleep ?? 0) || undefined)}</strong>
          </div>
          <svg viewBox={`0 0 ${CHART_WIDTH} ${SLEEP_CHART_HEIGHT}`} aria-label="Sleep timing trend">
            {#each weekendBands as band}
              <rect
                x={band.x}
                y={CHART_PAD_TOP}
                width={band.width}
                height={SLEEP_CHART_HEIGHT - CHART_PAD_TOP - CHART_PAD_BOTTOM}
                class="weekend-band"
              />
            {/each}
            {#each boundaryTicks as tick (tick.date)}
              <line
                x1={tick.x}
                y1={CHART_PAD_TOP}
                x2={tick.x}
                y2={SLEEP_CHART_HEIGHT - CHART_PAD_BOTTOM}
                class="day-boundary-line"
              />
            {/each}
            {#each SLEEP_AXIS_LABELS as tick}
              {@const y = sleepY((tick.percent / 100) * SLEEP_AXIS_MAX_MINUTES)}
              {#if SHOW_HORIZONTAL_GUIDES}
                <line
                  x1={CHART_PAD_LEFT}
                  y1={y}
                  x2={CHART_WIDTH - CHART_PAD_RIGHT}
                  y2={y}
                  class="guide-line"
                />
              {/if}
              <text x={CHART_PAD_LEFT - 8} y={y + 4} text-anchor="end" class="axis-label">{tick.label}</text>
            {/each}

            {#each sleepTrends as series (series.provider)}
              {#if series.sleepStartPoints.length > 1 && series.sleepBandPath}
                <path d={series.sleepBandPath} class="sleep-band {seriesClass(series.provider)}" />
              {/if}
            {/each}
            {#each sleepTrends as series (series.provider)}
              {#each series.sleepIntervals as interval (interval.key)}
                <rect
                  x={interval.x}
                  y={interval.y}
                  width={interval.width}
                  height={interval.height}
                  rx="1.4"
                  class="sleep-interval {seriesClass(series.provider)}"
                />
              {/each}
            {/each}
            {#each sleepTrends as series (series.provider)}
              {#if series.sleepStartPoints.length > 1 && series.sleepStartPath}
                <path d={series.sleepStartPath} class="trend-path sleep-start-path" />
              {/if}
              {#if series.sleepStartPoints.length > 1 && series.sleepEndPath}
                <path d={series.sleepEndPath} class="trend-path sleep-end-path" />
              {/if}
            {/each}

            {#each sleepTrends as series (series.provider)}
              {#if series.sleepStartPoints.length <= 40}
                {#each series.sleepStartPoints as point (point.key)}
                  <circle cx={point.x} cy={point.y} r="2.5" class="trend-dot sleep-start-dot {seriesClass(series.provider)}" />
                {/each}
                {#each series.sleepEndPoints as point (point.key)}
                  <circle cx={point.x} cy={point.y} r="2.5" class="trend-dot sleep-end-dot {seriesClass(series.provider)}" />
                {/each}
              {/if}
            {/each}

            <line
              x1={CHART_PAD_LEFT}
              y1={CHART_PAD_TOP}
              x2={CHART_PAD_LEFT}
              y2={SLEEP_CHART_HEIGHT - CHART_PAD_BOTTOM}
              class="axis-line"
            />
            <line
              x1={CHART_PAD_LEFT}
              y1={SLEEP_CHART_HEIGHT - CHART_PAD_BOTTOM}
              x2={CHART_WIDTH - CHART_PAD_RIGHT}
              y2={SLEEP_CHART_HEIGHT - CHART_PAD_BOTTOM}
              class="axis-line"
            />

            {#each xTicks as tick (tick.date)}
              <text
                x={tick.x}
                y={SLEEP_CHART_HEIGHT - 8}
                text-anchor="middle"
                class="axis-label"
              >
                {tick.label}
              </text>
            {/each}
          </svg>
        </div>

        <div class="trend-legend">
          {#if overlayMode}
            {#each sleepTrends as series (series.provider)}
              <span class="legend-line">
                <span
                  class="band-swatch"
                  class:series-oura={series.provider === "oura"}
                  class:series-google-health={series.provider === "google_health"}
                ></span>
                {providerLabel(series.provider)}
              </span>
            {/each}
          {/if}
          <span class="legend-line">
            <span class="line-swatch sleep-start-swatch"></span>
            Sleep start
          </span>
          <span class="legend-line">
            <span class="line-swatch sleep-end-swatch"></span>
            Sleep end
          </span>
        </div>

      </article>

      <article class="panel export-panel">
        <div class="export-stack">
          <div class="section-head">
            <div>
              <h2>Data Export</h2>
            </div>
          </div>

          <div class="export-subhead export-subhead-clean">
            <p class="eyebrow">Clean data</p>
          </div>

          <div class="export-actions">
            <a class="button button-primary" href={dashboard.export_urls.canonical_csv} download="somascope-visualised-data.csv">CSV</a>
            <a class="button button-ghost" href={dashboard.export_urls.canonical_jsonl} download="somascope-visualised-data.jsonl">JSONL</a>
          </div>

          {#if rawOuraExportURL || rawGoogleExportBaseURL}
            <div class="export-subhead">
              <p class="eyebrow">Raw provider data</p>
            </div>
          {/if}

          {#if rawOuraExportURL}
            <div class="provider-export-card">
              <div class="provider-export-head">
                <div>
                  <p class="eyebrow">Raw Data</p>
                  <h3>Oura</h3>
                </div>
              </div>

              <div class="export-actions">
                {#if rawOuraCanExport}
                  <a class="button button-ghost" href={rawOuraExportURL} download="somascope-oura-raw.jsonl">Oura Data (JSONL)</a>
                {:else}
                  <span class="button button-ghost button-disabled" aria-disabled="true">Choose types</span>
                {/if}
              </div>

              {#if rawOuraExportOptions}
                <div class="raw-export-controls">
                  <label class="export-field">
                    <span class="export-field-label">Start date</span>
                    <input
                      type="date"
                      value={rawOuraStartDate}
                      min={rawOuraExportOptions.start_date}
                      max={rawOuraEndDate || rawOuraExportOptions.end_date}
                      oninput={(event) => rawOuraStartDate = (event.currentTarget as HTMLInputElement).value}
                      onchange={(event) => rawOuraStartDate = (event.currentTarget as HTMLInputElement).value}
                    />
                  </label>

                  <label class="export-field">
                    <span class="export-field-label">End date</span>
                    <input
                      type="date"
                      value={rawOuraEndDate}
                      min={rawOuraStartDate || rawOuraExportOptions.start_date}
                      max={rawOuraExportOptions.end_date}
                      oninput={(event) => rawOuraEndDate = (event.currentTarget as HTMLInputElement).value}
                      onchange={(event) => rawOuraEndDate = (event.currentTarget as HTMLInputElement).value}
                    />
                  </label>

                  {#if rawOuraAvailableKinds.length}
                    <div class="export-field export-field-wide">
                      <span class="export-field-label">Types</span>
                      <details class="type-picker" ontoggle={handleRawTypePickerToggle}>
                        <summary class="type-picker-summary">
                          <span>{rawExportTypeSummary(rawOuraAvailableKinds, rawOuraSelectedKinds)}</span>
                        </summary>

                        <div class="type-picker-menu" bind:this={rawOuraTypePickerMenu}>
                          <div class="type-picker-actions">
                            <button class="text-link" type="button" onclick={selectAllRawOuraDocumentKinds}>Select all</button>
                            <button class="text-link" type="button" onclick={clearAllRawOuraDocumentKinds}>Clear all</button>
                          </div>

                          <div class="type-option-list">
                            {#each rawOuraAvailableKinds as kind}
                              <label class="type-option">
                                <input
                                  type="checkbox"
                                  checked={rawOuraSelectedKinds.includes(kind)}
                                  oninput={(event) => toggleRawOuraDocumentKind(kind, (event.currentTarget as HTMLInputElement).checked)}
                                  onchange={(event) => toggleRawOuraDocumentKind(kind, (event.currentTarget as HTMLInputElement).checked)}
                                />
                                <span>{documentKindLabel(kind)}</span>
                              </label>
                            {/each}
                          </div>
                        </div>
                      </details>
                    </div>
                  {/if}
                </div>
              {/if}
            </div>
          {/if}

          {#if rawGoogleExportBaseURL}
            <div class="provider-export-card">
              <div class="provider-export-head">
                <div>
                  <p class="eyebrow">Raw Data</p>
                  <h3>Google Health</h3>
                  {#if providerSubtitle("google_health")}
                    <p class="provider-subtitle">{providerSubtitle("google_health")}</p>
                  {/if}
                </div>
              </div>
              <div class="export-actions">
                <a class="button button-ghost" href={rawGoogleExportBaseURL} download="somascope-google-health-raw.jsonl">Google Health Data (JSONL)</a>
              </div>
            </div>
          {/if}
        </div>
      </article>
    </section>
  {/if}
</section>

<style>
  .dashboard-shell,
  .hero-stats,
  .visual-grid,
  .hero-actions,
  .controls-strip {
    display: grid;
    gap: 18px;
  }

  .panel {
    border: 1px solid var(--line);
    border-radius: 24px;
    padding: 22px;
    background:
      linear-gradient(180deg, rgba(255, 253, 247, 0.86), rgba(255, 250, 240, 0.82)),
      var(--panel);
    backdrop-filter: blur(14px);
    box-shadow: 0 18px 40px rgba(24, 32, 25, 0.07);
    min-width: 0;
  }

  .intro-panel {
    position: relative;
    overflow: hidden;
  }

  .intro-panel::after {
    content: "";
    position: absolute;
    inset: auto -10% -42% 45%;
    height: 280px;
    background: radial-gradient(circle, rgba(26, 106, 114, 0.18), transparent 65%);
    pointer-events: none;
  }

  .eyebrow-row,
  .section-head,
  .toolbar,
  .period-group,
  .nav-group,
  .source-switch,
  .trend-legend {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
  }

  .export-actions {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: flex-start;
    flex-wrap: wrap;
  }

  .eyebrow {
    margin: 0 0 8px;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.16em;
    font-size: 12px;
  }

  h2,
  p {
    margin: 0;
  }

  h2 {
    font-size: 1.55rem;
  }

  .window-copy {
    color: var(--muted);
    line-height: 1.5;
  }

  .source-switch {
    justify-content: flex-start;
    gap: 8px;
    flex-wrap: wrap;
  }

  .source-switch button {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    font: inherit;
    border-radius: 999px;
    padding: 8px 14px;
    cursor: pointer;
  }

  .source-switch button.active {
    background: var(--accent);
    color: white;
    border-color: var(--accent);
  }

  .hidden-panel {
    display: none;
  }

  .series-oura {
    --series-raw: rgba(26, 106, 114, 0.38);
    --series-smooth: #0f3f44;
    --series-dot: rgba(26, 106, 114, 0.48);
    --series-band: rgba(26, 106, 114, 0.16);
  }

  .series-google-health {
    --series-raw: rgba(196, 107, 45, 0.45);
    --series-smooth: #7a3814;
    --series-dot: rgba(196, 107, 45, 0.58);
    --series-band: rgba(196, 107, 45, 0.2);
  }

  .hero-actions {
    position: relative;
    z-index: 1;
    gap: 8px;
    display: inline-flex;
    flex-wrap: nowrap;
  }

  .hero-stats {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    margin-top: 12px;
    position: relative;
    z-index: 1;
  }

  .hero-stats article {
    border: 1px solid var(--line);
    border-radius: 18px;
    background: rgba(255, 255, 255, 0.56);
    padding: 14px;
  }

  .hero-stats strong {
    display: block;
    font-size: 1.7rem;
    line-height: 1;
    margin-bottom: 8px;
  }

  .hero-stats span {
    color: var(--muted);
    font-size: 0.95rem;
  }

  .controls-strip {
    justify-items: center;
    margin-top: 2px;
  }

  .toolbar {
    justify-content: center;
    flex-wrap: wrap;
  }

  .period-group {
    gap: 8px;
    flex-wrap: wrap;
    justify-content: center;
  }

  .window-copy {
    text-align: center;
  }

  .period-button,
  .nav-button,
  .button,
  .text-link {
    border: 1px solid var(--line);
    background: rgba(255, 255, 255, 0.62);
    color: var(--ink);
    font: inherit;
    text-decoration: none;
    cursor: pointer;
  }

  .period-button,
  .nav-button,
  .button,
  .text-link {
    border-radius: 999px;
    padding: 10px 14px;
  }

  .period-button.active,
  .button-primary,
  .text-link-strong {
    background: var(--accent);
    color: white;
    border-color: transparent;
  }

  .button-ghost {
    background: rgba(255, 255, 255, 0.62);
  }

  .visual-grid {
    margin-top: 18px;
  }

  .trend-legend {
    justify-content: flex-start;
    flex-wrap: wrap;
    margin-top: 16px;
  }

  .legend-line {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--muted);
    font-size: 0.92rem;
  }

  .line-swatch {
    width: 20px;
    height: 0;
    border-top: 3px solid currentColor;
    border-radius: 999px;
  }

  .line-swatch-raw {
    color: var(--series-raw, rgba(26, 106, 114, 0.38));
  }

  .line-swatch-smooth {
    color: var(--series-smooth, #0f3f44);
  }

  .band-swatch {
    width: 20px;
    height: 8px;
    border-radius: 3px;
    background: var(--series-band, rgba(26, 106, 114, 0.16));
  }

  .trend-wrap {
    position: relative;
    margin-top: 20px;
    border-radius: 20px;
    border: 1px solid var(--line);
    background: linear-gradient(180deg, rgba(26, 106, 114, 0.08), rgba(255, 255, 255, 0.55));
    padding: 16px;
  }

  .trend-wrap svg {
    width: 100%;
    height: auto;
    display: block;
  }

  .chart-stat-badge {
    position: absolute;
    top: 14px;
    right: 14px;
    z-index: 1;
    display: grid;
    gap: 2px;
    min-width: 118px;
    padding: 10px 12px;
    border: 1px solid rgba(28, 58, 52, 0.12);
    border-radius: 14px;
    background: rgba(255, 255, 255, 0.88);
    box-shadow: 0 8px 22px rgba(24, 32, 25, 0.08);
    text-align: right;
  }

  .chart-stat-badge span {
    color: var(--muted);
    font-size: 0.72rem;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .chart-stat-badge strong {
    font-size: 1.05rem;
    line-height: 1.1;
  }

  .sleep-stat-badge {
    background: rgba(248, 252, 253, 0.9);
  }

  .activity-stat-badge {
    background: rgba(248, 252, 251, 0.9);
  }

  .readiness-stat-badge {
    background: rgba(249, 251, 252, 0.9);
  }

  .guide-line {
    stroke: rgba(24, 32, 25, 0.1);
    stroke-width: 0.8;
  }

  .day-boundary-line {
    stroke: rgba(24, 32, 25, 0.08);
    stroke-width: 0.8;
    shape-rendering: crispEdges;
  }

  .weekend-band {
    fill: rgba(24, 32, 25, 0.045);
  }

  .axis-line {
    stroke: rgba(24, 32, 25, 0.2);
    stroke-width: 1;
  }

  .axis-label {
    fill: var(--muted);
    font-size: 10px;
  }

  .trend-path {
    fill: none;
    stroke-width: 2.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .trend-path-raw {
    stroke: var(--series-raw, rgba(26, 106, 114, 0.38));
    stroke-width: 1.9;
  }

  .trend-path-smooth {
    stroke: var(--series-smooth, #0f3f44);
  }

  .trend-dot {
    stroke: rgba(255, 255, 255, 0.9);
    stroke-width: 1.25;
  }

  .trend-dot-raw {
    fill: var(--series-dot, rgba(26, 106, 114, 0.48));
  }

  .export-stack {
    display: grid;
    gap: 18px;
  }

  .raw-export-controls {
    display: grid;
    gap: 12px;
    grid-template-columns: 150px 150px minmax(220px, 1fr);
    align-items: end;
  }

  .provider-export-card {
    display: grid;
    gap: 16px;
    padding: 18px;
    border: 1px solid var(--line);
    border-radius: 18px;
    background: rgba(255, 255, 255, 0.56);
  }

  .provider-export-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  h3 {
    margin: 0;
    font-size: 1.1rem;
  }

  .provider-subtitle {
    margin: 4px 0 0;
    color: var(--muted);
    line-height: 1.45;
  }

  .export-subhead {
    display: grid;
    gap: 4px;
    padding-top: 4px;
    border-top: 1px solid var(--line);
  }

  .export-subhead-clean {
    padding-top: 0;
    border-top: 0;
  }

  .export-field {
    display: grid;
    gap: 8px;
  }

  .export-field-wide {
    grid-column: auto;
  }

  .export-field-label {
    font-size: 13px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--accent);
  }

  .export-field input,
  .type-picker-summary {
    width: 100%;
    border-radius: 14px;
    border: 1px solid var(--line);
    background: rgba(255, 255, 255, 0.74);
    padding: 12px 14px;
    font: inherit;
    color: var(--ink);
    box-sizing: border-box;
  }

  .type-picker {
    position: relative;
    align-self: start;
  }

  .type-picker summary {
    list-style: none;
    cursor: pointer;
  }

  .type-picker summary::-webkit-details-marker {
    display: none;
  }

  .type-picker-menu {
    position: absolute;
    top: calc(100% + 8px);
    left: 0;
    right: 0;
    z-index: 10;
    display: grid;
    gap: 12px;
    padding: 14px;
    border: 1px solid var(--line);
    border-radius: 16px;
    background: rgba(255, 255, 255, 0.9);
    box-shadow: 0 12px 28px rgba(24, 32, 25, 0.08);
  }

  .type-picker-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .type-option-list {
    display: grid;
    gap: 10px;
  }

  .type-option {
    display: flex;
    gap: 10px;
    align-items: center;
    color: var(--ink);
    text-transform: capitalize;
  }

  .type-option input {
    width: 16px;
    height: 16px;
    margin: 0;
  }

  .button-disabled {
    opacity: 0.55;
    pointer-events: none;
  }

  .sleep-wrap {
    background: linear-gradient(180deg, rgba(28, 58, 92, 0.1), rgba(255, 255, 255, 0.58));
  }

  .sleep-band {
    fill: var(--series-band, rgba(26, 106, 114, 0.16));
    stroke: none;
  }

  .sleep-interval {
    fill: var(--series-band, rgba(26, 106, 114, 0.28));
    stroke: var(--series-smooth, #0f3f44);
    stroke-width: 1.1;
  }

  .sleep-start-path,
  .sleep-start-swatch {
    color: #24557b;
    stroke: #24557b;
  }

  .sleep-end-path,
  .sleep-end-swatch {
    color: #63a19c;
    stroke: #63a19c;
  }

  .sleep-start-dot {
    fill: #24557b;
  }

  .sleep-end-dot {
    fill: #63a19c;
  }

  .empty-panel {
    margin-top: 18px;
  }

  @media (max-width: 900px) {
    .hero-stats {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  @media (max-width: 720px) {
    h2 {
      font-size: 1.3rem;
    }

    .eyebrow-row,
    .section-head {
      align-items: flex-start;
      flex-direction: column;
    }
  }

  @media (max-width: 560px) {
    .panel {
      padding: 18px;
      border-radius: 20px;
    }

    .hero-stats {
      grid-template-columns: 1fr;
    }

    .hero-actions {
      width: 100%;
      flex-wrap: wrap;
    }

    .text-link {
      text-align: center;
    }

    .chart-stat-badge {
      min-width: 102px;
      padding: 8px 10px;
    }

    .raw-export-controls {
      grid-template-columns: 1fr;
    }

    .export-field-wide {
      grid-column: auto;
    }
  }
</style>
