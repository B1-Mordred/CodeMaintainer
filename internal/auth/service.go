package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	defaultSessionTTL = 12 * time.Hour
	defaultReauthTTL  = 5 * time.Minute
)

type passwordParameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	keyLength   uint32
}

var securePasswordParameters = passwordParameters{
	memory: 64 * 1024, iterations: 3, parallelism: 2, keyLength: 32,
}

type attemptWindow struct {
	started  time.Time
	failures int
}

type Service struct {
	store      Store
	now        func() time.Time
	passwords  passwordParameters
	sessionTTL time.Duration
	reauthTTL  time.Duration
	dummyHash  string
	attemptMu  sync.Mutex
	attempts   map[string]attemptWindow
}

type Credentials struct {
	Username    string
	DisplayName string
	Password    string
}

type Result struct {
	Principal Principal `json:"principal"`
	Token     string    `json:"-"`
	CSRFToken string    `json:"csrf_token"`
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("authentication store is required")
	}
	service := &Service{
		store: store, now: func() time.Time { return time.Now().UTC() },
		passwords: securePasswordParameters, sessionTTL: defaultSessionTTL,
		reauthTTL: defaultReauthTTL, attempts: make(map[string]attemptWindow),
	}
	dummy, err := service.hashPassword("dummy-password-never-used")
	if err != nil {
		return nil, err
	}
	service.dummyHash = dummy
	return service, nil
}

func (s *Service) BootstrapStatus(ctx context.Context) (bool, error) {
	return s.store.BootstrapStatus(ctx)
}

func (s *Service) Bootstrap(ctx context.Context, credentials Credentials, remote string) (Result, error) {
	complete, err := s.store.BootstrapStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if complete {
		return Result{}, ErrAlreadyBootstrapped
	}
	username, displayName, err := validateCredentials(credentials, true)
	if err != nil {
		return Result{}, err
	}
	if err := s.allowAttempt(remote, username); err != nil {
		return Result{}, err
	}
	passwordHash, err := s.hashPassword(credentials.Password)
	if err != nil {
		return Result{}, err
	}
	now := s.now()
	userID, err := randomID("user")
	if err != nil {
		return Result{}, err
	}
	result, session, err := s.newResult(User{
		ID: userID, Username: username, DisplayName: displayName,
		Role: RoleAdministrator, CreatedAt: now, UpdatedAt: now,
	}, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.store.BootstrapAdministrator(ctx, result.Principal.User, passwordHash, session, remote); err != nil {
		return Result{}, err
	}
	s.clearAttempts(remote, username)
	return result, nil
}

func (s *Service) Login(ctx context.Context, credentials Credentials, remote string) (Result, error) {
	username, _, err := validateCredentials(credentials, false)
	if err != nil {
		return Result{}, ErrInvalidCredentials
	}
	if err := s.allowAttempt(remote, username); err != nil {
		return Result{}, err
	}
	user, passwordHash, lookupErr := s.store.FindUserByUsername(ctx, username)
	hashToCheck := passwordHash
	if lookupErr != nil {
		hashToCheck = s.dummyHash
	}
	passwordOK := s.verifyPassword(credentials.Password, hashToCheck)
	if lookupErr != nil || !passwordOK || user.Disabled {
		s.recordFailure(remote, username)
		return Result{}, ErrInvalidCredentials
	}
	now := s.now()
	result, session, err := s.newResult(user, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.store.CreateSession(ctx, session, remote); err != nil {
		return Result{}, err
	}
	s.clearAttempts(remote, username)
	return result, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if len(token) < 32 || len(token) > 256 {
		return Principal{}, ErrInvalidCredentials
	}
	tokenHash := sha256.Sum256([]byte(token))
	session, err := s.store.SessionByTokenHash(ctx, tokenHash[:])
	if err != nil {
		return Principal{}, ErrInvalidCredentials
	}
	now := s.now()
	if session.User.Disabled || !session.RevokedAt.IsZero() || !now.Before(session.ExpiresAt) {
		return Principal{}, ErrSessionExpired
	}
	return Principal{
		User: session.User, SessionID: session.ID, ExpiresAt: session.ExpiresAt,
		ReauthenticatedUntil: session.ReauthenticatedUntil,
		CSRFHash:             append([]byte(nil), session.CSRFHash...),
	}, nil
}

func (s *Service) RotateCSRF(ctx context.Context, principal Principal) (string, Principal, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", Principal{}, err
	}
	now := s.now()
	if err := s.store.RotateSessionCSRF(ctx, principal.SessionID, hash, now); err != nil {
		return "", Principal{}, err
	}
	principal.CSRFHash = hash
	return raw, principal, nil
}

func (s *Service) VerifyCSRF(principal Principal, token string) bool {
	if token == "" || len(token) > 256 || len(principal.CSRFHash) != sha256.Size {
		return false
	}
	hash := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(principal.CSRFHash, hash[:]) == 1
}

func (s *Service) Reauthenticate(ctx context.Context, principal Principal, password, remote string) (Principal, error) {
	if err := s.allowAttempt(remote, principal.User.Username); err != nil {
		return Principal{}, err
	}
	user, passwordHash, err := s.store.FindUserByUsername(ctx, principal.User.Username)
	if err != nil || user.ID != principal.User.ID || !s.verifyPassword(password, passwordHash) {
		s.recordFailure(remote, principal.User.Username)
		return Principal{}, ErrInvalidCredentials
	}
	now := s.now()
	until := now.Add(s.reauthTTL)
	if err := s.store.MarkSessionReauthenticated(ctx, principal.SessionID, until, now, remote); err != nil {
		return Principal{}, err
	}
	principal.ReauthenticatedUntil = until
	s.clearAttempts(remote, principal.User.Username)
	return principal, nil
}

func (s *Service) Logout(ctx context.Context, principal Principal, remote string) error {
	return s.store.RevokeSession(ctx, principal.SessionID, s.now(), remote)
}

func (s *Service) newResult(user User, now time.Time) (Result, Session, error) {
	token, tokenHash, err := newToken()
	if err != nil {
		return Result{}, Session{}, err
	}
	csrf, csrfHash, err := newToken()
	if err != nil {
		return Result{}, Session{}, err
	}
	sessionID, err := randomID("session")
	if err != nil {
		return Result{}, Session{}, err
	}
	session := Session{
		ID: sessionID, TokenHash: tokenHash, CSRFHash: csrfHash, User: user,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.sessionTTL),
	}
	principal := Principal{User: user, SessionID: sessionID, ExpiresAt: session.ExpiresAt, CSRFHash: csrfHash}
	return Result{Principal: principal, Token: token, CSRFToken: csrf}, session, nil
}

