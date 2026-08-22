<script lang="ts">
  import { onDestroy, onMount, tick } from "svelte";
  import DashboardView from "./lib/DashboardView.svelte";
  import SettingsView from "./lib/SettingsView.svelte";
  import { addDays, PERIODS, clampDate, getPeriod, isEditableTarget } from "./lib/time";
  import type {
    AppInfo,
    AppView,
    DashboardOverview,
    PeriodId,
    ProviderName,
    ProviderRecent,
    ProviderSettings,
    ProviderStatus,
    SettingsPayload
  } from "./lib/types";
  import { PROVIDER_NAMES, providerLabel } from "./lib/types";

  const PROVIDER_DEFAULTS: Record<ProviderSettings["provider"], Omit<ProviderSettings, "configured" | "client_secret">> = {
    google_health: {
      provider: "google_health",
      client_id: "",
      redirect_uri: "http://localhost:18080/oauth/google_health/callback",
      default_scopes: "https://www.googleapis.com/auth/googlehealth.activity_and_fitness.readonly https://www.googleapis.com/auth/googlehealth.sleep.readonly https://www.googleapis.com/auth/googlehealth.health_metrics_and_measurements.readonly https://www.googleapis.com/auth/googlehealth.nutrition.readonly https://www.googleapis.com/auth/googlehealth.profile.readonly https://www.googleapis.com/auth/googlehealth.settings.readonly https://www.googleapis.com/auth/googlehealth.ecg.readonly https://www.googleapis.com/auth/googlehealth.irn.readonly https://www.googleapis.com/auth/googlehealth.location.readonly",
      notes: "Bring your own Google Cloud OAuth client. Secrets stay local on this device."
    },
    oura: {
      provider: "oura",
      client_id: "",
      redirect_uri: "http://localhost:18080/oauth/oura/callback",
      default_scopes: "",
      notes: "Use your own Oura app credentials in v1; shared brokered mode comes later."
    }
  };

  let appInfo: AppInfo | null = null;
  let dashboard: DashboardOverview | null = null;
  let providers: ProviderSettings[] = [];
  let providerStatus: Partial<Record<ProviderName, ProviderStatus | null>> = {};
  let providerStatusErrors: Partial<Record<ProviderName, string>> = {};
  let providerRecent: Partial<Record<ProviderName, ProviderRecent>> = {};
  let providerBusy: Partial<Record<ProviderName, boolean>> = {};
  let userTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  let activeView: AppView = "dashboard";
  let activePeriod: PeriodId = "1m";
  let windowEndDate = "";
  let appInfoLoading = true;
  let providerSettingsLoading = true;
  let dashboardLoading = true;
  let statusLoading = true;
  let recentLoading = true;
  let settingsLoading = true;
  let saving = false;
  let anySyncBusy = false;
  let dirty = false;
  let appInfoError = "";
  let settingsError = "";
  let dashboardError = "";
  let statusError = "";
  let recentError = "";
  let actionError = "";
  let settingsViewError = "";
  let success = "";
  let syncStartDate = "";
  let ouraStatusPollTimer: ReturnType<typeof window.setInterval> | null = null;

  $: settingsLoading = appInfoLoading || providerSettingsLoading || statusLoading || recentLoading;
  $: settingsViewError = actionError || statusError || settingsError || recentError || appInfoError;

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

    for (const provider of ["google_health", "oura"] as const) {
      mapped.set(provider, baseProvider(provider));
    }

    for (const item of items ?? []) {
      if (item.provider !== "google_health" && item.provider !== "oura") {
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

    return (["google_health", "oura"] as const).map((provider) => mapped.get(provider)!);
  }

  function viewFromLocation(pathname: string, hash: string): AppView {
    const normalizedPath = pathname.replace(/\/+$/, "") || "/";
    return normalizedPath.endsWith("/settings") || hash === "#settings" ? "settings" : "dashboard";
  }

  function syncViewFromLocation() {
    if (typeof window === "undefined") {
      return;
    }
    activeView = viewFromLocation(window.location.pathname, window.location.hash);
  }

  function consumeOAuthResult() {
    if (typeof window === "undefined") {
      return;
    }

    const url = new URL(window.location.href);
    const provider = url.searchParams.get("oauth_provider");
    const status = url.searchParams.get("oauth_status");

    if ((provider === "oura" || provider === "google_health") && status === "connected") {
      activeView = "settings";
      success = `${providerLabel(provider)} connected locally.`;
      url.searchParams.delete("oauth_provider");
      url.searchParams.delete("oauth_status");
      window.history.replaceState({}, "", `${url.pathname}${url.search}${url.hash}`);
    }
  }

  function messageForError(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  async function fetchJSON<T>(input: RequestInfo | URL, fallbackMessage: string): Promise<T> {
    const response = await fetch(input);
    if (!response.ok) {
      const payload = await response.json().catch(() => null);
      throw new Error(payload?.error || fallbackMessage);
    }
    return (await response.json()) as T;
  }

  function syncBusyFromStatus(status: ProviderStatus | null | undefined) {
    return status?.current_run?.status === "running";
  }

  function stopStatusPolling() {
    if (ouraStatusPollTimer !== null) {
      window.clearInterval(ouraStatusPollTimer);
      ouraStatusPollTimer = null;
    }
  }

  function startStatusPolling() {
    if (typeof window === "undefined" || ouraStatusPollTimer !== null) {
      return;
    }
    ouraStatusPollTimer = window.setInterval(() => {
      void loadAllProviderStatus();
    }, 2000);
  }

  function applyProviderStatus(status: ProviderStatus) {
    const provider = status.provider as ProviderName;
    const wasBusy = Boolean(providerBusy[provider]);
    providerStatus = { ...providerStatus, [provider]: status };
    statusError = "";
    providerBusy = { ...providerBusy, [provider]: syncBusyFromStatus(status) };
    anySyncBusy = Object.values(providerBusy).some(Boolean);
    if (anySyncBusy) {
      startStatusPolling();
    } else {
      stopStatusPolling();
      if (wasBusy) {
        void refreshPostRunData();
      }
    }
  }

  async function loadAppInfo() {
    appInfoLoading = true;
    appInfoError = "";
    try {
      appInfo = await fetchJSON<AppInfo>("/api/v1/app", "Failed to load app info.");
    } catch (err) {
      appInfoError = messageForError(err);
    } finally {
      appInfoLoading = false;
    }
  }

  async function loadSettingsData() {
    providerSettingsLoading = true;
    settingsError = "";
    try {
      const payload = await fetchJSON<SettingsPayload>("/api/v1/settings", "Failed to load local settings.");
      userTimezone = payload.user_timezone || userTimezone;
      providers = normalizeProviders(payload.providers);
      dirty = false;
    } catch (err) {
      settingsError = messageForError(err);
      providers = normalizeProviders(undefined);
    } finally {
      providerSettingsLoading = false;
    }
  }

  async function loadDashboardData() {
    dashboardLoading = true;
    dashboardError = "";
    try {
      const payload = await fetchJSON<DashboardOverview>("/api/v1/dashboard/overview", "Failed to load dashboard.");
      dashboard = payload;
      windowEndDate = clampDate(
        windowEndDate || payload.latest_date || "",
        payload.earliest_date,
        payload.latest_date
      );
    } catch (err) {
      dashboardError = messageForError(err);
      dashboard = null;
    } finally {
      dashboardLoading = false;
    }
  }

  async function loadProviderStatus(provider: ProviderName) {
    const payload = await fetchJSON<ProviderStatus>(
      `/api/v1/providers/${provider}/status`,
      `Failed to refresh ${providerLabel(provider)} sync status.`
    );
    applyProviderStatus(payload);
  }

  async function loadAllProviderStatus() {
    statusLoading = true;
    statusError = "";
    const results = await Promise.allSettled(
      PROVIDER_NAMES.map(async (provider) => {
        const status = await fetchJSON<ProviderStatus>(
          `/api/v1/providers/${provider}/status`,
          `Failed to refresh ${providerLabel(provider)} sync status.`
        );
        return [provider, status] as const;
      })
    );
    const failures: Partial<Record<ProviderName, string>> = {};
    for (const [index, result] of results.entries()) {
      const provider = PROVIDER_NAMES[index];
      if (result.status === "fulfilled") {
        applyProviderStatus(result.value[1]);
      } else {
        failures[provider] = messageForError(result.reason);
      }
    }
    providerStatusErrors = failures;
    statusError = Object.values(failures).join(" ");
    if (Object.keys(failures).length === PROVIDER_NAMES.length) {
      stopStatusPolling();
    }
    statusLoading = false;
  }

  async function loadAllProviderRecent() {
    recentLoading = true;
    recentError = "";
    try {
      const results = await Promise.allSettled(
        PROVIDER_NAMES.map(async (provider) => {
          const payload = await fetchJSON<ProviderRecent>(
            `/api/v1/providers/${provider}/recent`,
            `Failed to load recent ${providerLabel(provider)} data.`
          );
          return [provider, payload] as const;
        })
      );
      const next: Partial<Record<ProviderName, ProviderRecent>> = {};
      const failures: string[] = [];
      for (const result of results) {
        if (result.status === "fulfilled") {
          next[result.value[0]] = result.value[1];
        } else {
          failures.push(messageForError(result.reason));
        }
      }
      providerRecent = next;
      recentError = failures.join(" ");
    } catch (err) {
      recentError = messageForError(err);
    } finally {
      recentLoading = false;
    }
  }

  async function loadInitialData() {
    actionError = "";
    await Promise.all([
      loadAppInfo(),
      loadSettingsData(),
      loadDashboardData(),
      loadAllProviderStatus(),
      loadAllProviderRecent()
    ]);
  }

  async function refreshPostRunData() {
    await Promise.all([loadDashboardData(), loadAllProviderRecent(), loadAllProviderStatus()]);
  }

  async function scrollToAnchor(anchor: string) {
    await tick();
    document.getElementById(anchor)?.scrollIntoView({
      behavior: "smooth",
      block: "start"
    });
  }

  function setActiveView(view: AppView, anchor = "") {
    activeView = view;

    if (typeof window !== "undefined") {
      const nextPath = view === "settings" ? "/settings" : "/";
      const nextUrl = anchor ? `${nextPath}#${anchor}` : nextPath;
      window.history.pushState({ view }, "", nextUrl);
    }

    if (anchor) {
      void scrollToAnchor(anchor);
    }
  }

  function updateProvider(index: number, field: keyof ProviderSettings, value: string | boolean) {
    providers = providers.map((provider, currentIndex) =>
      currentIndex === index ? ({ ...provider, [field]: value } as ProviderSettings) : provider
    );
    dirty = true;
    success = "";
    actionError = "";
  }

  function updateTimezone(value: string) {
    userTimezone = value;
    dirty = true;
    success = "";
    actionError = "";
  }

  function resetUnsaved() {
    void loadSettingsData();
  }

  async function save() {
    saving = true;
    actionError = "";
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
        const payload = await response.json().catch(() => null);
        throw new Error(payload?.error || "Failed to save local settings.");
      }

      await Promise.all([loadSettingsData(), loadAllProviderStatus()]);
      success = "Saved locally. Secrets are not re-displayed after write.";
    } catch (err) {
      actionError = messageForError(err);
    } finally {
      saving = false;
    }
  }

  async function connectProvider(provider: ProviderName) {
    providerBusy = { ...providerBusy, [provider]: true };
    anySyncBusy = true;
    actionError = "";
    success = "";

    try {
      const response = await fetch(`/api/v1/providers/${provider}/auth/start`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({
          return_to: window.location.href
        })
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => null);
        throw new Error(payload?.error || `Failed to start ${providerLabel(provider)} authorization.`);
      }
      const payload = await response.json();
      if (!payload.authorize_url) {
        throw new Error(`Missing ${providerLabel(provider)} authorize URL.`);
      }
      window.location.href = payload.authorize_url;
    } catch (err) {
      actionError = messageForError(err);
      providerBusy = { ...providerBusy, [provider]: false };
      anySyncBusy = Object.values(providerBusy).some(Boolean);
    }
  }

  async function syncProvider(provider: ProviderName, options?: { startDate?: string; modeLabel?: string }) {
    actionError = "";
    success = "";

    try {
      const body: Record<string, string> = {};
      if (options?.startDate) {
        body.start_date = options.startDate;
      }

      const response = await fetch(`/api/v1/providers/${provider}/sync`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify(body)
      });

      const payload = await response.json().catch(() => null);
      if (response.status === 409) {
        if (payload?.current_run) {
          applyProviderStatus({
            ...(providerStatus[provider] ?? {
              provider,
              configured: true,
              connected: true,
              status: "connected",
              daily_record_count: 0,
              sleep_session_count: 0
            }),
            current_run: payload.current_run
          });
        }
        success = `${providerLabel(provider)} sync is already running in the local app.`;
        return;
      }

      if (!response.ok) {
        throw new Error(payload?.error || `Failed to sync ${providerLabel(provider)} data.`);
      }

      if (payload?.run) {
        applyProviderStatus({
          ...(providerStatus[provider] ?? {
            provider,
            configured: true,
            connected: true,
            status: "connected",
            daily_record_count: 0,
            sleep_session_count: 0
          }),
          current_run: payload.run
        });
      } else {
        providerBusy = { ...providerBusy, [provider]: true };
        anySyncBusy = true;
        startStatusPolling();
      }

      const modeLabel = options?.modeLabel ?? (payload?.run?.mode === "backfill" ? "Backfill" : "Update");
      success = `${modeLabel} started for ${providerLabel(provider)}. The local app will keep syncing even if you refresh this page.`;
    } catch (err) {
      actionError = messageForError(err);
    }
  }

  async function syncAllIncremental() {
    actionError = "";
    success = "";

    try {
      const response = await fetch("/api/v1/sync", {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({})
      });
      const payload = await response.json().catch(() => null);
      if (response.status === 409) {
        success = "All connected providers are already syncing.";
        await loadAllProviderStatus();
        return;
      }
      if (!response.ok) {
        throw new Error(payload?.error || "Failed to start provider sync.");
      }

      for (const item of payload?.started ?? []) {
        if (item.provider && item.run) {
          applyProviderStatus({
            ...(providerStatus[item.provider as ProviderName] ?? {
              provider: item.provider,
              configured: true,
              connected: true,
              status: "connected",
              daily_record_count: 0,
              sleep_session_count: 0
            }),
            current_run: item.run
          });
        }
      }
      success = "Update started for connected providers.";
    } catch (err) {
      actionError = messageForError(err);
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
    consumeOAuthResult();
    void loadInitialData();
  });

  onDestroy(() => {
    stopStatusPolling();
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
      <span>Your local wearable health data store</span>
    </div>

    <nav class="view-switch" aria-label="Primary views">
      <button class:active={activeView === "dashboard"} type="button" onclick={() => setActiveView("dashboard")}>Dashboard</button>
      <button class:active={activeView === "settings"} type="button" onclick={() => setActiveView("settings")}>Settings</button>
    </nav>
  </header>

  <section hidden={activeView !== "dashboard"} aria-hidden={activeView !== "dashboard"}>
    <DashboardView
      {dashboard}
      {activePeriod}
      {windowEndDate}
      loading={dashboardLoading}
      busy={anySyncBusy}
      statuses={providerStatus}
      error={dashboardError}
      onSelectPeriod={selectPeriod}
      onShiftWindow={shiftWindow}
      onSyncIncremental={() => void syncAllIncremental()}
    />
  </section>

  <section hidden={activeView !== "settings"} aria-hidden={activeView !== "settings"}>
    <SettingsView
      {appInfo}
      {providers}
      statuses={providerStatus}
      recents={providerRecent}
      busy={providerBusy}
      {userTimezone}
      loading={settingsLoading}
      {statusLoading}
      statusError={statusError}
      statusErrors={providerStatusErrors}
      {saving}
      {syncStartDate}
      {dirty}
      error={settingsViewError}
      {success}
      onReset={resetUnsaved}
      onSave={save}
      onRefresh={() => void Promise.all([loadAllProviderStatus(), loadAllProviderRecent()])}
      onConnect={(provider: ProviderName) => void connectProvider(provider)}
      onSync={(provider: ProviderName) => void syncProvider(provider, { modeLabel: "Update" })}
      onSyncFromDate={(provider: ProviderName) => {
        if (!syncStartDate) {
          actionError = "Choose a backfill start date first.";
          success = "";
          return;
        }
        void syncProvider(provider, { startDate: syncStartDate, modeLabel: "Backfill" });
      }}
      onSyncStartDateInput={(value: string) => {
        syncStartDate = value;
        actionError = "";
        success = "";
      }}
      onTimezoneInput={updateTimezone}
      onProviderInput={updateProvider}
    />
  </section>
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
    align-items: flex-end;
    justify-content: space-between;
    margin-bottom: 22px;
  }

  .brand {
    display: grid;
    gap: 4px;
    min-width: 0;
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
    .page-shell {
      padding-inline: 16px;
    }

    .topbar {
      flex-direction: column;
      align-items: stretch;
    }

    .view-switch {
      align-self: center;
    }
  }
</style>
