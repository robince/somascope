<script lang="ts">
  import { onMount } from "svelte";

  type AppInfo = {
    name: string;
    auth_mode: string;
    data_dir: string;
    db_path: string;
    schema_version: number;
    version?: {
      version?: string;
      commit?: string;
      build_date?: string;
    };
  };

  type ExportFormat = {
    id: string;
    label: string;
    description: string;
    status: string;
  };

  type OuraStatus = {
    provider: string;
    configured: boolean;
    connected: boolean;
    status: string;
    scope?: string;
    connected_at?: string;
    token_expires_at?: string;
    last_sync_at?: string;
    daily_record_count: number;
    sleep_session_count: number;
  };

  type DailyRecord = {
    provider: string;
    record_kind: string;
    local_date: string;
    zone_offset?: string;
    source_device?: string;
    external_id?: string;
    summary: Record<string, unknown>;
    raw_document_id?: number;
  };

  type SleepSession = {
    provider: string;
    local_date: string;
    zone_offset?: string;
    external_id?: string;
    start_time: string;
    end_time: string;
    duration_minutes?: number;
    time_in_bed_minutes?: number;
    efficiency_percent?: number;
    is_nap?: boolean;
    stages?: Record<string, number>;
    metrics?: Record<string, unknown>;
    raw_document_id?: number;
  };

  type OuraRecent = {
    daily_records: DailyRecord[];
    sleep_sessions: SleepSession[];
  };

  type ProviderSettings = {
    provider: "fitbit" | "oura";
    configured: boolean;
    client_id: string;
    client_secret: string;
    redirect_uri: string;
    default_scopes: string;
    notes: string;
  };

  type SettingsPayload = {
    user_timezone: string;
    providers: Array<{
      provider: "fitbit" | "oura";
      configured?: boolean;
      client_id?: string;
      client_secret?: string;
      redirect_uri?: string;
      default_scopes?: string;
      notes?: string;
    }>;
  };

  const PROVIDER_DEFAULTS: Record<ProviderSettings["provider"], Omit<ProviderSettings, "configured" | "client_secret">> = {
    fitbit: {
      provider: "fitbit",
      client_id: "",
      redirect_uri: "http://localhost:18080/oauth/fitbit/callback",
      default_scopes: "activity heartrate sleep profile",
      notes: "Best for development and single-user local setups."
    },
    oura: {
      provider: "oura",
      client_id: "",
      redirect_uri: "http://localhost:18080/oauth/oura/callback",
      default_scopes: "email personal daily heartrate tag workout session spo2",
      notes: "Use your own Oura app credentials in v1; shared brokered mode comes later."
    }
  };

  let appInfo: AppInfo | null = null;
  let formats: ExportFormat[] = [];
  let providers: ProviderSettings[] = [];
  let userTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  let loading = true;
  let saving = false;
  let ouraBusy = false;
  let dirty = false;
  let error = "";
  let success = "";
  let ouraStatus: OuraStatus | null = null;
  let ouraRecent: OuraRecent = { daily_records: [], sleep_sessions: [] };

  function baseProvider(provider: ProviderSettings["provider"]): ProviderSettings {
    const defaults = PROVIDER_DEFAULTS[provider];
    return {
      ...defaults,
      configured: false,
      client_secret: ""
    };
  }

  function normalizeProviders(items: SettingsPayload["providers"] | undefined): ProviderSettings[] {
    const mapped = new Map<ProviderSettings["provider"], ProviderSettings>();

    for (const provider of ["fitbit", "oura"] as const) {
      mapped.set(provider, baseProvider(provider));
    }

    for (const item of items ?? []) {
      if (item.provider !== "fitbit" && item.provider !== "oura") {
        continue;
      }
      mapped.set(item.provider, {
        ...baseProvider(item.provider),
        configured: Boolean(item.configured),
        client_id: item.client_id ?? "",
        client_secret: "",
        redirect_uri: item.redirect_uri || PROVIDER_DEFAULTS[item.provider].redirect_uri,
        default_scopes: item.default_scopes || PROVIDER_DEFAULTS[item.provider].default_scopes,
        notes: item.notes || PROVIDER_DEFAULTS[item.provider].notes
      });
    }

    return (["fitbit", "oura"] as const).map((provider) => mapped.get(provider)!);
  }

  async function load() {
    loading = true;
    error = "";
    success = "";

    try {
      const [appRes, exportRes, settingsRes, ouraStatusRes, ouraRecentRes] = await Promise.all([
        fetch("/api/v1/app"),
        fetch("/api/v1/export/formats"),
        fetch("/api/v1/settings"),
        fetch("/api/v1/providers/oura/status"),
        fetch("/api/v1/providers/oura/recent")
      ]);

      if (!appRes.ok || !exportRes.ok || !settingsRes.ok || !ouraStatusRes.ok || !ouraRecentRes.ok) {
        throw new Error("Failed to load application settings.");
      }

      appInfo = await appRes.json();
      const exportPayload = await exportRes.json();
      const settingsPayload: SettingsPayload = await settingsRes.json();
      ouraStatus = await ouraStatusRes.json();
      ouraRecent = await ouraRecentRes.json();

      formats = exportPayload.items ?? [];
      userTimezone = settingsPayload.user_timezone || userTimezone;
      providers = normalizeProviders(settingsPayload.providers);
      dirty = false;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  function numberValue(value: unknown): string {
    if (typeof value === "number") {
      return Number.isInteger(value) ? `${value}` : value.toFixed(2);
    }
    if (typeof value === "string") {
      return value;
    }
    return "—";
  }

  function metricValue(summary: Record<string, unknown>, key: string): string {
    return numberValue(summary[key]);
  }

  function timeLabel(value: string): string {
    if (!value) {
      return "—";
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

  function updateProvider(index: number, field: keyof ProviderSettings, value: string | boolean) {
    providers = providers.map((provider, currentIndex) =>
      currentIndex === index ? ({ ...provider, [field]: value } as ProviderSettings) : provider
    );
    dirty = true;
    success = "";
  }

  function resetUnsaved() {
    load();
  }

  async function save() {
    saving = true;
    error = "";
    success = "";

    try {
      const response = await fetch("/api/v1/settings", {
        method: "PUT",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({
          user_timezone: userTimezone,
          providers: providers.map((provider) => ({
            provider: provider.provider,
            configured: provider.configured,
            client_id: provider.client_id,
            client_secret: provider.client_secret,
            redirect_uri: provider.redirect_uri,
            default_scopes: provider.default_scopes,
            notes: provider.notes
          }))
        })
      });

      if (!response.ok) {
        throw new Error("Failed to save local settings.");
      }

      await load();
      success = "Saved locally. Secrets are not re-displayed after write.";
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  async function connectOura() {
    ouraBusy = true;
    error = "";
    success = "";

    try {
      const response = await fetch("/api/v1/providers/oura/auth/start", {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({
          return_to: window.location.href
        })
      });
      if (!response.ok) {
        throw new Error("Failed to start Oura authorization.");
      }
      const payload = await response.json();
      if (!payload.authorize_url) {
        throw new Error("Missing Oura authorize URL.");
      }
      window.location.href = payload.authorize_url;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      ouraBusy = false;
    }
  }

  async function syncOura() {
    ouraBusy = true;
    error = "";
    success = "";

    try {
      const response = await fetch("/api/v1/providers/oura/sync", {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({})
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => null);
        throw new Error(payload?.error || "Failed to sync Oura data.");
      }
      const payload = await response.json();
      ouraStatus = payload.overview ?? ouraStatus;
      success = `Oura sync complete: ${payload.sync.daily_activity_rows} activity, ${payload.sync.daily_readiness_rows} readiness, ${payload.sync.sleep_rows} sleep rows.`;
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      ouraBusy = false;
    }
  }

  onMount(load);
</script>

<svelte:head>
  <title>somascope</title>
</svelte:head>

<main class="page">
  <section class="hero">
    <div class="intro panel">
      <p class="eyebrow">Somascope</p>
      <h1>Configure local access to your wearable data.</h1>
      <p class="lede">
        V1 uses your own Fitbit and Oura app credentials. The goal is simple local setup,
        local storage, and explicit export without making a hosted auth broker part of the
        security boundary yet.
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
          <strong>Timezone</strong>
          <span>{userTimezone}</span>
        </article>
      </div>
    </div>

    <aside class="panel side-panel">
      <p class="eyebrow">Oura Status</p>
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
  </section>

  <section class="panel settings-panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Provider Credentials</p>
        <h2>Bring your own app settings</h2>
      </div>
      <div class="actions">
        <button class="button button-ghost" type="button" onclick={resetUnsaved} disabled={loading || saving || !dirty}>
          Reset
        </button>
        <button class="button button-primary" type="button" onclick={save} disabled={loading || saving}>
          {saving ? "Saving..." : "Save local settings"}
        </button>
      </div>
    </div>

    <p class="helper">
      Secrets are only entered when you want to write them. After save, somascope treats them
      as present locally but does not render them back into the page. For Oura, the next step is
      browser authorization against your local callback and then a manual sync.
    </p>

    <div class="timezone-row">
      <label class="field">
        <span class="field-label">User timezone</span>
        <input
          type="text"
          value={userTimezone}
          oninput={(event) => {
            userTimezone = (event.currentTarget as HTMLInputElement).value;
            dirty = true;
            success = "";
          }}
          placeholder="Europe/Paris"
        />
      </label>
    </div>

    {#if loading}
      <p class="status-copy">Loading settings…</p>
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
                  oninput={(event) => updateProvider(index, "client_id", (event.currentTarget as HTMLInputElement).value)}
                  placeholder="Paste your provider app client ID"
                />
              </label>

              <label class="field field-wide">
                <span class="field-label">Client secret</span>
                <input
                  type="password"
                  value={provider.client_secret}
                  oninput={(event) => updateProvider(index, "client_secret", (event.currentTarget as HTMLInputElement).value)}
                  placeholder={provider.configured ? "Stored locally; enter a new value to replace it" : "Paste your provider app client secret"}
                />
              </label>

              <label class="field field-wide">
                <span class="field-label">Redirect URI</span>
                <input
                  type="text"
                  value={provider.redirect_uri}
                  oninput={(event) => updateProvider(index, "redirect_uri", (event.currentTarget as HTMLInputElement).value)}
                />
              </label>

              <label class="field field-wide">
                <span class="field-label">Default scopes</span>
                <input
                  type="text"
                  value={provider.default_scopes}
                  oninput={(event) => updateProvider(index, "default_scopes", (event.currentTarget as HTMLInputElement).value)}
                />
              </label>
            </div>

            <p class="provider-notes">{provider.notes}</p>

            {#if provider.provider === "oura"}
              <div class="provider-actions">
                <button
                  class="button button-ghost"
                  type="button"
                  onclick={load}
                  disabled={loading || saving || ouraBusy}
                >
                  Refresh status
                </button>
                <button
                  class="button button-ghost"
                  type="button"
                  onclick={connectOura}
                  disabled={loading || saving || ouraBusy || !provider.configured}
                >
                  {ouraBusy ? "Working..." : "Connect Oura"}
                </button>
                <button
                  class="button button-primary"
                  type="button"
                  onclick={syncOura}
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
  </section>

  <section class="panel exports-panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Exports</p>
        <h2>Keep raw and canonical escape hatches visible</h2>
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
  </section>

  <section class="panel data-panel">
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
                  <span>{session.duration_minutes ?? "—"} min asleep</span>
                  <span>{session.efficiency_percent ?? "—"}% eff</span>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </article>
    </div>
  </section>
</main>

<style>
  .page {
    max-width: 1100px;
    margin: 0 auto;
    padding: 40px 22px 80px;
  }

  .hero {
    display: grid;
    grid-template-columns: 1.35fr 0.95fr;
    gap: 18px;
  }

  .panel {
    border: 1px solid var(--line);
    border-radius: 20px;
    padding: 20px;
    background: var(--panel);
    backdrop-filter: blur(12px);
    box-shadow: 0 12px 32px rgba(24, 32, 25, 0.06);
  }

  .eyebrow {
    margin: 0 0 10px;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.16em;
    font-size: 12px;
  }

  h1 {
    margin: 0;
    max-width: 12ch;
    font-size: clamp(1.9rem, 4.1vw, 3rem);
    line-height: 0.92;
  }

  .lede {
    margin-top: 14px;
    color: var(--muted);
    max-width: 46rem;
    line-height: 1.55;
  }

  .facts,
  .formats,
  .provider-grid,
  .field-grid,
  .stack {
    display: grid;
    gap: 12px;
  }

  .facts {
    grid-template-columns: repeat(3, 1fr);
    margin-top: 18px;
  }

  article {
    border: 1px solid var(--line);
    border-radius: 14px;
    background: rgba(255, 255, 255, 0.45);
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
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  h2,
  h3 {
    margin: 0;
  }

  h2 {
    font-size: 1.6rem;
  }

  h3 {
    font-size: 1.2rem;
  }

  p {
    margin: 0;
  }

  .side-panel {
    align-self: stretch;
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
    color: var(--muted);
    line-height: 1.5;
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

  .actions {
    display: flex;
    gap: 10px;
  }

  .provider-notes {
    margin-top: 14px;
    color: var(--muted);
    line-height: 1.55;
  }

  .provider-actions {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
    margin-top: 16px;
  }

  .button {
    border-radius: 999px;
    border: 1px solid var(--line);
    padding: 12px 18px;
    cursor: pointer;
    transition: 160ms ease;
  }

  .button:disabled {
    opacity: 0.55;
    cursor: default;
  }

  .button-primary {
    background: var(--accent);
    color: white;
    border-color: transparent;
  }

  .button-ghost {
    background: rgba(255, 255, 255, 0.4);
    color: var(--ink);
  }

  .helper,
  .status-copy {
    margin-top: 14px;
    color: var(--muted);
    line-height: 1.55;
  }

  .success {
    color: #246c4d;
  }

  .error {
    color: #8b2d1f;
  }

  .timezone-row {
    margin-top: 18px;
  }

  .field-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    margin-top: 18px;
  }

  .field,
  .field-wide {
    display: grid;
    gap: 8px;
  }

  .field-wide {
    grid-column: 1 / -1;
  }

  .field-label {
    font-size: 12px;
    color: var(--accent);
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  input {
    width: 100%;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: rgba(255, 255, 255, 0.55);
    padding: 12px 14px;
    color: var(--ink);
    font: inherit;
  }

  input:focus {
    outline: 2px solid rgba(26, 106, 114, 0.16);
    border-color: rgba(26, 106, 114, 0.35);
  }

  .provider-grid {
    margin-top: 18px;
  }

  .provider-card {
    padding: 18px;
    background: rgba(255, 255, 255, 0.35);
  }

  .provider-head {
    display: flex;
    gap: 12px;
    align-items: start;
    justify-content: space-between;
  }

  .provider-head p {
    margin-top: 6px;
    color: var(--muted);
    line-height: 1.45;
  }

  .badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 112px;
    border-radius: 999px;
    border: 1px solid var(--line);
    padding: 8px 12px;
    background: rgba(255, 255, 255, 0.5);
    color: var(--muted);
    font-size: 12px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .badge-on {
    background: rgba(26, 106, 114, 0.12);
    border-color: rgba(26, 106, 114, 0.18);
    color: var(--accent);
  }

  .formats {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    margin-top: 18px;
  }

  .data-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px;
    margin-top: 18px;
  }

  .data-card {
    padding: 18px;
    background: rgba(255, 255, 255, 0.35);
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

  .format small {
    display: block;
    margin-top: 10px;
    color: var(--accent);
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  @media (max-width: 900px) {
    .hero,
    .facts,
    .formats,
    .data-grid,
    .field-grid,
    .section-head {
      grid-template-columns: 1fr;
    }

    .section-head {
      align-items: start;
    }

    .actions,
    .provider-head {
      flex-direction: column;
      align-items: stretch;
    }

    .record-row {
      grid-template-columns: 1fr;
    }

    .record-metrics {
      justify-items: start;
      text-align: left;
    }
  }
</style>
