package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"task005-notify/internal/notify"
)

var ErrBadJSON = errors.New("请求体不是合法的单个 JSON 对象")

type API struct {
	store *notify.Store
	now   func() time.Time
}

func New() *API { return &API{store: notify.New(), now: time.Now} }

func NewWithClock(now func() time.Time) *API {
	return &API{store: notify.New(), now: now}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/notifications", a.create)
	mux.HandleFunc("GET /api/notifications", a.list)
	mux.HandleFunc("GET /api/notifications/{id}", a.get)
	mux.HandleFunc("POST /api/notifications/{id}/send", a.send)
	mux.HandleFunc("POST /api/notifications/{id}/read", a.read)
	mux.HandleFunc("DELETE /api/notifications/{id}", a.delete)
	return mux
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return ErrBadJSON
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return ErrBadJSON
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, notify.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, notify.ErrDuplicateID),
		errors.Is(err, notify.ErrAlreadySent),
		errors.Is(err, notify.ErrAlreadyRead),
		errors.Is(err, notify.ErrNotSent),
		errors.Is(err, notify.ErrScheduledNotReached):
		status = http.StatusConflict
	case errors.Is(err, notify.ErrEmptyID),
		errors.Is(err, notify.ErrEmptyRecipient),
		errors.Is(err, notify.ErrEmptyContent),
		errors.Is(err, notify.ErrInvalidPriority),
		errors.Is(err, notify.ErrInvalidSchedule),
		errors.Is(err, ErrBadJSON):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"error": err.Error(), "status": status})
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req notify.CreateInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	n, err := a.store.Create(req, a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"notification": n})
}

func (a *API) send(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.MarkSent(r.PathValue("id"), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notification": n})
}

func (a *API) read(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.MarkRead(r.PathValue("id"), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notification": n})
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	items := a.store.List()
	writeJSON(w, http.StatusOK, map[string]any{"notifications": items, "total": len(items)})
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notification": n})
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Delete(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
