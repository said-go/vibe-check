package usage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAggregateKnownZeroAndUnknown(t *testing.T) {
	log := `{"msg":"http_request","request_id":"private","ip":"secret"}
{"msg":"provider_attempt","provider":"yandex","outcome":"success","usage_available":true,"input_tokens":7,"output_tokens":3,"total_tokens":10,"body":"secret"}
{"msg":"provider_attempt","provider":"yandex","outcome":"success","usage_available":true,"input_tokens":0,"output_tokens":0,"total_tokens":0}
{"msg":"provider_attempt","provider":"openai","outcome":"timeout","usage_available":false,"total_tokens":999}
{"msg":"provider_attempt","provider":"yandex","outcome":"invalid_response"}`
	got := Read(strings.NewReader(log))
	if got.Attempts != 4 || got.UnknownUsage != 2 || got.TotalTokens != 10 || got.InputTokens != 7 || got.OutputTokens != 3 || got.SkippedLines != 1 || len(got.Groups) != 3 || got.Groups[0].Provider != "openai" {
		t.Fatalf("%+v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private") {
		t.Fatal("private data escaped")
	}
}
func TestInvalidRecordsDoNotFabricateUsage(t *testing.T) {
	log := `broken secret
{"msg":"provider_attempt","provider":"secret","outcome":"success"}
{"msg":"provider_attempt","provider":"yandex","outcome":"secret"}
{"msg":"provider_attempt","provider":"yandex","outcome":"success","usage_available":true,"input_tokens":1}
{"msg":"provider_attempt","provider":"yandex","outcome":"success","usage_available":true,"input_tokens":-1,"output_tokens":0,"total_tokens":0}
{"msg":"provider_attempt","provider":"yandex","outcome":"success","usage_available":true,"input_tokens":1.5,"output_tokens":0,"total_tokens":0}`
	got := Read(strings.NewReader(log))
	if got.InvalidLines != 6 || got.Attempts != 0 {
		t.Fatalf("%+v", got)
	}
}
func TestOversizedLineMarksPartial(t *testing.T) {
	got := Read(strings.NewReader(strings.Repeat("s", MaxLineBytes+1)))
	if !got.InputIncomplete || got.Attempts != 0 {
		t.Fatalf("%+v", got)
	}
}
