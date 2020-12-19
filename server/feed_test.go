package server

import (
	"net/http"
	"testing"
)

func TestFileFeed(t *testing.T) {
	router := setupTest(t, createRSSFeed(Config{}))

	t.Run("ok", func(t *testing.T) {
		u := createTestUser(t)
		createTestFile(t, u.ID)
		assertOK(t, router, testFilePath, http.MethodGet)
	})
}