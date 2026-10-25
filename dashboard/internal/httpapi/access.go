package httpapi

import (
	"net/http"

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
// exact patterns passed to HandleFunc or Handle.
var routeTable = map[string]routeAccess{}

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
