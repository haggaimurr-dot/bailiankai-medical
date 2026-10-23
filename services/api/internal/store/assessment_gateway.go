package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Gateway protocol v1 accepts normalized private image bytes and returns the
// documented quality/result schemas. Vendor-specific model adapters live behind
// this explicitly configured service; no deployment is silently simulated.
type AssessmentGateway struct {
	endpoint string
	key      string
	client   *http.Client
}

func NewAssessmentGateway(endpoint, key string) (*AssessmentGateway, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return nil, ErrAssessmentConfiguration
	}
	if u.Scheme == "https" && strings.TrimSpace(key) == "" {
		return nil, ErrAssessmentConfiguration
	}
	return &AssessmentGateway{endpoint: strings.TrimRight(endpoint, "/"), key: key, client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func LoadAssessmentGateway() (AssessmentProvider, AssessmentPipelineConfig, error) {
	cfg := AssessmentPipelineConfig{CheckerVersion: strings.TrimSpace(os.Getenv("ASSESSMENT_CHECKER_VERSION")), ThresholdProfileVersion: strings.TrimSpace(os.Getenv("ASSESSMENT_THRESHOLD_PROFILE_VERSION")), Model: AssessmentModel{Provider: strings.TrimSpace(os.Getenv("ASSESSMENT_MODEL_PROVIDER")), ModelID: strings.TrimSpace(os.Getenv("ASSESSMENT_MODEL_ID")), ModelVersion: strings.TrimSpace(os.Getenv("ASSESSMENT_MODEL_VERSION")), DeploymentVersion: strings.TrimSpace(os.Getenv("ASSESSMENT_DEPLOYMENT_VERSION")), PromptVersion: strings.TrimSpace(os.Getenv("ASSESSMENT_PROMPT_VERSION")), AdapterVersion: strings.TrimSpace(os.Getenv("ASSESSMENT_ADAPTER_VERSION"))}}
	endpoint := strings.TrimSpace(os.Getenv("ASSESSMENT_GATEWAY_URL"))
	key := os.Getenv("ASSESSMENT_GATEWAY_KEY")
	if endpoint == "" {
		if key != "" || cfg != (AssessmentPipelineConfig{}) {
			return nil, cfg, ErrAssessmentConfiguration
		}
		return nil, cfg, nil
	}
	gateway, err := NewAssessmentGateway(endpoint, key)
	if err != nil {
		return nil, cfg, err
	}
	probe := NewAssessmentService(nil, nil, gateway, cfg)
	if !probe.ModelAvailable() {
		return nil, cfg, ErrAssessmentConfiguration
	}
	return gateway, cfg, nil
}

type assessmentGatewayPhoto struct {
	ID         string `json:"id"`
	Angle      string `json:"angle"`
	SHA256     string `json:"sha256"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	JPEGBase64 string `json:"jpegBase64"`
}
type assessmentGatewayRequest struct {
	SchemaVersion string                   `json:"schemaVersion"`
	Configuration AssessmentPipelineConfig `json:"configuration"`
	Photos        []assessmentGatewayPhoto `json:"photos"`
}

func gatewayRequest(input AssessmentFrozenInput, data map[string][]byte) assessmentGatewayRequest {
	request := assessmentGatewayRequest{SchemaVersion: "assessment-gateway-v1", Configuration: input.Configuration, Photos: []assessmentGatewayPhoto{}}
	for _, p := range input.Photos {
		request.Photos = append(request.Photos, assessmentGatewayPhoto{ID: p.ID, Angle: p.Angle, SHA256: p.SHA256, Width: p.Width, Height: p.Height, JPEGBase64: base64.StdEncoding.EncodeToString(data[p.ID])})
	}
	return request
}
func (g *AssessmentGateway) call(ctx context.Context, path, key string, request any, response any) error {
	encoded, err := json.Marshal(request)
	if err != nil {
		return &AssessmentProviderError{Code: "MODEL_CONFIGURATION_ERROR"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint+path, bytes.NewReader(encoded))
	if err != nil {
		return &AssessmentProviderError{Code: "MODEL_CONFIGURATION_ERROR"}
	}
	req.Header.Set("Content-Type", "application/json")
	if g.key != "" {
		req.Header.Set("Authorization", "Bearer "+g.key)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := g.client.Do(req)
	if err != nil {
		code := "MODEL_UNAVAILABLE"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "MODEL_TIMEOUT"
		}
		return &AssessmentProviderError{Code: code, Retryable: true}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests {
		delay := time.Duration(0)
		if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds > 0 {
			if seconds > 86400 {
				seconds = 86400
			}
			delay = time.Duration(seconds) * time.Second
		} else if stamp, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
			delay = time.Until(stamp)
			if delay > 24*time.Hour {
				delay = 24 * time.Hour
			}
		}
		return &AssessmentProviderError{Code: "MODEL_RATE_LIMITED", Retryable: true, RetryAfter: delay}
	}
	if res.StatusCode >= 500 {
		return &AssessmentProviderError{Code: "MODEL_UNAVAILABLE", Retryable: true}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &AssessmentProviderError{Code: "MODEL_CONFIGURATION_ERROR"}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || DecodeAssessmentJSON(body, response) != nil {
		return &AssessmentProviderError{Code: "MODEL_OUTPUT_INVALID"}
	}
	return nil
}
func (g *AssessmentGateway) Check(ctx context.Context, input AssessmentFrozenInput, data map[string][]byte) (map[string]AssessmentQuality, error) {
	var response struct {
		Qualities map[string]AssessmentQuality `json:"qualities"`
	}
	err := g.call(ctx, "/quality", "", gatewayRequest(input, data), &response)
	return response.Qualities, err
}
func (g *AssessmentGateway) Analyze(ctx context.Context, input AssessmentFrozenInput, data map[string][]byte, key string) (AssessmentAnalysis, string, error) {
	var response struct {
		Analysis          AssessmentAnalysis `json:"analysis"`
		ProviderRequestID string             `json:"providerRequestId"`
	}
	err := g.call(ctx, "/analyze", key, gatewayRequest(input, data), &response)
	return response.Analysis, response.ProviderRequestID, err
}
