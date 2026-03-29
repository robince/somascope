More charts from data you already sync

  You're syncing 14 entity types but only normalizing/plotting a few. The low-hanging fruit:
  - HRV trend — daily_sleep already has average_hrv, could plot as a line with rolling average overlay
  - Resting heart rate — same source, lowest_heart_rate field, classic longitudinal metric
  - Temperature deviation — daily_sleep.temperature_deviation is one of Oura's most interesting
  signals, small floating point deviations around baseline
  - Readiness & activity scores — daily_readiness.score and daily_activity.score are the headline Oura
  numbers, good for a combined "scores overview" row
  - SpO2 and stress — you sync daily_spo2 and daily_stress but don't normalize them yet. Stress
  especially is newer and less exposed in most tools

  Chart types

  Your current hand-rolled SVG approach scales well to:
  - Sparkline rows — compact single-metric lines (HR, HRV, temp) stacked vertically, great information
  density
  - Heatmap/calendar view — date on x, metric as color intensity. Very effective for spotting patterns
  across weeks/months
  - Correlation scatter — pick any two metrics (e.g., HRV vs sleep duration) and plot them against each
   other. Simple but surprisingly useful for self-quantifiers
  - Distribution histograms — "how often do I get 7+ hours" type questions

  Dashboard layout

  The current single-column fixed layout could evolve to:
  - User-configurable chart grid saved in settings (which charts, what order, what size)
  - Per-chart date range controls (some people want 30-day HRV but 90-day sleep)
  - Collapsible sections or tabs for different "views" (sleep-focused, activity-focused,
  recovery-focused)

  Fitbit integration

  Architecturally this should be clean — your syncEntity / store.UpsertRawDocument / normalization
  pipeline is already provider-agnostic in the store layer. The main work is:
  - Fitbit OAuth (similar BYO flow, different token endpoint)
  - Mapping Fitbit's sleep stages vocabulary to your canonical schema (they use "deep", "light", "rem",
   "wake" — close to Oura but not identical)
  - Fitbit's API pagination is offset-based rather than token-based, so the sync loop shape differs
  slightly

  Export enhancements

  - Date-range-aware canonical exports (you have this for raw but not canonical)
  - Per-metric column selection for CSV
  - A "research packet" export: zipped bundle of canonical CSV + raw JSON + a manifest describing the
  data schema and provenance

  The temperature deviation and HRV trend charts would probably give the most "wow" factor for the
  least effort, since the data is already flowing through your pipeline — you'd just need normalization
   rows and a new chart component for each.