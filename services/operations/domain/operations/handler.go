package operations

import (
	"errors"
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// handler binds the package's endpoints to its service. Every handler
// returns its error, and the group's error writer maps it to a problem.
type handler struct {
	service *Service
}

// Routes builds the package's route group, rooted at /operations. Its one
// read is each faction's operation in an exercise (GET /{exercise}). The
// commands have no route: their inputs arrive as events, through reactors.
func Routes(service *Service) *web.Group {
	h := &handler{service: service}
	g := web.NewGroup("/operations")
	g.SetErrorWriter(web.NewErrorWriter(status))
	g.HandleErr("GET", "/{exercise}", h.find)
	return g
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) error {
	ops, err := h.service.Find(r.Context(), r.PathValue("exercise"))
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, ops)
}

// status maps the domain's errors to problems.
func status(err error) (web.Problem, bool) {
	if errors.Is(err, ErrNotFound) {
		return web.Problem{Status: http.StatusNotFound}, true
	}
	return web.Problem{}, false
}
