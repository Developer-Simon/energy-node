package httpapi

import (
	"net/http"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

// routeGroup says which view permission a route needs before it is served.
type routeGroup int

const (
	// groupOpen routes have no view gate; their handlers keep their own checks.
	groupOpen routeGroup = iota
	// groupAnyView routes need at least one view_* permission.
	groupAnyView
	// groupPage routes need one specific view_* permission, which may depend on the request.
	groupPage
)

// routeAccess is the access rule of one route pattern. resolve is set only for
// groupPage routes whose permission depends on the path; otherwise view applies.
type routeAccess struct {
	group   routeGroup
	view    string
	resolve func(r *http.Request) string
}

// routeTable maps every registered pattern to its access rule. Keys are the
// exact patterns passed to HandleFunc or Handle. Open routes keep their own
// checks in the handler; anyView routes need one view_* permission; page routes
// need the view permission of their page.
var routeTable = map[string]routeAccess{
	// open
	"/":                                {group: groupOpen},
	"/api/v1/health":                   {group: groupOpen},
	"/api/v1/runtime-cache":            {group: groupOpen},
	"/static/":                         {group: groupOpen},
	"/i18n/":                           {group: groupOpen},
	"/redeploy/":                       {group: groupOpen},
	"/api/v1/versions":                 {group: groupOpen},
	"/api/v1/changelog":                {group: groupOpen},
	"/api/v1/changelog/highlights":     {group: groupOpen},
	"/api/v1/updates/status":           {group: groupOpen},
	"/api/v1/updates/check":            {group: groupOpen},
	"/api/v1/updates/progress":         {group: groupOpen},
	"/api/v1/updates/install":          {group: groupOpen},
	"/api/v1/auth/login":               {group: groupOpen},
	"/api/v1/auth/guest":               {group: groupOpen},
	"/api/v1/auth/session":             {group: groupOpen},
	"/api/v1/auth/logout":              {group: groupOpen},
	"/api/v1/companion/":               {group: groupOpen},
	"/api/v1/system/restart-dashboard": {group: groupOpen},
	"/api/v1/system/reboot":            {group: groupOpen},
	"/api/v1/system/poweroff":          {group: groupOpen},
	"/api/v1/system/logs":              {group: groupOpen},
	"/api/v1/system/logs/units":        {group: groupOpen},
	"/api/v1/system/config":            {group: groupOpen},
	"/api/v1/system/config/schema":     {group: groupOpen},
	"/api/v1/health/storage":           {group: groupOpen},
	"/api/v1/settings":                 {group: groupOpen},
	"/api/v1/settings/":                {group: groupOpen},
	"/api/v1/mqtt":                     {group: groupOpen},
	"/api/v1/mqtt/credentials":         {group: groupOpen},
	"/api/v1/mqtt/energy-device":       {group: groupOpen},
	"/api/v1/mqtt/node-settings":       {group: groupOpen},
	"/api/v1/mqtt/test":                {group: groupOpen},
	"/api/v1/mqtt/reconnect":           {group: groupOpen},
	"/api/v1/mqtt/status":              {group: groupOpen},
	"/api/v1/mqtt/bridge":              {group: groupOpen},
	"/api/v1/mqtt/bridge/":             {group: groupOpen},
	"/api/v1/mqtt/bridge/apply":        {group: groupOpen},
	"/api/v1/mqtt/bridge/credentials":  {group: groupOpen},
	"/api/v1/mqtt/bridge/restart":      {group: groupOpen},
	"/api/v1/mqtt/bridge/status":       {group: groupOpen},
	"/api/v1/tailscale/status":         {group: groupOpen},
	"/api/v1/tailscale/prereqs":        {group: groupOpen},
	"/api/v1/tailscale/login":          {group: groupOpen},
	"/api/v1/tailscale/logout":         {group: groupOpen},
	"/api/v1/tailscale/restart":        {group: groupOpen},
	"/api/v1/tiny-tuya/devices":        {group: groupOpen},
	"/api/v1/tiny-tuya/status":         {group: groupOpen},
	"/api/v1/tiny-tuya/configure":      {group: groupOpen},
	"/api/v1/tiny-tuya/credentials":    {group: groupOpen},
	"/api/v1/users":                    {group: groupOpen},
	"/api/v1/users/":                   {group: groupOpen},
	"/api/v1/user-sessions":            {group: groupOpen},
	"/api/v1/user-sessions/":           {group: groupOpen},
	"/api/v1/history/exchange":         {group: groupOpen},
	"/api/v1/history/exchange/stream":  {group: groupOpen},
	"/api/v1/history/exchange/offer":   {group: groupOpen},
	"/api/v1/history/exchange/request": {group: groupOpen},
	"/api/v1/history/exchange/deliver": {group: groupOpen},
	"/api/v1/history/exchange/buffer":  {group: groupOpen},

	// anyView
	"/api/v1/events":                     {group: groupAnyView},
	"/api/v1/devices":                    {group: groupAnyView},
	"/api/v1/devices/switches":           {group: groupAnyView},
	"/api/v1/devices/ignored":            {group: groupAnyView},
	"/api/v1/devices/":                   {group: groupAnyView},
	"/api/v1/entities/":                  {group: groupAnyView},
	"/api/v1/energy":                     {group: groupAnyView},
	"/api/v1/energy/roles":               {group: groupAnyView},
	"/api/v1/energy/stats":               {group: groupAnyView},
	"/api/v1/energy/stats/hours":         {group: groupAnyView},
	"/api/v1/energy/category-references": {group: groupAnyView},
	// history.js calls the interpretation with the history page's view_history
	// alone, so it cannot be bound to view_energy.
	"/api/v1/energy/interpretation":   {group: groupAnyView},
	"/api/v1/history/entities":        {group: groupAnyView},
	"/api/v1/device/map":              {group: groupAnyView},
	"/api/v1/device/map/":             {group: groupAnyView},
	"/api/v1/device/icons":            {group: groupAnyView},
	"/api/v1/device/prefs":            {group: groupAnyView},
	"/api/v1/device/prefs/":           {group: groupAnyView},
	"/api/v1/automation/notification": {group: groupAnyView},

	// page
	"/api/v1/discovery":            {group: groupPage, view: auth.RoleViewDiagnostics},
	"/api/v1/discovery/summary":    {group: groupPage, view: auth.RoleViewDiagnostics},
	"/api/v1/diagnostics":          {group: groupPage, view: auth.RoleViewDiagnostics},
	"/api/v1/diagnostics/rules":    {group: groupPage, view: auth.RoleViewDiagnostics},
	"/api/v1/diagnostics/health":   {group: groupPage, view: auth.RoleViewDiagnostics},
	"/api/v1/topics":               {group: groupPage, view: auth.RoleViewConfig},
	"/api/v1/topics/samples":       {group: groupPage, view: auth.RoleViewConfig},
	"/api/v1/shelly/presets":       {group: groupPage, view: auth.RoleViewConfig},
	"/api/v1/configurations":       {group: groupPage, view: auth.RoleViewConfig},
	"/api/v1/configurations/":      {group: groupPage, resolve: configurationView},
	"/api/v1/automation/history":   {group: groupPage, view: auth.RoleViewAutomations},
	"/api/v1/automation/state":     {group: groupPage, view: auth.RoleViewAutomations},
	"/api/v1/automations/test":     {group: groupPage, view: auth.RoleViewAutomations},
	"/api/v1/automations/history/": {group: groupPage, view: auth.RoleViewAutomations},
	"/api/v1/energy/":              {group: groupPage, view: auth.RoleViewEnergy},
	"/api/v1/layout":               {group: groupPage, view: auth.RoleViewOverview},
	"/api/v1/layout/":              {group: groupPage, view: auth.RoleViewOverview},
}

// configurationView gives the view permission of /api/v1/configurations/{name}:
// the automation rules need view_automations, every other configuration view_config.
func configurationView(r *http.Request) string {
	name := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/v1/configurations/"), "/", 2)[0]
	if name == "automation_rules" {
		return auth.RoleViewAutomations
	}
	return auth.RoleViewConfig
}

// routeRegistrar is the part of a mux that route tables register into.
type routeRegistrar interface {
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
	Handle(string, http.Handler)
}

// guardedMux registers routes only when routeTable names their access group,
// so a new route cannot ship without a decision about who may view it.
type guardedMux struct {
	mux      *http.ServeMux
	patterns []string
}

// HandleFunc registers handler under pattern, behind its view gate.
func (g *guardedMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	g.Handle(pattern, http.HandlerFunc(handler))
}

// Handle registers handler under pattern, behind its view gate. It panics when
// pattern has no entry in routeTable.
func (g *guardedMux) Handle(pattern string, handler http.Handler) {
	access, ok := routeTable[pattern]
	if !ok {
		panic("httpapi: route without access group: " + pattern)
	}
	g.patterns = append(g.patterns, pattern)
	if access.group != groupOpen {
		handler = viewGate(access, handler)
	}
	g.mux.Handle(pattern, handler)
}

// ServeMux returns the mux with every route registered through g.
func (g *guardedMux) ServeMux() *http.ServeMux {
	return g.mux
}

// viewGate lets a request through when its user may view the route. Without a
// user in the context no gate applies, as before accounts existed.
func viewGate(access routeAccess, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		allowed := true
		switch access.group {
		case groupAnyView:
			allowed = auth.HasAnyView(user)
		case groupPage:
			view := access.view
			if access.resolve != nil {
				view = access.resolve(r)
			}
			allowed = auth.HasRole(user, view)
		}
		if !allowed {
			writeErrorKey(w, http.StatusForbidden, "view_forbidden", "error.view_forbidden", nil, "Für diese Seite fehlt die Berechtigung zum Ansehen.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
