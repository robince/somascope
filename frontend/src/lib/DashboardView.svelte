<script lang="ts">
  import {
    SLEEP_AXIS_LABELS,
    activitySegments,
    averageDefined,
    buildSparkPath,
    clockTimeLabel,
    fillWindow,
    minutesToHoursLabel,
    movingAverage,
    sleepOpacity,
    sleepPosition,
    sumDefined
  } from "./dashboard";
  import { PERIODS, formatLongDate, formatMonthDayCompact, formatRangeLabel, formatWeekday, getPeriod, getWindowStart } from "./time";
  import type { AppInfo, DashboardOverview, ExportFormat, PeriodId } from "./types";

  export let appInfo: AppInfo | null = null;
  export let dashboard: DashboardOverview | null = null;
  export let formats: ExportFormat[] = [];
  export let activePeriod: PeriodId = "1m";
  export let windowEndDate = "";
  export let loading = false;
  export let error = "";
  export let onSelectPeriod: (period: PeriodId) => void = () => {};
  export let onShiftWindow: (direction: -1 | 1) => void = () => {};
  export let onOpenSettings: () => void = () => {};

  $: period = getPeriod(activePeriod);
  $: resolvedEndDate = windowEndDate || dashboard?.latest_date || "";
  $: windowStartDate = resolvedEndDate ? getWindowStart(resolvedEndDate, period.days) : "";
  $: visibleDays = dashboard && windowStartDate ? fillWindow(dashboard.daily, windowStartDate, resolvedEndDate) : [];
  $: rangeLabel = visibleDays.length ? formatRangeLabel(visibleDays[0].date, visibleDays[visibleDays.length - 1].date) : "No visible range";
  $: dayWidth = visibleDays.length > 180 ? 8 : visibleDays.length > 90 ? 12 : visibleDays.length > 42 ? 20 : 58;
  $: chartMode = visibleDays.length > 180 ? "micro" : visibleDays.length > 42 ? "dense" : "regular";
  $: readinessSeries = visibleDays.map((day) => day.readiness?.score ?? null);
  $: smoothingWindow = visibleDays.length > 180 ? 21 : visibleDays.length > 90 ? 14 : visibleDays.length > 42 ? 7 : 3;
  $: smoothedReadiness = movingAverage(readinessSeries, smoothingWindow);
  $: sparkWidth = Math.max(visibleDays.length - 1, 1);
  $: readinessPath = buildSparkPath(smoothedReadiness, sparkWidth, 96, 40, 100);
  $: rawReadinessDots = readinessSeries.map((score, index) => ({
    score,
    x: visibleDays.length === 1 ? sparkWidth / 2 : (index / Math.max(visibleDays.length - 1, 1)) * sparkWidth,
    y: score == null ? 0 : 96 - Math.min(Math.max((score - 40) / 60, 0), 1) * 96
  }));
  $: latestDay = [...visibleDays].reverse().find((day) => day.activity || day.readiness || day.sleep) ?? null;
  $: averageReadiness = averageDefined(visibleDays.map((day) => day.readiness?.score));
  $: averageSleep = averageDefined(visibleDays.map((day) => day.sleep?.duration_minutes));
  $: totalSteps = sumDefined(visibleDays.map((day) => day.activity?.steps));
  $: activeDays = visibleDays.filter((day) => day.activity || day.readiness || day.sleep).length;
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
</script>

