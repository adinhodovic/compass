package config

import (
	"reflect"
	"strings"
	"testing"
)

const statusTestSources = `
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`

func TestSetStatusDefaultsExpandsPresets(t *testing.T) {
	tests := []struct {
		name string
		in   StatusConfig
		want StatusConfig
	}{
		{
			name: "prometheus defaults",
			in:   StatusConfig{Type: "prometheus", URL: "http://prom:9090/"},
			want: StatusConfig{
				Type:     StatusTypePrometheus,
				Name:     StatusTypePrometheus,
				URL:      "http://prom:9090/",
				Query:    "probe_success",
				Interval: "30s",
				Items:    "data.result",
				Service:  "metric.instance",
				Up:       "value.1",
				Endpoint: "http://prom:9090/api/v1/query?query=probe_success",
			},
		},
		{
			name: "prometheus uptime kuma recipe escapes query",
			in: StatusConfig{
				Type:    "Prometheus",
				Name:    "kuma",
				URL:     "http://prom:9090",
				Query:   `monitor_status{job="kuma"}`,
				Service: "metric.monitor_url",
			},
			want: StatusConfig{
				Type:     StatusTypePrometheus,
				Name:     "kuma",
				URL:      "http://prom:9090",
				Query:    `monitor_status{job="kuma"}`,
				Interval: "30s",
				Items:    "data.result",
				Service:  "metric.monitor_url",
				Up:       "value.1",
				Endpoint: "http://prom:9090/api/v1/query?query=monitor_status%7Bjob%3D%22kuma%22%7D",
			},
		},
		{
			name: "gatus defaults",
			in:   StatusConfig{Type: "gatus", URL: "http://gatus:8080", Interval: "1m"},
			want: StatusConfig{
				Type:     StatusTypeGatus,
				Name:     StatusTypeGatus,
				URL:      "http://gatus:8080",
				Interval: "1m",
				Service:  "results.@reverse.0.hostname",
				Up:       "results.@reverse.0.success",
				Endpoint: "http://gatus:8080/api/v1/endpoints/statuses",
			},
		},
		{
			name: "json fetches url as-is",
			in:   StatusConfig{URL: "http://health/checks", Service: "host", Up: "ok"},
			want: StatusConfig{
				Type:     StatusTypeJSON,
				Name:     StatusTypeJSON,
				URL:      "http://health/checks",
				Interval: "30s",
				Service:  "host",
				Up:       "ok",
				Endpoint: "http://health/checks",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in
			setStatusDefaults(&got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestLoadValidatesStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		wantErr string
	}{
		{name: "valid prometheus", status: `{type: prometheus, url: "http://prom:9090"}`},
		{
			name:    "unknown type",
			status:  `{type: nagios, url: "http://nagios"}`,
			wantErr: "type must be json, prometheus, or gatus",
		},
		{name: "url required", status: `{type: gatus}`, wantErr: "url must be an http(s) URL"},
		{
			name:    "url must be http",
			status:  `{url: "ftp://health", service: host, up: ok}`,
			wantErr: "url must be an http(s) URL",
		},
		{
			name: "duplicate names",
			status: `{type: prometheus, url: "http://prom-a:9090"},
  {type: prometheus, url: "http://prom-b:9090"}`,
			wantErr: `duplicate status name "prometheus"`,
		},
		{
			name:    "bad interval",
			status:  `{type: prometheus, url: "http://prom:9090", interval: soon}`,
			wantErr: "interval must be a positive duration",
		},
		{
			name:    "zero interval",
			status:  `{type: prometheus, url: "http://prom:9090", interval: 0s}`,
			wantErr: "interval must be a positive duration",
		},
		{
			name:    "json needs service",
			status:  `{url: "http://health", up: ok}`,
			wantErr: "service is required for type json",
		},
		{
			name:    "json needs up",
			status:  `{url: "http://health", service: host}`,
			wantErr: "up is required for type json",
		},
		{
			name:    "removed fields are rejected",
			status:  `{type: prometheus, url: "http://prom:9090", match_on: host}`,
			wantErr: "field match_on not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, statusTestSources+"status: ["+tt.status+"]\n")
			_, err := Load(path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
