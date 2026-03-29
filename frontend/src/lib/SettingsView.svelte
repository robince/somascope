<script lang="ts">
  import type { AppInfo, OuraRecent, OuraStatus, ProviderSettings } from "./types";

  export let appInfo: AppInfo | null = null;
  export let providers: ProviderSettings[] = [];
  export let ouraStatus: OuraStatus | null = null;
  export let ouraRecent: OuraRecent = { daily_records: [], sleep_sessions: [] };
  export let userTimezone = "";
  export let loading = false;
  export let statusLoading = false;
  export let statusError = "";
  export let saving = false;
  export let ouraBusy = false;
  export let syncStartDate = "";
  export let dirty = false;
  export let error = "";
  export let success = "";
  export let onReset: () => void = () => {};
  export let onSave: () => void = () => {};
  export let onRefresh: () => void = () => {};
  export let onConnectOura: () => void = () => {};
  export let onSyncOura: () => void = () => {};
  export let onSyncOuraFromDate: () => void = () => {};
  export let onSyncStartDateInput: (value: string) => void = () => {};
  export let onTimezoneInput: (value: string) => void = () => {};
  export let onProviderInput: (index: number, field: keyof ProviderSettings, value: string | boolean) => void = () => {};

  const showFitbitCredentialCard = false;
  const ouraApplicationURL = "https://developer.ouraring.com/applications";
  const ouraApplicationFields = [
    { label: "Name", value: "My Somastat" },
    { label: "Description", value: "Local data viewer" },
    { label: "Contact Email", value: "your email" },
    { label: "Website", value: "https://github.com/robince/somascope" },
    { label: "Privacy policy", value: "https://github.com/robince/somascope" },
    { label: "Terms of service", value: "https://github.com/robince/somascope" },
    { label: "Redirect URI", value: "http://localhost:18080/oauth/oura/callback" },
    { label: "Scopes", value: "Select all" }
  ];

  function numberValue(value: unknown): string {
    if (typeof value === "number") {
      return Number.isInteger(value) ? `${value}` : value.toFixed(2);
    }
    if (typeof value === "string") {
      return value;
    }
    return "--";
  }

  function metricValue(summary: Record<string, unknown>, key: string): string {
    return numberValue(summary[key]);
  }

  function timeLabel(value: string): string {
    if (!value) {
      return "--";
    }
    try {
      return new Date(value).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit"
      });
    } catch {
      return value;
    }
  }

  function entityLabel(value: string | undefined): string {
    return value ? value.replaceAll("_", " ") : "--";
  }

  function handleSyncStartDateEvent(event: Event) {
    onSyncStartDateInput((event.currentTarget as HTMLInputElement).value);
  }

  function connectionLabel(status: OuraStatus | null, loadingStatus: boolean, statusMessage: string): string {
    if (status?.connected) {
      return "Connected";
    }
    if (loadingStatus) {
      return "Checking...";
    }
    if (statusMessage) {
      return "Status unavailable";
    }
    if (status) {
      return "Not connected";
    }
    return "Unknown";
  }
</script>