<section class="dashboard-shell">
  <div class="hero-grid">
    <article class="panel intro-panel">
      <div class="eyebrow-row">
        <p class="eyebrow">Dashboard</p>
        <button class="text-link" type="button" onclick={onOpenSettings}>Open settings</button>
      </div>
      <h1>Daily body signals, local-first.</h1>
      <p class="lede">
        Start with blocks and strips that make the synced Oura record feel tangible: activity as the main
        daily block view, readiness as a compact trend, and sleep as a shared nightly timeline.
      </p>

      <div class="hero-stats">
        <article>
          <strong>{latestDay?.readiness?.score ?? "--"}</strong>
          <span>Latest readiness</span>
        </article>
        <article>
          <strong>{minutesToHoursLabel(Math.round(averageSleep ?? 0) || undefined)}</strong>
          <span>Average sleep in range</span>
        </article>
        <article>
          <strong>{new Intl.NumberFormat().format(totalSteps)}</strong>
          <span>Total steps in range</span>
        </article>
        <article>
          <strong>{activeDays}</strong>
          <span>Days with visible data</span>
        </article>
      </div>
    </article>

    <aside class="panel status-panel">
      <p class="eyebrow">Sync snapshot</p>
      <dl class="status-stack">
        <div class="status-row">
          <dt>Latest local day</dt>
          <dd>{dashboard?.latest_date ? formatLongDate(dashboard.latest_date) : "No data"}</dd>
        </div>
        <div class="status-row">
          <dt>Providers</dt>
          <dd>{dashboard?.providers.length ? dashboard.providers.join(", ") : "None"}</dd>
        </div>
        <div class="status-row">
          <dt>Range loaded</dt>
          <dd>{dashboard?.available_days ?? 0} day entries</dd>
        </div>
        <div class="status-row">
          <dt>Store</dt>
          <dd>{appInfo?.db_path ?? "Local SQLite path"}</dd>
        </div>
      </dl>
    </aside>
  </div>

  <article class="panel controls-panel">
    <div class="controls-row">
      <div>
        <p class="eyebrow">Navigation</p>
        <h2>{period.label} window</h2>
        <p class="helper-copy">{rangeLabel}</p>
      </div>

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
              <small>{option.shortcut}</small>
            </button>
          {/each}
        </div>

        <div class="nav-group">
          <button class="nav-button" type="button" onclick={() => onShiftWindow(-1)}>&larr; Prev</button>
          <button class="nav-button" type="button" onclick={() => onShiftWindow(1)}>Next &rarr;</button>
        </div>
      </div>
    </div>

    <p class="shortcut-copy">Keyboard: `w`, `m`, `q`, `y` switch scale; left and right arrows move the visible period.</p>
  </article>

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
          <p class="section-note">Activity is the right first block plot: it is dense, comparable day to day, and it already carries a 24h composition.</p>
        </div>

        <div class="legend-row">
          <span class="legend-item high">High</span>
          <span class="legend-item medium">Medium</span>
          <span class="legend-item low">Low</span>
          <span class="legend-item rest">Rest</span>
          <span class="legend-item off">Off-body</span>
        </div>

        <div class="block-scroll">
          <div class="activity-grid {chartMode}" style={`--day-width:${dayWidth}px;`}>
            {#each visibleDays as day, index}
              {@const segments = activitySegments(day)}
              <article class:missing={!segments.length} class="activity-day">
                <header class="day-head">
                  <small>{formatWeekday(day.date)}</small>
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
            <h2>Compact readiness trend</h2>
          </div>
          <div class="section-metrics">
            <span>Latest {latestDay?.readiness?.score ?? "--"}</span>
            <span>Avg {averageReadiness ? Math.round(averageReadiness) : "--"}</span>
          </div>
        </div>

        <div class="trend-wrap">
          <svg viewBox={`0 0 ${sparkWidth} 96`} preserveAspectRatio="none" aria-label="Readiness trend">
            <line x1="0" y1="16" x2={sparkWidth} y2="16" class="guide-line" />
            <line x1="0" y1="48" x2={sparkWidth} y2="48" class="guide-line" />
            <line x1="0" y1="80" x2={sparkWidth} y2="80" class="guide-line" />
            {#if readinessPath}
              <path d={readinessPath} class="trend-path" />
            {/if}
            {#if visibleDays.length <= 90}
              {#each rawReadinessDots as point}
                {#if point.score != null}
                  <circle cx={point.x} cy={point.y} r="1.7" class="trend-dot" />
                {/if}
              {/each}
            {/if}
          </svg>
        </div>

        <div class="trend-footer">
          <p>Smoothing uses a {smoothingWindow}-day moving average so the range shortcuts can scale without changing the chart grammar.</p>
          <div class="trend-labels">
            <span>40</span>
            <span>70</span>
            <span>100</span>
          </div>
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

          <div class="sleep-scroll">
            <div class="sleep-grid {chartMode}" style={`--day-width:${dayWidth}px;`}>
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
  .status-stack,
  .visual-grid,
  .planned-list {
    display: grid;
    gap: 18px;
  }

  .hero-grid {
    display: grid;
    grid-template-columns: 1.4fr 0.9fr;
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
  .controls-row,
  .section-head,
  .toolbar,
  .period-group,
  .nav-group,
  .legend-row,
  .export-actions,
  .section-metrics,
  .trend-labels {
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
  .helper-copy,
  .section-note,
  .trend-footer p,
  .shortcut-copy,
  .status-row dd {
    color: var(--muted);
    line-height: 1.5;
  }

  .lede {
    max-width: 52rem;
    margin-top: 14px;
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

  .status-stack {
    margin-top: 12px;
  }

  .status-row {
    display: grid;
    gap: 4px;
    padding: 12px 0;
    border-bottom: 1px solid var(--line);
  }

  .status-row:last-child {
    border-bottom: 0;
  }

  dt,
  .planned-card strong {
    font-size: 12px;
    color: var(--accent);
    letter-spacing: 0.1em;
    text-transform: uppercase;
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .controls-panel {
    margin-top: 18px;
  }

  .shortcut-copy {
    margin-top: 12px;
  }

  .period-group {
    gap: 8px;
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
  .button {
    border-radius: 999px;
    padding: 10px 14px;
  }

  .period-button {
    display: inline-flex;
    gap: 8px;
    align-items: center;
  }

  .period-button small {
    color: var(--muted);
  }

  .period-button.active,
  .button-primary {
    background: var(--accent);
    color: white;
    border-color: transparent;
  }

  .button-ghost {
    background: rgba(255, 255, 255, 0.62);
  }

  .text-link {
    border-radius: 999px;
    padding: 8px 12px;
  }

  .visual-grid {
    margin-top: 18px;
  }

  .activity-grid,
  .sleep-grid {
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: var(--day-width);
    gap: 10px;
    align-items: end;
  }

  .activity-day,
  .sleep-day {
    display: grid;
    gap: 10px;
  }

  .activity-day.missing,
  .sleep-day.missing {
    opacity: 0.48;
  }

  .block-scroll,
  .sleep-scroll {
    overflow-x: auto;
    padding-bottom: 8px;
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
    border-radius: 16px;
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
    height: 150px;
    border-radius: 20px;
    border: 1px solid var(--line);
    background: linear-gradient(180deg, rgba(26, 106, 114, 0.08), rgba(255, 255, 255, 0.55));
    padding: 16px;
  }

  .trend-wrap svg {
    width: 100%;
    height: 100%;
    overflow: visible;
  }

  .guide-line {
    stroke: rgba(24, 32, 25, 0.1);
    stroke-width: 0.8;
  }

  .trend-path {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .trend-dot {
    fill: rgba(26, 106, 114, 0.28);
  }

  .trend-footer {
    display: grid;
    gap: 10px;
    margin-top: 12px;
  }

  .trend-labels {
    justify-content: space-between;
    color: var(--muted);
    font-size: 0.82rem;
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
    border-radius: 16px;
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

  @media (max-width: 980px) {
    .hero-grid,
    .sleep-layout {
      grid-template-columns: 1fr;
    }

    .hero-stats {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .controls-row,
    .toolbar {
      align-items: flex-start;
      flex-direction: column;
    }
  }

  @media (max-width: 720px) {
    .hero-stats {
      grid-template-columns: 1fr;
    }

    .period-group,
    .nav-group,
    .export-actions {
      width: 100%;
      flex-wrap: wrap;
    }

    .period-button,
    .nav-button,
    .button {
      flex: 1 1 0;
      justify-content: center;
    }

    .panel {
      padding: 18px;
    }
  }
</style>
