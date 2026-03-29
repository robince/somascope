export type AppInfo = {
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

export type ExportFormat = {
  id: string;
  label: string;
  description: string;
  status: string;
};

export type ProviderName = "fitbit" | "oura";

export type ProviderSettings = {
  provider: ProviderName;
  configured: boolean;
  client_id: string;
  client_secret: string;
  redirect_uri: string;
  default_scopes: string;
  notes: string;
};

export type SettingsPayload = {
  user_timezone: string;
  providers: Array<{
    provider: ProviderName;
    configured?: boolean;
    client_id?: string;
    client_secret?: string;
    redirect_uri?: string;
    default_scopes?: string;
    notes?: string;
  }>;
};

export type OuraStatus = {
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
  sync_state?: SyncStateEntry[];
  last_sync?: OuraLastSync | null;
};

export type SyncStateEntry = {
  provider: string;
  entity_kind: string;
  cursor_value: string;
  synced_at?: string;
};

export type OuraLastSyncEntity = {
  entity: string;
  start_date: string;
  end_date: string;
  cursor: string;
  rows: number;
  chunk_count: number;
};

export type OuraLastSync = {
  fetched_at: string;
  mode: string;
  start_date: string;
  end_date: string;
  daily_activity_rows: number;
  daily_readiness_rows: number;
  sleep_rows: number;
  entities: OuraLastSyncEntity[];
};

export type DailyRecord = {
  provider: string;
  record_kind: string;
  local_date: string;
  zone_offset?: string;
  source_device?: string;
  external_id?: string;
  summary: Record<string, unknown>;
  raw_document_id?: number;
};

export type RecentSleepSession = {
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

export type OuraRecent = {
  daily_records: DailyRecord[];
  sleep_sessions: RecentSleepSession[];
};

export type DashboardActivity = {
  score?: number;
  steps?: number;
  active_calories?: number;
  total_calories?: number;
  equivalent_walking_distance?: number;
  high_activity_minutes?: number;
  medium_activity_minutes?: number;
  low_activity_minutes?: number;
  resting_minutes?: number;
  non_wear_minutes?: number;
};

export type DashboardReadiness = {
  score?: number;
  temperature_deviation?: number;
};

export type DashboardSleep = {
  start_time?: string;
  end_time?: string;
  duration_minutes?: number;
  time_in_bed_minutes?: number;
  efficiency_percent?: number;
  average_heart_rate?: number;
  average_hrv?: number;
  deep_minutes?: number;
  light_minutes?: number;
  rem_minutes?: number;
  awake_minutes?: number;
  naps_count?: number;
  nap_minutes?: number;
  sleep_type?: string;
};

export type DashboardDay = {
  date: string;
  activity?: DashboardActivity;
  readiness?: DashboardReadiness;
  sleep?: DashboardSleep;
};

export type DashboardOverview = {
  earliest_date?: string;
  latest_date?: string;
  available_days: number;
  providers: string[];
  export_urls: {
    canonical_jsonl: string;
    canonical_csv: string;
  };
  daily: DashboardDay[];
};

export type AppView = "dashboard" | "settings";

export type PeriodId = "1w" | "1m" | "3m" | "1y";

export type PeriodOption = {
  id: PeriodId;
  label: string;
  shortcut: string;
  days: number;
};
