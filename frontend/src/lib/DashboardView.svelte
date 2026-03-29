<script lang="ts">
  import {
    SLEEP_AXIS_LABELS,
    activitySegments,
    averageDefined,
    clockTimeLabel,
    fillWindow,
    minutesToHoursLabel,
    sleepOpacity,
    sleepPosition
  } from "./dashboard";
  import { PERIODS, formatMonthDayCompact, formatRangeLabel, formatWeekday, getPeriod, getWindowStart } from "./time";
  import type { DashboardOverview, ExportFormat, OuraStatus, PeriodId } from "./types";

  export let dashboard: DashboardOverview | null = null;
  export let formats: ExportFormat[] = [];
  export let activePeriod: PeriodId = "1m";
  export let windowEndDate = "";
  export let loading = false;
  export let ouraBusy = false;
  export let ouraStatus: OuraStatus | null = null;
  export let error = "";
  export let onSelectPeriod: (period: PeriodId) => void = () => {};
  export let onShiftWindow: (direction: -1 | 1) => void = () => {};
  export let onOpenSettings: (anchor?: string) => void = () => {};
  export let onSyncIncremental: () => void = () => {};

  const READINESS_CHART_WIDTH = 720;
  const READINESS_CHART_HEIGHT = 220;
  const READINESS_CHART_PAD_LEFT = 42;
  const READINESS_CHART_PAD_RIGHT = 12;
  const READINESS_CHART_PAD_TOP = 16;
  const READINESS_CHART_PAD_BOTTOM = 28;
  const READINESS_MIN = 40;
  const READINESS_MAX = 100;
  const READINESS_TICKS = [100, 85, 70, 55, 40];

  $: period = getPeriod(activePeriod);
  $: resolvedEndDate = windowEndDate || dashboard?.latest_date || "";
  $: windowStartDate = resolvedEndDate ? getWindowStart(resolvedEndDate, period.days) : "";
  $: visibleDays = dashboard && windowStartDate ? fillWindow(dashboard.daily, windowStartDate, resolvedEndDate) : [];
  $: rangeLabel = visibleDays.length ? formatRangeLabel(visibleDays[0].date, visibleDays[visibleDays.length - 1].date) : "No visible range";
  $: chartMode = visibleDays.length > 180 ? "micro" : visibleDays.length > 42 ? "dense" : "regular";
  $: chartGap = chartMode === "micro" ? 2 : chartMode === "dense" ? 4 : 8;
  $: readinessPoints = visibleDays.map((day, index) => {
    const score = day.readiness?.score;
    return score == null
      ? null
      : {
          date: day.date,
          score,
          x: readinessX(index, visibleDays.length),
          y: readinessY(score)
        };
  });
  $: readinessPath = buildReadinessPath(readinessPoints);
  $: readinessDotCount = readinessPoints.filter(Boolean).length;
  $: latestDay = [...visibleDays].reverse().find((day) => day.activity || day.readiness || day.sleep) ?? null;
  $: averageReadiness = averageDefined(visibleDays.map((day) => day.readiness?.score));
  $: averageSleep = averageDefined(visibleDays.map((day) => day.sleep?.duration_minutes));
  $: averageDailySteps = averageDefined(visibleDays.map((day) => day.activity?.steps));
  $: plannedFormats = formats.filter((format) => format.id === "raw-json");

  function scoreTone(score?: number): string {
    if (score == null) {
      return "muted";
    }
    if (score >= 85) {
      return "excellent";
    }
    if (score >= 72) {
      return "steady";
    }
    if (score >= 60) {
      return "watch";
    }
    return "low";
  }

  function showTick(index: number, total: number): boolean {
    if (total <= 14) {
      return true;
    }
    if (total <= 31) {
      return index % 5 === 0 || index === total - 1;
    }
    if (total <= 90) {
      return index % 14 === 0 || index === total - 1;
    }
    if (total <= 180) {
      return index % 30 === 0 || index === total - 1;
    }
    return index % 60 === 0 || index === total - 1;
  }

  function readinessX(index: number, total: number): number {
    const plotWidth = READINESS_CHART_WIDTH - READINESS_CHART_PAD_LEFT - READINESS_CHART_PAD_RIGHT;
    if (total <= 1) {
      return READINESS_CHART_PAD_LEFT + plotWidth / 2;
    }
    return READINESS_CHART_PAD_LEFT + (index / (total - 1)) * plotWidth;
  }

  function readinessY(score: number): number {
    const plotHeight = READINESS_CHART_HEIGHT - READINESS_CHART_PAD_TOP - READINESS_CHART_PAD_BOTTOM;
    const normalized = Math.min(Math.max((score - READINESS_MIN) / (READINESS_MAX - READINESS_MIN), 0), 1);
    return READINESS_CHART_HEIGHT - READINESS_CHART_PAD_BOTTOM - normalized * plotHeight;
  }

  function buildReadinessPath(points: Array<{ x: number; y: number } | null>): string {
    let path = "";
    let activeSegment = false;

    for (const point of points) {
      if (!point) {
        activeSegment = false;
        continue;
      }

      path += activeSegment ? ` L ${point.x} ${point.y}` : `M ${point.x} ${point.y}`;
      activeSegment = true;
    }

    return path;
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
        <button class="text-link" type="button" onclick={() => onOpenSettings()}>Open settings</button>
      </div>
    </div>
    <h1>Daily body signals, local-first.</h1>
    <p class="lede">
      Keep the dashboard dense and readable: activity as the main daily block view, readiness as a simple line, and sleep as a shared nightly timeline.
    </p>

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

    <p class="window-copy">{period.label} window · {rangeLabel}</p>
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
      <p>Once a provider sync lands, this page can fill the same period framework with activity blocks, readiness trends, and sleep strips.</p>
    </article>
  {:else}
    <section class="visual-grid">
      <article class="panel activity-panel">
        <div class="section-head">
          <div>
            <p class="eyebrow">Block View</p>
            <h2>Daily activity blocks</h2>
          </div>
        </div>

        <div class="legend-row">
          <span class="legend-item high">High</span>
          <span class="legend-item medium">Medium</span>
          <span class="legend-item low">Low</span>
          <span class="legend-item rest">Rest</span>
          <span class="legend-item off">Off-body</span>
        </div>

        <div class="block-frame">
          <div class="activity-grid {chartMode}" style={`--day-count:${visibleDays.length}; --grid-gap:${chartGap}px;`}>
            {#each visibleDays as day, index}
              {@const segments = activitySegments(day)}
              <article class:missing={!segments.length} class="activity-day">
                <header class="day-head">
                  <small>{showTick(index, visibleDays.length) ? formatWeekday(day.date) : ""}</small>
                  <strong class={`score-pill ${scoreTone(day.activity?.score)}`}>{day.activity?.score ?? "--"}</strong>
                </header>

                <div class="segments">
                  {#if segments.length}
                    {#each segments as segment}
                      <div
                        class={`segment ${segment.className}`}
                        style={`height:${segment.percent}%;`}
                        title={`${segment.label}: ${segment.minutes} min`}
                      ></div>
                    {/each}
                  {:else}
                    <div class="segment-empty"></div>
                  {/if}
                </div>

                <div class="day-meta">
                  <span>{day.activity?.steps ? new Intl.NumberFormat().format(day.activity.steps) : "--"}</span>
                  {#if showTick(index, visibleDays.length)}
                    <small>{formatMonthDayCompact(day.date)}</small>
                  {/if}
                </div>
              </article>
            {/each}
          </div>
        </div>
      </article>

      <article class="panel readiness-panel">
        <div class="section-head">
          <div>
            <p class="eyebrow">Trend</p>
            <h2>Readiness trend</h2>
          </div>
          <div class="section-metrics">
            <span>Latest {latestDay?.readiness?.score ?? "--"}</span>
            <span>Avg {averageReadiness ? Math.round(averageReadiness) : "--"}</span>
          </div>
        </div>

        <div class="trend-wrap">
          <svg viewBox={`0 0 ${READINESS_CHART_WIDTH} ${READINESS_CHART_HEIGHT}`} aria-label="Readiness trend">
            {#each READINESS_TICKS as tick}
              {@const y = readinessY(tick)}
              <line
                x1={READINESS_CHART_PAD_LEFT}
                y1={y}
                x2={READINESS_CHART_WIDTH - READINESS_CHART_PAD_RIGHT}
                y2={y}
                class="guide-line"
              />
              <text x={READINESS_CHART_PAD_LEFT - 8} y={y + 4} text-anchor="end" class="axis-label">{tick}</text>
            {/each}

            {#if readinessPath}
              <path d={readinessPath} class="trend-path" />
            {/if}

            {#if readinessDotCount <= 62}
              {#each readinessPoints as point}
                {#if point}
                  <circle cx={point.x} cy={point.y} r="3" class="trend-dot" />
                {/if}
              {/each}
            {/if}

            <line
              x1={READINESS_CHART_PAD_LEFT}
              y1={READINESS_CHART_PAD_TOP}
              x2={READINESS_CHART_PAD_LEFT}
              y2={READINESS_CHART_HEIGHT - READINESS_CHART_PAD_BOTTOM}
              class="axis-line"
            />
            <line
              x1={READINESS_CHART_PAD_LEFT}
              y1={READINESS_CHART_HEIGHT - READINESS_CHART_PAD_BOTTOM}
              x2={READINESS_CHART_WIDTH - READINESS_CHART_PAD_RIGHT}
              y2={READINESS_CHART_HEIGHT - READINESS_CHART_PAD_BOTTOM}
              class="axis-line"
            />

            {#each visibleDays as day, index}
              {#if showTick(index, visibleDays.length)}
                <text
                  x={readinessX(index, visibleDays.length)}
                  y={READINESS_CHART_HEIGHT - 8}
                  text-anchor="middle"
                  class="axis-label"
                >
                  {formatMonthDayCompact(day.date)}
                </text>
              {/if}
            {/each}
          </svg>
        </div>

        <div class="trend-footer">
          <p>Daily readiness score across the visible range.</p>
        </div>
      </article>

      <article class="panel sleep-panel">
        <div class="section-head">
          <div>
            <p class="eyebrow">Sleep Strip</p>
            <h2>Night timeline</h2>
          </div>
          <p class="section-note">Shared vertical time axis from 6pm to 12pm next day, using the main nightly sleep and keeping naps as a side count.</p>
        </div>

        <div class="sleep-layout">
          <div class="sleep-axis">
            {#each SLEEP_AXIS_LABELS as tick}
              <span style={`top:${tick.percent}%;`}>{tick.label}</span>
            {/each}
          </div>

          <div class="sleep-frame">
            <div class="sleep-grid {chartMode}" style={`--day-count:${visibleDays.length}; --grid-gap:${chartGap}px;`}>
              {#each visibleDays as day, index}
                {@const position = sleepPosition(day.sleep)}
                <article class:missing={!position} class="sleep-day">
                  <div class="sleep-track">
                    {#each SLEEP_AXIS_LABELS as tick}
                      <div class="sleep-guide" style={`top:${tick.percent}%;`}></div>
                    {/each}
                    {#if position}
                      <div
                        class="sleep-bar"
                        style={`top:${position.top}%; height:${position.height}%; opacity:${sleepOpacity(day.sleep)};`}
                        title={`${clockTimeLabel(day.sleep?.start_time)} - ${clockTimeLabel(day.sleep?.end_time)}`}
                      ></div>
                    {/if}
                  </div>

                  <div class="sleep-meta">
                    <strong>{minutesToHoursLabel(day.sleep?.duration_minutes)}</strong>
                    <span>{day.sleep?.naps_count ? `${day.sleep.naps_count} nap` : ""}</span>
                    {#if showTick(index, visibleDays.length)}
                      <small>{formatMonthDayCompact(day.date)}</small>
                    {/if}
                  </div>
                </article>
              {/each}
            </div>
          </div>
        </div>
      </article>

      <article class="panel export-panel">
        <div class="section-head">
          <div>
            <p class="eyebrow">Export</p>
            <h2>Downloads over the same stored records</h2>
          </div>
        </div>

        <div class="export-actions">
          <a class="button button-primary" href={dashboard.export_urls.canonical_csv} download="somascope-canonical.csv">Download CSV</a>
          <a class="button button-ghost" href={dashboard.export_urls.canonical_jsonl} download="somascope-canonical.jsonl">Download JSONL</a>
        </div>

        {#if plannedFormats.length}
          <div class="planned-list">
            {#each plannedFormats as format}
              <article class="planned-card">
                <strong>{format.label}</strong>
                <span>{format.description}</span>
                <small>{format.status}</small>
              </article>
            {/each}
          </div>
        {/if}
      </article>
    </section>
  {/if}
</section>

<style>
  .dashboard-shell,
  .hero-stats,
  .visual-grid,
  .planned-list,
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
  .legend-row,
  .export-actions,
  .section-metrics {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
  }

  .eyebrow {
    margin: 0 0 8px;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.16em;
    font-size: 12px;
  }

  h1,
  h2,
  p {
    margin: 0;
  }

  h1 {
    max-width: 12ch;
    font-size: clamp(2rem, 4.6vw, 3.35rem);
    line-height: 0.95;
  }

  h2 {
    font-size: 1.55rem;
  }

  .lede,
  .section-note,
  .trend-footer p,
  .window-copy {
    color: var(--muted);
    line-height: 1.5;
  }

  .lede {
    max-width: 52rem;
    margin-top: 14px;
  }

  .hero-actions {
    position: relative;
    z-index: 1;
    gap: 8px;
  }

  .hero-stats {
    grid-template-columns: repeat(4, minmax(0, 1fr));
    margin-top: 24px;
    position: relative;
    z-index: 1;
  }

  .hero-stats article,
  .planned-card {
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

  .hero-stats span,
  .planned-card span {
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

  .activity-grid,
  .sleep-grid {
    display: grid;
    grid-template-columns: repeat(var(--day-count), minmax(0, 1fr));
    gap: var(--grid-gap);
    align-items: end;
    min-width: 0;
  }

  .activity-day,
  .sleep-day {
    display: grid;
    gap: 10px;
    min-width: 0;
  }

  .activity-day.missing,
  .sleep-day.missing {
    opacity: 0.48;
  }

  .block-frame,
  .sleep-frame {
    width: 100%;
    min-width: 0;
    overflow: hidden;
    padding-bottom: 4px;
  }

  .day-head,
  .day-meta,
  .sleep-meta {
    display: grid;
    gap: 4px;
  }

  .day-head small,
  .day-meta small,
  .sleep-meta small {
    color: var(--muted);
    font-size: 0.72rem;
    min-height: 1rem;
  }

  .score-pill {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 2rem;
    padding: 6px 10px;
    border-radius: 999px;
    width: fit-content;
    background: rgba(91, 109, 99, 0.12);
    font-size: 0.92rem;
  }

  .score-pill.excellent {
    background: rgba(53, 128, 82, 0.16);
    color: #1f6c40;
  }

  .score-pill.steady {
    background: rgba(26, 106, 114, 0.16);
    color: #115158;
  }

  .score-pill.watch {
    background: rgba(196, 136, 51, 0.17);
    color: #875a14;
  }

  .score-pill.low {
    background: rgba(163, 73, 56, 0.16);
    color: #8a3324;
  }

  .segments {
    height: 250px;
    border-radius: 12px;
    border: 1px solid var(--line);
    overflow: hidden;
    background: rgba(255, 255, 255, 0.52);
    display: flex;
    flex-direction: column;
    justify-content: end;
  }

  .segment {
    width: 100%;
  }

  .segment-empty {
    flex: 1;
    background:
      repeating-linear-gradient(
        -45deg,
        rgba(91, 109, 99, 0.05),
        rgba(91, 109, 99, 0.05) 8px,
        transparent 8px,
        transparent 16px
      );
  }

  .segment.high,
  .legend-item.high::before {
    background: #17656f;
  }

  .segment.medium,
  .legend-item.medium::before {
    background: #4d8d8a;
  }

  .segment.low,
  .legend-item.low::before {
    background: #97b6a8;
  }

  .segment.rest,
  .legend-item.rest::before {
    background: #e6d7b3;
  }

  .segment.off,
  .legend-item.off::before {
    background: #d7d5cf;
  }

  .legend-row {
    justify-content: flex-start;
    flex-wrap: wrap;
    margin: 16px 0 12px;
  }

  .legend-item {
    display: inline-flex;
    gap: 8px;
    align-items: center;
    color: var(--muted);
    font-size: 0.92rem;
  }

  .legend-item::before {
    content: "";
    width: 12px;
    height: 12px;
    border-radius: 999px;
  }

  .day-meta span,
  .sleep-meta strong {
    font-size: 0.78rem;
  }

  .day-meta span {
    color: var(--ink);
  }

  .trend-wrap {
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

  .guide-line {
    stroke: rgba(24, 32, 25, 0.1);
    stroke-width: 0.8;
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
    stroke: var(--accent);
    stroke-width: 2.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .trend-dot {
    fill: var(--accent);
    stroke: rgba(255, 255, 255, 0.9);
    stroke-width: 1.25;
  }

  .trend-footer {
    display: grid;
    gap: 8px;
    margin-top: 10px;
  }

  .sleep-layout {
    display: grid;
    grid-template-columns: 40px 1fr;
    gap: 12px;
    margin-top: 18px;
  }

  .sleep-axis {
    position: relative;
    min-height: 250px;
  }

  .sleep-axis span {
    position: absolute;
    transform: translateY(-50%);
    color: var(--muted);
    font-size: 0.76rem;
  }

  .sleep-track {
    position: relative;
    height: 250px;
    border-radius: 12px;
    border: 1px solid var(--line);
    background: rgba(255, 255, 255, 0.52);
    overflow: hidden;
  }

  .sleep-guide {
    position: absolute;
    left: 0;
    right: 0;
    height: 1px;
    background: rgba(24, 32, 25, 0.08);
  }

  .sleep-bar {
    position: absolute;
    left: 18%;
    right: 18%;
    border-radius: 999px;
    background: linear-gradient(180deg, rgba(38, 94, 126, 0.92), rgba(91, 154, 164, 0.74));
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.24);
  }

  .sleep-meta span {
    color: var(--muted);
    min-height: 1rem;
    font-size: 0.72rem;
  }

  .planned-list {
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    margin-top: 16px;
  }

  .planned-card small {
    display: inline-block;
    margin-top: 10px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .empty-panel {
    margin-top: 18px;
  }

  .dense .segments,
  .dense .sleep-track {
    height: 220px;
  }

  .dense .score-pill,
  .dense .day-meta span,
  .dense .sleep-meta strong,
  .micro .score-pill,
  .micro .day-meta span,
  .micro .sleep-meta strong {
    display: none;
  }

  .micro .segments,
  .micro .sleep-track {
    height: 180px;
  }

  @media (max-width: 900px) {
    .hero-stats {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .sleep-layout {
      grid-template-columns: 30px 1fr;
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

    .sleep-layout {
      grid-template-columns: 1fr;
    }

    .sleep-axis {
      display: none;
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
    }

    .text-link {
      text-align: center;
    }
  }
</style>
