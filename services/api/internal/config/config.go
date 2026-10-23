package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/mediaconfig"
)

type Config struct {
	Environment         string
	WechatAppID         string
	WechatAppSecret     string
	Address             string
	DSN                 string
	PendingConfirmation time.Duration
	MediaRoot           string
	Media               mediaconfig.Config
}

// Load keeps local demo and CloudBase container configuration separate.
func Load() (Config, error) {
	environment := os.Getenv("APP_ENV")
	if environment != "development" && environment != "cloudrun" && environment != "server" {
		return Config{}, errors.New("APP_ENV must be development, cloudrun or server")
	}
	if environment == "development" && os.Getenv("ENABLE_DEMO") != "true" {
		return Config{}, errors.New("local development requires ENABLE_DEMO=true")
	}
	if environment != "development" && os.Getenv("ENABLE_DEMO") != "false" {
		return Config{}, errors.New("hosted environments require ENABLE_DEMO=false")
	}
	for _, origin := range strings.Split(os.Getenv("ADMIN_ALLOWED_ORIGINS"), ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(origin, "*") {
			return Config{}, errors.New("ADMIN_ALLOWED_ORIGINS entries must be exact HTTPS origins without paths or wildcards")
		}
	}
	c := Config{Environment: environment, Address: os.Getenv("HTTP_ADDR"), DSN: os.Getenv("DB_DSN"), PendingConfirmation: 120 * time.Minute, MediaRoot: os.Getenv("MEDIA_ROOT")}
	c.WechatAppID, c.WechatAppSecret = strings.TrimSpace(os.Getenv("WECHAT_APP_ID")), strings.TrimSpace(os.Getenv("WECHAT_APP_SECRET"))
	if (c.WechatAppID == "") != (c.WechatAppSecret == "") {
		return Config{}, errors.New("WECHAT_APP_ID and WECHAT_APP_SECRET must be configured together")
	}
	if raw := os.Getenv("PENDING_CONFIRMATION_MINUTES"); raw != "" {
		minutes, err := strconv.Atoi(raw)
		if err != nil || minutes < 1 || minutes > 1440 {
			return Config{}, errors.New("PENDING_CONFIRMATION_MINUTES must be between 1 and 1440")
		}
		c.PendingConfirmation = time.Duration(minutes) * time.Minute
	}
	if c.Address == "" {
		c.Address = "127.0.0.1:8080"
		if environment == "cloudrun" {
			c.Address = "0.0.0.0:8080"
		}
	}
	host, _, err := net.SplitHostPort(c.Address)
	if environment != "cloudrun" && (err != nil || (host != "localhost" && host != "127.0.0.1" && host != "::1")) {
		return Config{}, errors.New("HTTP_ADDR must bind to localhost behind the server reverse proxy")
	}
	if environment == "cloudrun" && c.Address != "0.0.0.0:8080" {
		return Config{}, errors.New("cloudrun requires HTTP_ADDR=0.0.0.0:8080")
	}
	if strings.TrimSpace(c.DSN) == "" {
		return Config{}, errors.New("DB_DSN is required")
	}
	if c.MediaRoot == "" {
		c.MediaRoot = "../../.local/private-media"
	}
	c.Media, err = mediaconfig.Load()
	if err != nil {
		return Config{}, err
	}
	if environment == "cloudrun" && c.Media.Driver != "cos" {
		return Config{}, errors.New("cloudrun requires persistent COS media")
	}
	return c, nil
}
