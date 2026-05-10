package kubernetes

import (
	"context"
	"strings"
	"testing"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
	corev1 "k8s.io/api/core/v1"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestKubernetesLoadsDefaultHTTPRoute(t *testing.T) {
	testEnv, dynamicClient := startEnvtest(t, httpRouteCRD())
	defer stopEnvtest(t, testEnv)

	ctx := context.Background()
	createNamespace(t, testEnv, "observability")
	createUnstructured(
		t,
		ctx,
		dynamicClient,
		httpRouteGVR(),
		"observability",
		&unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "gateway.networking.k8s.io/v1",
				"kind":       "HTTPRoute",
				"metadata": map[string]any{
					"name":      "grafana",
					"namespace": "observability",
					"annotations": map[string]any{
						meta.AnnotationTags:          "core,monitoring",
						meta.AnnotationPrimaryTag:    "monitoring",
						meta.AnnotationDescription:   "Dashboards",
						meta.AnnotationGrafanaPanels: "CPU=https://grafana.local/d-solo/cpu",
					},
				},
				"spec": map[string]any{
					"hostnames": []any{"grafana.local"},
				},
			},
		},
	)

	source := newWithClient("cluster", []string{"observability"}, dynamicClient)

	services, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("load kubernetes services: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	service := services[0]
	if service.Name != "grafana" || service.URL != "https://grafana.local" {
		t.Fatalf("unexpected service name/url: %#v", service)
	}
	if service.Tags[1] != "monitoring" {
		t.Fatalf("unexpected annotations mapping: %#v", service)
	}
	if service.PrimaryTag != "monitoring" {
		t.Fatalf("expected primary tag annotation, got %q", service.PrimaryTag)
	}
	if len(service.Panels) != 1 || service.Panels[0].Title != "CPU" {
		t.Fatalf("unexpected panels: %#v", service.Panels)
	}
}

func TestKubernetesLoadsDefaultGRPCRoute(t *testing.T) {
	testEnv, dynamicClient := startEnvtest(t, grpcRouteCRD())
	defer stopEnvtest(t, testEnv)

	ctx := context.Background()
	createNamespace(t, testEnv, "apps")
	createUnstructured(
		t,
		ctx,
		dynamicClient,
		grpcRouteGVR(),
		"apps",
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "GRPCRoute",
			"metadata": map[string]any{
				"name":      "greeter",
				"namespace": "apps",
			},
			"spec": map[string]any{"hostnames": []any{"greeter.local"}},
		}},
	)

	source := newWithClient("cluster", []string{"apps"}, dynamicClient)
	services, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("load grpc route: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one grpc service, got %d", len(services))
	}
	if services[0].Name != "greeter" || services[0].URL != "https://greeter.local" {
		t.Fatalf("unexpected grpc service: %#v", services[0])
	}
	if services[0].Metadata["protocol"] != "grpc" {
		t.Fatalf("expected grpc protocol metadata, got %#v", services[0].Metadata)
	}
}

func TestKubernetesLoadsIngress(t *testing.T) {
	testEnv, dynamicClient := startEnvtest(t)
	defer stopEnvtest(t, testEnv)

	ctx := context.Background()
	createNamespace(t, testEnv, "apps")
	createUnstructured(
		t,
		ctx,
		dynamicClient,
		ingressGVR(),
		"apps",
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]any{
				"name":      "whoami",
				"namespace": "apps",
				"annotations": map[string]any{
					meta.AnnotationName: "Whoami",
					meta.AnnotationTags: "web,demo",
				},
			},
			"spec": map[string]any{
				"rules": []any{
					map[string]any{"host": "whoami.local"},
				},
			},
		}},
	)

	source := newWithClient("cluster", []string{"apps"}, dynamicClient)
	services, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("load ingress: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one ingress service, got %d", len(services))
	}
	if services[0].Name != "Whoami" || services[0].URL != "https://whoami.local" {
		t.Fatalf("unexpected ingress service: %#v", services[0])
	}
	if services[0].Metadata["kind"] != "Ingress" {
		t.Fatalf("expected Ingress metadata, got %#v", services[0].Metadata)
	}
}

