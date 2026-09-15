package easyllm

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	tokensPerMillion       = 1_000_000
	minutesPerDay          = 24 * 60
	defaultPricingLocation = "Asia/Shanghai"
)

// Rates are per-million-token prices. The currency is chosen by the caller:
// this package performs no conversion and only adds the amounts together.
type Rates struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	CachedInput float64 `json:"cached_input"`
}

func (r Rates) validate(scope string) error {
	if r.Input < 0 || r.Output < 0 || r.CachedInput < 0 {
		return fmt.Errorf("%s: rates cannot be negative", scope)
	}
	return nil
}

// RateWindow overrides the base rates inside a recurring local-time window.
// Windows are declared without JSON tags because the wire form differs: see
// NewPricingJSON.
type RateWindow struct {
	// Start is "HH:MM", inclusive.
	Start string
	// End is "HH:MM", exclusive. An End at or before Start wraps past midnight,
	// so "22:00"-"06:00" covers one night. "24:00" is accepted as the day end.
	End string
	// Weekdays limits the window to the days it opens on. Empty means every day.
	Weekdays []time.Weekday
	Rates    Rates
}

// PricingConfig describes a price list that is independent of the model. Model
// specific pricing belongs to a custom Pricer built with PricerFunc.
type PricingConfig struct {
	// Location is the time zone the windows are evaluated in. Nil means
	// Asia/Shanghai.
	Location *time.Location
	// Rates applies outside every window.
	Rates Rates
	// Windows override Rates while they match. The first match wins.
	Windows []RateWindow
}

// Pricing is a compiled price list. It is immutable and safe for concurrent use.
type Pricing struct {
	location *time.Location
	rates    Rates
	windows  []compiledWindow
}

type compiledWindow struct {
	start    int
	length   int
	weekdays [7]bool
	anyDay   bool
	rates    Rates
}

// CostInput describes one model call.
type CostInput struct {
	Provider string
	Model    string
	// InputTokens includes cached input tokens.
	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
	// Time is the instant of the call, in UTC.
	Time time.Time
}

// Cost is the priced result of one model call. The total is derived by the
// engine as the sum of the three components, so it is not repeated here.
type Cost struct {
	Input       float64
	Output      float64
	CachedInput float64
}

// Pricer computes the cost of one model call. Implementations return a zero
// Cost when the price is unknown: a missing price must not fail a run that has
// already spent tokens.
type Pricer interface {
	Price(CostInput) Cost
}

// PricerFunc adapts a function to Pricer. Use it for pricing that the built-in
// price list cannot express.
type PricerFunc func(CostInput) Cost

func (f PricerFunc) Price(in CostInput) Cost {
	return f(in)
}

// SimplePricing returns a price list that charges the same rates at all times.
// Unlike NewPricing it does not validate rates, because it cannot fail.
func SimplePricing(rates Rates) *Pricing {
	return &Pricing{location: resolvePricingLocation(nil), rates: rates}
}

// NewPricing validates and compiles a price list. Malformed windows, negative
// rates, and out-of-range weekdays fail here rather than mid-run.
func NewPricing(config PricingConfig) (*Pricing, error) {
	if err := config.Rates.validate("base"); err != nil {
		return nil, err
	}
	pricing := &Pricing{
		location: resolvePricingLocation(config.Location),
		rates:    config.Rates,
		windows:  make([]compiledWindow, 0, len(config.Windows)),
	}
	for index, window := range config.Windows {
		compiled, err := compileWindow(window, index)
		if err != nil {
			return nil, err
		}
		pricing.windows = append(pricing.windows, compiled)
	}
	return pricing, nil
}

// NewPricingJSON parses and validates a price list from JSON. Unknown fields
// are rejected, so a misspelled key cannot silently fall back to base rates.
func NewPricingJSON(text string) (*Pricing, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	var wire pricingJSON
	if err := decoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("pricing: %w", err)
	}
	location, err := resolvePricingLocationName(wire.Location)
	if err != nil {
		return nil, err
	}
	config := PricingConfig{Location: location, Rates: wire.Rates}
	if len(wire.Windows) > 0 {
		config.Windows = make([]RateWindow, 0, len(wire.Windows))
		for _, window := range wire.Windows {
			config.Windows = append(config.Windows, RateWindow{
				Start:    window.Start,
				End:      window.End,
				Weekdays: window.Weekdays,
				Rates:    window.Rates,
			})
		}
	}
	return NewPricing(config)
}

