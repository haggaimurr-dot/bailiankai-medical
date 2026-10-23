package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
}

func TestPlatformVerification(t *testing.T) {
	client := New("test-app", "server-secret")
	tokenCalls := 0
	client.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.weixin.qq.com" {
			t.Fatal("unexpected platform endpoint")
		}
		switch r.URL.Path {
		case "/sns/jscode2session":
			q := r.URL.Query()
			if q.Get("appid") != "test-app" || q.Get("secret") != "server-secret" || q.Get("js_code") != "login-code" {
				t.Fatal("invalid login request")
			}
			return jsonResponse(`{"openid":"verified-user","session_key":"never-exposed"}`), nil
		case "/cgi-bin/stable_token":
			tokenCalls++
			return jsonResponse(`{"access_token":"platform-token","expires_in":7200}`), nil
		case "/wxa/business/getuserphonenumber":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["code"] != "phone-code" || body["openid"] != "verified-user" || r.URL.Query().Get("access_token") != "platform-token" {
				t.Fatal("phone request is not bound to verified user")
			}
			return jsonResponse(`{"errcode":0,"phone_info":{"purePhoneNumber":"13900000000","countryCode":"86","watermark":{"appid":"test-app"}}}`), nil
		default:
			t.Fatal("unexpected request")
		}
		return nil, nil
	})
	appID, openID, err := client.Login(context.Background(), "login-code")
	if err != nil || appID != "test-app" || openID != "verified-user" {
		t.Fatal(appID, openID, err)
	}
	for i := 0; i < 2; i++ {
		phone, err := client.Phone(context.Background(), "phone-code", openID)
		if err != nil || phone != "13900000000" {
			t.Fatal(phone, err)
		}
	}
	if tokenCalls != 1 {
		t.Fatal("platform token was not cached")
	}
}

func TestRejectedResponsesAndSecretRedaction(t *testing.T) {
	for _, body := range []string{`{"errcode":40029}`, `{}`, `{"openid":"forged","errcode":40029}`} {
		client := New("app", "secret")
		client.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return jsonResponse(body), nil })
		if _, _, err := client.Login(context.Background(), "code"); !errors.Is(err, ErrRejected) {
			t.Fatal("invalid platform response accepted", err)
		}
	}
	client := New("app", "server-secret")
	client.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New(r.URL.String()) })
	if _, _, err := client.Login(context.Background(), "sensitive-code"); err == nil || strings.Contains(err.Error(), "server-secret") || strings.Contains(err.Error(), "sensitive-code") {
		t.Fatal("request error leaked credentials", err)
	}
}

func TestPhoneRejectsWrongAppAndUnsupportedNumber(t *testing.T) {
	for _, body := range []string{
		`{"errcode":40029}`,
		`{"phone_info":{"purePhoneNumber":"13900000000","countryCode":"86","watermark":{"appid":"other-app"}}}`,
		`{"phone_info":{"purePhoneNumber":"13900000000","countryCode":"1","watermark":{"appid":"app"}}}`,
	} {
		client := New("app", "secret")
		client.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/cgi-bin/stable_token" {
				return jsonResponse(`{"access_token":"token","expires_in":7200}`), nil
			}
			return jsonResponse(body), nil
		})
		if _, err := client.Phone(context.Background(), "code", "user"); !errors.Is(err, ErrRejected) {
			t.Fatal("invalid phone grant accepted", err)
		}
	}
}