func validateCredentials(credentials Credentials, bootstrap bool) (string, string, error) {
	username := strings.ToLower(strings.TrimSpace(credentials.Username))
	if len(username) < 3 || len(username) > 64 {
		return "", "", ErrInvalidInput
	}
	for _, character := range username {
		if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-", character)) {
			return "", "", ErrInvalidInput
		}
	}
	if len(credentials.Password) < 14 || len(credentials.Password) > 1024 || !utf8.ValidString(credentials.Password) {
		return "", "", ErrInvalidInput
	}
	displayName := strings.TrimSpace(credentials.DisplayName)
	if bootstrap {
		if displayName == "" {
			displayName = username
		}
		if len(displayName) > 128 || !utf8.ValidString(displayName) {
			return "", "", ErrInvalidInput
		}
	}
	return username, displayName, nil
}

func (s *Service) hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, s.passwords.iterations, s.passwords.memory, s.passwords.parallelism, s.passwords.keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		s.passwords.memory, s.passwords.iterations, s.passwords.parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (s *Service) verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return false
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}
	if memory < 8*1024 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 8 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(expected, actual) == 1
}

func newToken() (string, []byte, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", nil, fmt.Errorf("generate authentication token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(raw))
	return raw, hash[:], nil
}

func randomID(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate authentication identifier: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(bytes), nil
}

func (s *Service) attemptKey(remote, username string) string {
	return strings.TrimSpace(remote) + "\x00" + strings.ToLower(strings.TrimSpace(username))
}

func (s *Service) allowAttempt(remote, username string) error {
	now := s.now()
	key := s.attemptKey(remote, username)
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	window := s.attempts[key]
	if window.started.IsZero() || now.Sub(window.started) >= 5*time.Minute {
		delete(s.attempts, key)
		return nil
	}
	if window.failures >= 5 {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) recordFailure(remote, username string) {
	now := s.now()
	key := s.attemptKey(remote, username)
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	window := s.attempts[key]
	if window.started.IsZero() || now.Sub(window.started) >= 5*time.Minute {
		window = attemptWindow{started: now}
	}
	window.failures++
	s.attempts[key] = window
}

func (s *Service) clearAttempts(remote, username string) {
	s.attemptMu.Lock()
	delete(s.attempts, s.attemptKey(remote, username))
	s.attemptMu.Unlock()
}
