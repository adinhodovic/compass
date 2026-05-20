// Package dnssd implements Prometheus-style DNS service discovery for SRV
// records.
package dnssd

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
)

type Source struct {
	name      string
	names     []string
	tags      []string
	urlScheme string
	resolver  srvResolver
}

type srvResolver interface {
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
}

type netResolver struct{}

func (netResolver) LookupSRV(
	ctx context.Context,
	service, proto, name string,
) (string, []*net.SRV, error) {
	return net.DefaultResolver.LookupSRV(ctx, service, proto, name)
}

type fixedResolver struct {
	nameservers []string
	next        atomic.Uint64
}

func newFixedResolver(nameservers []string) *fixedResolver {
	return &fixedResolver{nameservers: append([]string(nil), nameservers...)}
}

func (r *fixedResolver) LookupSRV(
	ctx context.Context,
	service, proto, name string,
) (string, []*net.SRV, error) {
	resolver := net.Resolver{PreferGo: true, Dial: r.dial}
	return resolver.LookupSRV(ctx, service, proto, name)
}

func (r *fixedResolver) dial(ctx context.Context, network, _ string) (net.Conn, error) {
	if len(r.nameservers) == 0 {
		return nil, fmt.Errorf("no nameservers configured")
	}
	idx := int(r.next.Add(1)-1) % len(r.nameservers)
	return (&net.Dialer{}).DialContext(ctx, network, r.nameservers[idx])
}

func New(cfg config.SourceConfig) Source {
	name := cfg.Name
	if name == "" {
		name = compass.SourceTypeDNSSD
	}
	return Source{
		name:      name,
		names:     append([]string(nil), cfg.DNSSD.Names...),
		tags:      cfg.Tags,
		urlScheme: strings.TrimSpace(cfg.DNSSD.URLScheme),
		resolver:  resolverFor(cfg.DNSSD.Nameservers),
	}
}

func resolverFor(nameservers []string) srvResolver {
	nameservers = trimmedStrings(nameservers)
	if len(nameservers) > 0 {
		return newFixedResolver(nameservers)
	}
	return netResolver{}
}

func trimmedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (s Source) Name() string { return s.name }

func (s Source) Type() string { return compass.SourceTypeDNSSD }

func (s Source) Load(ctx context.Context) ([]compass.Service, error) {
	services := make([]compass.Service, 0)
	for _, rawName := range s.names {
		service, proto, name, ok := config.ParseDNSSDName(rawName)
		if !ok {
			return nil, fmt.Errorf("source %s: parse name %q: invalid DNS-SD name", s.name, rawName)
		}

		_, records, err := s.resolver.LookupSRV(ctx, service, proto, name)
		if err != nil {
			return nil, fmt.Errorf("source %s: lookup SRV %s: %w", s.name, rawName, err)
		}
		for _, record := range records {
			services = append(services, s.serviceFromRecord(rawName, service, proto, record))
		}
	}
	return services, nil
}

func (s Source) serviceFromRecord(rawName, service, proto string, record *net.SRV) compass.Service {
	target := strings.TrimSuffix(record.Target, ".")
	port := strconv.Itoa(int(record.Port))
	scheme := s.urlScheme
	if scheme == "" {
		scheme = schemeForService(service)
	}

	return compass.Service{
		ID:         "dns_sd/" + rawName + "/" + target + "/" + port,
		Name:       displayName(target),
		URL:        meta.WithScheme(net.JoinHostPort(target, port), scheme),
		Source:     s.name,
		SourceType: compass.SourceTypeDNSSD,
		Tags:       meta.MergeTags(s.tags, []string{service}),
		Metadata: map[string]any{
			"name":     rawName,
			"service":  service,
			"proto":    proto,
			"target":   target,
			"port":     record.Port,
			"priority": record.Priority,
			"weight":   record.Weight,
		},
	}
}

func schemeForService(service string) string {
	if strings.EqualFold(service, "https") {
		return "https"
	}
	return "http"
}

func displayName(target string) string {
	parts := strings.Split(strings.TrimSuffix(target, "."), ".")
	if len(parts) == 0 || parts[0] == "" {
		return target
	}
	return parts[0]
}
