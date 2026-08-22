package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/robince/somascope/internal/store"
)

type dashboardOverview struct {
	EarliestDate       string                  `json:"earliest_date,omitempty"`
	LatestDate         string                  `json:"latest_date,omitempty"`
	AvailableDays      int                     `json:"available_days"`
	Providers          []string                `json:"providers"`
	ConnectedProviders []string                `json:"connected_providers,omitempty"`
	AvailableSources   []string                `json:"available_sources,omitempty"`
	ExportURLs         dashboardOverviewExport `json:"export_urls"`
	Daily              []dashboardOverviewDay  `json:"daily"`
}

type dashboardOverviewExport struct {
	CanonicalJSONL       string                            `json:"canonical_jsonl"`
	CanonicalCSV         string                            `json:"canonical_csv"`
	RawJSONLByProvider   map[string]string                 `json:"raw_jsonl_by_provider,omitempty"`
	RawOptionsByProvider map[string]store.RawExportOptions `json:"raw_options_by_provider,omitempty"`
}

type dashboardOverviewDay struct {
	Date                string                                 `json:"date"`
	ActivityByProvider  map[string]*dashboardOverviewActivity  `json:"activity_by_provider,omitempty"`
	ReadinessByProvider map[string]*dashboardOverviewReadiness `json:"readiness_by_provider,omitempty"`
	SleepByProvider     map[string]*dashboardOverviewSleep     `json:"sleep_by_provider,omitempty"`
}

type dashboardOverviewActivity struct {
	Score                     *int `json:"score,omitempty"`
	Steps                     *int `json:"steps,omitempty"`
	ActiveCalories            *int `json:"active_calories,omitempty"`
	TotalCalories             *int `json:"total_calories,omitempty"`
	EquivalentWalkingDistance *int `json:"equivalent_walking_distance,omitempty"`
	HighActivityMinutes       *int `json:"high_activity_minutes,omitempty"`
	MediumActivityMinutes     *int `json:"medium_activity_minutes,omitempty"`
	LowActivityMinutes        *int `json:"low_activity_minutes,omitempty"`
	RestingMinutes            *int `json:"resting_minutes,omitempty"`
	NonWearMinutes            *int `json:"non_wear_minutes,omitempty"`
}

type dashboardOverviewReadiness struct {
	Score                *int     `json:"score,omitempty"`
	TemperatureDeviation *float64 `json:"temperature_deviation,omitempty"`
}

type dashboardOverviewSleep struct {
	StartTime         string   `json:"start_time,omitempty"`
	EndTime           string   `json:"end_time,omitempty"`
	DurationMinutes   *int     `json:"duration_minutes,omitempty"`
	TimeInBedMinutes  *int     `json:"time_in_bed_minutes,omitempty"`
	EfficiencyPercent *float64 `json:"efficiency_percent,omitempty"`
	AverageHeartRate  *float64 `json:"average_heart_rate,omitempty"`
	AverageHRV        *float64 `json:"average_hrv,omitempty"`
	DeepMinutes       *int     `json:"deep_minutes,omitempty"`
	LightMinutes      *int     `json:"light_minutes,omitempty"`
	REMMinutes        *int     `json:"rem_minutes,omitempty"`
	AwakeMinutes      *int     `json:"awake_minutes,omitempty"`
	NapsCount         int      `json:"naps_count,omitempty"`
	NapMinutes        int      `json:"nap_minutes,omitempty"`
	SleepType         string   `json:"sleep_type,omitempty"`
}

type dashboardSleepAccumulator struct {
	primaryDuration int
}

