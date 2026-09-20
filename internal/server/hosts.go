package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/remote"
	"github.com/arthurr0/backvault/internal/store"
)

type hostRequest struct {
	ID                    string        `json:"id"`
	Name                  string        `json:"name"`
	Description           string        `json:"description"`
	Address               string        `json:"address"`
	Port                  int           `json:"port"`
	User                  string        `json:"user"`
	Auth                  core.HostAuth `json:"auth"`
	PrivateKey            string        `json:"privateKey"`
	KeyPassphrase         string        `json:"keyPassphrase"`
	Password              string        `json:"password"`
	PublicKey             string        `json:"publicKey"`
	HostKey               string        `json:"hostKey"`
	Sudo                  bool          `json:"sudo"`
	ConnectTimeoutSeconds int           `json:"connectTimeoutSeconds"`
	Tags                  []string      `json:"tags"`
}

type keygenResponse struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

func hostFromRequest(req hostRequest) core.Host {
	h := core.Host{
		Name:           strings.TrimSpace(req.Name),
		Description:    req.Description,
		Address:        strings.TrimSpace(req.Address),
		Port:           req.Port,
		User:           strings.TrimSpace(req.User),
		Auth:           req.Auth,
		PrivateKey:     req.PrivateKey,
		KeyPassphrase:  req.KeyPassphrase,
		Password:       req.Password,
		PublicKey:      strings.TrimSpace(req.PublicKey),
		HostKey:        strings.TrimSpace(req.HostKey),
		Sudo:           req.Sudo,
		ConnectTimeout: req.ConnectTimeoutSeconds,
		Tags:           req.Tags,
	}
	if h.Auth == "" {
		h.Auth = core.HostAuthKey
	}
	if h.Port <= 0 {
		h.Port = remote.DefaultPort
	}
	if h.Tags == nil {
		h.Tags = []string{}
	}
	return h
}

func mergeHostSecrets(h core.Host, previous core.Host) core.Host {
	if h.PrivateKey == core.SecretMask {
		h.PrivateKey = previous.PrivateKey
	}
	if h.KeyPassphrase == core.SecretMask {
		h.KeyPassphrase = previous.KeyPassphrase
	}
	if h.Password == core.SecretMask {
		h.Password = previous.Password
	}
	return h
}

func validateHost(h core.Host) error {
	ve := newValidationError("invalid host")
	if strings.TrimSpace(h.Name) == "" {
		ve.field("name", "required")
	}
	if strings.TrimSpace(h.Address) == "" {
		ve.field("address", "required")
	}
	if strings.TrimSpace(h.User) == "" {
		ve.field("user", "required")
	}
	if h.Port < 1 || h.Port > 65535 {
		ve.field("port", "must be between 1 and 65535")
	}
	switch h.Auth {
	case core.HostAuthKey:
		if strings.TrimSpace(h.PrivateKey) == "" {
			ve.field("privateKey", "required for key authentication")
		}
	case core.HostAuthPassword:
		if h.Password == "" {
			ve.field("password", "required for password authentication")
		}
	default:
		ve.field("auth", "must be key or password")
	}
	if hk := strings.TrimSpace(h.HostKey); hk != "" && !strings.HasPrefix(hk, "SHA256:") {
		ve.field("hostKey", "must look like SHA256:abc...")
	}
	if !ve.empty() {
		return ve
	}
	if err := remote.Validate(h); err != nil {
		field := "privateKey"
		if h.Auth == core.HostAuthPassword {
			field = "password"
		}
		return newValidationError(err.Error()).field(field, err.Error())
	}
	return nil
}

func derivePublicKey(h core.Host, candidates ...string) (core.Host, error) {
	if h.Auth != core.HostAuthKey {
		h.PublicKey = firstNonEmpty(candidates)
		return h, nil
	}
	derived, err := remote.PublicKeyOf(h)
	if err != nil {
		return h, newValidationError(err.Error()).field("privateKey", err.Error())
	}
	for _, candidate := range candidates {
		if candidate != "" && strings.HasPrefix(candidate, derived) {
			h.PublicKey = candidate
			return h, nil
		}
	}
	h.PublicKey = derived
	return h, nil
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	page, err := pageFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	filter := store.HostFilter{
		Q:    strings.TrimSpace(r.URL.Query().Get("q")),
		Tag:  strings.TrimSpace(r.URL.Query().Get("tag")),
		Page: page,
	}
	items, total, err := s.store.Hosts.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	masked := make([]core.Host, len(items))
	for i, item := range items {
		masked[i] = item.Masked()
	}
	writeList(w, masked, total)
}

