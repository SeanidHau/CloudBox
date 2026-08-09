package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRequestIDSetsResponseHeaderAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID())

	var contextRequestID string
	router.GET("/health", func(c *gin.Context) {
		requestID, ok := CurrentRequestID(c)
		if !ok {
			t.Fatal("request ID missing from context")
		}

		contextRequestID = requestID
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}

	responseRequestID := response.Header().Get(RequestIDHeader)
	if responseRequestID == "" {
		t.Fatalf("%s response header is empty", RequestIDHeader)
	}
	if _, err := uuid.Parse(responseRequestID); err != nil {
		t.Fatalf("response request ID = %q, want UUID: %v", responseRequestID, err)
	}
	if contextRequestID != responseRequestID {
		t.Fatalf("context request ID = %q, want %q", contextRequestID, responseRequestID)
	}
}

func TestRequestIDGeneratesDifferentValuesForDifferentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID())
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, httptest.NewRequest(http.MethodGet, "/health", nil))

	secondResponse := httptest.NewRecorder()
	router.ServeHTTP(secondResponse, httptest.NewRequest(http.MethodGet, "/health", nil))

	firstRequestID := firstResponse.Header().Get(RequestIDHeader)
	secondRequestID := secondResponse.Header().Get(RequestIDHeader)
	if firstRequestID == secondRequestID {
		t.Fatalf("request IDs are equal: %q", firstRequestID)
	}
}
