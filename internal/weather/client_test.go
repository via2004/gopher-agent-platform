package weather

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const validProviderResponse = `{
  "current_condition": [{
    "temp_C": "23",
    "humidity": "68",
    "windspeedKmph": "12",
    "weatherDesc": [{"value": "晴"}]
  }],
  "nearest_area": [{"areaName": [{"value": "上海"}]}]
}`

func TestClientGet(t *testing.T) {
	var gotPath, gotEscapedPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotEscapedPath, gotQuery = r.URL.Path, r.URL.EscapedPath(), r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validProviderResponse))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Get(context.Background(), "上海")
	if err != nil {
		t.Fatal(err)
	}
	if result.Location != "上海" || result.TemperatureC != 23 || result.HumidityPercent != 68 || result.WindSpeedKMPH != 12 || result.Condition != "晴" {
		t.Fatalf("result = %#v", result)
	}
	if gotPath != "/上海" || gotEscapedPath != "/%E4%B8%8A%E6%B5%B7" || !strings.Contains(gotQuery, "format=j1") || !strings.Contains(gotQuery, "lang=zh") {
		t.Fatalf("request = path %q escaped path %q query %q", gotPath, gotEscapedPath, gotQuery)
	}
}

func TestClientGetRejectsInvalidInputAndResponse(t *testing.T) {
	tests := []struct {
		name    string
		city    string
		status  int
		body    string
		wantErr error
	}{
		{name: "empty city", wantErr: ErrInvalidCity},
		{name: "long city", city: strings.Repeat("中", maxCityLength+1), wantErr: ErrInvalidCity},
		{name: "provider status", city: "上海", status: http.StatusBadGateway, body: "bad", wantErr: ErrProviderFailed},
		{name: "invalid json", city: "上海", status: http.StatusOK, body: "{", wantErr: ErrResponseInvalid},
		{name: "invalid field", city: "上海", status: http.StatusOK, body: `{"current_condition":[{"temp_C":"bad","humidity":"68","windspeedKmph":"12","weatherDesc":[{"value":"晴"}]}]}`, wantErr: ErrResponseInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := NewClient(Config{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Get(context.Background(), test.city)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestClientGetPreservesContextCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Get(ctx, "上海")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not stop after cancellation")
	}
}
