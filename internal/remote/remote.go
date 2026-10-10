// Package remote reads what Proclaim shows on air through the local server
// that Proclaim's own Remote app uses. It must be enabled in Proclaim under
// Settings, Remote, Local server.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultURL is Proclaim's local server on this computer.
const DefaultURL = "http://127.0.0.1:52195"

// ErrOffAir means no presentation is on air.
var ErrOffAir = errors.New("no presentation is on air")

// Client talks to one Proclaim.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{}}
}

// Status is what is on screen.
type Status struct {
	PresentationID string `json:"presentationId"`
	LocalRevision  int64  `json:"presentationLocalRevision"`
	Status         struct {
		Revision    int    `json:"revision"`
		ItemID      string `json:"itemId"`
		SlideIndex  int    `json:"slideIndex"`
		QuickScreen string `json:"quickScreen,omitempty"`
		MediaState  string `json:"mediaState,omitempty"`
	} `json:"status"`
}

// Item is one service item of the on-air presentation.
type Item struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	Slides []struct {
		Index int `json:"index"`
	} `json:"slides"`
}

// Session returns the on-air session id, or ErrOffAir.
func (c *Client) Session(ctx context.Context) (string, error) {
	body, code, err := c.get(ctx, "/onair/session", "", 5*time.Second)
	if err != nil {
		return "", err
	}
	if code == http.StatusNoContent {
		return "", ErrOffAir
	}
	if code != http.StatusOK {
		return "", fmt.Errorf("onair/session: HTTP %d", code)
	}
	// The id comes as a JSON string, but accept it bare too.
	id := strings.Trim(strings.TrimSpace(string(body)), `"`)
	if id == "" {
		return "", fmt.Errorf("onair/session: empty answer while on air")
	}
	return id, nil
}

// Items lists the service items of the on-air presentation.
func (c *Client) Items(ctx context.Context, session string) ([]Item, error) {
	body, code, err := c.get(ctx, "/presentations/onair", session, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, ErrOffAir
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("presentations/onair: HTTP %d", code)
	}
	var p struct {
		ServiceItems []Item `json:"serviceItems"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("presentations/onair: %w", err)
	}
	return p.ServiceItems, nil
}

// WaitForChange returns the status once it differs from last. With last nil
// it returns the current status at once. Proclaim holds the request open
// until something changes, so this is a long poll.
func (c *Client) WaitForChange(ctx context.Context, session string, last *Status) (Status, error) {
	path := "/onair/statusChanged"
	if last != nil {
		path += "?" + url.Values{
			"localrevision": {strconv.FormatInt(last.LocalRevision, 10)},
			"step":          {strconv.Itoa(last.Status.Revision)},
		}.Encode()
	}
	body, code, err := c.get(ctx, path, session, 2*time.Minute)
	if err != nil {
		return Status{}, err
	}
	if code == http.StatusNotFound {
		return Status{}, ErrOffAir
	}
	if code != http.StatusOK {
		return Status{}, fmt.Errorf("onair/statusChanged: HTTP %d", code)
	}
	var s Status
	if err := json.Unmarshal(body, &s); err != nil {
		return Status{}, fmt.Errorf("onair/statusChanged: %w", err)
	}
	return s, nil
}

func (c *Client) get(ctx context.Context, path, session string, timeout time.Duration) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if session != "" {
		req.Header.Set("OnAirSessionId", session)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("reach Proclaim's local server (is it enabled under Settings, Remote?): %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}