func TestKubernetesRouteCreatesServicePerHostname(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]any{
			"name":      "apps",
			"namespace": "apps",
			"annotations": map[string]any{
				meta.AnnotationName: "apps",
			},
		},
		"spec": map[string]any{
			"hostnames": []any{"example.com", "alt.example.com", "other.example.org"},
			"parentRefs": []any{map[string]any{
				"name":      "apps",
				"namespace": "apps",
			}},
		},
	}}
	route := defaultRouteResources()[0]

	endpoints := routeEndpoints(item, route)
	if len(endpoints) != 3 {
		t.Fatalf("expected 3 endpoints, got %#v", endpoints)
	}
	services := make([]compass.Service, 0, len(endpoints))
	for _, endpoint := range endpoints {
		services = append(services, serviceFromRoute("cluster", item, route, endpoint))
	}

	if services[0].Name != "apps · example.com" || services[0].URL != "https://example.com" {
		t.Fatalf("unexpected first hostname service: %#v", services[0])
	}
	if services[1].ID != "HTTPRoute/apps/apps/alt.example.com" {
		t.Fatalf("expected hostname in service ID, got %q", services[1].ID)
	}
	hostnames, ok := services[2].Metadata["hostnames"].([]string)
	if !ok || len(hostnames) != 3 {
		t.Fatalf("expected all hostnames in metadata, got %#v", services[2].Metadata["hostnames"])
	}
	if services[2].Metadata["parent_refs"] == nil {
		t.Fatalf("expected parent refs metadata: %#v", services[2].Metadata)
	}
}

func TestKubernetesRouteAnnotationURLOverridesHostnames(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]any{
			"name":      "apps",
			"namespace": "apps",
			"annotations": map[string]any{
				meta.AnnotationURLs: "https://docs.local/apps",
			},
		},
		"spec": map[string]any{"hostnames": []any{"one.local", "two.local"}},
	}}

	endpoints := routeEndpoints(item, defaultRouteResources()[0])
	if len(endpoints) != 1 || endpoints[0].URL != "https://docs.local/apps" {
		t.Fatalf("expected annotation URL to produce one endpoint, got %#v", endpoints)
	}
}

func TestKubernetesRouteMultiURLAnnotationFansOut(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]any{
			"name":      "grafana",
			"namespace": "monitoring",
			"annotations": map[string]any{
				meta.AnnotationURLs: "Public=https://grafana.hodovic.co,Internal=https://grafana.monitoring.svc",
			},
		},
		"spec": map[string]any{"hostnames": []any{"grafana.cluster.local"}},
	}}

	endpoints := routeEndpoints(item, defaultRouteResources()[0])
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d: %#v", len(endpoints), endpoints)
	}
	if endpoints[0].Title != "Public" || endpoints[0].URL != "https://grafana.hodovic.co" {
		t.Fatalf("unexpected first endpoint: %#v", endpoints[0])
	}
	if endpoints[1].Title != "Internal" || endpoints[1].URL != "https://grafana.monitoring.svc" {
		t.Fatalf("unexpected second endpoint: %#v", endpoints[1])
	}
	if endpoints[0].IDSuffix == "" || endpoints[0].IDSuffix == endpoints[1].IDSuffix {
		t.Fatalf("expected distinct ID suffixes, got %q and %q",
			endpoints[0].IDSuffix, endpoints[1].IDSuffix)
	}

	svc0 := serviceFromRoute("k8s", item, defaultRouteResources()[0], endpoints[0])
	svc1 := serviceFromRoute("k8s", item, defaultRouteResources()[0], endpoints[1])
	if svc0.Name != "Public" || svc1.Name != "Internal" {
		t.Fatalf("expected titles as service names, got %q / %q", svc0.Name, svc1.Name)
	}
	if svc0.ID == svc1.ID {
		t.Fatalf("expected distinct service IDs, both %q", svc0.ID)
	}
}

func TestKubernetesRouteMultiURLAnnotationBareURLsUseRouteName(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]any{
			"name":      "grafana",
			"namespace": "monitoring",
			"annotations": map[string]any{
				meta.AnnotationURLs: "https://a.example.com,https://b.example.com",
			},
		},
	}}

	endpoints := routeEndpoints(item, defaultRouteResources()[0])
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(endpoints))
	}

	svc0 := serviceFromRoute("k8s", item, defaultRouteResources()[0], endpoints[0])
	svc1 := serviceFromRoute("k8s", item, defaultRouteResources()[0], endpoints[1])
	if svc0.Name != "grafana" || svc1.Name != "grafana" {
		t.Fatalf("expected route name fallback, got %q / %q", svc0.Name, svc1.Name)
	}
	if svc0.ID == svc1.ID {
		t.Fatalf("expected distinct IDs even with shared name, both %q", svc0.ID)
	}
}

