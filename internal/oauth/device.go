// Package oauth implements the OAuth 2.0 device authorization grant, so the CLI
// can acquire tokens for integrations without pasting long-lived secrets.
package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceCode is the device-authorization response.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

// Config points at an OAuth provider's device + token endpoints.
type Config struct {
	ClientID   string
	DeviceURL  string
	TokenURL   string
	Scope      string
	HTTPClient *http.Client
	// Now/Sleep are injectable for testing; nil uses real time.
	Sleep func(time.Duration)
}

func (c Config) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// RequestDeviceCode starts the device flow, returning the codes to show the user.
func (c Config) RequestDeviceCode() (*DeviceCode, error) {
	form := url.Values{"client_id": {c.ClientID}, "scope": {c.Scope}}
	resp, err := c.client().PostForm(c.DeviceURL, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("device code request returned %d", resp.StatusCode)
	}
	var dc DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, err
	}
	if dc.Interval == 0 {
		dc.Interval = 5
	}
	return &dc, nil
}

// PollToken polls the token endpoint until the user authorizes (returning the
// access token) or maxAttempts is reached. "authorization_pending" is retried;
// other errors abort.
func (c Config) PollToken(dc *DeviceCode, maxAttempts int) (string, error) {
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	form := url.Values{
		"client_id":   {c.ClientID},
		"device_code": {dc.DeviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	for i := 0; i < maxAttempts; i++ {
		resp, err := c.client().PostForm(c.TokenURL, form)
		if err != nil {
			return "", err
		}
		var body struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if body.AccessToken != "" {
			return body.AccessToken, nil
		}
		if body.Error != "" && !strings.Contains(body.Error, "pending") && !strings.Contains(body.Error, "slow_down") {
			return "", fmt.Errorf("device flow error: %s", body.Error)
		}
		sleep(time.Duration(dc.Interval) * time.Second)
	}
	return "", fmt.Errorf("device flow timed out after %d attempts", maxAttempts)
}