<section class="settings-shell">
  <div class="hero-grid">
    <article class="panel intro-panel">
      <p class="eyebrow">Settings</p>
      <h1>Somascope Settings</h1>

      <div class="facts">
        <article>
          <strong>Auth Mode</strong>
          <span>{appInfo?.auth_mode ?? "Loading..."}</span>
        </article>
        <article>
          <strong>Store</strong>
          <span>{appInfo?.db_path ?? "Reserved local SQLite path"}</span>
        </article>
        <article>
          <strong>Version</strong>
          <span>{appInfo?.version?.version ?? "dev"}</span>
        </article>
      </div>
    </article>

    <aside class="panel side-panel">
      <p class="eyebrow">Oura status</p>
      <dl class="stack">
        <div class="stack-row">
          <dt>Connection</dt>
          <dd>{connectionLabel(ouraStatus, statusLoading, statusError)}</dd>
        </div>
        <div class="stack-row">
          <dt>Daily records</dt>
          <dd>{ouraStatus?.daily_record_count ?? 0}</dd>
        </div>
        <div class="stack-row">
          <dt>Sleep sessions</dt>
          <dd>{ouraStatus?.sleep_session_count ?? 0}</dd>
        </div>
        <div class="stack-row">
          <dt>Last sync</dt>
          <dd>{ouraStatus?.last_sync_at ?? "Not synced yet"}</dd>
        </div>
        <div class="stack-row">
          <dt>Run state</dt>
          <dd>{ouraStatus?.current_run?.status ?? ouraStatus?.last_completed_run?.status ?? "Idle"}</dd>
        </div>
      </dl>
    </aside>
  </div>

  <article class="panel sync-panel" id="oura-sync">
    <div class="section-head">
      <div>
        <p class="eyebrow">Sync controls</p>
        <h2>Backfill Oura data</h2>
      </div>
    </div>

    {#if statusLoading}
      <p class="status-copy">Checking local Oura connection status...</p>
    {:else if statusError}
      <p class="status-copy error">
        Oura status could not be loaded from the local app. {statusError}
      </p>
    {:else if !ouraStatus?.connected}
      <p class="status-copy warning">
        Oura sync actions are disabled until this app is connected to Oura. Save local credentials if needed,
        then use <strong>Connect Oura</strong> below.
      </p>
    {/if}

    <div class="sync-grid">
      <article class="sync-card">
        <p class="sync-copy">Pull older history from a specific day up to the present. Pick the earliest date you actually want to fetch.</p>
        <label class="field">
          <span class="field-label">Start date</span>
          <input
            type="date"
            value={syncStartDate}
            oninput={handleSyncStartDateEvent}
            onchange={handleSyncStartDateEvent}
          />
        </label>
        <button
          class="button button-ghost"
          type="button"
          onclick={onSyncOuraFromDate}
          disabled={loading || saving || ouraBusy || statusLoading || !ouraStatus?.connected || !syncStartDate}
        >
          {#if ouraBusy}
            Running...
          {:else if statusLoading}
            Checking connection...
          {:else if !ouraStatus?.connected}
            Connect Oura first
          {:else if !syncStartDate}
            Choose a start date
          {:else}
            Backfill from date
          {/if}
        </button>
      </article>
    </div>

    {#if ouraStatus?.current_run}
      <div class="sync-meta">
        <p class="eyebrow">Current run</p>
        <div class="sync-meta-grid">
          <article class="sync-meta-card">
            <strong>Status</strong>
            <span>{ouraStatus.current_run.status}</span>
          </article>
          <article class="sync-meta-card">
            <strong>Rows</strong>
            <span>{ouraStatus.current_run.rows_written}</span>
          </article>
          <article class="sync-meta-card">
            <strong>Updated</strong>
            <span>{ouraStatus.current_run.updated_at}</span>
          </article>
          <article class="sync-meta-card">
            <strong>Chunk</strong>
            <span>{entityLabel(ouraStatus.current_run.current_entity_kind)} {ouraStatus.current_run.current_chunk_start_date} {ouraStatus.current_run.current_chunk_end_date ? `to ${ouraStatus.current_run.current_chunk_end_date}` : ""}</span>
          </article>
          <article class="sync-meta-card">
            <strong>Retries</strong>
            <span>{ouraStatus.current_run.retry_count}</span>
          </article>
        </div>

        {#if ouraStatus.current_run.entities?.length}
          <div class="cursor-list">
            {#each ouraStatus.current_run.entities as entity}
              <div class="cursor-row">
                <span>{entityLabel(entity.entity_kind)} ({entity.status})</span>
                <span>
                  {entity.rows_written} rows
                  {#if entity.total_chunks > 0}
                    , {entity.completed_chunks}/{entity.total_chunks} chunks
                  {/if}
                </span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}

    {#if ouraStatus?.last_error?.message}
      <p class="status-copy error">
        {entityLabel(ouraStatus.last_error.entity_kind)} failed
        {#if ouraStatus.last_error.chunk_start_date}
          on {ouraStatus.last_error.chunk_start_date}{#if ouraStatus.last_error.chunk_end_date} to {ouraStatus.last_error.chunk_end_date}{/if}
        {/if}
        : {ouraStatus.last_error.message}
      </p>
    {/if}

    {#if ouraStatus?.last_completed_run}
      <div class="sync-meta">
        <p class="eyebrow">Last finished run</p>
        <div class="sync-meta-grid">
          <article class="sync-meta-card">
            <strong>Status</strong>
            <span>{ouraStatus.last_completed_run.status}</span>
          </article>
          <article class="sync-meta-card">
            <strong>Range</strong>
            <span>{ouraStatus.last_completed_run.effective_start_date ?? "--"} to {ouraStatus.last_completed_run.effective_end_date ?? "--"}</span>
          </article>
          <article class="sync-meta-card">
            <strong>Rows</strong>
            <span>{ouraStatus.last_completed_run.rows_written}</span>
          </article>
          <article class="sync-meta-card">
            <strong>Finished</strong>
            <span>{ouraStatus.last_completed_run.finished_at ?? ouraStatus.last_completed_run.updated_at}</span>
          </article>
        </div>

        {#if ouraStatus.sync_state?.length}
          <div class="cursor-list">
            {#each ouraStatus.sync_state as entry}
              <div class="cursor-row">
                <span>{entry.entity_kind.replaceAll("_", " ")}</span>
                <span>{entry.cursor_value}</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </article>

  <article class="panel settings-panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Provider Credentials</p>
        <h2>Bring your own app settings</h2>
      </div>
      <div class="actions">
        <button class="button button-ghost" type="button" onclick={onReset} disabled={loading || saving || !dirty}>
          Reset
        </button>
        <button class="button button-primary" type="button" onclick={onSave} disabled={loading || saving}>
          {saving ? "Saving..." : "Save local settings"}
        </button>
      </div>
    </div>

    <p class="helper">
      Secrets are only entered when you want to write them. After save, somascope treats them as present locally
      but does not render them back into the page. For Oura, the next step is browser authorization against your
      local callback and then a manual sync.
    </p>

    <div class="timezone-row">
      <label class="field">
        <span class="field-label">User timezone</span>
        <input
          type="text"
          value={userTimezone}
          oninput={(event) => onTimezoneInput((event.currentTarget as HTMLInputElement).value)}
          placeholder="Europe/Paris"
        />
      </label>
    </div>

    {#if loading}
      <p class="status-copy">Loading settings...</p>
    {:else}
      <div class="provider-grid">
        {#each providers as provider, index}
          {#if provider.provider !== "fitbit" || showFitbitCredentialCard}
          <article class="provider-card">
            <div class="provider-head">
              <div>
                <h3>{provider.provider === "fitbit" ? "Fitbit" : "Oura"}</h3>
                <p>{provider.notes}</p>
              </div>
              <span class:badge-on={provider.configured} class="badge">
                {provider.configured ? "Configured" : "Not configured"}
              </span>
            </div>

            {#if provider.provider === "oura"}
              <div class="setup-guide">
                <div class="setup-guide-head">
                  <div>
                    <p class="setup-guide-kicker">Oura app setup</p>
                    <h4>Create an Oura developer app first</h4>
                  </div>
                  <a class="button button-ghost" href={ouraApplicationURL} target="_blank" rel="noreferrer">Open Oura developer page</a>
                </div>

                <div class="setup-guide-grid">
                  <div class="setup-copy">
                    <p class="setup-copy-intro">
                      Create the app in Oura first, then paste the generated credentials into somascope below.
                    </p>
                    <ol class="setup-steps">
                      <li>Open the Oura applications page and click <strong>Create new</strong>.</li>
                      <li>Enter the values shown in the details column.</li>
                      <li>Copy the generated client ID and client secret into somascope, save local settings, then use <strong>Connect Oura</strong>.</li>
                    </ol>
                  </div>

                  <dl class="setup-values">
                    {#each ouraApplicationFields as field}
                      <div class="setup-value">
                        <dt>{field.label}</dt>
                        <dd><code>{field.value}</code></dd>
                      </div>
                    {/each}
                  </dl>
                </div>
              </div>
            {/if}

            <div class="field-grid">
              <label class="field field-wide">
                <span class="field-label">Client ID</span>
                <input
                  type="text"
                  value={provider.client_id}
                  oninput={(event) => onProviderInput(index, "client_id", (event.currentTarget as HTMLInputElement).value)}
                  placeholder="Paste your provider app client ID"
                />
              </label>

              <label class="field field-wide">
                <span class="field-label">Client secret</span>
                <input
                  type="password"
                  value={provider.client_secret}
                  oninput={(event) => onProviderInput(index, "client_secret", (event.currentTarget as HTMLInputElement).value)}
                  placeholder={provider.configured ? "Stored locally; enter a new value to replace it" : "Paste your provider app client secret"}
                />
              </label>

              <label class="field field-wide">
                <span class="field-label">Default scopes</span>
                <input
                  type="text"
                  value={provider.default_scopes}
                  placeholder={provider.provider === "oura" ? "Leave blank to request all app scopes" : undefined}
                  oninput={(event) => onProviderInput(index, "default_scopes", (event.currentTarget as HTMLInputElement).value)}
                />
              </label>
            </div>

            {#if provider.provider === "oura"}
              <div class="provider-actions">
                <button
                  class="button button-ghost"
                  type="button"
                  onclick={onRefresh}
                  disabled={loading || saving || ouraBusy}
                >
                  Refresh status
                </button>
                <button
                  class="button button-ghost"
                  type="button"
                  onclick={onConnectOura}
                  disabled={loading || saving || ouraBusy || !provider.configured}
                >
                  {ouraBusy ? "Working..." : "Connect Oura"}
                </button>
                <button
                  class="button button-primary"
                  type="button"
                  onclick={onSyncOura}
                  disabled={loading || saving || ouraBusy || statusLoading || !ouraStatus?.connected}
                >
                  {#if ouraBusy}
                    Working...
                  {:else if statusLoading}
                    Checking connection...
                  {:else}
                    Update now
                  {/if}
                </button>
              </div>

              {#if statusLoading}
                <p class="status-copy">Checking stored Oura connection...</p>
              {:else if statusError}
                <p class="status-copy warning">Connection status is currently unavailable. Use Refresh status to re-check the local app.</p>
              {:else if !provider.configured}
                <p class="status-copy warning">Save your local Oura client ID and secret before connecting.</p>
              {:else if !ouraStatus?.connected}
                <p class="status-copy warning">Credentials are saved locally. Use Connect Oura to finish authentication.</p>
              {/if}
            {/if}
          </article>
          {/if}
        {/each}
      </div>
    {/if}

    {#if error}
      <p class="status-copy error">{error}</p>
    {/if}
    {#if success}
      <p class="status-copy success">{success}</p>
    {/if}
  </article>

  <article class="panel data-panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Recent data</p>
        <h2>Last Oura data</h2>
      </div>
    </div>

    <div class="data-grid">
      <article class="data-card">
        <strong>Daily records</strong>
        {#if ouraRecent.daily_records.length === 0}
          <p class="empty-copy">No daily records yet. Connect Oura and run a sync.</p>
        {:else}
          <div class="record-list">
            {#each ouraRecent.daily_records as record}
              <div class="record-row">
                <div>
                  <p class="record-kind">{record.record_kind.replaceAll("_", " ")}</p>
                  <p class="record-date">{record.local_date}</p>
                </div>
                <div class="record-metrics">
                  {#if record.record_kind === "daily_activity"}
                    <span>{metricValue(record.summary, "steps")} steps</span>
                    <span>{metricValue(record.summary, "active_calories")} active kcal</span>
                  {:else if record.record_kind === "daily_readiness"}
                    <span>{metricValue(record.summary, "score")} score</span>
                    <span>{metricValue(record.summary, "temperature_deviation")} temp dev</span>
                  {:else}
                    <span>{Object.keys(record.summary).length} fields</span>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </article>

      <article class="data-card">
        <strong>Sleep sessions</strong>
        {#if ouraRecent.sleep_sessions.length === 0}
          <p class="empty-copy">No sleep sessions yet. The first sync will populate them.</p>
        {:else}
          <div class="record-list">
            {#each ouraRecent.sleep_sessions as session}
              <div class="record-row">
                <div>
                  <p class="record-kind">{session.is_nap ? "nap" : "sleep"}</p>
                  <p class="record-date">{timeLabel(session.start_time)} to {timeLabel(session.end_time)}</p>
                </div>
                <div class="record-metrics">
                  <span>{session.duration_minutes ?? "--"} min asleep</span>
                  <span>{session.efficiency_percent ?? "--"}% eff</span>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </article>
    </div>
  </article>
</section>

<style>
  .settings-shell,
  .facts,
  .provider-grid,
  .field-grid,
  .stack {
    display: grid;
    gap: 12px;
  }

  .hero-grid {
    display: grid;
    grid-template-columns: 1fr;
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

  .eyebrow {
    margin: 0 0 10px;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.16em;
    font-size: 12px;
  }

  h1,
  h2,
  h3,
  p {
    margin: 0;
  }

  h1 {
    font-size: clamp(1.9rem, 4.1vw, 3rem);
    line-height: 0.95;
  }

  h2 {
    font-size: 1.55rem;
  }

  h3 {
    font-size: 1.2rem;
  }

  .helper,
  .status-copy,
  .provider-head p,
  .stack-row dd {
    color: var(--muted);
    line-height: 1.55;
  }

  .facts {
    grid-template-columns: repeat(3, 1fr);
    margin-top: 20px;
  }

  .facts article,
  .provider-card,
  .data-card {
    border: 1px solid var(--line);
    border-radius: 16px;
    background: rgba(255, 255, 255, 0.56);
    padding: 14px;
  }

  span {
    color: var(--muted);
    overflow-wrap: anywhere;
  }

  .facts article strong,
  .sync-meta-card strong,
  .data-card > strong {
    display: block;
    margin-bottom: 8px;
    font-size: 13px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .stack {
    margin-top: 14px;
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }

  .stack-row {
    display: grid;
    gap: 4px;
    padding: 0 14px 0 0;
    border-right: 1px solid var(--line);
  }

  .stack-row:last-child {
    border-right: 0;
    padding-right: 0;
  }

  dt {
    font-size: 12px;
    color: var(--accent);
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .settings-panel,
  .data-panel {
    margin-top: 18px;
  }

  .section-head {
    display: flex;
    gap: 16px;
    align-items: end;
    justify-content: space-between;
  }

  .actions,
  .provider-actions {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }

  .button {
    border-radius: 999px;
    border: 1px solid var(--line);
    padding: 10px 16px;
    font: inherit;
    cursor: pointer;
  }

  .button:disabled {
    cursor: default;
    opacity: 0.6;
  }

  .button-primary {
    background: var(--accent);
    color: white;
    border-color: transparent;
  }

  .button-ghost {
    background: rgba(255, 255, 255, 0.6);
    color: var(--ink);
  }

  .helper {
    margin-top: 12px;
    max-width: 48rem;
  }

  .timezone-row {
    margin-top: 18px;
  }

  .provider-grid {
    margin-top: 18px;
  }

  .setup-guide {
    margin-top: 16px;
    border: 1px solid var(--line);
    border-radius: 16px;
    background: rgba(248, 244, 233, 0.72);
    padding: 16px;
  }

  .setup-guide-head {
    display: flex;
    gap: 16px;
    align-items: start;
    justify-content: space-between;
  }

  .setup-guide-kicker {
    margin: 0 0 6px;
    color: var(--accent);
    font-size: 0.78rem;
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  h4 {
    margin: 0;
    font-size: 1rem;
  }

  .setup-guide-grid {
    display: grid;
    grid-template-columns: minmax(0, 0.95fr) minmax(0, 1.05fr);
    gap: 18px;
    margin-top: 14px;
    align-items: start;
  }

  .setup-copy {
    display: grid;
    gap: 10px;
  }

  .setup-copy-intro {
    color: var(--muted);
    font-size: 0.92rem;
    line-height: 1.55;
  }

  .setup-steps {
    margin: 0;
    padding-left: 20px;
    color: var(--muted);
    font-size: 0.9rem;
    line-height: 1.55;
  }

  .setup-values {
    display: grid;
    gap: 10px;
    margin: 0;
  }

  .setup-value {
    display: grid;
    gap: 6px;
    border-top: 1px solid var(--line);
    padding-top: 10px;
  }

  .setup-value:first-child {
    border-top: 0;
    padding-top: 0;
  }

  .setup-value dt {
    margin: 0;
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--accent);
    letter-spacing: 0.02em;
  }

  .setup-value dd {
    margin: 0;
  }

  code {
    font-family: "IBM Plex Mono", "SFMono-Regular", ui-monospace, monospace;
    font-size: 0.86rem;
    color: var(--ink);
    overflow-wrap: anywhere;
  }

  .provider-head {
    display: flex;
    gap: 16px;
    justify-content: space-between;
    align-items: start;
  }

  .badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 8px 12px;
    border-radius: 999px;
    background: rgba(91, 109, 99, 0.12);
    color: var(--muted);
    white-space: nowrap;
  }

  .badge-on {
    background: rgba(26, 106, 114, 0.14);
    color: var(--accent);
  }

  .field-grid {
    margin-top: 16px;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .field {
    display: grid;
    gap: 8px;
  }

  .field-wide {
    grid-column: span 2;
  }

  .field-label {
    font-size: 0.82rem;
    font-weight: 600;
    color: var(--accent);
  }

  input {
    width: 100%;
    border-radius: 14px;
    border: 1px solid var(--line);
    background: rgba(255, 255, 255, 0.74);
    padding: 12px 14px;
    font: inherit;
    color: var(--ink);
  }

  .status-copy {
    margin-top: 14px;
  }

  .status-copy.error {
    color: #913f30;
  }

  .status-copy.warning {
    color: #8a5a16;
  }

  .status-copy.success {
    color: #246646;
  }

  .data-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px;
    margin-top: 16px;
  }

  .record-list {
    display: grid;
    gap: 10px;
    margin-top: 14px;
  }

  .record-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 12px;
    align-items: start;
    border-top: 1px solid var(--line);
    padding-top: 10px;
  }

  .record-row:first-child {
    border-top: 0;
    padding-top: 0;
  }

  .record-kind {
    text-transform: uppercase;
    letter-spacing: 0.08em;
    font-size: 12px;
    color: var(--accent);
  }

  .record-date {
    margin-top: 4px;
    color: var(--muted);
    line-height: 1.45;
  }

  .record-metrics {
    display: grid;
    gap: 4px;
    justify-items: end;
    text-align: right;
  }

  .record-metrics span {
    color: var(--ink);
  }

  .empty-copy {
    margin-top: 12px;
    color: var(--muted);
    line-height: 1.5;
  }

  @media (max-width: 960px) {
    .facts,
    .field-grid,
    .data-grid {
      grid-template-columns: 1fr;
    }

    .stack {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .setup-guide-grid {
      grid-template-columns: 1fr;
    }

    .field-wide {
      grid-column: span 1;
    }
  }

  @media (max-width: 720px) {
    .section-head,
    .provider-head,
    .actions,
    .setup-guide-head {
      flex-direction: column;
      align-items: flex-start;
    }

    .record-row {
      grid-template-columns: 1fr;
    }

    .record-metrics {
      justify-items: start;
      text-align: left;
    }

    .stack {
      grid-template-columns: 1fr;
    }

    .stack-row {
      padding: 0 0 12px;
      border-right: 0;
      border-bottom: 1px solid var(--line);
    }

    .stack-row:last-child {
      padding-bottom: 0;
      border-bottom: 0;
    }

    .panel {
      padding: 18px;
    }
  }
</style>
