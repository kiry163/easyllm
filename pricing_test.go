package easyllm

import (
	"math"
	"testing"
	"time"
)

func testLocation() *time.Location {
	return time.FixedZone("CST", 8*60*60)
}

func assertCost(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("unexpected %s: got %v want %v", label, got, want)
	}
}

func TestSimplePricingChargesUncachedInputSeparately(t *testing.T) {
	pricing := SimplePricing(Rates{Input: 2, Output: 8, CachedInput: 0.5})

	cost := pricing.Price(CostInput{
		InputTokens:       1_000_000,
		CachedInputTokens: 400_000,
		OutputTokens:      1_000_000,
	})

	assertCost(t, "input", cost.Input, 1.2)
	assertCost(t, "cached input", cost.CachedInput, 0.2)
	assertCost(t, "output", cost.Output, 8)
}

func TestPricingDoesNotChargeNegativeInputWhenCachedExceedsInput(t *testing.T) {
	pricing := SimplePricing(Rates{Input: 2, Output: 8, CachedInput: 0.5})

	cost := pricing.Price(CostInput{InputTokens: 100, CachedInputTokens: 400})

	assertCost(t, "input", cost.Input, 0)
	assertCost(t, "cached input", cost.CachedInput, 0.0002)
}

func TestPricingMatchesDailyWindowBounds(t *testing.T) {
	location := testLocation()
	pricing, err := NewPricing(PricingConfig{
		Location: location,
		Rates:    Rates{Input: 2},
		Windows: []RateWindow{{
			Start: "00:30",
			End:   "08:30",
			Rates: Rates{Input: 1},
		}},
	})
	if err != nil {
		t.Fatalf("NewPricing returned error: %v", err)
	}

	tests := []struct {
		name  string
		local time.Time
		want  float64
	}{
		{"before window", time.Date(2026, time.March, 9, 0, 29, 0, 0, location), 2},
		{"window start is inclusive", time.Date(2026, time.March, 9, 0, 30, 0, 0, location), 1},
		{"inside window", time.Date(2026, time.March, 9, 4, 0, 0, 0, location), 1},
		{"window end is exclusive", time.Date(2026, time.March, 9, 8, 30, 0, 0, location), 2},
		{"after window", time.Date(2026, time.March, 9, 9, 0, 0, 0, location), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := pricing.Price(CostInput{InputTokens: 1_000_000, Time: tt.local.UTC()})
			assertCost(t, "input", cost.Input, tt.want)
		})
	}
}

func TestPricingMatchesCrossMidnightWindow(t *testing.T) {
	location := testLocation()
	pricing, err := NewPricing(PricingConfig{
		Location: location,
		Rates:    Rates{Input: 2},
		Windows: []RateWindow{{
			Start: "22:00",
			End:   "06:00",
			Rates: Rates{Input: 1},
		}},
	})
	if err != nil {
		t.Fatalf("NewPricing returned error: %v", err)
	}

	tests := []struct {
		name  string
		local time.Time
		want  float64
	}{
		{"before opening", time.Date(2026, time.March, 9, 21, 59, 0, 0, location), 2},
		{"at opening", time.Date(2026, time.March, 9, 22, 0, 0, 0, location), 1},
		{"after midnight", time.Date(2026, time.March, 10, 2, 0, 0, 0, location), 1},
		{"at closing", time.Date(2026, time.March, 10, 6, 0, 0, 0, location), 2},
		{"midday", time.Date(2026, time.March, 10, 12, 0, 0, 0, location), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := pricing.Price(CostInput{InputTokens: 1_000_000, Time: tt.local.UTC()})
			assertCost(t, "input", cost.Input, tt.want)
		})
	}
}