func (s *Server) handleGetHost(w http.ResponseWriter, r *http.Request) {
	host, err := s.store.Hosts.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, host.Masked())
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var req hostRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	host := mergeHostSecrets(hostFromRequest(req), core.Host{})
	if err := validateHost(host); err != nil {
		s.fail(w, r, err)
		return
	}
	host, err := derivePublicKey(host, host.PublicKey)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	stored, err := s.secrets.EncryptHost(host)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	saved, err := s.store.Hosts.Create(r.Context(), stored)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "host.create", "host", saved.ID, saved.Name, map[string]any{"address": saved.Address, "auth": string(saved.Auth)})
	writeJSON(w, http.StatusCreated, saved.Masked())
}

func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	existing, err := s.store.Hosts.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	previous, err := s.secrets.DecryptHost(existing)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req hostRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	host := mergeHostSecrets(hostFromRequest(req), previous)
	host.ID = existing.ID
	if err := validateHost(host); err != nil {
		s.fail(w, r, err)
		return
	}
	host, err = derivePublicKey(host, host.PublicKey, previous.PublicKey)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	stored, err := s.secrets.EncryptHost(host)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.Hosts.Update(ctx, stored); err != nil {
		s.fail(w, r, err)
		return
	}
	saved, err := s.store.Hosts.Get(ctx, existing.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "host.update", "host", saved.ID, saved.Name, map[string]any{"address": saved.Address, "auth": string(saved.Auth)})
	writeJSON(w, http.StatusOK, saved.Masked())
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	host, err := s.store.Hosts.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if host.SourceCount > 0 {
		s.writeError(w, r, http.StatusConflict, codeConflict, "host is used by sources")
		return
	}
	if err := s.store.Hosts.Delete(ctx, host.ID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.writeError(w, r, http.StatusConflict, codeConflict, "host is used by sources")
			return
		}
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "host.delete", "host", host.ID, host.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestHostConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req hostRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	var previous core.Host
	if id := strings.TrimSpace(req.ID); id != "" {
		if existing, err := s.store.Hosts.Get(ctx, id); err == nil {
			previous, err = s.secrets.DecryptHost(existing)
			if err != nil {
				s.fail(w, r, err)
				return
			}
		}
	}
	host := mergeHostSecrets(hostFromRequest(req), previous)
	result := s.engine.TestHost(ctx, host, s.log)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTestHost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stored, err := s.store.Hosts.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	host, err := s.secrets.DecryptHost(stored)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	result := s.engine.TestHost(ctx, host, s.log)
	message := ""
	if !result.OK {
		message = result.Message
	}
	if err := s.store.Hosts.SetTestResult(ctx, stored.ID, result.OK, message, result.OS, result.Tools); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "host.test", "host", stored.ID, stored.Name, map[string]any{"ok": result.OK})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) keyComment(r *http.Request) string {
	site := ""
	if settings, err := s.store.Settings.Get(r.Context()); err == nil {
		site = settings.SiteName
	}
	return "backvault@" + sanitizeComment(site)
}

func sanitizeComment(value string) string {
	var sb strings.Builder
	for _, ru := range strings.TrimSpace(value) {
		switch {
		case ru >= 'a' && ru <= 'z', ru >= '0' && ru <= '9', ru == '.', ru == '-', ru == '_':
			sb.WriteRune(ru)
		case ru >= 'A' && ru <= 'Z':
			sb.WriteRune(ru + 32)
		case ru == ' ':
			sb.WriteByte('-')
		}
	}
	cleaned := strings.Trim(sb.String(), "-.")
	if cleaned == "" {
		return "backvault"
	}
	return cleaned
}

func (s *Server) handleKeygen(w http.ResponseWriter, r *http.Request) {
	private, public, err := remote.GenerateKey(s.keyComment(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "host.keygen", "host", "", "", nil)
	writeJSON(w, http.StatusOK, keygenResponse{PrivateKey: private, PublicKey: public})
}

func (s *Server) handleHostKeygen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	existing, err := s.store.Hosts.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	previous, err := s.secrets.DecryptHost(existing)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	private, public, err := remote.GenerateKey(s.keyComment(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	host := previous
	host.Auth = core.HostAuthKey
	host.PrivateKey = private
	host.KeyPassphrase = ""
	host.PublicKey = public
	stored, err := s.secrets.EncryptHost(host)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.Hosts.Update(ctx, stored); err != nil {
		s.fail(w, r, err)
		return
	}
	saved, err := s.store.Hosts.Get(ctx, existing.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "host.keygen", "host", saved.ID, saved.Name, nil)
	writeJSON(w, http.StatusOK, saved.Masked())
}
