package server

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/logo"
	"github.com/adinhodovic/compass/internal/metrics"
	"github.com/adinhodovic/compass/internal/pages"
	"github.com/adinhodovic/compass/internal/registry"
	"github.com/samber/lo"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// ServiceProvider is implemented by registry.Registry. Defining it as an
// interface keeps server tests independent of the registry's lifecycle.
type ServiceProvider interface {
	Services() []compass.Service
	SourceStatuses() []registry.SourceStatus
	DroppedServices() []registry.DroppedService
}

type Server struct {
	cfg            config.Config
	provider       ServiceProvider
	logger         *slog.Logger
	tmpl           *template.Template
	pages          *pages.Loader
	trustedProxies []trustedProxyMatcher
	sourceAccess   map[string][]string
	debugGroups    []string
	// org is the precomputed organization config (with defaults applied).
	// Never changes after New(), so we resolve once instead of every
	// baseData() call.
	org config.OrganizationConfig
	// orgLogo is the resolved logo for org. Same lifecycle as org.
	orgLogo logo.Logo
	// devMode short-circuits production cache headers so a hot-reload
	// loop (air, manual rebuilds) sees fresh CSS/JS/HTML without stale
	// browser caches. Driven by APP_ENV=dev, which `.air.toml` sets for
	// the air-managed binary; production builds leave it unset.
	devMode bool
}

// User represents a forwarded-auth user. Fields are empty / nil when no
// trusted headers are present.
type User struct {
	Name   string
	Email  string
	Groups []string
}

// LoggedIn reports whether forwarded-auth headers identified a user.
func (u User) LoggedIn() bool {
	return u.Name != "" || u.Email != ""
}

// Base is the common data every page template gets. The command palette
// (rendered in base.html so it's available everywhere) reads Services
// and Pages from here.
type Base struct {
	Organization    config.OrganizationConfig
	Pages           []pages.Section
	User            User
	AuthMode        string // "basic" | "optional-basic" | "forwarded" | "open"
	Services        []compass.Service
	HasSourceErrors bool
	DebugVisible    bool
	CanLogin        bool
	CanLogout       bool
	NotesEditable   bool
	PrimaryColor    string
	HeaderLinks     []config.LinkConfig
	FooterLinks     []config.LinkConfig
	Meta            Meta
}

// Meta drives <title>, <meta name="description">, Open Graph, and Twitter
// card tags rendered in base.html. baseData fills sensible defaults from
// the organization config and request URL; handlers overlay page-specific
// values (service name/description, page title, etc.) before render.
type Meta struct {
	Title       string
	Description string
	Image       string
	URL         string
	Type        string // og:type — "website" or "article"
}

type homeData struct {
	Base
	GroupBy    string
	GroupNames []string
	Groups     map[string][]compass.Service
}

type detailData struct {
	Base
	Service   compass.Service
	Backlinks []pages.Page
}

type pageData struct {
	Base
	Page    pages.Page
	Content template.HTML
	TOC     []pages.TOCEntry
	// IsHome is true when this page is being rendered at "/" via
	// `home.page`, so the template can skip the redundant "Home > X"
	// breadcrumb.
	IsHome bool
}

type debugData struct {
	Base
	Statuses      []registry.SourceStatus
	Groups        map[string][]compass.Service // keyed by source name
	AccessGroups  map[string][]string          // keyed by source ID
	Dropped       []registry.DroppedService
	DroppedGroups map[string][]registry.DroppedService
}

