package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson"
	"udr-test/internal/api"
	"udr-test/internal/datastore"
)

func TestUDRServerFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try to connect to a local MongoDB instance. If not available, skip.
	mongoURI := "mongodb://localhost:27017"
	dbName := "udr_test"
	db, err := datastore.NewDatastore(ctx, mongoURI, dbName)
	if err != nil {
		t.Skipf("Skipping integration test; MongoDB is not running at %s: %v", mongoURI, err)
		return
	}
	defer func() {
		// Clean up testing databases
		_ = db.DropDatabase(context.Background(), "udr_test")
		_ = db.DropDatabase(context.Background(), "udr_session_sessionA")
		_ = db.DropDatabase(context.Background(), "udr_session_sessionB")
		_ = db.Close(context.Background())
	}()

	// Setup Server and Router
	server := api.NewServer(db)
	r := chi.NewRouter()

	// Apply databaseRoutingMiddleware (mimic main.go middleware setup)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dbName := ""
			if sessionID := r.Header.Get("X-Session-ID"); sessionID != "" {
				dbName = "udr_session_" + sessionID
			} else if customDB := r.Header.Get("X-UDR-Database"); customDB != "" {
				dbName = customDB
			}

			if dbName != "" {
				ctx := context.WithValue(r.Context(), datastore.DbNameKey, dbName)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				next.ServeHTTP(w, r)
			}
		})
	})

	h := api.HandlerFromMux(server, chi.NewRouter())
	r.Mount("/nudr-dr/v1", h)

	ts := httptest.NewServer(r)
	defer ts.Close()

	client := ts.Client()
	// Use 5G VN Group path which supports GET, PUT, PATCH, and DELETE in 3GPP spec
	testPath := "/nudr-dr/v1/subscription-data/group-data/5g-vn-groups/groupA"

	// 1. GET - Should return 404 originally (sessionA)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+testPath, nil)
	req.Header.Set("X-Session-ID", "sessionA")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}

	// 2. PUT - Create resource in sessionA
	groupData := bson.M{
		"vnGroupData": bson.M{
			"routingIndicator": "1234",
		},
		"internalGroupId": "intGroupA",
	}
	bodyBytes, _ := json.Marshal(groupData)
	req, _ = http.NewRequest(http.MethodPut, ts.URL+testPath, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", "sessionA")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}

	// 3. GET (sessionA) - Verify the created resource is visible in sessionA
	req, _ = http.NewRequest(http.MethodGet, ts.URL+testPath, nil)
	req.Header.Set("X-Session-ID", "sessionA")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var getResult bson.M
	if err := json.Unmarshal(body, &getResult); err != nil {
		t.Fatalf("Failed to decode GET response: %v", err)
	}
	if getResult["internalGroupId"] != "intGroupA" {
		t.Errorf("Unexpected payload returned: %+v", getResult)
	}

	// 4. GET (sessionB) - Should return 404 (isolation check)
	req, _ = http.NewRequest(http.MethodGet, ts.URL+testPath, nil)
	req.Header.Set("X-Session-ID", "sessionB")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404 for sessionB, got %d", resp.StatusCode)
	}

	// 5. GET (No Session Header) - Should return 404 (isolation check)
	req, _ = http.NewRequest(http.MethodGet, ts.URL+testPath, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404 for default db, got %d", resp.StatusCode)
	}

	// 6. PATCH (sessionA) - Partial update
	patchData := bson.M{
		"internalGroupId": "intGroupAPatched",
	}
	patchBytes, _ := json.Marshal(patchData)
	req, _ = http.NewRequest(http.MethodPatch, ts.URL+testPath, bytes.NewBuffer(patchBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", "sessionA")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PATCH request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}

	// 7. GET (sessionA) - Verify the patched resource is updated
	req, _ = http.NewRequest(http.MethodGet, ts.URL+testPath, nil)
	req.Header.Set("X-Session-ID", "sessionA")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	var patchResult bson.M
	json.Unmarshal(body, &patchResult)
	if patchResult["internalGroupId"] != "intGroupAPatched" {
		t.Errorf("Unexpected patched payload: %+v", patchResult)
	}

	// 8. DELETE (sessionA) - Remove the resource
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+testPath, nil)
	req.Header.Set("X-Session-ID", "sessionA")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}

	// 9. GET (sessionA) - Verify deleted resource returns 404
	req, _ = http.NewRequest(http.MethodGet, ts.URL+testPath, nil)
	req.Header.Set("X-Session-ID", "sessionA")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404 after deletion, got %d", resp.StatusCode)
	}
}
