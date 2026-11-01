package config

import (
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
				Type:       StatusTypePrometheus,
				Name:       StatusTypePrometheus,
				URL:        "http://prom:9090/",
				Query:      "probe_success",
				MatchLabel: "instance",
				Endpoint:   "http://prom:9090/api/v1/query?query=probe_success",
				Interval:   "30s",
				MatchOn:    StatusMatchHost,
				Mapping: StatusMappingConfig{
					ItemsPath: "data.result",
					Match:     "metric.instance",
					Up:        "value.1",
					UpValue:   "1",
				},
			},
		},
		{
			name: "prometheus uptime kuma recipe escapes query",
			in: StatusConfig{
				Type:       "Prometheus",
				Name:       "kuma",
				URL:        "http://prom:9090",
				Query:      `monitor_status{job="kuma"}`,
				MatchLabel: "monitor_url",
			},
			want: StatusConfig{
				Type:       StatusTypePrometheus,
				Name:       "kuma",
				URL:        "http://prom:9090",
				Query:      `monitor_status{job="kuma"}`,
				MatchLabel: "monitor_url",
				Endpoint:   "http://prom:9090/api/v1/query?query=monitor_status%7Bjob%3D%22kuma%22%7D",
				Interval:   "30s",
				MatchOn:    StatusMatchHost,
				Mapping: StatusMappingConfig{
					ItemsPath: "data.result",
					Match:     "metric.monitor_url",
					Up:        "value.1",
					UpValue:   "1",
				},
			},
		},
		{
			name: "gatus defaults",
			in:   StatusConfig{Type: "gatus", URL: "http://gatus:8080"},
			want: StatusConfig{
				Type:     StatusTypeGatus,
				Name:     StatusTypeGatus,
				URL:      "http://gatus:8080",
				Endpoint: "http://gatus:8080/api/v1/endpoints/statuses",
				Interval: "30s",
				MatchOn:  StatusMatchHost,
				Mapping: StatusMappingConfig{
					Match: "results.@reverse.0.hostname",
					Up:    "results.@reverse.0.success",
				},
			},
		},
		{
			name: "preset fields are overridable",
			in: StatusConfig{
				Type:     "gatus",
				URL:      "http://gatus:8080",
				Interval: "1m",
				MatchOn:  "name",
				Mapping:  StatusMappingConfig{Match: "name"},
			},
			want: StatusConfig{
				Type:     StatusTypeGatus,
				Name:     StatusTypeGatus,
				URL:      "http://gatus:8080",
				Endpoint: "http://gatus:8080/api/v1/endpoints/statuses",
				Interval: "1m",
				MatchOn:  StatusMatchName,
				Mapping: StatusMappingConfig{
					Match: "name",
					Up:    "results.@reverse.0.success",
				},
			},
		},
		{
			name: "json is left as written",
			in: StatusConfig{
				Endpoint: "http://health/checks",
				Mapping:  StatusMappingConfig{Match: "host", Up: "ok"},
			},
			want: StatusConfig{
				Type:     StatusTypeJSON,
				Name:     StatusTypeJSON,
				Endpoint: "http://health/checks",
				Interval: "30s",
				MatchOn:  StatusMatchHost,
				Mapping:  StatusMappingConfig{Match: "host", Up: "ok"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in
			setStatusDefaults(&got)
			if got.Name != tt.want.Name || got.Type != tt.want.Type || got.URL != tt.want.URL ||
				got.Query != tt.want.Query || got.MatchLabel != tt.want.MatchLabel ||
				got.Endpoint != tt.want.Endpoint || got.Interval != tt.want.Interval ||
				got.MatchOn != tt.want.MatchOn || got.Mapping != tt.want.Mapping {
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
		{
			name: "valid prometheus",
			status: `
  - type: prometheus
    url: http://prom:9090`,
		},
		{
			name: "unknown type",
			status: `
  - type: nagios
    url: http://nagios`,
			wantErr: "type must be json, prometheus, or gatus",
		},
		{
			name: "json needs endpoint",
			status: `
  - type: json
    mapping: {match: host, up: ok}`,
			wantErr: "endpoint is required for type json",
		},
		{
			name: "preset needs url",
			status: `
  - type: gatus`,
			wantErr: "url is required for type gatus",
		},
		{
			name: "endpoint must be http",
			status: `
  - type: json
    endpoint: ftp://health
    mapping: {match: host, up: ok}`,
			wantErr: "endpoint must be an http(s) URL",
		},
		{
			name: "duplicate names",
			status: `
  - type: prometheus
    url: http://prom-a:9090
  - type: prometheus
    url: http://prom-b:9090`,
			wantErr: `duplicate status name "prometheus"`,
		},
		{
			name: "bad interval",
			status: `
  - type: prometheus
    url: http://prom:9090
    interval: soon`,
			wantErr: "interval must be a positive duration",
		},
		{
			name: "zero interval",
			status: `
  - type: prometheus
    url: http://prom:9090
    interval: 0s`,
			wantErr: "interval must be a positive duration",
		},
		{
			name: "bad match_on",
			status: `
  - type: prometheus
    url: http://prom:9090
    match_on: ip`,
			wantErr: "match_on must be host, url, name, or id",
		},
		{
			name: "bad items_mode",
			status: `
  - type: json
    endpoint: http://health
    mapping: {items_mode: keys, match: host, up: ok}`,
			wantErr: "mapping.items_mode must be array or values",
		},
		{
			name: "json needs mapping.match",
			status: `
  - type: json
    endpoint: http://health
    mapping: {up: ok}`,
			wantErr: "mapping.match is required",
		},
		{
			name: "json needs mapping.up",
			status: `
  - type: json
    endpoint: http://health
    mapping: {match: host}`,
			wantErr: "mapping.up is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, statusTestSources+"status:"+tt.status+"\n")
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