func New(
	cfg config.Config,
	provider ServiceProvider,
	logger *slog.Logger,
) http.Handler {
	assetsDir := strings.TrimSpace(cfg.Assets.Dir)
	matchers, err := compileTrustedProxies(cfg.Auth.TrustedProxies)
	if err != nil {
		// config.Load already validates trusted_proxies; reaching here means
		// the config skipped validation, so a hard failure is appropriate.
		panic(fmt.Sprintf("auth.trusted_proxies: %v", err))
	}
	org := resolveOrganization(cfg.Organization)
	devMode := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "dev")
	s := Server{
		cfg:            cfg,
		provider:       provider,
		logger:         logger,
		pages:          pages.NewLoader(cfg.Pages.Dir),
		trustedProxies: matchers,
		sourceAccess:   sourceAccessGroups(cfg.Services.Sources),
		debugGroups:    trimGroups(cfg.Debug.RequiredGroups),
		org:            org,
		orgLogo:        logo.Resolve(org.Logo, org.Name, "organization"),
		devMode:        devMode,
		tmpl: template.Must(template.New("compass").Funcs(template.FuncMap{
			"logo":         logo.Resolve,
			"icon":         logo.IconHTML,
			"sub":          sub,
			"sourceLabel":  sourceLabel,
			"groupLabel":   groupLabel,
			"serviceIDs":   serviceIDs,
			"sourceNames":  sourceNames,
			"metadata":     metadataItems,
			"json":         servicesJSON,
			"commandIndex": commandIndex,
			"timeAgo":      timeAgo,
		}).ParseFS(
			templatesFS,
			"templates/layouts/*.html",
			"templates/pages/*.html",
			"templates/partials/*.html",
		)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.home)
	mux.HandleFunc("/services", s.services)
	mux.HandleFunc("/services/", s.detail)
	mux.HandleFunc("/pages/", s.page)
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/manifest.webmanifest", s.manifest)
	if cfg.Debug.IsEnabled() {
		mux.HandleFunc("/debug", s.debug)
	}
	staticRoot, staticErr := fs.Sub(staticFS, "static")
	if staticErr != nil {
		panic(staticErr)
	}
	mux.HandleFunc("/static/chroma.css", chromaCSSHandler)
	mux.Handle("/static/", http.StripPrefix("/static/",
		staticCacheHeaders(devMode, http.FileServer(http.FS(staticRoot)))))
	if assetsDir != "" {
		mux.Handle("/assets/", http.StripPrefix("/assets/",
			staticCacheHeaders(devMode, http.FileServer(noDirFileSystem{fs: http.Dir(assetsDir)}))))
	}
	mux.Handle("/metrics", metrics.Handler())

	return loggingMiddleware(
		metricsMiddleware(authMiddleware(mux, cfg.Auth, matchers, logger)),
		logger,
	)
}

type noDirFileSystem struct {
	fs http.FileSystem
}

func (fsys noDirFileSystem) Open(name string) (http.File, error) {
	file, err := fsys.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, os.ErrNotExist
	}
	return file, nil
}

func (s Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// metricsMiddleware records request count + duration against the
// normalized route pattern. /metrics observations are skipped to keep
// scrapes from polluting their own histogram.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := metrics.NormalizeRoute(r.URL.Path)
		if route == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		metrics.HTTPRequests.WithLabelValues(route, r.Method, http.StatusText(rec.status)).Inc()
		metrics.HTTPDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

// home dispatches "/" — either to a configured markdown home page, or the
// services dashboard when no markdown home is set.
func (s Server) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if s.cfg.Home.Page != "" && s.pages != nil && s.pages.Enabled() {
		page, content, toc, err := s.pages.Get(
			s.cfg.Home.Section,
			s.cfg.Home.Page,
			s.visibleServicesFor(r),
		)
		if err == nil {
			base := s.baseData(r)
			base.Meta.Title = page.Title
			base.Meta.Type = "article"
			s.render(w, "page", pageData{
				Base:    base,
				Page:    page,
				Content: content,
				TOC:     toc,
				IsHome:  true,
			})
			return
		}
		s.logger.Warn("Configured home.page failed to load; falling back to services dashboard",
			"section", s.cfg.Home.Section, "page", s.cfg.Home.Page, "err", err)
	}
	s.renderServices(w, r)
}

// services renders the services dashboard at /services. It's also the
// fallback for "/" when no custom home is configured.
func (s Server) services(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/services" {
		http.NotFound(w, r)
		return
	}
	s.renderServices(w, r)
}

func (s Server) renderServices(w http.ResponseWriter, r *http.Request) {
	groupBy := r.URL.Query().Get("group")
	if groupBy == "" {
		groupBy = s.cfg.UI.DefaultGroupBy
	}
	if groupBy != compass.GroupBySource {
		groupBy = compass.GroupByTags
	}
	base := s.baseData(r)
	groups := registry.Group(base.Services, groupBy)
	data := homeData{
		Base:       base,
		GroupBy:    groupBy,
		Groups:     groups,
		GroupNames: registry.GroupNames(groups),
	}
	s.render(w, "home", data)
}

