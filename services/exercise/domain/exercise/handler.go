package exercise

import (
	"context"
	"errors"
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// maxCommandBody bounds a command's request body. A create carries the
// map and the elements, which stay well inside it for an exercise meant to
// show the event layer.
const maxCommandBody = 1 << 20

// handler binds the layer's endpoints to its service. Every handler
// returns its error; the group's writer maps it to a problem.
type handler struct {
	service *Service
}

// Routes builds the layer's route group, rooted at /exercises. The reads
// are the umpire's view (GET /{id}) and the round history (GET
// /{id}/history). The commands are create (POST) and the transitions,
// each an action on its own path: start, pause, resume, and stop (POST
// /{id}/<action>), each answering with the umpire's view. RecordOrders has
// no route: orders arrive from operations as events, through a reactor.
func Routes(service *Service) *web.Group {
	h := &handler{service: service}
	g := web.NewGroup("/exercises")
	g.SetErrorWriter(web.NewErrorWriter(status))
	g.HandleErr("POST", "", h.create)
	g.HandleErr("GET", "/{id}", h.find)
	g.HandleErr("GET", "/{id}/history", h.history)
	g.HandleErr("POST", "/{id}/start", h.transition(service.Start))
	g.HandleErr("POST", "/{id}/pause", h.transition(service.Pause))
	g.HandleErr("POST", "/{id}/resume", h.transition(service.Resume))
	g.HandleErr("POST", "/{id}/stop", h.transition(service.Stop))
	return g
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[CreateExercise](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	ex, err := h.service.Create(r.Context(), body)
	if err != nil {
		return err
	}
	w.Header().Set("Location", r.URL.Path+"/"+ex.ID)
	return web.WriteJSON(w, http.StatusCreated, ex)
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) error {
	ex, err := h.service.Find(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, ex)
}

func (h *handler) history(w http.ResponseWriter, r *http.Request) error {
	rounds, err := h.service.History(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, rounds)
}

// transition binds one status transition, a command that takes the
// exercise's ID and answers with its view.
func (h *handler) transition(cmd func(ctx context.Context, id string) (Exercise, error)) web.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ex, err := cmd(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		return web.WriteJSON(w, http.StatusOK, ex)
	}
}

// status maps the layer's errors to problems: a rejected command is 400,
// an unknown exercise 404, and a command its status does not allow 409.
func status(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, ErrValidation):
		return web.Problem{Status: http.StatusBadRequest, Detail: err.Error()}, true
	case errors.Is(err, ErrNotFound):
		return web.Problem{Status: http.StatusNotFound}, true
	case errors.Is(err, ErrConflict):
		return web.Problem{Status: http.StatusConflict, Detail: err.Error()}, true
	}
	return web.Problem{}, false
}