func (s *Server) handleDashboardOverview(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("local store unavailable"))
		return
	}

	payload, err := s.buildDashboardOverview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) buildDashboardOverview(ctx context.Context) (dashboardOverview, error) {
	rows, err := s.store.CanonicalExportRows(ctx)
	if err != nil {
		return dashboardOverview{}, fmt.Errorf("load canonical export rows: %w", err)
	}

	dailyByDate := map[string]*dashboardOverviewDay{}
	sleepState := map[string]*dashboardSleepAccumulator{}
	providerSet := map[string]struct{}{}
	var earliestDate string
	var latestDate string

	for _, row := range rows {
		if row.LocalDate == "" {
			continue
		}

		day := dailyByDate[row.LocalDate]
		if day == nil {
			day = &dashboardOverviewDay{Date: row.LocalDate}
			dailyByDate[row.LocalDate] = day
		}

		providerSet[row.Provider] = struct{}{}
		if earliestDate == "" || row.LocalDate < earliestDate {
			earliestDate = row.LocalDate
		}
		if latestDate == "" || row.LocalDate > latestDate {
			latestDate = row.LocalDate
		}

		switch row.RecordType {
		case "daily_record":
			switch row.RecordKind {
			case "daily_activity":
				if activity := buildDashboardActivity(row.Summary); activity != nil {
					if day.ActivityByProvider == nil {
						day.ActivityByProvider = map[string]*dashboardOverviewActivity{}
					}
					day.ActivityByProvider[row.Provider] = activity
				}
			case "daily_readiness":
				if readiness := buildDashboardReadiness(row.Summary); readiness != nil {
					if day.ReadinessByProvider == nil {
						day.ReadinessByProvider = map[string]*dashboardOverviewReadiness{}
					}
					day.ReadinessByProvider[row.Provider] = readiness
				}
			}
		case "sleep_session":
			stateKey := row.Provider + "\x00" + row.LocalDate
			state := sleepState[stateKey]
			if state == nil {
				state = &dashboardSleepAccumulator{}
				sleepState[stateKey] = state
			}
			if day.SleepByProvider == nil {
				day.SleepByProvider = map[string]*dashboardOverviewSleep{}
			}
			accumulateDashboardSleep(day.SleepByProvider, row.Provider, state, row)
		}
	}

	dates := make([]string, 0, len(dailyByDate))
	for date := range dailyByDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	daily := make([]dashboardOverviewDay, 0, len(dates))
	for _, date := range dates {
		daily = append(daily, *dailyByDate[date])
	}

	providers := make([]string, 0, len(providerSet))
	for provider := range providerSet {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	rawJSONLByProvider := map[string]string{}
	rawOptionsByProvider := map[string]store.RawExportOptions{}
	for _, provider := range knownProviders() {
		options, err := s.store.RawExportOptions(ctx, provider)
		if err != nil {
			return dashboardOverview{}, fmt.Errorf("load raw export options for %s: %w", provider, err)
		}
		if len(options.DocumentKinds) == 0 && options.StartDate == "" && options.EndDate == "" {
			continue
		}
		rawOptionsByProvider[provider] = options
		rawJSONLByProvider[provider] = "/api/v1/export/raw?provider=" + provider + "&format=jsonl"
	}

	connectedProviders, err := s.connectedProviders(ctx)
	if err != nil {
		return dashboardOverview{}, err
	}

	return dashboardOverview{
		EarliestDate:       earliestDate,
		LatestDate:         latestDate,
		AvailableDays:      len(daily),
		Providers:          providers,
		ConnectedProviders: connectedProviders,
		AvailableSources:   mergeUniqueStrings(providers, connectedProviders),
		ExportURLs: dashboardOverviewExport{
			CanonicalJSONL:       "/api/v1/export/canonical?format=jsonl",
			CanonicalCSV:         "/api/v1/export/canonical?format=csv",
			RawJSONLByProvider:   rawJSONLByProvider,
			RawOptionsByProvider: rawOptionsByProvider,
		},
		Daily: daily,
	}, nil
}

func (s *Server) connectedProviders(ctx context.Context) ([]string, error) {
	var connected []string
	for _, provider := range knownProviders() {
		connection, err := s.store.ConnectionByProvider(ctx, provider)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load %s connection: %w", provider, err)
		}
		if connection.Status == "connected" && strings.TrimSpace(connection.AccessToken) != "" {
			connected = append(connected, provider)
		}
	}
	return connected, nil
}

