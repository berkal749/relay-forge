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
	"golang.org/x/crypto/bcrypt"

	"relayforge/internal/database/db"
)

const (
	minPasswordLength = 8
	maxPasswordLength = 72
	maxEmailLength    = 254
	sessionTokenBytes = 32
)

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrWeakPassword       = errors.New("weak password")
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidSession     = errors.New("invalid session")
	ErrSessionExpired     = errors.New("session expired")
)

type Service struct {
	queries    *db.Queries
	sessionTTL time.Duration
}

type Session struct {
	Token     string
	ExpiresAt time.Time
	User      db.User
}

func NewService(queries *db.Queries, sessionTTL time.Duration) *Service {
	return &Service{
		queries:    queries,
		sessionTTL: sessionTTL,
	}
}

func (s *Service) Register(ctx context.Context, email, password string) (db.User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return db.User{}, err
	}
	if err := validatePassword(password); err != nil {
		return db.User{}, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return db.User{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.queries.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		PasswordHash: string(passwordHash),
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