func TestPricingUsesWeekdayOfTheOpeningDayForCrossMidnightWindows(t *testing.T) {
	location := testLocation()
	pricing, err := NewPricing(PricingConfig{
		Location: location,
		Rates:    Rates{Input: 2},
		Windows: []RateWindow{{
			Start:    "22:00",
			End:      "06:00",
			Weekdays: []time.Weekday{time.Monday},
			Rates:    Rates{Input: 1},
		}},
	})
	if err != nil {
		t.Fatalf("NewPricing returned error: %v", err)
	}

	tests := []struct {
		name  string
		local time.Time
		want  float64
	}{
		{"monday opens the window", time.Date(2026, time.March, 9, 23, 0, 0, 0, location), 1},
		{"tuesday night belongs to monday", time.Date(2026, time.March, 10, 2, 0, 0, 0, location), 1},
		{"tuesday does not open a window", time.Date(2026, time.March, 10, 23, 0, 0, 0, location), 2},
		{"sunday night does not open a window", time.Date(2026, time.March, 8, 23, 0, 0, 0, location), 2},
		{"monday night belongs to sunday", time.Date(2026, time.March, 9, 2, 0, 0, 0, location), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := pricing.Price(CostInput{InputTokens: 1_000_000, Time: tt.local.UTC()})
			assertCost(t, "input", cost.Input, tt.want)
		})
	}
}

func TestPricingRestrictsNonWrappingWindowToWeekdays(t *testing.T) {
	location := testLocation()
	pricing, err := NewPricing(PricingConfig{
		Location: location,
		Rates:    Rates{Input: 2},
		Windows: []RateWindow{{
			Start:    "09:00",
			End:      "18:00",
			Weekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
			Rates:    Rates{Input: 1},
		}},
	})
	if err != nil {
		t.Fatalf("NewPricing returned error: %v", err)
	}

	tests := []struct {
		name  string
		local time.Time
		want  float64
	}{
		{"weekday business hours", time.Date(2026, time.March, 13, 10, 0, 0, 0, location), 1},
		{"weekday outside hours", time.Date(2026, time.March, 13, 20, 0, 0, 0, location), 2},
		{"saturday business hours", time.Date(2026, time.March, 14, 10, 0, 0, 0, location), 2},
		{"sunday business hours", time.Date(2026, time.March, 15, 10, 0, 0, 0, location), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := pricing.Price(CostInput{InputTokens: 1_000_000, Time: tt.local.UTC()})
			assertCost(t, "input", cost.Input, tt.want)
		})
	}
}

func TestPricingUsesFirstMatchingWindow(t *testing.T) {
	location := testLocation()
	pricing, err := NewPricing(PricingConfig{
		Location: location,
		Rates:    Rates{Input: 2},
		Windows: []RateWindow{
			{Start: "09:00", End: "17:00", Rates: Rates{Input: 3}},
			{Start: "09:00", End: "17:00", Rates: Rates{Input: 1}},
		},
	})
	if err != nil {
		t.Fatalf("NewPricing returned error: %v", err)
	}

	cost := pricing.Price(CostInput{
		InputTokens: 1_000_000,
		Time:        time.Date(2026, time.March, 9, 10, 0, 0, 0, location).UTC(),
	})
	assertCost(t, "input", cost.Input, 3)
}

func TestPricingSupportsFullDayWindow(t *testing.T) {
	location := testLocation()
	pricing, err := NewPricing(PricingConfig{
		Location: location,
		Rates:    Rates{Input: 2},
		Windows: []RateWindow{{
			Start: "00:00",
			End:   "24:00",
			Rates: Rates{Input: 1},
		}},
	})
	if err != nil {
		t.Fatalf("NewPricing returned error: %v", err)
	}

	cost := pricing.Price(CostInput{
		InputTokens: 1_000_000,
		Time:        time.Date(2026, time.March, 9, 13, 0, 0, 0, location).UTC(),
	})
	assertCost(t, "input", cost.Input, 1)
}

func TestNewPricingDefaultsToLocalTime(t *testing.T) {
	pricing, err := NewPricing(PricingConfig{
		Rates:   Rates{Input: 2},
		Windows: []RateWindow{{Start: "00:30", End: "08:30", Rates: Rates{Input: 1}}},
	})
	if err != nil {
		t.Fatalf("NewPricing returned error: %v", err)
	}
	if pricing.location != time.Local {
		t.Fatalf("unexpected default location: %v, want the host local zone", pricing.location)
	}

	// The window is matched on the local wall clock, so 04:00 of that local
	// day is inside it whatever zone the test host runs in.
	cost := pricing.Price(CostInput{
		InputTokens: 1_000_000,
		Time:        time.Date(2026, time.March, 9, 4, 0, 0, 0, time.Local).UTC(),
	})
	assertCost(t, "input", cost.Input, 1)
}

func TestNewPricingRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config PricingConfig
	}{
		{
			name:   "negative base rate",
			config: PricingConfig{Rates: Rates{Input: -1}},
		},
		{
			name: "negative window rate",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "00:00", End: "01:00", Rates: Rates{Output: -1}}},
			},
		},
		{
			name: "malformed start",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "8", End: "09:00"}},
			},
		},
		{
			name: "out of range end",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "08:00", End: "25:00"}},
			},
		},
		{
			name: "equal bounds",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "08:00", End: "08:00"}},
			},
		},
		{
			name: "single digit minute",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "08:3", End: "09:00"}},
			},
		},
		{
			name: "day end as start",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "24:00", End: "01:00"}},
			},
		},
		{
			name: "minute out of range",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "08:00", End: "08:60"}},
			},
		},
		{
			name: "out of range weekday",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "00:00", End: "01:00", Weekdays: []time.Weekday{7}}},
			},
		},
		{
			name: "negative weekday",
			config: PricingConfig{
				Windows: []RateWindow{{Start: "00:00", End: "01:00", Weekdays: []time.Weekday{-1}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewPricing(tt.config); err == nil {
				t.Fatal("expected NewPricing to reject the config")
			}
		})
	}
}

func TestNewPricingAcceptsSingleDigitHour(t *testing.T) {
	if _, err := NewPricing(PricingConfig{
		Windows: []RateWindow{{Start: "8:30", End: "9:00", Rates: Rates{Input: 1}}},
	}); err != nil {
		t.Fatalf("NewPricing returned error: %v", err)
	}
}

func TestNewPricingJSONDefaultsToLocalTime(t *testing.T) {
	pricing, err := NewPricingJSON(`{
		"rates": {"input": 2},
		"windows": [{"start": "00:30", "end": "08:30", "rates": {"input": 1}}]
	}`)
	if err != nil {
		t.Fatalf("NewPricingJSON returned error: %v", err)
	}
	if pricing.location != time.Local {
		t.Fatalf("unexpected default location: %v, want the host local zone", pricing.location)
	}
}

func TestNewPricingJSONLoadsConfig(t *testing.T) {
	pricing, err := NewPricingJSON(`{
		"location": "Asia/Shanghai",
		"rates": {"input": 2, "output": 8, "cached_input": 0.5},
		"windows": [
			{
				"start": "00:30",
				"end": "08:30",
				"weekdays": [1, 2, 3, 4, 5],
				"rates": {"input": 1, "output": 4, "cached_input": 0.25}
			}
		]
	}`)
	if err != nil {
		t.Fatalf("NewPricingJSON returned error: %v", err)
	}

	location := testLocation()
	tests := []struct {
		name  string
		local time.Time
		want  float64
	}{
		{"weekday inside window", time.Date(2026, time.March, 9, 4, 0, 0, 0, location), 1},
		{"weekend inside window", time.Date(2026, time.March, 14, 4, 0, 0, 0, location), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := pricing.Price(CostInput{InputTokens: 1_000_000, Time: tt.local.UTC()})
			assertCost(t, "input", cost.Input, tt.want)
		})
	}
}

func TestNewPricingJSONRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"malformed json", `{`},
		{"unknown field", `{"rate": {"input": 1}}`},
		{"unknown location", `{"location": "Mars/Olympus"}`},
		{"weekday out of range", `{"windows": [{"start": "00:00", "end": "01:00", "weekdays": [9]}]}`},
		{"negative weekday", `{"windows": [{"start": "00:00", "end": "01:00", "weekdays": [-1]}]}`},
		{"equal window bounds", `{"windows": [{"start": "00:00", "end": "00:00"}]}`},
		{"negative rate", `{"rates": {"input": -1}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewPricingJSON(tt.text); err == nil {
				t.Fatal("expected NewPricingJSON to reject the input")
			}
		})
	}
}

func TestPricerFuncAdaptsFunctions(t *testing.T) {
	var pricer Pricer = PricerFunc(func(in CostInput) Cost {
		if in.Model != "custom" {
			t.Fatalf("unexpected model: %q", in.Model)
		}
		return Cost{Input: 1, Output: 2, CachedInput: 3}
	})

	cost := pricer.Price(CostInput{Model: "custom"})

	assertCost(t, "input", cost.Input, 1)
	assertCost(t, "output", cost.Output, 2)
	assertCost(t, "cached input", cost.CachedInput, 3)
}
