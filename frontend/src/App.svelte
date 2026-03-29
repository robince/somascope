<script lang="ts">
  import { onMount } from "svelte";
  import DashboardView from "./lib/DashboardView.svelte";
  import SettingsView from "./lib/SettingsView.svelte";
  import { addDays, PERIODS, clampDate, getPeriod, isEditableTarget } from "./lib/time";
  import type {
    AppInfo,
    AppView,
    DashboardOverview,
    ExportFormat,
    OuraRecent,
    OuraStatus,
    PeriodId,
    ProviderSettings,
    SettingsPayload
  } from "./lib/types";

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
  let dashboard: DashboardOverview | null = null;
  let providers: ProviderSettings[] = [];
  let ouraStatus: OuraStatus | null = null;
  let ouraRecent: OuraRecent = { daily_records: [], sleep_sessions: [] };
  let userTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  let activeView: AppView = "dashboard";
  let activePeriod: PeriodId = "1m";
  let windowEndDate = "";
  let loading = true;
  let saving = false;
  let ouraBusy = false;
  let dirty = false;
  let error = "";
  let success = "";

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

  function syncViewFromLocation() {
    if (typeof window === "undefined") {
      return;
    }
    activeView = window.location.pathname === "/settings" || window.location.hash === "#settings" ? "settings" : "dashboard";
  }

  async function load() {
    loading = true;
    error = "";
    success = "";

    try {
      const [appRes, exportRes, settingsRes, dashboardRes, ouraStatusRes, ouraRecentRes] = await Promise.all([
        fetch("/api/v1/app"),
        fetch("/api/v1/export/formats"),
        fetch("/api/v1/settings"),
        fetch("/api/v1/dashboard/overview"),
        fetch("/api/v1/providers/oura/status"),
        fetch("/api/v1/providers/oura/recent")
      ]);

      if (!appRes.ok || !exportRes.ok || !settingsRes.ok || !dashboardRes.ok || !ouraStatusRes.ok || !ouraRecentRes.ok) {
        throw new Error("Failed to load dashboard and settings.");
      }

      appInfo = await appRes.json();
      const exportPayload = await exportRes.json();
      const settingsPayload: SettingsPayload = await settingsRes.json();
      const dashboardPayload: DashboardOverview = await dashboardRes.json();
      ouraStatus = await ouraStatusRes.json();
      ouraRecent = await ouraRecentRes.json();

      formats = exportPayload.items ?? [];
      dashboard = dashboardPayload;
      userTimezone = settingsPayload.user_timezone || userTimezone;
      providers = normalizeProviders(settingsPayload.providers);
      dirty = false;
      windowEndDate = clampDate(
        windowEndDate || dashboardPayload.latest_date || "",
        dashboardPayload.earliest_date,
        dashboardPayload.latest_date
      );
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  function setActiveView(view: AppView) {
    activeView = view;
    if (typeof window !== "undefined") {
      const nextPath = view === "settings" ? "/settings" : "/";
      window.history.pushState({}, "", nextPath);
    }
  }

  function updateProvider(index: number, field: keyof ProviderSettings, value: string | boolean) {
    providers = providers.map((provider, currentIndex) =>
      currentIndex === index ? ({ ...provider, [field]: value } as ProviderSettings) : provider
    );
    dirty = true;
    success = "";
  }

  function updateTimezone(value: string) {
    userTimezone = value;
    dirty = true;
    success = "";
  }

  function resetUnsaved() {
    void load();
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

  function selectPeriod(period: PeriodId) {
    activePeriod = period;
  }

  function shiftWindow(direction: -1 | 1) {
    if (!dashboard?.latest_date) {
      return;
    }

    const period = getPeriod(activePeriod);
    const nextEndDate = PERIODS.some((option) => option.id === activePeriod)
      ? clampDate(
          addDays(windowEndDate || dashboard.latest_date, direction * period.days),
          dashboard.earliest_date,
          dashboard.latest_date
        )
      : dashboard.latest_date;
    windowEndDate = nextEndDate;
  }

  function handleKeydown(event: KeyboardEvent) {
    if (activeView !== "dashboard" || isEditableTarget(event.target) || event.metaKey || event.ctrlKey || event.altKey) {
      return;
    }

    switch (event.key.toLowerCase()) {
      case "w":
        event.preventDefault();
        selectPeriod("1w");
        break;
      case "m":
        event.preventDefault();
        selectPeriod("1m");
        break;
      case "q":
        event.preventDefault();
        selectPeriod("3m");
        break;
      case "y":
        event.preventDefault();
        selectPeriod("1y");
        break;
      case "arrowleft":
        event.preventDefault();
        shiftWindow(-1);
        break;
      case "arrowright":
        event.preventDefault();
        shiftWindow(1);
        break;
    }
  }

  onMount(() => {
    syncViewFromLocation();
    void load();
  });
</script>

<svelte:head>
  <title>somascope</title>
</svelte:head>

<svelte:window onpopstate={syncViewFromLocation} onkeydown={handleKeydown} />

<main class="page-shell">
  <header class="topbar">
    <div class="brand">
      <p class="brand-mark">SOMASCOPE</p>
      <span>{dashboard?.providers.length ? `${dashboard.providers.join(", ")} synced` : "Local-first wearable dashboard"}</span>
    </div>

    <nav class="view-switch" aria-label="Primary views">
      <button class:active={activeView === "dashboard"} type="button" onclick={() => setActiveView("dashboard")}>Dashboard</button>
      <button class:active={activeView === "settings"} type="button" onclick={() => setActiveView("settings")}>Settings</button>
    </nav>
  </header>

  {#if activeView === "dashboard"}
    <DashboardView
      {appInfo}
      {dashboard}
      {formats}
      {activePeriod}
      {windowEndDate}
      {loading}
      {error}
      onSelectPeriod={selectPeriod}
      onShiftWindow={shiftWindow}
      onOpenSettings={() => setActiveView("settings")}
    />
  {:else}
    <SettingsView
      {appInfo}
      {formats}
      {providers}
      {ouraStatus}
      {ouraRecent}
      {userTimezone}
      {loading}
      {saving}
      {ouraBusy}
      {dirty}
      {error}
      {success}
      onReset={resetUnsaved}
      onSave={save}
      onRefresh={() => void load()}
      onConnectOura={() => void connectOura()}
      onSyncOura={() => void syncOura()}
      onTimezoneInput={updateTimezone}
      onProviderInput={updateProvider}
    />
  {/if}
</main>

<style>
  .page-shell {
    max-width: 1220px;
    margin: 0 auto;
    padding: 28px 20px 80px;
  }

  .topbar {
    display: flex;
    gap: 16px;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 22px;
  }

  .brand {
    display: grid;
    gap: 4px;
  }

  .brand-mark {
    margin: 0;
    font-size: 0.78rem;
    letter-spacing: 0.26em;
    text-transform: uppercase;
    color: var(--accent);
  }

  .brand span {
    color: var(--muted);
  }

  .view-switch {
    display: inline-flex;
    gap: 8px;
    padding: 6px;
    border-radius: 999px;
    border: 1px solid var(--line);
    background: rgba(255, 250, 240, 0.72);
    backdrop-filter: blur(12px);
  }

  .view-switch button {
    border: 0;
    background: transparent;
    color: var(--muted);
    font: inherit;
    border-radius: 999px;
    padding: 10px 16px;
    cursor: pointer;
  }

  .view-switch button.active {
    background: var(--accent);
    color: white;
  }

  @media (max-width: 720px) {
    .topbar {
      flex-direction: column;
      align-items: flex-start;
    }
  }
</style>
