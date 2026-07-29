package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pquerna/otp/totp"
	_ "modernc.org/sqlite"

	"github.com/jeni0101/vpn/server/internal/security"
)

var ErrUnauthorized = errors.New("unauthorized")
var ErrNoAdmin = errors.New("administrator is not initialized")

type Store struct {
	db     *sql.DB
	sealer *security.Sealer
	now    func() time.Time
}

type InitResult struct {
	Username      string
	ProvisionURI  string
	RecoveryCodes []string
}

type Session struct {
	Token     string
	CSRF      string
	Username  string
	Stage     string
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
}

func Open(path string, sealer *security.Sealer) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, sealer: sealer, now: time.Now}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS administrator (
			id INTEGER PRIMARY KEY CHECK(id=1),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			totp_sealed BLOB NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS recovery_codes (
			code_hash BLOB PRIMARY KEY,
			used_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash BLOB PRIMARY KEY,
			csrf TEXT NOT NULL,
			username TEXT NOT NULL,
			stage TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_seen TEXT NOT NULL,
			expires_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Initialize(ctx context.Context, username, password string) (InitResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 || strings.ContainsAny(username, "\r\n\t") {
		return InitResult{}, errors.New("invalid username")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM administrator`).Scan(&count); err != nil {
		return InitResult{}, err
	}
	if count != 0 {
		return InitResult{}, errors.New("administrator is already initialized")
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return InitResult{}, err
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "TNest VPN",
		AccountName: username,
		Period:      30,
		SecretSize:  20,
	})
	if err != nil {
		return InitResult{}, err
	}
	sealed, err := s.sealer.Seal([]byte(key.Secret()), "admin-totp")
	if err != nil {
		return InitResult{}, err
	}
	codes, hashes, err := recoveryCodes(10)
	if err != nil {
		return InitResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InitResult{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO administrator(id,username,password_hash,totp_sealed,created_at)
		VALUES(1,?,?,?,?)`, username, passwordHash, sealed,
		s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return InitResult{}, err
	}
	for _, hash := range hashes {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO recovery_codes(code_hash) VALUES(?)`, hash); err != nil {
			return InitResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return InitResult{}, err
	}
	return InitResult{Username: username, ProvisionURI: key.URL(), RecoveryCodes: codes}, nil
}

func (s *Store) CheckPassword(ctx context.Context, username, password string) bool {
	var expectedUser, hash string
	err := s.db.QueryRowContext(ctx, `
		SELECT username,password_hash FROM administrator WHERE id=1`).Scan(&expectedUser, &hash)
	if err != nil || subtleString(expectedUser, strings.TrimSpace(username)) == false {
		dummy, _ := HashPassword("not-the-real-password")
		_ = VerifyPassword(dummy, password)
		return false
	}
	return VerifyPassword(hash, password)
}

func (s *Store) CheckTOTP(ctx context.Context, code string) bool {
	var sealed []byte
	if err := s.db.QueryRowContext(ctx, `
		SELECT totp_sealed FROM administrator WHERE id=1`).Scan(&sealed); err != nil {
		return false
	}
	secret, err := s.sealer.Open(sealed, "admin-totp")
	if err != nil {
		return false
	}
	return totp.Validate(strings.TrimSpace(code), string(secret))
}

func (s *Store) UseRecoveryCode(ctx context.Context, code string) bool {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	hash := sha256.Sum256([]byte(normalized))
	result, err := s.db.ExecContext(ctx, `
		UPDATE recovery_codes SET used_at=? WHERE code_hash=? AND used_at IS NULL`,
		s.now().UTC().Format(time.RFC3339Nano), hash[:])
	if err != nil {
		return false
	}
	count, _ := result.RowsAffected()
	return count == 1
}

func (s *Store) NewSession(ctx context.Context, username, stage string) (Session, error) {
	token, tokenHash, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, _, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	now := s.now().UTC()
	session := Session{
		Token: token, CSRF: csrf, Username: username, Stage: stage,
		CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(12 * time.Hour),
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions(token_hash,csrf,username,stage,created_at,last_seen,expires_at)
		VALUES(?,?,?,?,?,?,?)`, tokenHash, csrf, username, stage,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		session.ExpiresAt.Format(time.RFC3339Nano))
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) Session(ctx context.Context, token string) (Session, error) {
	hash := sha256.Sum256([]byte(token))
	var session Session
	var created, seen, expires string
	err := s.db.QueryRowContext(ctx, `
		SELECT csrf,username,stage,created_at,last_seen,expires_at
		FROM sessions WHERE token_hash=?`, hash[:]).Scan(&session.CSRF,
		&session.Username, &session.Stage, &created, &seen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	session.Token = token
	session.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	session.LastSeen, _ = time.Parse(time.RFC3339Nano, seen)
	session.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	now := s.now().UTC()
	if now.After(session.ExpiresAt) || now.Sub(session.LastSeen) > 30*time.Minute {
		_ = s.DeleteSession(ctx, token)
		return Session{}, ErrUnauthorized
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen=? WHERE token_hash=?`,
		now.Format(time.RFC3339Nano), hash[:])
	session.LastSeen = now
	return session, nil
}

func (s *Store) PromoteSession(ctx context.Context, token string) error {
	hash := sha256.Sum256([]byte(token))
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET stage='authenticated',last_seen=?
		WHERE token_hash=? AND stage='totp'`,
		s.now().UTC().Format(time.RFC3339Nano), hash[:])
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrUnauthorized
	}
	return nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, hash[:])
	return err
}

func randomToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func recoveryCodes(count int) ([]string, [][]byte, error) {
	codes := make([]string, 0, count)
	hashes := make([][]byte, 0, count)
	for range count {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		plain := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
		code := fmt.Sprintf("%s-%s-%s-%s", plain[0:4], plain[4:8], plain[8:12], plain[12:16])
		hash := sha256.Sum256([]byte(plain))
		codes = append(codes, code)
		hashes = append(hashes, append([]byte(nil), hash[:]...))
	}
	return codes, hashes, nil
}

func subtleString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range len(a) {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	now      func() time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{attempts: make(map[string][]time.Time), now: time.Now}
}

func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	cutoff := now.Add(-15 * time.Minute)
	values := r.attempts[key][:0]
	for _, value := range r.attempts[key] {
		if value.After(cutoff) {
			values = append(values, value)
		}
	}
	r.attempts[key] = values
	return len(values) < 5
}

func (r *RateLimiter) Fail(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts[key] = append(r.attempts[key], r.now())
}

func (r *RateLimiter) Success(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.attempts, key)
}
