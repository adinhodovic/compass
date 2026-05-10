package compass

const (
	GroupByTags   = "tags"
	GroupBySource = "source"

	SourceTypeAPI        = "api"
	SourceTypeDocker     = "docker"
	SourceTypeHeadscale  = "headscale"
	SourceTypeKubernetes = "kubernetes"
	SourceTypeStatic     = "static"
	SourceTypeTailscale  = "tailscale"
)

type Service struct {
	ID          string         `json:"id"          yaml:"id"`
	Name        string         `json:"name"        yaml:"name"`
	URL         string         `json:"url"         yaml:"url"`
	Source      string         `json:"source"      yaml:"source"`
	SourceType  string         `json:"source_type" yaml:"source_type"`
	PrimaryTag  string         `json:"primary_tag" yaml:"primary_tag"`
	Tags        []string       `json:"tags"        yaml:"tags"`
	Description string         `json:"description" yaml:"description"`
	Icon        string         `json:"icon"        yaml:"icon"`
	Metadata    map[string]any `json:"metadata"    yaml:"metadata"`
	Panels      []Panel        `json:"panels"      yaml:"panels"`
}

type Panel struct {
	Title string `json:"title" yaml:"title"`
	URL   string `json:"url"   yaml:"url"`
}

// SourceID is the canonical "<type>/<name>" identifier for a service's
// source. The pair is unique even when two sources share a Name (e.g.
// docker `local` and kubernetes `local`); using only Source as a key
// silently buckets them together.
func (s Service) SourceID() string {
	return s.SourceType + "/" + s.Source
}

func CloneService(service Service) Service {
	service.Tags = append([]string(nil), service.Tags...)
	service.Panels = append([]Panel(nil), service.Panels...)
	if service.Metadata != nil {
		service.Metadata = cloneMetadata(service.Metadata)
	}
	return service
}

func CloneServices(services []Service) []Service {
	if services == nil {
		return nil
	}
	out := make([]Service, len(services))
	for i, service := range services {
		out[i] = CloneService(service)
	}
	return out
}

func cloneMetadata(metadata map[string]any) map[string]any {
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = cloneMetadataValue(value)
	}
	return out
}

func cloneMetadataValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMetadata(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneMetadataValue(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
