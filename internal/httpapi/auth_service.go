package httpapi

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	paseto "aidanwoods.dev/go-paseto"
)

const (
	issuerName  = "nekomimi"
	subjectName = "system"

	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
)

var (
	ErrInvalidPassphrase        = errors.New("invalid passphrase")
	ErrInvalidCurrentPassphrase = errors.New("invalid current passphrase")
	ErrWeakPassphrase           = errors.New("weak passphrase")
	ErrInvalidRefreshToken      = errors.New("invalid refresh token")
	ErrUnauthorized             = errors.New("unauthorized")
)

type AuthServiceConfig struct {
	KeyHex      string
	AccessTTL   time.Duration
	RefreshTTL  time.Duration
	Passphrases *authStateStore
}

type TokenPair struct {
	AccessToken      string
	RefreshToken     string
	TokenType        string
	ExpiresIn        int
	RefreshExpiresIn int
}

type AuthService struct {
	state      *authStateStore
	key        paseto.V4SymmetricKey
	accessTTL  time.Duration
	refreshTTL time.Duration
	refresh    *refreshStore
}

func NewAuthService(cfg AuthServiceConfig) (*AuthService, error) {
	if cfg.Passphrases == nil {
		return nil, fmt.Errorf("auth state store is required")
	}
	key, err := paseto.V4SymmetricKeyFromHex(cfg.KeyHex)
	if err != nil {
		return nil, fmt.Errorf("parse paseto key failed: %w", err)
	}

	return &AuthService{
		state:      cfg.Passphrases,
		key:        key,
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
		refresh:    newRefreshStore(),
	}, nil
}

func (s *AuthService) Login(passphrase string) (TokenPair, error) {
	version, err := s.state.authenticate(passphrase)
	if err != nil {
		if errors.Is(err, errStoreInvalidPassphrase) {
			return TokenPair{}, ErrInvalidPassphrase
		}
		return TokenPair{}, err
	}
	return s.issueTokenPair(version)
}

func (s *AuthService) Refresh(refreshToken string) (TokenPair, error) {
	claims, err := s.parseAndValidateToken(refreshToken, tokenTypeRefresh)
	if err != nil {
		return TokenPair{}, ErrInvalidRefreshToken
	}
	version, err := s.state.currentTokenVersion()
	if err != nil {
		return TokenPair{}, err
	}
	if version != claims.Version {
		return TokenPair{}, ErrInvalidRefreshToken
	}
	if !s.refresh.consume(claims.JTI) {
		return TokenPair{}, ErrInvalidRefreshToken
	}
	return s.issueTokenPair(version)
}

func (s *AuthService) VerifyAccessToken(accessToken string) error {
	claims, err := s.parseAndValidateToken(accessToken, tokenTypeAccess)
	if err != nil {
		return ErrUnauthorized
	}
	version, err := s.state.currentTokenVersion()
	if err != nil {
		return ErrUnauthorized
	}
	if version != claims.Version {
		return ErrUnauthorized
	}
	return nil
}

func (s *AuthService) RotatePassphrase(currentPassphrase string, newPassphrase string) error {
	if _, err := s.state.rotatePassphrase(currentPassphrase, newPassphrase); err != nil {
		if errors.Is(err, errStoreInvalidPassphrase) {
			return ErrInvalidCurrentPassphrase
		}
		if errors.Is(err, errStoreWeakPassphrase) {
			return ErrWeakPassphrase
		}
		return err
	}
	s.refresh.reset()
	return nil
}

func ParseBearerToken(raw string) (string, error) {
	const prefix = "Bearer "
	if raw == "" || !strings.HasPrefix(raw, prefix) {
		return "", ErrUnauthorized
	}
	token := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	if token == "" {
		return "", ErrUnauthorized
	}
	return token, nil
}

type tokenClaims struct {
	Type    string
	JTI     string
	Version int64
}

func (s *AuthService) parseAndValidateToken(raw string, expectedType string) (tokenClaims, error) {
	parser := paseto.NewParser()
	parser.AddRule(paseto.ValidAt(time.Now()))

	token, err := parser.ParseV4Local(s.key, raw, nil)
	if err != nil {
		return tokenClaims{}, err
	}

	issuer, err := token.GetString("iss")
	if err != nil || issuer != issuerName {
		return tokenClaims{}, ErrUnauthorized
	}
	sub, err := token.GetString("sub")
	if err != nil || sub != subjectName {
		return tokenClaims{}, ErrUnauthorized
	}
	typ, err := token.GetString("typ")
	if err != nil || typ != expectedType {
		return tokenClaims{}, ErrUnauthorized
	}
	jti, err := token.GetString("jti")
	if err != nil || jti == "" {
		return tokenClaims{}, ErrUnauthorized
	}
	var version int64
	if err := token.Get("ver", &version); err != nil || version < 1 {
		return tokenClaims{}, ErrUnauthorized
	}

	return tokenClaims{
		Type:    typ,
		JTI:     jti,
		Version: version,
	}, nil
}

func (s *AuthService) issueTokenPair(version int64) (TokenPair, error) {
	now := time.Now().UTC()

	accessJTI, err := randomTokenID()
	if err != nil {
		return TokenPair{}, err
	}
	refreshJTI, err := randomTokenID()
	if err != nil {
		return TokenPair{}, err
	}

	accessExp := now.Add(s.accessTTL)
	refreshExp := now.Add(s.refreshTTL)

	accessToken, err := s.issueToken(tokenTypeAccess, accessJTI, version, now, accessExp)
	if err != nil {
		return TokenPair{}, err
	}
	refreshToken, err := s.issueToken(tokenTypeRefresh, refreshJTI, version, now, refreshExp)
	if err != nil {
		return TokenPair{}, err
	}

	s.refresh.put(refreshJTI, refreshExp)

	return TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenType:        "Bearer",
		ExpiresIn:        int(s.accessTTL / time.Second),
		RefreshExpiresIn: int(s.refreshTTL / time.Second),
	}, nil
}

func (s *AuthService) issueToken(typ string, jti string, version int64, issuedAt time.Time, expiresAt time.Time) (string, error) {
	token := paseto.NewToken()
	token.SetIssuer(issuerName)
	token.SetSubject(subjectName)
	token.SetJti(jti)
	token.SetString("typ", typ)
	if err := token.Set("ver", version); err != nil {
		return "", fmt.Errorf("set token version failed: %w", err)
	}
	token.SetIssuedAt(issuedAt)
	token.SetNotBefore(issuedAt)
	token.SetExpiration(expiresAt)
	return token.V4Encrypt(s.key, nil), nil
}

func randomTokenID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token id failed: %w", err)
	}
	// RFC 4122 variant + version 4 UUID.
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}
