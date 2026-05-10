package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const allNamespaces = ""

type Source struct {
	name            string
	tags            []string
	namespaces      []string
	autoDiscoverAll bool
	routes          []routeResource
	dynamic         dynamic.Interface
}

type routeResource struct {
	group    string
	version  string
	resource string
	kind     string
	protocol string
	scheme   string
}

func New(cfg config.SourceConfig) (Source, error) {
	name := cfg.Name
	if name == "" {
		name = compass.SourceTypeKubernetes
	}

	restConfig, credSource, err := restConfig(cfg.Kubernetes)
	if err != nil {
		return Source{}, fmt.Errorf("source %s: %w", name, err)
	}
	slog.Debug("kubernetes credentials resolved", "source", name, "from", credSource)
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return Source{}, fmt.Errorf("source %s: dynamic client: %w", name, err)
	}

	return Source{
		name:            name,
		tags:            cfg.Tags,
		namespaces:      cfg.Kubernetes.Namespaces,
		autoDiscoverAll: meta.DefaultBool(cfg.Kubernetes.AutoDiscoverAll, true),
		routes:          defaultRouteResources(),
		dynamic:         dynamicClient,
	}, nil
}

func newWithClient(
	name string,
	namespaces []string,
	dynamicClient dynamic.Interface,
) Source {
	return Source{
		name:            name,
		namespaces:      namespaces,
		autoDiscoverAll: true,
		routes:          defaultRouteResources(),
		dynamic:         dynamicClient,
	}
}

func (s Source) Name() string {
	return s.name
}

func (s Source) Type() string {
	return compass.SourceTypeKubernetes
}

func (s Source) LogAttributes() []slog.Attr {
	return []slog.Attr{
		slog.String("cluster", s.name),
		slog.String("namespaces", strings.Join(namespaceNames(s.namespaces), ",")),
		slog.String("resources", strings.Join(routeResourceNames(s.routes), ",")),
	}
}

func (s Source) Load(ctx context.Context) ([]compass.Service, error) {
	namespaces := s.namespaces
	if len(namespaces) == 0 {
		namespaces = []string{allNamespaces}
	}

	var services []compass.Service
	for _, route := range s.routes {
		for _, namespace := range namespaces {
			loaded, err := s.loadRoute(ctx, namespace, route)
			if err != nil {
				return nil, err
			}
			services = append(services, loaded...)
		}
	}

	return services, nil
}

func namespaceNames(namespaces []string) []string {
	if len(namespaces) == 0 {
		return []string{"all"}
	}

	return namespaces
}

func routeResourceNames(routes []routeResource) []string {
	names := make([]string, 0, len(routes))
	for _, route := range routes {
		if route.group == "" {
			names = append(names, route.resource+"/"+route.version)
			continue
		}
		names = append(names, route.resource+"."+route.group+"/"+route.version)
	}

	return names
}

func (s Source) loadRoute(
	ctx context.Context,
	namespace string,
	route routeResource,
) ([]compass.Service, error) {
	gvr := schema.GroupVersionResource{
		Group:    route.group,
		Version:  route.version,
		Resource: route.resource,
	}
	list, err := s.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf(
			"source %s: list %s.%s/%s: %w",
			s.name,
			route.resource,
			route.group,
			route.version,
			err,
		)
	}

	services := make([]compass.Service, 0, len(list.Items))
	for _, item := range list.Items {
		if !resourceEnabled(item, s.autoDiscoverAll) {
			continue
		}
		endpoints := routeEndpoints(item, route)
		if len(endpoints) == 0 {
			continue
		}
		for _, endpoint := range endpoints {
			svc := serviceFromRoute(s.name, item, route, endpoint)
			svc.Tags = meta.MergeTags(s.tags, svc.Tags)
			services = append(services, svc)
		}
	}

	return services, nil
}

func resourceEnabled(item unstructured.Unstructured, autoDiscoverAll bool) bool {
	return meta.EnabledByLabel(item.GetLabels(), autoDiscoverAll)
}

type resourceEndpoint struct {
	URL       string
	Hostname  string
	Hostnames []string
	// Title overrides the derived service name when set (multi-URL annotation
	// entries may carry an explicit "Title=URL" name).
	Title string
	// IDSuffix is appended to the registry ID when a single resource emits
	// multiple cards (multi-URL annotation fan-out). Empty for the default
	// single-card path.
	IDSuffix string
}

