package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestHandlerRegisterWithInvitationAndLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(t, "test-secret")
	admin := createTestAdmin(t, service)
	created, err := service.CreateInvitation(admin.ID)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	handler := NewHandler(service)
	router := gin.New()
	router.POST("/register", handler.Register)
	router.POST("/login", handler.Login)

	request := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(`{"username":"sean","password":"123456","invite_code":"`+created.Code+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"username":"sean","password":"123456"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d: %s", loginResponse.Code, http.StatusOK, loginResponse.Body.String())
	}
	var loggedIn struct {
		Token string `json:"token"`
		User  User   `json:"user"`
	}
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &loggedIn); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if loggedIn.Token == "" || loggedIn.User.Username != "sean" {
		t.Fatalf("login body = %#v", loggedIn)
	}
}

func TestHandlerAdminRoutesRejectOrdinaryUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(t, "test-secret")
	user := inviteAndRegister(t, service, "sean")
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.UserIDKey, user.ID) })
	router.GET("/admin/users", handler.ListUsers)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/users", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("admin users status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestHandlerLoginRejectsWrongPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(t, "test-secret")
	inviteAndRegister(t, service, "sean")
	router := gin.New()
	router.POST("/login", NewHandler(service).Login)
	request := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"username":"sean","password":"wrong-password"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(response.Body.String(), "invalid username or password") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}