// baseData fills the fields every template needs. Services is populated
// from the provider; Pages from the loader; User from request headers.
func (s Server) baseData(r *http.Request) Base {
	hasErrors := sourceStatusesHaveErrors(s.visibleSourceStatusesFor(r))
	org := s.org
	origin := absoluteOrigin(r, s.trustedProxies)
	image := ""
	if s.orgLogo.Kind == "image" {
		image = absoluteImageURL(origin, s.orgLogo.URL)
	}
	return Base{
		Organization:    org,
		Pages:           s.pageList(),
		User:            s.userFrom(r),
		AuthMode:        authModeName(s.cfg.Auth),
		Services:        s.visibleServicesFor(r),
		HasSourceErrors: hasErrors,
		DebugVisible:    s.debugVisibleTo(r),
		CanLogin:        !s.cfg.Auth.Required && len(s.cfg.Auth.Basic.Users) > 0,
		CanLogout:       len(s.cfg.Auth.Basic.Users) > 0,
		NotesEditable:   s.notesEditable(r),
		PrimaryColor:    s.cfg.UI.PrimaryColor,
		HeaderLinks:     s.cfg.HeaderLinks,
		FooterLinks:     s.cfg.FooterLinks,
		Meta: Meta{
			Title:       org.Name,
			Description: org.Description,
			Image:       image,
			URL:         origin + r.URL.Path,
			Type:        "website",
		},
	}
}

func (s Server) notesEditable(r *http.Request) bool {
	if s.userFrom(r).LoggedIn() {
		return true
	}
	return len(s.cfg.Auth.Basic.Users) == 0 && !s.cfg.Auth.Required &&
		len(s.cfg.Auth.TrustedProxies) == 0
}

func (s Server) debugVisibleTo(r *http.Request) bool {
	if !s.cfg.Debug.IsEnabled() {
		return false
	}
	if len(s.debugGroups) == 0 {
		return true
	}
	return hasAnyGroup(s.userFrom(r).Groups, s.debugGroups)
}

func (s Server) visibleServicesFor(r *http.Request) []compass.Service {
	services := s.provider.Services()
	if len(s.sourceAccess) == 0 {
		return services
	}
	user := s.userFrom(r)
	return lo.Filter(services, func(service compass.Service, _ int) bool {
		return s.sourceVisibleTo(service.SourceID(), user)
	})
}

func (s Server) visibleSourceStatusesFor(r *http.Request) []registry.SourceStatus {
	statuses := s.provider.SourceStatuses()
	if len(s.sourceAccess) == 0 {
		return statuses
	}
	user := s.userFrom(r)
	return lo.Filter(statuses, func(status registry.SourceStatus, _ int) bool {
		return s.sourceVisibleTo(status.ID(), user)
	})
}

func (s Server) sourceVisibleTo(sourceID string, user User) bool {
	groups, ok := s.sourceAccess[sourceID]
	if !ok {
		return true
	}
	return hasAnyGroup(user.Groups, groups)
}

// absoluteOrigin reconstructs the public scheme://host the client used to
// reach us. X-Forwarded-Proto / X-Forwarded-Host are only honoured when the
// request comes from a trusted proxy (matchers slice empty means "trust
// every caller", same semantics as the auth middleware so OG tags don't
// expose a path that the auth side would reject).
func absoluteOrigin(r *http.Request, trustedProxies []trustedProxyMatcher) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if len(trustedProxies) == 0 || remoteAllowed(r, trustedProxies) {
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			scheme = proto
		}
		if h := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); h != "" {
			host = h
		}
	}
	return scheme + "://" + host
}

// staticCacheHeaders sets a 1-day cache header on /static/* in production
// so browsers don't refetch CSS/JS on every navigation; asset filenames
// change with each release so the cache invalidates naturally. In dev
// mode the header flips to no-store, so a hot-reload loop's freshly
// rebuilt assets show up on the very next refresh.
func staticCacheHeaders(devMode bool, next http.Handler) http.Handler {
	header := "public, max-age=86400"
	if devMode {
		header = "no-store"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", header)
		next.ServeHTTP(w, r)
	})
}

