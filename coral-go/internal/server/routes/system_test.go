package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/store"
)

func setupSystemTestServer(t *testing.T) (*httptest.Server, *SystemHandler) {
	t.Helper()

	cfg := &config.Config{
		LogDir: t.TempDir(),
	}

	dbPath := t.TempDir() + "/test.db"
	db, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	handler := NewSystemHandler(db, cfg)

	r := chi.NewRouter()
	r.Get("/api/system/status", handler.Status)
	r.Get("/api/system/update-check", handler.UpdateCheck)
	r.Get("/api/settings", handler.GetSettings)
	r.Put("/api/settings", handler.PutSettings)
	r.Get("/api/tags", handler.ListTags)
	r.Post("/api/tags", handler.CreateTag)
	r.Delete("/api/tags/{tagID}", handler.DeleteTag)
	r.Get("/api/folder-tags", handler.GetAllFolderTags)
	r.Get("/api/folder-tags/{folderName}", handler.GetFolderTags)
	r.Post("/api/folder-tags/{folderName}", handler.AddFolderTag)
	r.Delete("/api/folder-tags/{folderName}/{tagID}", handler.RemoveFolderTag)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return server, handler
}

func TestSystemStatus(t *testing.T) {
	server, _ := setupSystemTestServer(t)

	resp, err := http.Get(server.URL + "/api/system/status")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, true, body["startup_complete"])
}

func TestUpdateCheck(t *testing.T) {
	server, _ := setupSystemTestServer(t)

	resp, err := http.Get(server.URL + "/api/system/update-check")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, false, body["available"])
	assert.NotEmpty(t, body["current"])
}

