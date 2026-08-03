package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandlerRegisterAndLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(newTestService(t, "test-secret"))
	router := gin.New()
	router.POST("/register", handler.Register)
	router.POST("/login", handler.Login)

	registerRequest := httptest.NewRequest(
		http.MethodPost,
		"/register",
		bytes.NewBufferString(`{"username":"sean","password":"123456"}`),
	)
	registerRequest.Header.Set("Content-Type", "application/json")
	registerResponse := httptest.NewRecorder()
	router.ServeHTTP(registerResponse, registerRequest)

	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d: %s", registerResponse.Code, http.StatusCreated, registerResponse.Body.String())
	}

	var registered struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(registerResponse.Body.Bytes(), &registered); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if registered.User.ID == 0 {
		t.Fatal("expected registered user ID")
	}
	if registered.User.Username != "sean" {
		t.Fatalf("registered username = %q, want %q", registered.User.Username, "sean")
	}

	loginRequest := httptest.NewRequest(
		http.MethodPost,
		"/login",
		bytes.NewBufferString(`{"username":"sean","password":"123456"}`),
	)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, loginRequest)

	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d: %s", loginResponse.Code, http.StatusOK, loginResponse.Body.String())
	}

	var loggedIn struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loggedIn.Token == "" {
		t.Fatal("expected login token")
	}
}

func TestHandlerLoginRejectsWrongPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := newTestService(t, "test-secret")
	if _, err := service.Register("sean", "123456"); err != nil {
		t.Fatalf("register user: %v", err)
	}

	router := gin.New()
	router.POST("/login", NewHandler(service).Login)

	request := httptest.NewRequest(
		http.MethodPost,
		"/login",
		bytes.NewBufferString(`{"username":"sean","password":"wrong-password"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(response.Body.String(), "invalid username or password") {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}
