package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/robince/somascope/internal/store"
)

type dashboardOverview struct {
	EarliestDate  string                  `json:"earliest_date,omitempty"`
	LatestDate    string                  `json:"latest_date,omitempty"`
	AvailableDays int                     `json:"available_days"`
	Providers     []string                `json:"providers"`
	ExportURLs    dashboardOverviewExport `json:"export_urls"`
	Daily         []dashboardOverviewDay  `json:"daily"`
}

type dashboardOverviewExport struct {
	CanonicalJSONL string `json:"canonical_jsonl"`
	CanonicalCSV   string `json:"canonical_csv"`
}

type dashboardOverviewDay struct {
	Date      string                      `json:"date"`
	Activity  *dashboardOverviewActivity  `json:"activity,omitempty"`
	Readiness *dashboardOverviewReadiness `json:"readiness,omitempty"`
	Sleep     *dashboardOverviewSleep     `json:"sleep,omitempty"`
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
				day.Activity = buildDashboardActivity(row.Summary)
			case "daily_readiness":
				day.Readiness = buildDashboardReadiness(row.Summary)
			}
		case "sleep_session":
			state := sleepState[row.LocalDate]
			if state == nil {
				state = &dashboardSleepAccumulator{}
				sleepState[row.LocalDate] = state
			}
			accumulateDashboardSleep(day, state, row)
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

	return dashboardOverview{
		EarliestDate:  earliestDate,
		LatestDate:    latestDate,
		AvailableDays: len(daily),
		Providers:     providers,
		ExportURLs: dashboardOverviewExport{
			CanonicalJSONL: "/api/v1/export/canonical?format=jsonl",
			CanonicalCSV:   "/api/v1/export/canonical?format=csv",
		},
		Daily: daily,
	}, nil
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
		HighActivityMinutes:       secondsToMinutesValue(values, "high_activity_time"),
		MediumActivityMinutes:     secondsToMinutesValue(values, "medium_activity_time"),
		LowActivityMinutes:        secondsToMinutesValue(values, "low_activity_time"),
		RestingMinutes:            secondsToMinutesValue(values, "resting_time"),
		NonWearMinutes:            secondsToMinutesValue(values, "non_wear_time"),
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

func accumulateDashboardSleep(day *dashboardOverviewDay, state *dashboardSleepAccumulator, row store.CanonicalExportRow) {
	duration := valueOrZero(row.DurationMinutes)
	if row.IsNap {
		if day.Sleep == nil {
			day.Sleep = &dashboardOverviewSleep{}
		}
		day.Sleep.NapsCount++
		day.Sleep.NapMinutes += duration
		return
	}

	if day.Sleep == nil {
		day.Sleep = &dashboardOverviewSleep{}
	}

	if duration < state.primaryDuration {
		return
	}
	state.primaryDuration = duration

	metrics := decodeJSONObject(row.Metrics)
	stages := decodeJSONObject(row.Stages)

	day.Sleep.StartTime = row.StartTime
	day.Sleep.EndTime = row.EndTime
	day.Sleep.DurationMinutes = row.DurationMinutes
	day.Sleep.TimeInBedMinutes = row.TimeInBedMinutes
	day.Sleep.EfficiencyPercent = row.EfficiencyPercent
	day.Sleep.AverageHeartRate = floatValue(metrics, "average_heart_rate")
	day.Sleep.AverageHRV = floatValue(metrics, "average_hrv")
	day.Sleep.DeepMinutes = secondsToMinutesValue(stages, "deep_sleep_duration")
	day.Sleep.LightMinutes = secondsToMinutesValue(stages, "light_sleep_duration")
	day.Sleep.REMMinutes = secondsToMinutesValue(stages, "rem_sleep_duration")
	day.Sleep.AwakeMinutes = secondsToMinutesValue(stages, "awake_time")
	day.Sleep.SleepType = stringValue(metrics, "type")
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
