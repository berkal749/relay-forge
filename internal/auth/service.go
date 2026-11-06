package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"relayforge/internal/database/db"
	"relayforge/internal/invite"
)

const (
	minPasswordLength = 8
	maxPasswordLength = 72
	maxEmailLength    = 254
	sessionTokenBytes = 32

	RegistrationOpen   = "open"
	RegistrationInvite = "invite"
	RegistrationClosed = "closed"
)

var (
	ErrInvalidEmail         = errors.New("invalid email")
	ErrWeakPassword         = errors.New("weak password")
	ErrEmailTaken           = errors.New("email already registered")
	ErrInvalidCredentials   = errors.New("invalid email or password")
	ErrInvalidSession       = errors.New("invalid session")
	ErrSessionExpired       = errors.New("session expired")
	ErrRegistrationClosed   = errors.New("registration is closed")
	ErrInviteRequired       = errors.New("an invite token is required to register")
	ErrInviteInvalid        = errors.New("invite token is invalid or already used")
	ErrInviteExpired        = errors.New("invite token has expired")
	ErrInviteEmailMismatch  = errors.New("invite token was issued for a different email")
	ErrRegistrationDisabled = errors.New("registration is not enabled")
)

type Service struct {
	queries             *db.Queries
	pool                *pgxpool.Pool
	sessionTTL          time.Duration
	registrationMode    string
	bootstrapAdminEmail string
}

type Options struct {
	Pool                *pgxpool.Pool
	RegistrationMode    string
	BootstrapAdminEmail string
}

type Session struct {
	Token     string
	ExpiresAt time.Time
	User      db.User
}

func NewService(queries *db.Queries, sessionTTL time.Duration, options Options) *Service {
	mode := strings.ToLower(strings.TrimSpace(options.RegistrationMode))
	if mode == "" {
		mode = RegistrationOpen
	}
	return &Service{
		queries:             queries,
		pool:                options.Pool,
		sessionTTL:          sessionTTL,
		registrationMode:    mode,
		bootstrapAdminEmail: strings.ToLower(strings.TrimSpace(options.BootstrapAdminEmail)),
	}
}

func (s *Service) RegistrationMode() string {
	return s.registrationMode
}

func (s *Service) Register(ctx context.Context, email, password, inviteToken string) (db.User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return db.User{}, err
	}
	if err := validatePassword(password); err != nil {
		return db.User{}, err
	}

	bootstrapAdmin := s.bootstrapAdminEmail != "" && email == s.bootstrapAdminEmail

	switch s.registrationMode {
	case RegistrationClosed:
		return db.User{}, ErrRegistrationClosed
	case RegistrationInvite:
		if !bootstrapAdmin && strings.TrimSpace(inviteToken) == "" {
			return db.User{}, ErrInviteRequired
		}
	case RegistrationOpen:
		// Open registration is intended for local development only.
	default:
		return db.User{}, ErrRegistrationDisabled
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return db.User{}, fmt.Errorf("hash password: %w", err)
	}

	if s.pool == nil {
		return s.registerWithoutTransaction(ctx, email, string(passwordHash), inviteToken, bootstrapAdmin)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.User{}, fmt.Errorf("begin registration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	role := invite.RoleUser
	if bootstrapAdmin {
		role = invite.RoleAdmin
	}

	var pendingInvite *db.Invite
	if s.registrationMode == RegistrationInvite && !bootstrapAdmin {
		found, err := queries.GetInviteByTokenHashForUpdate(ctx, invite.HashToken(inviteToken))
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, ErrInviteInvalid
		}
		if err != nil {
			return db.User{}, fmt.Errorf("get invite: %w", err)
		}
		if found.ConsumedAt != nil {
			return db.User{}, ErrInviteInvalid
		}
		if !found.ExpiresAt.After(time.Now()) {
			return db.User{}, ErrInviteExpired
		}
		if found.Email != nil && *found.Email != email {
			return db.User{}, ErrInviteEmailMismatch
		}
		role = found.Role
		pendingInvite = &found
	}

	user, err := queries.CreateUserWithRole(ctx, db.CreateUserWithRoleParams{
		Email:        email,
		PasswordHash: string(passwordHash),
		Role:         role,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.User{}, ErrEmailTaken
		}
		return db.User{}, fmt.Errorf("create user: %w", err)
	}

	if pendingInvite != nil {
		if _, err := queries.ConsumeInvite(ctx, db.ConsumeInviteParams{
			ID:         pendingInvite.ID,
			ConsumedBy: &user.ID,
		}); err != nil {
			return db.User{}, fmt.Errorf("consume invite: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return db.User{}, fmt.Errorf("commit registration transaction: %w", err)
	}

	return user, nil
}

// registerWithoutTransaction supports unit tests and non-transactional callers.
// The API always supplies a pool, which keeps user creation and invite
// consumption atomic.
func (s *Service) registerWithoutTransaction(ctx context.Context, email, passwordHash, inviteToken string, bootstrapAdmin bool) (db.User, error) {
	if s.registrationMode == RegistrationInvite && !bootstrapAdmin {
		found, err := s.queries.GetInviteByTokenHash(ctx, invite.HashToken(inviteToken))
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, ErrInviteInvalid
		}
		if err != nil {
			return db.User{}, fmt.Errorf("get invite: %w", err)
		}
		if found.ConsumedAt != nil {
			return db.User{}, ErrInviteInvalid
		}
		if !found.ExpiresAt.After(time.Now()) {
			return db.User{}, ErrInviteExpired
		}
		if found.Email != nil && *found.Email != email {
			return db.User{}, ErrInviteEmailMismatch
		}
	}

	role := invite.RoleUser
	if bootstrapAdmin {
		role = invite.RoleAdmin
	}
	user, err := s.queries.CreateUserWithRole(ctx, db.CreateUserWithRoleParams{
		Email:        email,
		PasswordHash: passwordHash,
		Role:         role,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.User{}, ErrEmailTaken
		}
		return db.User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	email, err := normalizeEmail(email)
	if err != nil || password == "" {
		return Session{}, ErrInvalidCredentials
	}

	user, err := s.queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return Session{}, ErrInvalidCredentials
	}

	token, tokenHash, err := newSessionToken()
	if err != nil {
		return Session{}, err
	}

	expiresAt := time.Now().Add(s.sessionTTL)
	if _, err := s.queries.CreateSession(ctx, db.CreateSessionParams{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}); err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}

	return Session{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      user,
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (db.User, error) {
	if token == "" {
		return db.User{}, ErrInvalidSession
	}

	session, err := s.queries.GetSessionByTokenHash(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, ErrInvalidSession
	}
	if err != nil {
		return db.User{}, fmt.Errorf("get session: %w", err)
	}

	if session.RevokedAt != nil {
		return db.User{}, ErrInvalidSession
	}
	if !session.ExpiresAt.After(time.Now()) {
		return db.User{}, ErrSessionExpired
	}

	user, err := s.queries.GetUserByID(ctx, session.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, ErrInvalidSession
	}
	if err != nil {
		return db.User{}, fmt.Errorf("get session user: %w", err)
	}

	return user, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return ErrInvalidSession
	}
	if err := s.queries.RevokeSession(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func normalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" || len(email) > maxEmailLength {
		return "", ErrInvalidEmail
	}

	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", ErrInvalidEmail
	}

	return email, nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return ErrWeakPassword
	}
	return nil
}

func newSessionToken() (token string, tokenHash string, err error) {
	random := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(random)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
