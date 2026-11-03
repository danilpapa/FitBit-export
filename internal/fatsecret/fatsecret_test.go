package fatsecret

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"fitbit-export/internal/config"
)

const (
	consumerSecret = "consumer-secret"
	accessSecret   = "access-secret"
)

func testClient(server *httptest.Server) *Client {
	client := New(config.FatSecret{
		ConsumerKey:    "consumer-key",
		ConsumerSecret: consumerSecret,
		AccessToken:    "access-token",
		AccessSecret:   accessSecret,
	})
	client.urls = endpoints{
		api:          server.URL + "/rest/server.api",
		requestToken: server.URL + "/oauth/request_token",
		authorize:    server.URL + "/oauth/authorize",
		accessToken:  server.URL + "/oauth/access_token",
	}
	return client
}

// verify checks the OAuth 1.0 signature the way the server does.
func verify(t *testing.T, request *http.Request, tokenSecret string) url.Values {
	t.Helper()

	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}
	params := url.Values{}
	for name, values := range request.Form {
		params[name] = slices.Clone(values)
	}
	got := params.Get("oauth_signature")
	params.Del("oauth_signature")

	endpoint := "http://" + request.Host + request.URL.Path
	if want := signature(request.Method, endpoint, params, consumerSecret, tokenSecret); got != want {
		t.Errorf("%s signature = %q, want %q", request.URL.Path, got, want)
	}
	return params
}

func TestSignatureMatchesOAuthSpecExample(t *testing.T) {
	params := url.Values{
		"file":                   {"vacation.jpg"},
		"size":                   {"original"},
		"oauth_consumer_key":     {"dpf43f3p2l4k3l03"},
		"oauth_token":            {"nnch734d00sl2jdk"},
		"oauth_signature_method": {"HMAC-SHA1"},
		"oauth_timestamp":        {"1191242096"},
		"oauth_nonce":            {"kllo9940pd9333jh"},
		"oauth_version":          {"1.0"},
	}

	got := signature(http.MethodGet, "http://photos.example.net/photos", params, "kd94hf93k423kf44", "pfkkdhi9sl3r4s00")
	if want := "tR3+Ty81lMeYAr/Fid0kMTYa/WM="; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestEncodeUsesRFC3986(t *testing.T) {
	if got, want := encode("a b+c~*é"), "a%20b%2Bc~%2A%C3%A9"; got != want {
		t.Fatalf("encode() = %q, want %q", got, want)
	}
}

func TestDailyTotalsReadsEveryMonthInRange(t *testing.T) {
	var dates []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := verify(t, r, accessSecret)
		if params.Get("method") != monthMethod || params.Get("format") != "json" || params.Get("oauth_token") != "access-token" {
			t.Errorf("params = %v", params)
		}
		dates = append(dates, params.Get("date"))

		// 20392 is 2025-10-31, 20393 is 2025-11-01.
		if params.Get("date") == "20362" {
			fmt.Fprint(w, `{"month":{"day":[
				{"date_int":"20380","calories":"1500","protein":"90","carbohydrate":"150","fat":"50"},
				{"date_int":"20392","calories":"2400.5","protein":"150.6","carbohydrate":"250","fat":"70.2"}
			],"from_date_int":"20362","to_date_int":"20392"}}`)
			return
		}
		fmt.Fprint(w, `{"month":{"day":{"date_int":"20393","calories":2000,"protein":120,"carbohydrate":200,"fat":60}}}`)
	}))
	defer server.Close()

	moscow := time.FixedZone("MSK", 3*60*60)
	start := time.Date(2025, time.October, 25, 0, 0, 0, 0, moscow)
	end := time.Date(2025, time.November, 2, 0, 0, 0, 0, moscow)

	days, err := testClient(server).DailyTotals(context.Background(), start, end)
	if err != nil {
		t.Fatalf("DailyTotals() error = %v", err)
	}

	if want := []string{"20362", "20393"}; !slices.Equal(dates, want) {
		t.Fatalf("requested months = %v, want %v", dates, want)
	}
	want := []Day{
		{Date: "2025-10-31", CaloriesKcal: 2400.5, ProteinG: 150.6, CarbsG: 250, FatG: 70.2},
		{Date: "2025-11-01", CaloriesKcal: 2000, ProteinG: 120, CarbsG: 200, FatG: 60},
	}
	if !slices.Equal(days, want) {
		t.Fatalf("days = %+v, want %+v", days, want)
	}
}

func TestDailyTotalsHandlesEmptyMonth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"month":{"from_date_int":"20393","to_date_int":"20422"}}`)
	}))
	defer server.Close()

	day := time.Date(2025, time.November, 2, 0, 0, 0, 0, time.UTC)
	days, err := testClient(server).DailyTotals(context.Background(), day, day.AddDate(0, 0, 1))
	if err != nil || len(days) != 0 {
		t.Fatalf("DailyTotals() = %v, %v, want no days", days, err)
	}
}

func TestDailyTotalsReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":{"code":"21","message":"Invalid IP address detected: '1.2.3.4'"}}`)
	}))
	defer server.Close()

	day := time.Date(2025, time.November, 2, 0, 0, 0, 0, time.UTC)
	_, err := testClient(server).DailyTotals(context.Background(), day, day.AddDate(0, 0, 1))
	if err == nil || !strings.Contains(err.Error(), "error 21: Invalid IP address") {
		t.Fatalf("DailyTotals() error = %v, want the FatSecret error", err)
	}
}

func TestLoginExchangesVerifierForAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/request_token":
			if r.Method != http.MethodPost {
				t.Errorf("request token method = %s", r.Method)
			}
			params := verify(t, r, "")
			if params.Get("oauth_callback") != "oob" || params.Has("oauth_token") {
				t.Errorf("request token params = %v", params)
			}
			fmt.Fprint(w, "oauth_token=request-token&oauth_token_secret=request-secret&oauth_callback_confirmed=true")
		case "/oauth/access_token":
			params := verify(t, r, "request-secret")
			if params.Get("oauth_token") != "request-token" || params.Get("oauth_verifier") != "123456" {
				t.Errorf("access token params = %v", params)
			}
			fmt.Fprint(w, "oauth_token=user-token&oauth_token_secret=user-secret")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var opened string
	open := func(target string) error {
		opened = target
		return nil
	}

	var out bytes.Buffer
	token, secret, err := testClient(server).Login(context.Background(), open, strings.NewReader(" 123456 \n"), &out)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token != "user-token" || secret != "user-secret" {
		t.Fatalf("Login() = %q, %q", token, secret)
	}
	if opened != server.URL+"/oauth/authorize?oauth_token=request-token" {
		t.Fatalf("opened %q", opened)
	}
}

func TestLoginReportsRejectedRequestToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Invalid consumer key", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, _, err := testClient(server).Login(context.Background(), func(string) error { return nil }, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401: Invalid consumer key") {
		t.Fatalf("Login() error = %v", err)
	}
}
