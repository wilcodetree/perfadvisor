// Package todo is the one optional network feature of perfadvisor: it reads
// the user's own Microsoft To Do tasks via Microsoft Graph. It is off by
// default, requires an explicit interactive sign-in (device code flow), and
// its data is only ever shown live in the TUI, never written to reports.
package todo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Public client id used for the device-code sign-in. Defaults to the
// well-known "Microsoft Graph Command Line Tools" app; override with
// PERFADVISOR_CLIENT_ID if your tenant blocks it or IT provides a dedicated
// app registration.
const defaultClientID = "14d82eec-204b-4c2f-b7e8-296a70dab67e"

const (
	scope     = "Tasks.Read offline_access"
	deviceURL = "https://login.microsoftonline.com/common/oauth2/v2.0/devicecode"
	tokenURL  = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	graphBase = "https://graph.microsoft.com/v1.0"
)

func clientID() string {
	if v := os.Getenv("PERFADVISOR_CLIENT_ID"); v != "" {
		return v
	}
	return defaultClientID
}

var httpc = &http.Client{Timeout: 20 * time.Second}

type token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type tokenResp struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func cachePath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = home
	}
	dir := filepath.Join(base, "perfadvisor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "msgraph-token.json"), nil
}

func saveToken(tr tokenResp) error {
	p, err := cachePath()
	if err != nil {
		return err
	}
	t := token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn-60) * time.Second),
	}
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func loadToken() (*token, error) {
	p, err := cachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var t token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func postForm(u string, form url.Values, out any) error {
	resp, err := httpc.PostForm(u, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// Login runs the OAuth device-code flow interactively and caches the token.
func Login(w io.Writer) error {
	var dc struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
		Message         string `json:"message"`
	}
	form := url.Values{"client_id": {clientID()}, "scope": {scope}}
	if err := postForm(deviceURL, form, &dc); err != nil {
		return err
	}
	if dc.DeviceCode == "" {
		return errors.New("could not start device sign-in; your tenant may block this client id (set PERFADVISOR_CLIENT_ID)")
	}
	msg := dc.Message
	if msg == "" {
		msg = fmt.Sprintf("Open %s and enter the code %s", dc.VerificationURI, dc.UserCode)
	}
	fmt.Fprintln(w, msg)
	interval := dc.Interval
	if interval <= 0 {
		interval = 5
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		var tr tokenResp
		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {clientID()},
			"device_code": {dc.DeviceCode},
		}
		if err := postForm(tokenURL, form, &tr); err != nil {
			return err
		}
		if tr.AccessToken != "" {
			return saveToken(tr)
		}
		switch tr.Error {
		case "authorization_pending":
			// keep polling
		case "slow_down":
			interval += 5
		default:
			if tr.ErrorDescription != "" {
				return errors.New(tr.ErrorDescription)
			}
			return errors.New("sign-in failed: " + tr.Error)
		}
	}
	return errors.New("sign-in timed out")
}

// Logout removes the cached token.
func Logout() error {
	p, err := cachePath()
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func accessToken() (string, error) {
	t, err := loadToken()
	if err != nil {
		return "", errors.New("not signed in: run 'perfadvisor todo login' once")
	}
	if time.Now().Before(t.ExpiresAt) && t.AccessToken != "" {
		return t.AccessToken, nil
	}
	var tr tokenResp
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID()},
		"refresh_token": {t.RefreshToken},
		"scope":         {scope},
	}
	if err := postForm(tokenURL, form, &tr); err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", errors.New("sign-in expired: run 'perfadvisor todo login' again")
	}
	if err := saveToken(tr); err != nil {
		return "", err
	}
	return tr.AccessToken, nil
}

func get(u, tok string, out any) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Microsoft Graph returned %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Item is one task shown in the panel.
type Item struct {
	Title   string
	List    string
	DueDate string // yyyy-mm-dd, empty when the task has no due date
	Overdue bool
}

// TodayTasks returns open tasks due today or overdue, across all lists.
// Note: Graph does not expose To Do's "My Day", so due date is the criterion.
func TodayTasks() ([]Item, error) {
	tok, err := accessToken()
	if err != nil {
		return nil, err
	}
	var lists struct {
		Value []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := get(graphBase+"/me/todo/lists?$top=20", tok, &lists); err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	var out []Item
	for i, l := range lists.Value {
		if i >= 10 {
			break
		}
		var tasks struct {
			Value []struct {
				Title       string `json:"title"`
				Status      string `json:"status"`
				DueDateTime *struct {
					DateTime string `json:"dateTime"`
				} `json:"dueDateTime"`
			} `json:"value"`
		}
		u := graphBase + "/me/todo/lists/" + url.PathEscape(l.ID) +
			"/tasks?$top=100&$filter=" + url.QueryEscape("status ne 'completed'")
		if err := get(u, tok, &tasks); err != nil {
			continue // one broken list should not kill the panel
		}
		for _, t := range tasks.Value {
			if t.DueDateTime == nil || len(t.DueDateTime.DateTime) < 10 {
				continue
			}
			due := t.DueDateTime.DateTime[:10]
			if due > today {
				continue
			}
			out = append(out, Item{Title: t.Title, List: l.DisplayName, DueDate: due, Overdue: due < today})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Overdue != out[j].Overdue {
			return out[i].Overdue
		}
		return out[i].DueDate < out[j].DueDate
	})
	return out, nil
}
