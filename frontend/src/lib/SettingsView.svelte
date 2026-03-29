<script lang="ts">
  import type { AppInfo, ExportFormat, OuraRecent, OuraStatus, ProviderSettings } from "./types";

  export let appInfo: AppInfo | null = null;
  export let formats: ExportFormat[] = [];
  export let providers: ProviderSettings[] = [];
  export let ouraStatus: OuraStatus | null = null;
  export let ouraRecent: OuraRecent = { daily_records: [], sleep_sessions: [] };
  export let userTimezone = "";
  export let loading = false;
  export let saving = false;
  export let ouraBusy = false;
  export let dirty = false;
  export let error = "";
  export let success = "";
  export let onReset: () => void = () => {};
  export let onSave: () => void = () => {};
  export let onRefresh: () => void = () => {};
  export let onConnectOura: () => void = () => {};
  export let onSyncOura: () => void = () => {};
  export let onTimezoneInput: (value: string) => void = () => {};
  export let onProviderInput: (index: number, field: keyof ProviderSettings, value: string | boolean) => void = () => {};

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
</script>

<section class="settings-shell">
  <div class="hero-grid">
    <article class="panel intro-panel">
      <p class="eyebrow">Settings</p>
      <h1>Local OAuth and storage controls.</h1>
      <p class="lede">
        The dashboard is now the default face of the app. Settings stays where connection state,
        credentials, sync controls, and export boundaries remain explicit.
      </p>

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
          <dd>{ouraStatus?.connected ? "Connected" : "Not connected"}</dd>
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
      </dl>
    </aside>
  </div>

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
                <span class="field-label">Redirect URI</span>
                <input
                  type="text"
                  value={provider.redirect_uri}
                  oninput={(event) => onProviderInput(index, "redirect_uri", (event.currentTarget as HTMLInputElement).value)}
                />
              </label>

              <label class="field field-wide">
                <span class="field-label">Default scopes</span>
                <input
                  type="text"
                  value={provider.default_scopes}
                  oninput={(event) => onProviderInput(index, "default_scopes", (event.currentTarget as HTMLInputElement).value)}
                />
              </label>

              <label class="field field-wide">
                <span class="field-label">Notes</span>
                <textarea
                  rows="3"
                  oninput={(event) => onProviderInput(index, "notes", (event.currentTarget as HTMLTextAreaElement).value)}
                >{provider.notes}</textarea>
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
                  disabled={loading || saving || ouraBusy || !ouraStatus?.connected}
                >
                  {ouraBusy ? "Working..." : "Sync last 30 days"}
                </button>
              </div>
            {/if}
          </article>
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

  <article class="panel exports-panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Export roadmap</p>
        <h2>Keep escape hatches explicit</h2>
      </div>
    </div>

    <div class="formats">
      {#each formats as format}
        <article class="format">
          <strong>{format.label}</strong>
          <span>{format.description}</span>
          <small>{format.status}</small>
        </article>
      {/each}
    </div>
  </article>

  <article class="panel data-panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Recent Oura Data</p>
        <h2>See what the sync actually pulled</h2>
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
  .formats,
  .provider-grid,
  .field-grid,
  .stack {
    display: grid;
    gap: 12px;
  }

  .hero-grid {
    display: grid;
    grid-template-columns: 1.3fr 0.95fr;
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
    max-width: 13ch;
    font-size: clamp(1.9rem, 4.1vw, 3rem);
    line-height: 0.95;
  }

  h2 {
    font-size: 1.55rem;
  }

  h3 {
    font-size: 1.2rem;
  }

  .lede,
  .helper,
  .status-copy,
  .provider-head p,
  .stack-row dd,
  .format span {
    color: var(--muted);
    line-height: 1.55;
  }

  .lede {
    margin-top: 14px;
    max-width: 46rem;
  }

  .facts {
    grid-template-columns: repeat(3, 1fr);
    margin-top: 20px;
  }

  .facts article,
  .format,
  .provider-card,
  .data-card {
    border: 1px solid var(--line);
    border-radius: 16px;
    background: rgba(255, 255, 255, 0.56);
    padding: 14px;
  }

  strong {
    display: block;
    margin-bottom: 8px;
    font-size: 13px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  span {
    color: var(--muted);
    overflow-wrap: anywhere;
  }

  .stack {
    margin-top: 14px;
  }

  .stack-row {
    display: grid;
    gap: 4px;
    padding: 12px 0;
    border-bottom: 1px solid var(--line);
  }

  .stack-row:last-child {
    border-bottom: 0;
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
  .exports-panel,
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
    font-size: 13px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--accent);
  }

  input,
  textarea {
    width: 100%;
    border-radius: 14px;
    border: 1px solid var(--line);
    background: rgba(255, 255, 255, 0.74);
    padding: 12px 14px;
    font: inherit;
    color: var(--ink);
  }

  textarea {
    resize: vertical;
  }

  .status-copy {
    margin-top: 14px;
  }

  .status-copy.error {
    color: #913f30;
  }

  .status-copy.success {
    color: #246646;
  }

  .formats {
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    margin-top: 16px;
  }

  .format small {
    display: inline-block;
    margin-top: 10px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
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
    .hero-grid,
    .facts,
    .field-grid,
    .data-grid {
      grid-template-columns: 1fr;
    }

    .field-wide {
      grid-column: span 1;
    }
  }

  @media (max-width: 720px) {
    .section-head,
    .provider-head,
    .actions {
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

    .panel {
      padding: 18px;
    }
  }
</style>
