package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

const (
	CookieName     = "backvault_session"
	SessionTTL     = 30 * 24 * time.Hour
	TokenPrefix    = "bvt_"
	tokenBodyLen   = 32
	base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrSessionExpired     = errors.New("session expired")
	ErrTokenExpired       = errors.New("token expired")
)

type Manager struct {
	store *store.Store
}

func NewManager(s *store.Store) *Manager {
	return &Manager{store: s}
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomBase62(n int) (string, error) {
	max := big.NewInt(int64(len(base62Alphabet)))
	var sb strings.Builder
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate token: %w", err)
		}
		sb.WriteByte(base62Alphabet[idx.Int64()])
	}
	return sb.String(), nil
}

func (m *Manager) Login(ctx context.Context, email, password string) (core.User, error) {
	rec, err := m.store.Users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			_, _ = VerifyPassword("$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", password)
			return core.User{}, ErrInvalidCredentials
		}
		return core.User{}, err
	}
	ok, err := VerifyPassword(rec.PasswordHash, password)
	if err != nil || !ok {
		return core.User{}, ErrInvalidCredentials
	}
	now := time.Now().UTC()
	if err := m.store.Users.TouchLogin(ctx, rec.ID, now); err != nil {
		return core.User{}, err
	}
	rec.LastLoginAt = &now
	return rec.User, nil
}

func (m *Manager) CreateSession(ctx context.Context, userID, ip, userAgent string) (string, time.Time, error) {
	secret, err := randomSecret(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(SessionTTL)
	sess := store.Session{
		ID:         hashSecret(secret),
		UserID:     userID,
		CreatedAt:  now,
		ExpiresAt:  expires,
		LastSeenAt: now,
		IP:         ip,
		UserAgent:  userAgent,
	}
	if err := m.store.Sessions.Create(ctx, sess); err != nil {
		return "", time.Time{}, err
	}
	return secret, expires, nil
}

func (m *Manager) ResolveSession(ctx context.Context, secret string) (*Principal, error) {
	id := hashSecret(secret)
	sess, err := m.store.Sessions.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) {
		_ = m.store.Sessions.Delete(ctx, id)
		return nil, ErrSessionExpired
	}
	rec, err := m.store.Users.Get(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	if now.Sub(sess.LastSeenAt) > time.Hour {
		if err := m.store.Sessions.Touch(ctx, id, now, now.Add(SessionTTL)); err != nil {
			return nil, err
		}
	}
	user := rec.User
	return &Principal{
		Type:      AuthSession,
		User:      &user,
		Scopes:    ScopesForRole(user.Role),
		SessionID: id,
	}, nil
}

func (m *Manager) DestroySession(ctx context.Context, secret string) error {
	return m.store.Sessions.Delete(ctx, hashSecret(secret))
}

type NewToken struct {
	Token  core.APIToken
	Secret string
}

func (m *Manager) CreateToken(ctx context.Context, t core.APIToken) (NewToken, error) {
	body, err := randomBase62(tokenBodyLen)
	if err != nil {
		return NewToken{}, err
	}
	secret := TokenPrefix + body
	t.Prefix = secret[:len(TokenPrefix)+8]
	saved, err := m.store.Tokens.Create(ctx, t, hashSecret(secret))
	if err != nil {
		return NewToken{}, err
	}
	return NewToken{Token: saved, Secret: secret}, nil
}

func (m *Manager) ResolveToken(ctx context.Context, secret string) (*Principal, error) {
	tok, err := m.store.Tokens.GetByHash(ctx, hashSecret(secret))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if tok.ExpiresAt != nil && now.After(*tok.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	if tok.LastUsedAt == nil || now.Sub(*tok.LastUsedAt) > time.Minute {
		if err := m.store.Tokens.TouchUsed(ctx, tok.ID, now); err != nil {
			return nil, err
		}
	}
	return &Principal{
		Type:      AuthToken,
		TokenID:   tok.ID,
		TokenName: tok.Name,
		Scopes:    tok.Scopes,
		JobSlugs:  tok.JobSlugs,
	}, nil
}

func (m *Manager) SetPassword(ctx context.Context, userID, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return m.store.Users.SetPassword(ctx, userID, hash)
}

func (m *Manager) CreateUser(ctx context.Context, u core.User, password string) (core.User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return core.User{}, err
	}
	return m.store.Users.Create(ctx, u, hash)
}

func (m *Manager) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := m.store.Users.Count(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func (m *Manager) BootstrapAdmin(ctx context.Context, email, password string) (bool, error) {
	if strings.TrimSpace(email) == "" || password == "" {
		return false, nil
	}
	needs, err := m.NeedsSetup(ctx)
	if err != nil {
		return false, err
	}
	if !needs {
		return false, nil
	}
	if _, err := m.CreateUser(ctx, core.User{Email: email, Name: "Administrator", Role: core.RoleAdmin}, password); err != nil {
		return false, err
	}
	return true, nil
}

func ValidatePassword(password string) error {
	if len(password) < 10 {
		return errors.New("password must be at least 10 characters")
	}
	if len(password) > 512 {
		return errors.New("password must be at most 512 characters")
	}
	return nil
}