func defaultRouteResources() []routeResource {
	return []routeResource{
		{
			group:    "gateway.networking.k8s.io",
			version:  "v1",
			resource: "httproutes",
			kind:     "HTTPRoute",
			protocol: "http",
			scheme:   "https",
		},
		{
			group:    "gateway.networking.k8s.io",
			version:  "v1",
			resource: "grpcroutes",
			kind:     "GRPCRoute",
			protocol: "grpc",
			scheme:   "https",
		},
		{
			group:    "networking.k8s.io",
			version:  "v1",
			resource: "ingresses",
			kind:     "Ingress",
			protocol: "http",
			scheme:   "https",
		},
	}
}

// restConfig resolves Kubernetes credentials for a source. When any of the
// inline cluster_url / bearer_token / bearer_token_file fields is set the
// rest.Config is built directly from those fields. Otherwise the loader
// falls through: explicit kubeconfig path → KUBECONFIG env → ~/.kube/config
// → in-cluster service account. The second return value names the path that
// won so operators can debug unexpected credential resolution.
func restConfig(cfg config.KubernetesConfig) (*rest.Config, string, error) {
	if hasInlineCredentials(cfg) {
		rc, err := restConfigFromFields(cfg)
		return rc, "inline", err
	}
	if cfg.Kubeconfig != "" {
		rc, err := clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
		return rc, "kubeconfig:" + cfg.Kubeconfig, err
	}
	if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
		rc, err := clientcmd.BuildConfigFromFlags("", envKubeconfig)
		return rc, "env:KUBECONFIG", err
	}
	if home, err := os.UserHomeDir(); err == nil {
		defaultKubeconfig := filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(defaultKubeconfig); err == nil {
			rc, err := clientcmd.BuildConfigFromFlags("", defaultKubeconfig)
			return rc, "kubeconfig:~/.kube/config", err
		}
	}

	rc, err := rest.InClusterConfig()
	return rc, "in-cluster", err
}

func hasInlineCredentials(cfg config.KubernetesConfig) bool {
	return cfg.ClusterURL != "" || cfg.BearerToken != "" || cfg.BearerTokenFile != ""
}

// restConfigFromFields assembles a rest.Config from the inline cluster
// fields. Argo-style: API server URL + CA + bearer token, optionally with
// the token sourced from a file (so projected SA volumes / sidecar-rotated
// tokens work without restart).
func restConfigFromFields(cfg config.KubernetesConfig) (*rest.Config, error) {
	if cfg.ClusterURL == "" {
		return nil, fmt.Errorf("kubernetes.cluster_url is required when using inline credentials")
	}
	if cfg.BearerToken == "" && cfg.BearerTokenFile == "" {
		return nil, fmt.Errorf(
			"kubernetes.bearer_token or kubernetes.bearer_token_file is required when using inline credentials",
		)
	}
	if cfg.ClusterCA != "" && cfg.ClusterCAFile != "" {
		return nil, fmt.Errorf(
			"kubernetes.cluster_ca and kubernetes.cluster_ca_file are mutually exclusive",
		)
	}
	if cfg.InsecureSkipVerify && (cfg.ClusterCA != "" || cfg.ClusterCAFile != "") {
		return nil, fmt.Errorf(
			"kubernetes.insecure_skip_verify cannot be combined with cluster_ca / cluster_ca_file",
		)
	}

	rc := &rest.Config{
		Host:        cfg.ClusterURL,
		BearerToken: cfg.BearerToken,
	}
	if cfg.BearerTokenFile != "" {
		rc.BearerTokenFile = cfg.BearerTokenFile
	}
	switch {
	case cfg.InsecureSkipVerify:
		rc.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
	case cfg.ClusterCAFile != "":
		rc.TLSClientConfig = rest.TLSClientConfig{CAFile: cfg.ClusterCAFile}
	case cfg.ClusterCA != "":
		rc.TLSClientConfig = rest.TLSClientConfig{CAData: []byte(cfg.ClusterCA)}
	}
	return rc, nil
}

