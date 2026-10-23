package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var ErrRejected = errors.New("wechat credential rejected")

// Credentials, session keys and platform responses must never be logged.
type Client struct {
	AppID   string
	Secret  string
	HTTP    *http.Client
	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(appID, secret string) *Client {
	return &Client{AppID: appID, Secret: secret, HTTP: &http.Client{Timeout: 4 * time.Second}}
}

func (c *Client) call(ctx context.Context, method, endpoint string, body any, target any) error {
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.weixin.qq.com"+endpoint, payload)
	if err != nil {
		return errors.New("wechat request unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("wechat service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("wechat service unavailable")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(target); err != nil {
		return errors.New("wechat response invalid")
	}
	return nil
}

func (c *Client) Login(ctx context.Context, code string) (string, string, error) {
	q := url.Values{"appid": {c.AppID}, "secret": {c.Secret}, "js_code": {code}, "grant_type": {"authorization_code"}}
	var result struct {
		OpenID  string `json:"openid"`
		ErrCode int    `json:"errcode"`
	}
	if err := c.call(ctx, "GET", "/sns/jscode2session?"+q.Encode(), nil, &result); err != nil {
		return "", "", err
	}
	if result.ErrCode != 0 || result.OpenID == "" {
		return "", "", ErrRejected
	}
	return c.AppID, result.OpenID, nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	var result struct {
		Token     string `json:"access_token"`
		ExpiresIn int    `json:"expires_in"`
		ErrCode   int    `json:"errcode"`
	}
	err := c.call(ctx, "POST", "/cgi-bin/stable_token", map[string]any{"grant_type": "client_credential", "appid": c.AppID, "secret": c.Secret}, &result)
	if err != nil {
		return "", err
	}
	if result.ErrCode != 0 || result.Token == "" || result.ExpiresIn <= 120 {
		return "", ErrRejected
	}
	c.token, c.expires = result.Token, time.Now().Add(time.Duration(result.ExpiresIn-120)*time.Second)
	return c.token, nil
}

func (c *Client) Phone(ctx context.Context, code, openID string) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	var result struct {
		ErrCode   int `json:"errcode"`
		PhoneInfo struct {
			PurePhoneNumber string `json:"purePhoneNumber"`
			CountryCode     string `json:"countryCode"`
			Watermark       struct {
				AppID string `json:"appid"`
			} `json:"watermark"`
		} `json:"phone_info"`
	}
	err = c.call(ctx, "POST", "/wxa/business/getuserphonenumber?access_token="+url.QueryEscape(token), map[string]string{"code": code, "openid": openID}, &result)
	if err != nil {
		return "", err
	}
	if result.ErrCode != 0 || result.PhoneInfo.CountryCode != "86" || result.PhoneInfo.Watermark.AppID != c.AppID {
		return "", ErrRejected
	}
	return result.PhoneInfo.PurePhoneNumber, nil
}