// Price returns the cost of one model call. Cached input tokens are a subset of
// input tokens, so only the uncached remainder is charged at the input rate.
func (p *Pricing) Price(in CostInput) Cost {
	rates := p.rates
	if len(p.windows) > 0 {
		local := in.Time.In(p.location)
		for _, window := range p.windows {
			if window.matches(local) {
				rates = window.rates
				break
			}
		}
	}
	uncached := in.InputTokens - in.CachedInputTokens
	if uncached < 0 {
		uncached = 0
	}
	return Cost{
		Input:       float64(uncached) * rates.Input / tokensPerMillion,
		Output:      float64(in.OutputTokens) * rates.Output / tokensPerMillion,
		CachedInput: float64(in.CachedInputTokens) * rates.CachedInput / tokensPerMillion,
	}
}

// matches reports whether a local wall-clock time falls inside the window. A
// window that wraps past midnight is attributed to the day it opened on, so a
// Monday 22:00-06:00 window covers Tuesday 02:00 but not Monday 02:00.
func (w compiledWindow) matches(local time.Time) bool {
	elapsed := local.Hour()*60 + local.Minute() - w.start
	opening := local.Weekday()
	if elapsed < 0 {
		elapsed += minutesPerDay
		opening = (opening + 6) % 7
	}
	if elapsed >= w.length {
		return false
	}
	return w.anyDay || w.weekdays[opening]
}

func compileWindow(window RateWindow, index int) (compiledWindow, error) {
	scope := fmt.Sprintf("window %d", index)
	start, err := parseTimeOfDay(window.Start, false)
	if err != nil {
		return compiledWindow{}, fmt.Errorf("%s: start %w", scope, err)
	}
	end, err := parseTimeOfDay(window.End, true)
	if err != nil {
		return compiledWindow{}, fmt.Errorf("%s: end %w", scope, err)
	}
	if start == end {
		return compiledWindow{}, fmt.Errorf("%s: start and end must differ, use rates for a whole-day price", scope)
	}
	if err := window.Rates.validate(scope); err != nil {
		return compiledWindow{}, err
	}
	length := end - start
	if length < 0 {
		length += minutesPerDay
	}
	compiled := compiledWindow{start: start, length: length, rates: window.Rates}
	if len(window.Weekdays) == 0 {
		compiled.anyDay = true
	}
	for _, day := range window.Weekdays {
		if day < time.Sunday || day > time.Saturday {
			return compiledWindow{}, fmt.Errorf("%s: weekday %d is out of range", scope, day)
		}
		compiled.weekdays[day] = true
	}
	return compiled, nil
}

// parseTimeOfDay parses "HH:MM" into minutes from midnight. Single digit hours
// are accepted; minutes must use two digits. allowDayEnd accepts "24:00".
func parseTimeOfDay(value string, allowDayEnd bool) (int, error) {
	text := strings.TrimSpace(value)
	hourText, minuteText, found := strings.Cut(text, ":")
	if !found || len(minuteText) != 2 || len(hourText) == 0 || len(hourText) > 2 {
		return 0, fmt.Errorf("%q is not a HH:MM time", value)
	}
	hour, err := strconv.Atoi(hourText)
	if err != nil {
		return 0, fmt.Errorf("%q is not a HH:MM time", value)
	}
	minute, err := strconv.Atoi(minuteText)
	if err != nil {
		return 0, fmt.Errorf("%q is not a HH:MM time", value)
	}
	if minute < 0 || minute > 59 {
		return 0, fmt.Errorf("%q is not a HH:MM time", value)
	}
	if hour < 0 || hour > 23 {
		if allowDayEnd && hour == 24 && minute == 0 {
			return minutesPerDay, nil
		}
		return 0, fmt.Errorf("%q is not a HH:MM time", value)
	}
	return hour*60 + minute, nil
}

// resolvePricingLocation falls back to Asia/Shanghai. Slim container images ship
// without tzdata, and China has had no daylight saving since 1991, so a fixed
// +08:00 is equivalent when the zone database is unavailable.
func resolvePricingLocation(location *time.Location) *time.Location {
	if location != nil {
		return location
	}
	parsed, err := time.LoadLocation(defaultPricingLocation)
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return parsed
}

func resolvePricingLocationName(name string) (*time.Location, error) {
	text := strings.TrimSpace(name)
	if text == "" {
		return resolvePricingLocation(nil), nil
	}
	location, err := time.LoadLocation(text)
	if err != nil {
		return nil, fmt.Errorf("pricing: unknown location %q", text)
	}
	return location, nil
}

// pricingJSON is the wire form of a price list. It exists so that JSON can use a
// zone name string while Go uses *time.Location. Weekdays are numbers, matching
// time.Weekday: 0 is Sunday.
type pricingJSON struct {
	Location string           `json:"location"`
	Rates    Rates            `json:"rates"`
	Windows  []rateWindowJSON `json:"windows"`
}

type rateWindowJSON struct {
	Start    string         `json:"start"`
	End      string         `json:"end"`
	Weekdays []time.Weekday `json:"weekdays"`
	Rates    Rates          `json:"rates"`
}