// absoluteImageURL returns raw unchanged when it already has a scheme;
// otherwise prepends origin so og:image points at a fetchable URL.
func absoluteImageURL(origin, raw string) string {
	if raw == "" || strings.Contains(raw, "://") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return origin + raw
	}
	return origin + "/" + raw
}

func (s Server) detail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/services/")
	if id == "" {
		http.Redirect(w, r, "/services", http.StatusFound)
		return
	}
	base := s.baseData(r)
	idx := slices.IndexFunc(base.Services, func(svc compass.Service) bool { return svc.ID == id })
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	var backlinks []pages.Page
	if s.pages != nil && s.pages.Enabled() {
		var err error
		if backlinks, err = s.pages.Backlinks(id, base.Services); err != nil {
			s.logger.Warn("backlinks failed", "service", id, "err", err)
		}
	}
	svc := base.Services[idx]
	base.Meta.Title = svc.Name
	if svc.Description != "" {
		base.Meta.Description = svc.Description
	}
	if svcLogo := logo.Resolve(svc.Icon, svc.Name, svc.SourceType); svcLogo.Kind == "image" {
		base.Meta.Image = absoluteImageURL(absoluteOrigin(r, s.trustedProxies), svcLogo.URL)
	}
	s.render(w, "detail", detailData{
		Base:      base,
		Service:   svc,
		Backlinks: backlinks,
	})
}

// debug renders an Alloy-style overview of every source's last load
// outcome plus the full flat services list. Mostly an operator tool.
func (s Server) debug(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/debug" {
		http.NotFound(w, r)
		return
	}
	if !s.debugVisibleTo(r) {
		http.NotFound(w, r)
		return
	}
	base := s.baseData(r)
	base.Services = s.provider.Services()
	dropped := s.provider.DroppedServices()
	statuses := s.provider.SourceStatuses()
	base.HasSourceErrors = sourceStatusesHaveErrors(statuses)
	s.render(w, "debug", debugData{
		Base:          base,
		Statuses:      statuses,
		Groups:        registry.Group(base.Services, compass.GroupBySource),
		AccessGroups:  s.sourceAccess,
		Dropped:       dropped,
		DroppedGroups: groupDroppedServices(dropped),
	})
}

func sourceAccessGroups(sources []config.SourceConfig) map[string][]string {
	return lo.Associate(
		lo.Filter(sources, func(source config.SourceConfig, _ int) bool {
			return len(source.Access.RequiredGroups) > 0
		}),
		func(source config.SourceConfig) (string, []string) {
			return strings.TrimSpace(source.Type) + "/" + strings.TrimSpace(source.Name),
				trimGroups(source.Access.RequiredGroups)
		},
	)
}

func sourceStatusesHaveErrors(statuses []registry.SourceStatus) bool {
	return lo.SomeBy(
		statuses,
		func(status registry.SourceStatus) bool { return status.Error != "" },
	)
}

func groupDroppedServices(services []registry.DroppedService) map[string][]registry.DroppedService {
	groups := make(map[string][]registry.DroppedService)
	for _, service := range services {
		groups[service.SourceID()] = append(groups[service.SourceID()], service)
	}
	return groups
}

// manifest serves a PWA web app manifest derived from the organization
// config so Compass can be installed on mobile / desktop.
func (s Server) manifest(w http.ResponseWriter, r *http.Request) {
	org := s.org
	iconURL := strings.TrimSpace(org.Logo)
	if iconURL != "" && s.orgLogo.Kind == "image" {
		iconURL = s.orgLogo.URL
	}
	manifest := map[string]any{
		"name":             org.Name,
		"short_name":       org.Name,
		"description":      org.Description,
		"start_url":        "/",
		"display":          "standalone",
		"background_color": "#ffffff",
		"theme_color":      "#570df8",
	}
	if iconURL != "" {
		manifest["icons"] = []map[string]any{
			{"src": iconURL, "sizes": "any", "purpose": "any"},
		}
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		s.logger.Error("Failed to encode manifest", "err", err)
	}
}

