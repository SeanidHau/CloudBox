package auth

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SeanidHau/CloudBox/internal/database"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func newTestService(t *testing.T, secret string) *Service {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := database.Migrate(db, "../../migrations/001_init.sql"); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	return NewService(NewRepository(db), secret)
}

func TestRegisterHashesPasswordAndLoginReturnsToken(t *testing.T) {
	const secret = "test-secret"

	service := newTestService(t, secret)

	user, err := service.Register("sean", "123456")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}

	if user.ID == 0 {
		t.Fatal("expected user id to be set")
	}
	if user.PasswordHash == "123456" {
		t.Fatal("password was stored as plain text")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("123456")); err != nil {
		t.Fatalf("password hash does not match password: %v", err)
	}

	tokenText, err := service.Login("sean", "123456")
	if err != nil {
		t.Fatalf("login user: %v", err)
	}
	if tokenText == "" {
		t.Fatal("expected login to return token")
	}

	token, err := jwt.Parse(tokenText, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if !token.Valid {
		t.Fatal("expected token to be valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected map claims")
	}
	if got := int64(claims["user_id"].(float64)); got != user.ID {
		t.Fatalf("user_id claim = %d, want %d", got, user.ID)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	service := newTestService(t, "test-secret")

	if _, err := service.Register("sean", "123456"); err != nil {
		t.Fatalf("register user: %v", err)
	}

	_, err := service.Login("sean", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("login error = %v, want %v", err, ErrInvalidCredentials)
	}
}