func TestSettings_GetPut(t *testing.T) {
	server, _ := setupSystemTestServer(t)

	// GET should return empty settings initially
	resp, err := http.Get(server.URL + "/api/settings")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var getBody map[string]any
	json.NewDecoder(resp.Body).Decode(&getBody)
	settings := getBody["settings"].(map[string]any)
	assert.Empty(t, settings)

	// PUT a setting
	payload, _ := json.Marshal(map[string]string{"theme": "dark"})
	req, _ := http.NewRequest("PUT", server.URL+"/api/settings", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	// GET should now return the setting
	resp3, err := http.Get(server.URL + "/api/settings")
	require.NoError(t, err)
	defer resp3.Body.Close()

	var getBody2 map[string]any
	json.NewDecoder(resp3.Body).Decode(&getBody2)
	settings2 := getBody2["settings"].(map[string]any)
	assert.Equal(t, "dark", settings2["theme"])
}

func TestTags_CRUD(t *testing.T) {
	server, _ := setupSystemTestServer(t)

	// List tags — initially empty
	resp, err := http.Get(server.URL + "/api/tags")
	require.NoError(t, err)
	defer resp.Body.Close()

	var tags []map[string]any
	json.NewDecoder(resp.Body).Decode(&tags)
	assert.Empty(t, tags)

	// Create a tag
	payload, _ := json.Marshal(map[string]string{"name": "bugfix", "color": "#ff0000"})
	resp2, err := http.Post(server.URL+"/api/tags", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var created map[string]any
	json.NewDecoder(resp2.Body).Decode(&created)
	assert.Equal(t, "bugfix", created["name"])
	assert.Equal(t, "#ff0000", created["color"])
	tagID := created["id"]

	// Create tag with default color
	payload2, _ := json.Marshal(map[string]string{"name": "feature"})
	resp3, err := http.Post(server.URL+"/api/tags", "application/json", bytes.NewReader(payload2))
	require.NoError(t, err)
	defer resp3.Body.Close()

	var created2 map[string]any
	json.NewDecoder(resp3.Body).Decode(&created2)
	assert.Equal(t, "#58a6ff", created2["color"]) // default color

	// List tags — should have 2
	resp4, err := http.Get(server.URL + "/api/tags")
	require.NoError(t, err)
	defer resp4.Body.Close()

	var tags2 []map[string]any
	json.NewDecoder(resp4.Body).Decode(&tags2)
	assert.Len(t, tags2, 2)

	// Delete the first tag
	req, _ := http.NewRequest("DELETE", server.URL+"/api/tags/"+formatFloat(tagID), nil)
	resp5, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp5.Body.Close()
	assert.Equal(t, http.StatusOK, resp5.StatusCode)

	// List tags — should have 1
	resp6, err := http.Get(server.URL + "/api/tags")
	require.NoError(t, err)
	defer resp6.Body.Close()

	var tags3 []map[string]any
	json.NewDecoder(resp6.Body).Decode(&tags3)
	assert.Len(t, tags3, 1)
	assert.Equal(t, "feature", tags3[0]["name"])
}

func TestCreateTag_EmptyName(t *testing.T) {
	server, _ := setupSystemTestServer(t)

	payload, _ := json.Marshal(map[string]string{"name": ""})
	resp, err := http.Post(server.URL+"/api/tags", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestFolderTags_CRUD(t *testing.T) {
	server, _ := setupSystemTestServer(t)

	// Create a tag first
	payload, _ := json.Marshal(map[string]string{"name": "backend", "color": "#00ff00"})
	resp, err := http.Post(server.URL+"/api/tags", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()

	var tag map[string]any
	json.NewDecoder(resp.Body).Decode(&tag)
	tagID := int64(tag["id"].(float64))

	// Get all folder tags — initially empty
	resp2, err := http.Get(server.URL + "/api/folder-tags")
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var allTags map[string]any
	json.NewDecoder(resp2.Body).Decode(&allTags)
	assert.Empty(t, allTags)

	// Add folder tag
	addPayload, _ := json.Marshal(map[string]int64{"tag_id": tagID})
	resp3, err := http.Post(server.URL+"/api/folder-tags/myproject", "application/json", bytes.NewReader(addPayload))
	require.NoError(t, err)
	defer resp3.Body.Close()
	assert.Equal(t, http.StatusOK, resp3.StatusCode)

	// Get folder tags for specific folder
	resp4, err := http.Get(server.URL + "/api/folder-tags/myproject")
	require.NoError(t, err)
	defer resp4.Body.Close()

	var folderTags []map[string]any
	json.NewDecoder(resp4.Body).Decode(&folderTags)
	assert.Len(t, folderTags, 1)
	assert.Equal(t, "backend", folderTags[0]["name"])

	// Get all folder tags — should have myproject
	resp5, err := http.Get(server.URL + "/api/folder-tags")
	require.NoError(t, err)
	defer resp5.Body.Close()

	var allTags2 map[string]any
	json.NewDecoder(resp5.Body).Decode(&allTags2)
	assert.Contains(t, allTags2, "myproject")

	// Remove folder tag
	req, _ := http.NewRequest("DELETE", server.URL+"/api/folder-tags/myproject/"+formatInt64(tagID), nil)
	resp6, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp6.Body.Close()
	assert.Equal(t, http.StatusOK, resp6.StatusCode)

	// Folder should have no tags now
	resp7, err := http.Get(server.URL + "/api/folder-tags/myproject")
	require.NoError(t, err)
	defer resp7.Body.Close()

	var folderTags2 []map[string]any
	json.NewDecoder(resp7.Body).Decode(&folderTags2)
	assert.Empty(t, folderTags2)
}

func TestAddFolderTag_MissingTagID(t *testing.T) {
	server, _ := setupSystemTestServer(t)

	payload, _ := json.Marshal(map[string]string{})
	resp, err := http.Post(server.URL+"/api/folder-tags/myproject", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func formatFloat(v any) string {
	f := v.(float64)
	return formatInt64(int64(f))
}

func formatInt64(v int64) string {
	return strconv.FormatInt(v, 10)
}
