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
	"sync"
	"time"
)

// Gateway failure handling: after gatewayBreakerThreshold consecutive
// transport/5xx failures the breaker opens for gatewayBreakerCooldown and
// calls fail fast with MODEL_UNAVAILABLE (mapped to 503
// "评估服务暂不可用，请稍后重试") instead of waiting on a dead gateway.
// When the cooldown expires the next call first probes /healthz; only a
// healthy gateway closes the breaker. Per-operation timeouts keep quality
// checks snappy while leaving room for a vision model call in analyze.
const (
	gatewayBreakerThreshold = 3
	gatewayBreakerCooldown  = 60 * time.Second
	gatewayHealthTimeout    = 5 * time.Second
	gatewayQualityTimeout   = 30 * time.Second
	gatewayAnalyzeTimeout   = 150 * time.Second
)

// Gateway protocol v1 accepts normalized private image bytes and returns the
// documented quality/result schemas. Vendor-specific model adapters live behind
// this explicitly configured service; no deployment is silently simulated.
type AssessmentGateway struct {
	endpoint  string
	key       string
	client    *http.Client
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

// allow reports whether a gateway call may proceed. A closed breaker allows
// immediately; an open breaker whose cooldown expired is half-open probed via
// /healthz, and only a healthy gateway closes it.
func (g *AssessmentGateway) allow(ctx context.Context) bool {
	g.mu.Lock()
	closed := g.failures < gatewayBreakerThreshold || time.Now().After(g.openUntil)
	if closed && g.failures >= gatewayBreakerThreshold {
		// Cooldown expired: probe health before allowing traffic again.
		g.mu.Unlock()
		healthy := g.Health(ctx)
		g.mu.Lock()
		if healthy {
			g.failures = 0
			g.openUntil = time.Time{}
		} else {
			g.openUntil = time.Now().Add(gatewayBreakerCooldown)
		}
	}
	open := g.failures >= gatewayBreakerThreshold && time.Now().Before(g.openUntil)
	g.mu.Unlock()
	return !open
}

func (g *AssessmentGateway) recordFailure() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures++
	if g.failures >= gatewayBreakerThreshold {
		g.openUntil = time.Now().Add(gatewayBreakerCooldown)
	}
}

func (g *AssessmentGateway) recordSuccess() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures = 0
	g.openUntil = time.Time{}
}

// Health probes the gateway /healthz endpoint with a short timeout.
func (g *AssessmentGateway) Health(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, gatewayHealthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, g.endpoint+"/healthz", nil)
	if err != nil {
		return false
	}
	res, err := g.client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode >= 200 && res.StatusCode < 300
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
	if !g.allow(ctx) {
		return &AssessmentProviderError{Code: "MODEL_UNAVAILABLE", Retryable: true}
	}
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
		g.recordFailure()
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
		g.recordFailure()
		return &AssessmentProviderError{Code: "MODEL_UNAVAILABLE", Retryable: true}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &AssessmentProviderError{Code: "MODEL_CONFIGURATION_ERROR"}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || DecodeAssessmentJSON(body, response) != nil {
		return &AssessmentProviderError{Code: "MODEL_OUTPUT_INVALID"}
	}
	g.recordSuccess()
	return nil
}
func (g *AssessmentGateway) Check(ctx context.Context, input AssessmentFrozenInput, data map[string][]byte) (map[string]AssessmentQuality, error) {
	var response struct {
		Qualities map[string]AssessmentQuality `json:"qualities"`
	}
	checkCtx, cancel := context.WithTimeout(ctx, gatewayQualityTimeout)
	defer cancel()
	err := g.call(checkCtx, "/quality", "", gatewayRequest(input, data), &response)
	return response.Qualities, err
}
func (g *AssessmentGateway) Analyze(ctx context.Context, input AssessmentFrozenInput, data map[string][]byte, key string) (AssessmentAnalysis, string, error) {
	var response struct {
		Analysis          AssessmentAnalysis `json:"analysis"`
		ProviderRequestID string             `json:"providerRequestId"`
	}
	analyzeCtx, cancel := context.WithTimeout(ctx, gatewayAnalyzeTimeout)
	defer cancel()
	err := g.call(analyzeCtx, "/analyze", key, gatewayRequest(input, data), &response)
	return response.Analysis, response.ProviderRequestID, err
}
