<script lang="ts">
  import { buildChartTimeAxis } from "./chartTime";
  import {
    SLEEP_AXIS_LABELS,
    averageDefined,
    buildDashboardBuckets,
    chartResolutionForDays,
    centeredMovingAverage,
    fillWindow,
    minutesToHoursLabel
  } from "./dashboard";
  import { PERIODS, formatRangeLabel, getPeriod, getWindowStart } from "./time";
  import type { DashboardOverview, OuraStatus, PeriodId } from "./types";

  export let dashboard: DashboardOverview | null = null;
  export let activePeriod: PeriodId = "1m";
  export let windowEndDate = "";
  export let loading = false;
  export let ouraBusy = false;
  export let ouraStatus: OuraStatus | null = null;
  export let error = "";
  export let onSelectPeriod: (period: PeriodId) => void = () => {};
  export let onShiftWindow: (direction: -1 | 1) => void = () => {};
  export let onSyncIncremental: () => void = () => {};

  const CHART_WIDTH = 720;
  const CHART_HEIGHT = 220;
  const CHART_PAD_LEFT = 42;
  const CHART_PAD_RIGHT = 12;
  const CHART_PAD_TOP = 16;
  const CHART_PAD_BOTTOM = 28;
  const SLEEP_CHART_HEIGHT = 240;
  const READINESS_MIN = 40;
  const READINESS_MAX = 100;
  const READINESS_TICKS = [100, 85, 70, 55, 40];
  const SLEEP_AXIS_MAX_MINUTES = 18 * 60;
  type SeriesPoint = { key: string; x: number; y: number };

  $: period = getPeriod(activePeriod);
  $: resolvedEndDate = windowEndDate || dashboard?.latest_date || "";
  $: windowStartDate = resolvedEndDate ? getWindowStart(resolvedEndDate, period.days) : "";
  $: visibleDays = dashboard && windowStartDate ? fillWindow(dashboard.daily, windowStartDate, resolvedEndDate) : [];
  $: rangeLabel = visibleDays.length ? formatRangeLabel(visibleDays[0].date, visibleDays[visibleDays.length - 1].date) : "No visible range";
  $: chartResolution = chartResolutionForDays(visibleDays.length);
  $: buckets = buildDashboardBuckets(visibleDays, chartResolution);
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
  $: activityValues = buckets.map((bucket) => bucket.activity_steps);
  $: readinessValues = buckets.map((bucket) => bucket.readiness_score);
  $: sleepStartValues = buckets.map((bucket) => bucket.sleep_start_minutes);
  $: sleepEndValues = buckets.map((bucket) => bucket.sleep_end_minutes);
  $: weekendBands =
    chartResolution === "daily" && chartTimeAxis
      ? buildWeekendBands(visibleDays, chartTimeAxis)
      : [];
  $: smoothingWindow = getSmoothingWindow(activePeriod);
  $: smoothingOffset = smoothingWindow % 2 === 0 ? 0.5 : 0;
  $: activitySmoothed = centeredMovingAverage(activityValues, smoothingWindow);
  $: readinessSmoothed = centeredMovingAverage(readinessValues, smoothingWindow);
  $: activityMax = niceUpperBound(activityValues, 1000);
  $: activityTicks = buildLinearTicks(activityMax, 4);
  $: activityRawPoints = buildSeriesPoints(activityValues, buckets, chartTimeAxis, activityY, CHART_HEIGHT);
  $: activitySmoothPoints = buildSeriesPoints(activitySmoothed, buckets, chartTimeAxis, activityY, CHART_HEIGHT, smoothingOffset);
  $: activityPath = buildPathFromPoints(activityRawPoints);
  $: activitySmoothPath = buildPathFromPoints(activitySmoothPoints);
  $: activityDotCount = activityRawPoints.length;
  $: readinessRawPoints = buildSeriesPoints(readinessValues, buckets, chartTimeAxis, readinessY, CHART_HEIGHT);
  $: readinessSmoothPoints = buildSeriesPoints(readinessSmoothed, buckets, chartTimeAxis, readinessY, CHART_HEIGHT, smoothingOffset);
  $: readinessPath = buildPathFromPoints(readinessRawPoints);
  $: readinessSmoothPath = buildPathFromPoints(readinessSmoothPoints);
  $: readinessDotCount = readinessRawPoints.length;
  $: sleepStartPoints = buildSeriesPoints(sleepStartValues, buckets, chartTimeAxis, sleepY, SLEEP_CHART_HEIGHT);
  $: sleepEndPoints = buildSeriesPoints(sleepEndValues, buckets, chartTimeAxis, sleepY, SLEEP_CHART_HEIGHT);
  $: sleepBandPath = buildBandPathFromPoints(sleepStartPoints, sleepEndPoints);
  $: sleepStartPath = buildPathFromPoints(sleepStartPoints);
  $: sleepEndPath = buildPathFromPoints(sleepEndPoints);
  $: sleepDotCount = sleepStartPoints.length;
  $: averageReadiness = averageDefined(visibleDays.map((day) => day.readiness?.score));
  $: averageSleep = averageDefined(visibleDays.map((day) => day.sleep?.duration_minutes));
  $: averageDailySteps = averageDefined(visibleDays.map((day) => day.activity?.steps));
  $: rawOuraExportURL = dashboard?.export_urls.raw_jsonl_by_provider?.oura ?? "";

  function bucketCenterX(index: number, offsetUnits = 0): number {
    const bucket = buckets[index];
    return scaledBucketCenterX(bucket, chartTimeAxis, offsetUnits);
  }

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
          disabled={ouraBusy || !ouraStatus?.connected}
        >
          {ouraBusy ? "Updating..." : "Update data"}
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
        <strong>{ouraStatus?.last_sync_at ? "Fresh" : "--"}</strong>
        <span>{ouraStatus?.last_sync_at ? `Last sync ${new Date(ouraStatus.last_sync_at).toLocaleString()}` : "No sync yet"}</span>
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
          <span class="legend-line">
            <span class="line-swatch line-swatch-raw"></span>
            Per {chartResolution === "weekly" ? "week" : "day"}
          </span>
          <span class="legend-line">
            <span class="line-swatch line-swatch-smooth"></span>
            {rollingLabel(smoothingWindow)}
          </span>
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
            {#each activityTicks as tick}
              {@const y = activityY(tick)}
              <line
                x1={CHART_PAD_LEFT}
                y1={y}
                x2={CHART_WIDTH - CHART_PAD_RIGHT}
                y2={y}
                class="guide-line"
              />
              <text x={CHART_PAD_LEFT - 8} y={y + 4} text-anchor="end" class="axis-label">{new Intl.NumberFormat().format(tick)}</text>
            {/each}

            {#if activitySmoothPath}
              <path d={activitySmoothPath} class="trend-path trend-path-smooth" />
            {/if}
            {#if activityPath}
              <path d={activityPath} class="trend-path trend-path-raw" />
            {/if}

            {#if activityDotCount <= 40}
              {#each activityRawPoints as point (point.key)}
                <circle cx={point.x} cy={point.y} r="2.6" class="trend-dot trend-dot-raw" />
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

      <article class="panel readiness-panel">
        <div class="section-head">
          <div>
            <p class="eyebrow">Trend</p>
            <h2>Readiness</h2>
          </div>
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
            {#each READINESS_TICKS as tick}
              {@const y = readinessY(tick)}
              <line
                x1={CHART_PAD_LEFT}
                y1={y}
                x2={CHART_WIDTH - CHART_PAD_RIGHT}
                y2={y}
                class="guide-line"
              />
              <text x={CHART_PAD_LEFT - 8} y={y + 4} text-anchor="end" class="axis-label">{tick}</text>
            {/each}

            {#if readinessSmoothPath}
              <path d={readinessSmoothPath} class="trend-path trend-path-smooth" />
            {/if}
            {#if readinessPath}
              <path d={readinessPath} class="trend-path trend-path-raw" />
            {/if}

            {#if readinessDotCount <= 40}
              {#each readinessRawPoints as point (point.key)}
                <circle cx={point.x} cy={point.y} r="2.6" class="trend-dot trend-dot-raw" />
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
            {#each SLEEP_AXIS_LABELS as tick}
              {@const y = sleepY((tick.percent / 100) * SLEEP_AXIS_MAX_MINUTES)}
              <line
                x1={CHART_PAD_LEFT}
                y1={y}
                x2={CHART_WIDTH - CHART_PAD_RIGHT}
                y2={y}
                class="guide-line"
              />
              <text x={CHART_PAD_LEFT - 8} y={y + 4} text-anchor="end" class="axis-label">{tick.label}</text>
            {/each}

            {#if sleepBandPath}
              <path d={sleepBandPath} class="sleep-band" />
            {/if}
            {#if sleepStartPath}
              <path d={sleepStartPath} class="trend-path sleep-start-path" />
            {/if}
            {#if sleepEndPath}
              <path d={sleepEndPath} class="trend-path sleep-end-path" />
            {/if}

            {#if sleepDotCount <= 40}
              {#each sleepStartPoints as point (point.key)}
                <circle cx={point.x} cy={point.y} r="2.5" class="trend-dot sleep-start-dot" />
              {/each}
              {#each sleepEndPoints as point (point.key)}
                <circle cx={point.x} cy={point.y} r="2.5" class="trend-dot sleep-end-dot" />
              {/each}
            {/if}

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

          {#if rawOuraExportURL}
            <div class="export-subhead">
              <p class="eyebrow">Raw provider data</p>
            </div>
            <div class="export-actions">
              <a class="button button-ghost" href={rawOuraExportURL} download="somascope-oura-raw.jsonl">Oura JSONL</a>
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
    color: rgba(26, 106, 114, 0.34);
  }

  .line-swatch-smooth {
    color: var(--accent);
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
    stroke: rgba(26, 106, 114, 0.34);
    stroke-width: 1.9;
  }

  .trend-path-smooth {
    stroke: var(--accent);
  }

  .trend-dot {
    stroke: rgba(255, 255, 255, 0.9);
    stroke-width: 1.25;
  }

  .trend-dot-raw {
    fill: rgba(26, 106, 114, 0.42);
  }

  .export-stack {
    display: grid;
    gap: 18px;
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

  .sleep-wrap {
    background: linear-gradient(180deg, rgba(28, 58, 92, 0.1), rgba(255, 255, 255, 0.58));
  }

  .sleep-band {
    fill: rgba(38, 94, 126, 0.12);
    stroke: none;
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
  }
</style>
