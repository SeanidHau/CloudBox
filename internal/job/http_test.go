package job

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestHTTPHandlerGetReturnsOnlyOwnerJob(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := NewService(newTestRepository(t))
	created, err := service.EnqueueForUser(1, TypeVerifyFile, map[string]int64{"file_id": 42})
	if err != nil {
		t.Fatalf("enqueue user job: %v", err)
	}

	ownerRouter := newTestHTTPRouter(service, 1)
	ownerResponse := httptest.NewRecorder()
	ownerRouter.ServeHTTP(ownerResponse, httptest.NewRequest(http.MethodGet, "/jobs/"+created.ID, nil))

	if ownerResponse.Code != http.StatusOK {
		t.Fatalf("owner get status = %d, want %d: %s", ownerResponse.Code, http.StatusOK, ownerResponse.Body.String())
	}

	var ownerResult struct {
		Job Job `json:"job"`
	}
	if err := json.Unmarshal(ownerResponse.Body.Bytes(), &ownerResult); err != nil {
		t.Fatalf("decode owner response: %v", err)
	}
	if ownerResult.Job.ID != created.ID || ownerResult.Job.UserID == nil || *ownerResult.Job.UserID != 1 {
		t.Fatalf("owner result = %#v, want job owned by user 1", ownerResult.Job)
	}

	otherUserRouter := newTestHTTPRouter(service, 2)
	otherUserResponse := httptest.NewRecorder()
	otherUserRouter.ServeHTTP(otherUserResponse, httptest.NewRequest(http.MethodGet, "/jobs/"+created.ID, nil))
	if otherUserResponse.Code != http.StatusNotFound {
		t.Fatalf("other user get status = %d, want %d: %s", otherUserResponse.Code, http.StatusNotFound, otherUserResponse.Body.String())
	}
}

func TestHTTPHandlerGetRequiresAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/jobs/:id", NewHTTPHandler(NewService(newTestRepository(t))).Get)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jobs/any-job", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated get status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func newTestHTTPRouter(service *Service, userID int64) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, userID)
		c.Next()
	})
	router.GET("/jobs/:id", NewHTTPHandler(service).Get)

	return router
}