func serviceFromRoute(
	sourceName string,
	item unstructured.Unstructured,
	route routeResource,
	endpoint resourceEndpoint,
) compass.Service {
	annotations := item.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	name := annotations[meta.AnnotationName]
	if name == "" {
		name = meta.StringFromPath(item.Object, "metadata.name")
	}
	if name == "" {
		name = item.GetName()
	}

	serviceURL := endpoint.URL

	serviceName := name
	switch {
	case endpoint.Title != "":
		serviceName = endpoint.Title
	case endpoint.Hostname != "" && len(endpoint.Hostnames) > 1:
		serviceName = name + " · " + endpoint.Hostname
	}

	kind := route.kind
	if kind == "" {
		kind = item.GetKind()
	}
	if kind == "" {
		kind = route.resource
	}

	metadata := map[string]any{
		"kind":      kind,
		"namespace": item.GetNamespace(),
		"name":      item.GetName(),
		"resource":  route.resource,
		"labels":    item.GetLabels(),
	}
	if route.protocol != "" {
		metadata["protocol"] = route.protocol
	}
	if len(endpoint.Hostnames) > 0 {
		metadata["hostnames"] = endpoint.Hostnames
	}
	if endpoint.Hostname != "" {
		metadata["hostname"] = endpoint.Hostname
	}
	if parentRefs := meta.LookupPath(item.Object, "spec.parentRefs"); parentRefs != nil {
		metadata["parent_refs"] = parentRefs
	}

	id := fmt.Sprintf("%s/%s/%s", kind, item.GetNamespace(), item.GetName())
	switch {
	case endpoint.IDSuffix != "":
		id += "/" + endpoint.IDSuffix
	case endpoint.Hostname != "" && len(endpoint.Hostnames) > 1 &&
		annotations[meta.AnnotationURLs] == "":
		id += "/" + endpoint.Hostname
	}

	return compass.Service{
		ID:          id,
		Name:        serviceName,
		URL:         serviceURL,
		Source:      sourceName,
		PrimaryTag:  annotations[meta.AnnotationPrimaryTag],
		Tags:        meta.CommaList(annotations[meta.AnnotationTags]),
		Description: annotations[meta.AnnotationDescription],
		Icon:        annotations[meta.AnnotationIcon],
		Metadata:    metadata,
		Panels:      meta.PanelsFromAnnotations(annotations),
	}
}

func routeEndpoints(
	item unstructured.Unstructured,
	route routeResource,
) []resourceEndpoint {
	if annotationURL := item.GetAnnotations()[meta.AnnotationURLs]; annotationURL != "" {
		entries := meta.URLEntriesFromAnnotation(annotationURL)
		if len(entries) == 0 {
			return nil
		}
		routeHosts := meta.StringSliceFromPath(item.Object, "spec.hostnames")
		endpoints := make([]resourceEndpoint, 0, len(entries))
		multi := len(entries) > 1
		for _, entry := range entries {
			ep := resourceEndpoint{URL: entry.URL, Title: entry.Title}
			if !multi {
				// Preserve the legacy contract: a single annotation URL keeps
				// the route's hostnames as metadata for context.
				ep.Hostnames = routeHosts
			} else {
				ep.IDSuffix = endpointIDSuffix(entry)
			}
			endpoints = append(endpoints, ep)
		}
		return endpoints
	}

	if hostnames := routeHostnames(item, route); len(hostnames) > 0 {
		endpoints := make([]resourceEndpoint, 0, len(hostnames))
		for _, hostname := range hostnames {
			endpoints = append(endpoints, resourceEndpoint{
				URL:       meta.WithScheme(hostname, route.scheme),
				Hostname:  hostname,
				Hostnames: hostnames,
			})
		}
		return endpoints
	}

	return nil
}

// endpointIDSuffix returns a stable disambiguator appended to a service ID
// when one resource emits multiple cards via the multi-URL annotation. The
// URL host is preferred (concise and unique within an annotation); the title
// is the fallback when the URL won't parse.
func endpointIDSuffix(entry meta.URLEntry) string {
	if u, err := url.Parse(entry.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return entry.Title
}

func routeHostnames(item unstructured.Unstructured, route routeResource) []string {
	if route.resource == "ingresses" {
		return ingressHostnames(item)
	}
	return meta.StringSliceFromPath(item.Object, "spec.hostnames")
}

func ingressHostnames(item unstructured.Unstructured) []string {
	rules, ok := meta.LookupPath(item.Object, "spec.rules").([]any)
	if !ok {
		return nil
	}
	hosts := make([]string, 0, len(rules))
	for _, rule := range rules {
		host := meta.StringFromPath(rule, "host")
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}
