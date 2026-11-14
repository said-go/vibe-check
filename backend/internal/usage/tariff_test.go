package usage

import (
	"strings"
	"testing"
)

const sampleTariff = `{"version":"synthetic-test-v1","currency":"RUB","rates":[{"provider":"yandex","model":"test-model","input_rub_per_million":"1.25","output_rub_per_million":"2.5"}]}`

func TestTariffExactAndPartial(t *testing.T) {
	tariff, e := DecodeTariff(strings.NewReader(sampleTariff))
	if e != nil {
		t.Fatal(e)
	}
	r := Report{Groups: []Group{{Provider: "yandex", Attempts: 1, InputTokens: 3, OutputTokens: 7}}}
	got, e := EstimateCost(r, tariff, "test-model")
	if e != nil || got.KnownCost != "0.000021250000" || got.Partial || got.PricedAttempts != 1 {
		t.Fatalf("%+v %v", got, e)
	}
	r.Groups = append(r.Groups, Group{Provider: "yandex", Attempts: 1, UnknownUsage: 1}, Group{Provider: "openai", Attempts: 2, InputTokens: 999})
	got, e = EstimateCost(r, tariff, "test-model")
	if e != nil || got.UnpricedAttempts != 3 || !got.Partial || got.KnownCost != "0.000021250000" {
		t.Fatalf("%+v %v", got, e)
	}
}
func TestTariffRejectsAmbiguity(t *testing.T) {
	for _, s := range []string{strings.Replace(sampleTariff, `"1.25"`, `"-1"`, 1), strings.Replace(sampleTariff, `"1.25"`, `1.25`, 1), strings.Replace(sampleTariff, `"currency":"RUB"`, `"currency":"RUB","currency":"USD"`, 1), strings.Replace(sampleTariff, `"version":"synthetic-test-v1",`, "", 1), strings.Replace(sampleTariff, `"1.25"`, `"1e9"`, 1), strings.Replace(sampleTariff, `"rates":[`, `"rates":[{"provider":"yandex","model":"test-model","input_rub_per_million":"0","output_rub_per_million":"0"},`, 1)} {
		if _, e := DecodeTariff(strings.NewReader(s)); e == nil {
			t.Fatal("accepted invalid tariff")
		}
	}
}
func TestObservedModelCannotBeOverridden(t *testing.T) {
	tariff, _ := DecodeTariff(strings.NewReader(sampleTariff))
	r := Read(strings.NewReader(`{"msg":"provider_attempt","provider":"yandex","model":"other-model","outcome":"success","usage_available":false}`))
	if _, e := EstimateCost(r, tariff, "test-model"); e == nil {
		t.Fatal("model assumption overrode log")
	}
	if _, e := EstimateCost(r, tariff, ""); e == nil {
		t.Fatal("no assumption accepted")
	}
}
