package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestAuth_MissingAuthorizationHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Auth("test-secret"))

	called := false
	router.GET("/protected", func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}

	if called {
		t.Fatal("expected protected handler not to be called")
	}

	if !strings.Contains(response.Body.String(), "missing authorization header") {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}

func TestAuth_ValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "test-secret"
	const expectedUserID int64 = 42

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": expectedUserID,
	})

	tokenText, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	router := gin.New()
	router.Use(Auth(secret))

	called := false
	var actualUserID int64

	router.GET("/protected", func(c *gin.Context) {
		called = true

		userID, ok := CurrentUserID(c)
		if !ok {
			t.Fatalf("expected user ID in context")
		}

		actualUserID = userID
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+tokenText)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}

	if !called {
		t.Fatal("expected protected handler to be called")
	}

	if actualUserID != expectedUserID {
		t.Fatalf("expected user ID %d, got %d", expectedUserID, actualUserID)
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": int64(42),
	})

	tokenText, err := token.SignedString([]byte("wrong-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	router := gin.New()
	router.Use(Auth("test-secret"))

	called := false
	router.GET("/protected", func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+tokenText)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}

	if called {
		t.Fatal("expected protected handler not to be called")
	}

	if !strings.Contains(response.Body.String(), "invalid token") {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}
