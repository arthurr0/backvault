package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/store"
)

const (
	codeValidation      = "validation_failed"
	codeUnauthenticated = "unauthenticated"
	codeForbidden       = "forbidden"
	codeNotFound        = "not_found"
	codeConflict        = "conflict"
	codeLocked          = "locked"
	codeInternal        = "internal"
	codeBadRequest      = "bad_request"
)

type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

type listEnvelope struct {
	Items any `json:"items"`
	Total int `json:"total"`
}

type validationError struct {
	Message string
	Fields  map[string]string
}

func (v *validationError) Error() string {
	if v.Message != "" {
		return v.Message
	}
	return "validation failed"
}

func newValidationError(message string) *validationError {
	return &validationError{Message: message, Fields: map[string]string{}}
}

func (v *validationError) field(name, message string) *validationError {
	if v.Fields == nil {
		v.Fields = map[string]string{}
	}
	v.Fields[name] = message
	return v
}

func (v *validationError) empty() bool {
	return len(v.Fields) == 0
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func writeList(w http.ResponseWriter, items any, total int) {
	writeJSON(w, http.StatusOK, listEnvelope{Items: items, Total: total})
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	s.writeErrorFields(w, r, status, code, message, nil)
}

func (s *Server) writeErrorFields(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	if status >= 500 {
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "status", status, "message", message)
	}
	writeJSON(w, status, errorEnvelope{Error: apiError{Code: code, Message: message, Fields: fields}})
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ve *validationError
	switch {
	case errors.As(err, &ve):
		message := ve.Message
		if message == "" {
			message = "validation failed"
		}
		s.writeErrorFields(w, r, http.StatusBadRequest, codeValidation, message, ve.Fields)
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, r, http.StatusNotFound, codeNotFound, err.Error())
	case errors.Is(err, store.ErrConflict):
		s.writeError(w, r, http.StatusConflict, codeConflict, err.Error())
	case errors.Is(err, engine.ErrJobBusy):
		s.writeError(w, r, http.StatusLocked, codeLocked, err.Error())
	case errors.Is(err, engine.ErrQueueFull):
		s.writeError(w, r, http.StatusServiceUnavailable, codeInternal, err.Error())
	default:
		s.writeError(w, r, http.StatusInternalServerError, codeInternal, err.Error())
	}
}

func decodeBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return newValidationError("request body is required")
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := dec.Decode(dst); err != nil {
		return newValidationError(fmt.Sprintf("invalid JSON body: %v", err))
	}
	return nil
}

func pageFromRequest(r *http.Request) (store.Page, error) {
	p := store.Page{}
	q := r.URL.Query()
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return p, newValidationError("limit must be a non-negative integer").field("limit", "invalid")
		}
		if n > store.MaxLimit {
			n = store.MaxLimit
		}
		p.Limit = n
	}
	if v := strings.TrimSpace(q.Get("offset")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return p, newValidationError("offset must be a non-negative integer").field("offset", "invalid")
		}
		p.Offset = n
	}
	return p, nil
}

func parseTimeParam(r *http.Request, name string) (time.Time, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return time.Time{}, nil
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), nil
		}
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(secs, 0).UTC(), nil
	}
	return time.Time{}, newValidationError("invalid timestamp for "+name).field(name, "expected RFC3339 or YYYY-MM-DD")
}

func queryBool(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
