package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"regexp"
)

type Rate struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Input    string `json:"input_rub_per_million"`
	Output   string `json:"output_rub_per_million"`
}
type Tariff struct {
	Version  string `json:"version"`
	Currency string `json:"currency"`
	Rates    []Rate `json:"rates"`
}
type Estimate struct {
	Version          string `json:"tariff_version"`
	Currency         string `json:"currency"`
	AssumedModel     string `json:"assumed_model"`
	Basis            string `json:"basis"`
	KnownCost        string `json:"known_cost_rub"`
	PricedAttempts   int64  `json:"priced_attempts"`
	UnpricedAttempts int64  `json:"unpriced_attempts"`
	Partial          bool   `json:"partial"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})(\.[0-9]{1,6})?$`)

func DecodeTariff(r io.Reader) (Tariff, error) {
	var t Tariff
	bad := errors.New("invalid tariff: require unique version, RUB currency and provider/model decimal rates")
	raw, e := io.ReadAll(io.LimitReader(r, (64<<10)+1))
	if e != nil || len(raw) > 64<<10 {
		return t, bad
	}
	// Reject duplicate object keys rather than silently applying the last price.
	d := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(d) != nil {
		return t, bad
	}
	if _, e = d.Token(); e != io.EOF {
		return t, bad
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&t) != nil || !identifier.MatchString(t.Version) || t.Currency != "RUB" || len(t.Rates) == 0 || len(t.Rates) > 100 {
		return Tariff{}, bad
	}
	seen := map[[2]string]bool{}
	for _, v := range t.Rates {
		key := [2]string{v.Provider, v.Model}
		if (v.Provider != "yandex" && v.Provider != "openai") || !identifier.MatchString(v.Model) || !decimal.MatchString(v.Input) || !decimal.MatchString(v.Output) || seen[key] {
			return Tariff{}, bad
		}
		seen[key] = true
	}
	return t, nil
}
func uniqueJSON(d *json.Decoder) error {
	tok, e := d.Token()
	if e != nil {
		return e
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return errors.New("duplicate key")
			}
			seen[s] = true
			if e = uniqueJSON(d); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = uniqueJSON(d); e != nil {
				return e
			}
		}
	default:
		return errors.New("unexpected delimiter")
	}
	_, e = d.Token()
	return e
}

// EstimateCost is deliberately opt-in: existing logs do not identify the model.
// The caller must explicitly assert that all attempts use the assumed model.
func EstimateCost(r Report, t Tariff, model string) (Estimate, error) {
	if !identifier.MatchString(model) {
		return Estimate{}, errors.New("a valid explicit assumed model is required")
	}
	out := Estimate{Version: t.Version, Currency: "RUB", AssumedModel: model, Basis: "Caller assumes all logged attempts use this model; token-only estimate, not an invoice.", Partial: r.InputIncomplete || r.InvalidLines > 0}
	rates := map[[2]string]Rate{}
	for _, v := range t.Rates {
		rates[[2]string{v.Provider, v.Model}] = v
	}
	sum := new(big.Rat)
	for _, g := range r.Groups {
		if g.Model != "" && g.Model != model {
			return Estimate{}, errors.New("observed model differs from assumed model; split logs before pricing")
		}
		v, ok := rates[[2]string{g.Provider, model}]
		if !ok {
			out.UnpricedAttempts += g.Attempts
			continue
		}
		out.PricedAttempts += g.Attempts - g.UnknownUsage
		out.UnpricedAttempts += g.UnknownUsage
		for _, part := range []struct {
			rate   string
			tokens int64
		}{{v.Input, g.InputTokens}, {v.Output, g.OutputTokens}} {
			rate, ok := new(big.Rat).SetString(part.rate)
			if !ok {
				return Estimate{}, errors.New("invalid rate")
			}
			rate.Mul(rate, new(big.Rat).SetInt64(part.tokens))
			rate.Quo(rate, big.NewRat(1000000, 1))
			sum.Add(sum, rate)
		}
	}
	out.KnownCost = sum.FloatString(12)
	out.Partial = out.Partial || out.UnpricedAttempts > 0
	return out, nil
}
