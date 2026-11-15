package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidPassword   = errors.New("invalid password")
	ErrRateLimited       = errors.New("too many attempts; try again later")
	ErrAlreadyConfigured = errors.New("password is already configured")
	ErrNotAuthenticated  = errors.New("not authenticated")
)

const (
	passwordSetting = "auth.password_hash"
	sessionTTL      = 7 * 24 * time.Hour
)

type Session struct {
	ID        string    `json:"-"`
	CSRF      string    `json:"csrf"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Manager struct {
	db          *sql.DB
	envPassword string
	limiter     *limiter
}

func New(ctx context.Context, db *sql.DB, envPassword string) (*Manager, error) {
	manager := &Manager{
		db:          db,
		envPassword: envPassword,
		limiter:     newLimiter(10, 5*time.Minute),
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return nil, fmt.Errorf("cleanup sessions: %w", err)
	}
	return manager, nil
}

func (m *Manager) PasswordConfigured(ctx context.Context) (bool, error) {
	if m.envPassword != "" {
		return true, nil
	}
	var value string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, passwordSetting).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read password setting: %w", err)
	}
	return value != "", nil
}

func (m *Manager) SetPassword(ctx context.Context, password string) error {
	if len(strings.TrimSpace(password)) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	configured, err := m.PasswordConfigured(ctx)
	if err != nil {
		return err
	}
	if configured {
		return ErrAlreadyConfigured
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	result, err := m.db.ExecContext(ctx, `
		INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value WHERE settings.value = ''`, passwordSetting, hash)
	if err != nil {
		return fmt.Errorf("store password: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check password setup result: %w", err)
	}
	if affected == 0 {
		return ErrAlreadyConfigured
	}
	return nil
}

func (m *Manager) Login(ctx context.Context, password, clientIP string) (*Session, error) {
	if !m.limiter.Allow(clientIP) {
		return nil, ErrRateLimited
	}
	ok, err := m.verify(ctx, password)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidPassword
	}
	m.limiter.Success(clientIP)

	id, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	session := &Session{
		ID:        id,
		CSRF:      csrf,
		Role:      "admin",
		CreatedAt: now,
		ExpiresAt: now.Add(sessionTTL),
	}
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO sessions(id, csrf, role, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		session.ID, session.CSRF, session.Role,
		session.CreatedAt.Format(time.RFC3339), session.ExpiresAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

func (m *Manager) Authenticate(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrNotAuthenticated
	}
	var session Session
	var created, expires string
	err := m.db.QueryRowContext(ctx, `
		SELECT id, csrf, role, created_at, expires_at
		FROM sessions WHERE id = ?`, token).Scan(&session.ID, &session.CSRF, &session.Role, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotAuthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	session.CreatedAt, _ = time.Parse(time.RFC3339, created)
	session.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = m.Logout(ctx, token)
		return nil, ErrNotAuthenticated
	}
	return &session, nil
}

func (m *Manager) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, token)
	return err
}

func (m *Manager) verify(ctx context.Context, password string) (bool, error) {
	if m.envPassword != "" {
		return subtle.ConstantTimeCompare([]byte(password), []byte(m.envPassword)) == 1, nil
	}
	var hash string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, passwordSetting).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read password hash: %w", err)
	}
	return VerifyPassword(password, hash)
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	const (
		memory      = 32 * 1024
		iterations  = 3
		parallelism = 2
		keyLength   = 32
	)
	hash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("unsupported password hash")
	}
	var memory, iterations, parallelism int
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, fmt.Errorf("parse password hash: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode salt: %w", err)
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func randomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

type limiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string]*attempt
}

type attempt struct {
	count int
	reset time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, attempts: map[string]*attempt{}}
}

func (l *limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for attemptKey, entry := range l.attempts {
		if now.After(entry.reset) {
			delete(l.attempts, attemptKey)
		}
	}
	entry, ok := l.attempts[key]
	if !ok || now.After(entry.reset) {
		l.attempts[key] = &attempt{count: 1, reset: now.Add(l.window)}
		return true
	}
	if entry.count >= l.max {
		return false
	}
	entry.count++
	return true
}

func (l *limiter) Success(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func SessionTTL() time.Duration { return sessionTTL }