func TestKubernetesResourceEnabledLabel(t *testing.T) {
	item := unstructured.Unstructured{}
	if !resourceEnabled(item, true) {
		t.Fatal("expected resources without enabled label to be enabled")
	}
	if resourceEnabled(item, false) {
		t.Fatal(
			"expected resources without enabled label to be disabled when auto discovery is off",
		)
	}

	item.SetLabels(map[string]string{meta.LabelEnabled: "false"})
	if resourceEnabled(item, true) {
		t.Fatal("expected enabled=false label to disable resource")
	}

	item.SetLabels(map[string]string{meta.LabelEnabled: "true"})
	if !resourceEnabled(item, false) {
		t.Fatal("expected enabled=true label to enable resource")
	}
}

func TestAutoDiscoverAllDefault(t *testing.T) {
	if !meta.DefaultBool(nil, true) {
		t.Fatal("expected auto discovery to default enabled")
	}

	disabled := false
	if meta.DefaultBool(&disabled, true) {
		t.Fatal("expected explicit false to disable auto discovery")
	}
}

func startEnvtest(
	t *testing.T,
	crds ...*apixv1.CustomResourceDefinition,
) (*envtest.Environment, dynamic.Interface) {
	t.Helper()
	binaryAssetsDirectory, _ := envtest.SetupEnvtestDefaultBinaryAssetsDirectory()
	testEnv := &envtest.Environment{
		CRDs:                  crds,
		BinaryAssetsDirectory: binaryAssetsDirectory,
	}
	restConfig, err := testEnv.Start()
	if err != nil {
		if strings.Contains(err.Error(), "unable to start control plane") ||
			strings.Contains(err.Error(), "no such file or directory") {
			t.Skipf("envtest binaries are unavailable: %v", err)
		}
		t.Fatalf("start envtest: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}

	return testEnv, dynamicClient
}

func stopEnvtest(t *testing.T, testEnv *envtest.Environment) {
	t.Helper()
	if err := testEnv.Stop(); err != nil {
		t.Fatalf("stop envtest: %v", err)
	}
}

func createNamespace(t *testing.T, testEnv *envtest.Environment, name string) {
	t.Helper()
	client, err := kubernetes.NewForConfig(testEnv.Config)
	if err != nil {
		t.Fatalf("create kubernetes client: %v", err)
	}
	_, err = client.CoreV1().Namespaces().Create(
		context.Background(),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}},
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("create namespace %q: %v", name, err)
	}
}

func createUnstructured(
	t *testing.T,
	ctx context.Context,
	dynamicClient dynamic.Interface,
	gvr schema.GroupVersionResource,
	namespace string,
	object *unstructured.Unstructured,
) {
	t.Helper()
	_, err := dynamicClient.Resource(gvr).
		Namespace(namespace).
		Create(ctx, object, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create %s/%s: %v", gvr.Resource, object.GetName(), err)
	}
}

func httpRouteGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "httproutes",
	}
}

func grpcRouteGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "grpcroutes",
	}
}

func ingressGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "networking.k8s.io",
		Version:  "v1",
		Resource: "ingresses",
	}
}

func httpRouteCRD() *apixv1.CustomResourceDefinition {
	return namespacedCRD(
		"httproutes.gateway.networking.k8s.io",
		"gateway.networking.k8s.io",
		"HTTPRoute",
		"httproutes",
		"httproute",
	)
}

func grpcRouteCRD() *apixv1.CustomResourceDefinition {
	return namespacedCRD(
		"grpcroutes.gateway.networking.k8s.io",
		"gateway.networking.k8s.io",
		"GRPCRoute",
		"grpcroutes",
		"grpcroute",
	)
}

