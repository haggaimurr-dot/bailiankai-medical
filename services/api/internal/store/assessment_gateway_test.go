package store_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/store"
)

func TestAssessmentGatewayProtocolAndFailureCodes(t *testing.T) {
	input := store.AssessmentFrozenInput{Configuration: assessmentConfig(), Photos: []store.AssessmentPhoto{{ID: "photo-test", Angle: "front", SHA256: strings.Repeat("a", 64), Width: 512, Height: 512}}}
	analysis, requestID, _ := assessmentProvider{}.Analyze(context.Background(), input, nil, "")
	status := 200
	response := map[string]any{"analysis": analysis, "providerRequestId": requestID}
	seen := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		if r.Header.Get("Authorization") != "Bearer controlled-secret" || r.Header.Get("Idempotency-Key") != "stable-input-key" {
			t.Error("gateway headers missing")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["schemaVersion"] != "assessment-gateway-v1" {
			t.Error(request)
		}
		photos := request["photos"].([]any)
		if photos[0].(map[string]any)["jpegBase64"] != "c3ludGhldGlj" {
			t.Error("missing private bytes")
		}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	gateway, err := store.NewAssessmentGateway(server.URL, "controlled-secret")
	if err != nil {
		t.Fatal(err)
	}
	got, id, err := gateway.Analyze(context.Background(), input, map[string][]byte{"photo-test": []byte("synthetic")}, "stable-input-key")
	if err != nil || got.Model != input.Configuration.Model || id != requestID {
		t.Fatal(got, id, err)
	}
	for _, tc := range []struct {
		status int
		code   string
		retry  bool
	}{{429, "MODEL_RATE_LIMITED", true}, {503, "MODEL_UNAVAILABLE", true}, {401, "MODEL_CONFIGURATION_ERROR", false}, {302, "MODEL_CONFIGURATION_ERROR", false}} {
		t.Run(tc.code, func(t *testing.T) {
			status = tc.status
			_, _, err := gateway.Analyze(context.Background(), input, map[string][]byte{"photo-test": []byte("synthetic")}, "stable-input-key")
			failure, ok := err.(*store.AssessmentProviderError)
			if !ok || failure.Code != tc.code || failure.Retryable != tc.retry || strings.Contains(err.Error(), "controlled-secret") {
				t.Fatal(err)
			}
			if status == 429 && failure.RetryAfter != time.Minute {
				t.Fatal(failure)
			}
		})
	}
	status = 200
	response["analysis"] = map[string]any{"schemaVersion": "assessment-result-v1", "unknownField": "controlled-secret"}
	if _, _, err := gateway.Analyze(context.Background(), input, map[string][]byte{"photo-test": []byte("synthetic")}, "stable-input-key"); err == nil || strings.Contains(err.Error(), "controlled-secret") {
		t.Fatal(err)
	}
	if seen != 6 {
		t.Fatal(seen)
	}
}
func TestAssessmentStrictJSONRejectsMissingDuplicateAndNestedFields(t *testing.T) {
	for _, raw := range []string{`{"version":1,"version":2}`, `{"version":null}`, `{"version":1} {}`, `{"version":1,"unknown":true}`, `{"version":1,"finalContent":{"summary":"incomplete"}}`} {
		t.Run(raw, func(t *testing.T) {
			var change store.AssessmentChange
			if store.DecodeAssessmentJSON([]byte(raw), &change) == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
	var change store.AssessmentChange
	if err := store.DecodeAssessmentJSON([]byte(`{"version":1}`), &change); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com?token=secret", "https://example.com#secret"} {
		if _, err := store.NewAssessmentGateway(url, "secret"); err == nil {
			t.Fatal("unsafe configured endpoint accepted")
		}
	}
}