// userFrom extracts the forwarded-auth user from request headers. Returns
// the zero User when no trusted headers are configured or set.
func (s Server) userFrom(r *http.Request) User {
	var u User
	if len(s.cfg.Auth.Basic.Users) > 0 && !basicAuthVerified(r) &&
		len(s.trustedProxies) > 0 && !remoteAllowed(r, s.trustedProxies) {
		return u
	}
	if len(s.cfg.Auth.Basic.Users) == 0 && len(s.trustedProxies) > 0 &&
		!remoteAllowed(r, s.trustedProxies) {
		return u
	}
	if h := s.cfg.Auth.UserHeader; h != "" {
		u.Name = strings.TrimSpace(r.Header.Get(h))
	}
	if h := s.cfg.Auth.EmailHeader; h != "" {
		u.Email = strings.TrimSpace(r.Header.Get(h))
	}
	if h := s.cfg.Auth.GroupsHeader; h != "" {
		u.Groups = parseGroups(r.Header.Get(h))
	}
	return u
}

// parseGroups splits a forwarded-groups header on the separators commonly
// emitted by reverse-auth proxies (oauth2-proxy uses commas, Authelia
// uses commas, some Traefik setups pipes), trimming whitespace and
// dropping empty entries. Returns nil when no groups are present so the
// User keeps a zero-valued slice.
func parseGroups(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	})
	out := lo.FilterMap(fields, func(field string, _ int) (string, bool) {
		group := strings.TrimSpace(field)
		return group, group != ""
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s Server) page(w http.ResponseWriter, r *http.Request) {
	if s.pages == nil || !s.pages.Enabled() {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/pages/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	var section, slug string
	switch parts := strings.Split(rest, "/"); len(parts) {
	case 1:
		slug = parts[0]
	case 2:
		section, slug = parts[0], parts[1]
	default:
		http.NotFound(w, r)
		return
	}
	page, content, toc, err := s.pages.Get(section, slug, s.visibleServicesFor(r))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("Failed to load page", "section", section, "slug", slug, "err", err)
		http.Error(w, "Page failed to load", http.StatusInternalServerError)
		return
	}
	base := s.baseData(r)
	base.Meta.Title = page.Title
	base.Meta.Type = "article"
	s.render(w, "page", pageData{
		Base:    base,
		Page:    page,
		Content: content,
		TOC:     toc,
	})
}

func (s Server) pageList() []pages.Section {
	if s.pages == nil || !s.pages.Enabled() {
		return nil
	}
	list, err := s.pages.Sections()
	if err != nil {
		s.logger.Error("Failed to list pages", "err", err)
		return nil
	}
	return s.withoutHomePage(list)
}

func (s Server) withoutHomePage(list []pages.Section) []pages.Section {
	homeSlug := strings.TrimSpace(s.cfg.Home.Page)
	if homeSlug == "" {
		return list
	}
	homeSection := strings.TrimSpace(s.cfg.Home.Section)
	filtered := make([]pages.Section, 0, len(list))
	for _, section := range list {
		pagesInSection := make([]pages.Page, 0, len(section.Pages))
		for _, page := range section.Pages {
			if page.Slug == homeSlug && page.Section == homeSection {
				continue
			}
			pagesInSection = append(pagesInSection, page)
		}
		if len(pagesInSection) == 0 {
			continue
		}
		section.Pages = pagesInSection
		filtered = append(filtered, section)
	}
	return filtered
}

// resolveOrganization applies hard-coded defaults for the organization
// block. Pulled into a free function so the result can be precomputed at
// New() time and reused across every request via s.org.
func resolveOrganization(o config.OrganizationConfig) config.OrganizationConfig {
	if strings.TrimSpace(o.Name) == "" {
		o.Name = "Compass"
	}
	if strings.TrimSpace(o.Description) == "" {
		o.Description = "Homelab services discovered from your infrastructure, rendered with operational context."
	}
	if strings.TrimSpace(o.Logo) == "" {
		o.Logo = "lucide:compass"
	}
	return o
}

func (s Server) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.logger.Error("Failed to render template", "template", name, "err", err)
		http.Error(w, "Template failed to render", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Pages may include the forward-auth user name; cache only privately
	// and briefly so dynamic source-status changes propagate within ~30s.
	// In dev mode a hot-reload loop should never serve a stale page.
	if w.Header().Get("Cache-Control") == "" {
		if s.devMode {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "private, max-age=30")
		}
	}
	_, _ = w.Write(buf.Bytes())
}