func namespacedCRD(name, group, kind, plural, singular string) *apixv1.CustomResourceDefinition {
	return &apixv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			// Required by the apiserver for any CRD whose group ends in
			// .k8s.io. "unapproved.<reason>" is the documented escape
			// hatch for fakes that don't have a real approval PR.
			Annotations: map[string]string{
				"api-approved.kubernetes.io": "unapproved.test-fake",
			},
		},
		Spec: apixv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apixv1.CustomResourceDefinitionNames{
				Kind:     kind,
				Plural:   plural,
				Singular: singular,
			},
			Scope: apixv1.NamespaceScoped,
			Versions: []apixv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apixv1.CustomResourceValidation{
					OpenAPIV3Schema: &apixv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: new(true),
					},
				},
			}},
		},
	}
}

func TestRestConfigFromInlineFields(t *testing.T) {
	cfg := config.KubernetesConfig{
		ClusterURL:  "https://cluster.example:6443",
		ClusterCA:   "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n",
		BearerToken: "static-token",
	}

	rc, _, err := restConfig(cfg)
	if err != nil {
		t.Fatalf("restConfig: %v", err)
	}
	if rc.Host != "https://cluster.example:6443" {
		t.Fatalf("Host = %q, want cluster.example", rc.Host)
	}
	if rc.BearerToken != "static-token" {
		t.Fatalf("BearerToken = %q, want static-token", rc.BearerToken)
	}
	if string(rc.CAData) != cfg.ClusterCA {
		t.Fatalf("CAData not propagated: %q", rc.CAData)
	}
}

func TestRestConfigFromInlineFieldsBearerTokenFile(t *testing.T) {
	cfg := config.KubernetesConfig{
		ClusterURL:      "https://cluster.example:6443",
		ClusterCAFile:   "/etc/compass/ca.crt",
		BearerTokenFile: "/var/run/secrets/compass/token",
	}

	rc, _, err := restConfig(cfg)
	if err != nil {
		t.Fatalf("restConfig: %v", err)
	}
	if rc.BearerTokenFile != cfg.BearerTokenFile {
		t.Fatalf("BearerTokenFile not propagated: %q", rc.BearerTokenFile)
	}
	if rc.CAFile != cfg.ClusterCAFile {
		t.Fatalf("CAFile not propagated: %q", rc.CAFile)
	}
}

func TestRestConfigInsecureSkipVerify(t *testing.T) {
	cfg := config.KubernetesConfig{
		ClusterURL:         "https://cluster.example:6443",
		BearerToken:        "x",
		InsecureSkipVerify: true,
	}

	rc, _, err := restConfig(cfg)
	if err != nil {
		t.Fatalf("restConfig: %v", err)
	}
	if !rc.Insecure {
		t.Fatalf("expected Insecure=true")
	}
	if len(rc.CAData) != 0 || rc.CAFile != "" {
		t.Fatalf("CA fields should be empty when insecure: %+v", rc.TLSClientConfig)
	}
}

func TestRestConfigInlineRequiresClusterURL(t *testing.T) {
	cfg := config.KubernetesConfig{BearerToken: "x"}
	if _, _, err := restConfig(cfg); err == nil ||
		!strings.Contains(err.Error(), "cluster_url is required") {
		t.Fatalf("expected cluster_url error, got %v", err)
	}
}

func TestRestConfigInlineRequiresToken(t *testing.T) {
	cfg := config.KubernetesConfig{ClusterURL: "https://x"}
	if _, _, err := restConfig(cfg); err == nil ||
		!strings.Contains(err.Error(), "bearer_token") {
		t.Fatalf("expected bearer_token error, got %v", err)
	}
}

func TestRestConfigInlineRejectsInsecureWithCA(t *testing.T) {
	cfg := config.KubernetesConfig{
		ClusterURL:         "https://x",
		BearerToken:        "x",
		InsecureSkipVerify: true,
		ClusterCAFile:      "/path",
	}
	if _, _, err := restConfig(cfg); err == nil ||
		!strings.Contains(err.Error(), "insecure_skip_verify cannot be combined") {
		t.Fatalf("expected insecure+CA conflict error, got %v", err)
	}
}

func TestRestConfigInlineRejectsBothCAForms(t *testing.T) {
	cfg := config.KubernetesConfig{
		ClusterURL:    "https://x",
		BearerToken:   "x",
		ClusterCA:     "inline",
		ClusterCAFile: "/path",
	}
	if _, _, err := restConfig(cfg); err == nil ||
		!strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually-exclusive error, got %v", err)
	}
}