func buildDashboardActivity(raw json.RawMessage) *dashboardOverviewActivity {
	values := decodeJSONObject(raw)
	if len(values) == 0 {
		return nil
	}

	return &dashboardOverviewActivity{
		Score:                     intValue(values, "score"),
		Steps:                     intValue(values, "steps"),
		ActiveCalories:            intValue(values, "active_calories"),
		TotalCalories:             intValue(values, "total_calories"),
		EquivalentWalkingDistance: intValue(values, "equivalent_walking_distance"),
		HighActivityMinutes:       firstMinutesValue(values, "high_activity_minutes", "high_activity_time"),
		MediumActivityMinutes:     firstMinutesValue(values, "medium_activity_minutes", "medium_activity_time"),
		LowActivityMinutes:        firstMinutesValue(values, "low_activity_minutes", "low_activity_time"),
		RestingMinutes:            firstMinutesValue(values, "resting_minutes", "resting_time"),
		NonWearMinutes:            firstMinutesValue(values, "non_wear_minutes", "non_wear_time"),
	}
}

func buildDashboardReadiness(raw json.RawMessage) *dashboardOverviewReadiness {
	values := decodeJSONObject(raw)
	if len(values) == 0 {
		return nil
	}

	return &dashboardOverviewReadiness{
		Score:                intValue(values, "score"),
		TemperatureDeviation: floatValue(values, "temperature_deviation"),
	}
}

func accumulateDashboardSleep(byProvider map[string]*dashboardOverviewSleep, provider string, state *dashboardSleepAccumulator, row store.CanonicalExportRow) {
	duration := valueOrZero(row.DurationMinutes)
	sleep := byProvider[provider]
	if sleep == nil {
		sleep = &dashboardOverviewSleep{}
		byProvider[provider] = sleep
	}
	if row.IsNap {
		sleep.NapsCount++
		sleep.NapMinutes += duration
		return
	}

	if duration < state.primaryDuration {
		return
	}
	state.primaryDuration = duration

	metrics := decodeJSONObject(row.Metrics)
	stages := decodeJSONObject(row.Stages)

	sleep.StartTime = row.StartTime
	sleep.EndTime = row.EndTime
	sleep.DurationMinutes = row.DurationMinutes
	sleep.TimeInBedMinutes = row.TimeInBedMinutes
	sleep.EfficiencyPercent = row.EfficiencyPercent
	sleep.AverageHeartRate = floatValue(metrics, "average_heart_rate")
	sleep.AverageHRV = floatValue(metrics, "average_hrv")
	sleep.DeepMinutes = firstMinutesValue(stages, "deep_minutes", "deep_sleep_duration")
	sleep.LightMinutes = firstMinutesValue(stages, "light_minutes", "light_sleep_duration")
	sleep.REMMinutes = firstMinutesValue(stages, "rem_minutes", "rem_sleep_duration")
	sleep.AwakeMinutes = firstMinutesValue(stages, "awake_minutes", "awake_time")
	sleep.SleepType = firstNonEmptyString(stringValue(metrics, "type"), stringValue(metrics, "sleep_type"))
}

func firstMinutesValue(values map[string]any, minuteKey, secondKey string) *int {
	if value := intValue(values, minuteKey); value != nil {
		return value
	}
	return secondsToMinutesValue(values, secondKey)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mergeUniqueStrings(groups ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, group := range groups {
		for _, value := range group {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func decodeJSONObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}

	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func intValue(values map[string]any, key string) *int {
	number := floatValue(values, key)
	if number == nil {
		return nil
	}
	value := int(*number)
	return &value
}

func secondsToMinutesValue(values map[string]any, key string) *int {
	number := floatValue(values, key)
	if number == nil {
		return nil
	}
	value := int(*number / 60)
	return &value
}

func floatValue(values map[string]any, key string) *float64 {
	if len(values) == 0 {
		return nil
	}

	raw, ok := values[key]
	if !ok {
		return nil
	}

	switch value := raw.(type) {
	case float64:
		copy := value
		return &copy
	case float32:
		copy := float64(value)
		return &copy
	case int:
		copy := float64(value)
		return &copy
	case int64:
		copy := float64(value)
		return &copy
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}

func stringValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}

	raw, ok := values[key]
	if !ok {
		return ""
	}

	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
