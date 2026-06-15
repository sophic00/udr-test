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
		_ = db.Delete(context.Background(), "/nudr-dr/v1/subscription-data/imsi-test/authentication-data/authentication-subscription")
		_ = db.Close(context.Background())
	}()

	// Setup Server and Router
	server := api.NewServer(db)
	r := chi.NewRouter()
	h := api.HandlerFromMux(server, chi.NewRouter())
	r.Mount("/nudr-dr/v1", h)

	ts := httptest.NewServer(r)
	defer ts.Close()

	client := ts.Client()
	authPath := "/nudr-dr/v1/subscription-data/imsi-test/authentication-data/authentication-subscription"

	// 1. GET - Should return 404 originally
	resp, err := client.Get(ts.URL + authPath)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}

	// 2. PUT - Create the resource
	authData := bson.M{
		"authenticationMethod": "5G_AKA",
		"sequenceNumber":       "000000000001",
	}
	bodyBytes, _ := json.Marshal(authData)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+authPath, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}

	// 3. GET - Verify the created resource
	resp, err = client.Get(ts.URL + authPath)
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
	if getResult["authenticationMethod"] != "5G_AKA" || getResult["sequenceNumber"] != "000000000001" {
		t.Errorf("Unexpected payload returned: %+v", getResult)
	}

	// 4. PATCH - Partial update
	patchData := bson.M{
		"sequenceNumber": "000000000002",
	}
	patchBytes, _ := json.Marshal(patchData)
	req, _ = http.NewRequest(http.MethodPatch, ts.URL+authPath, bytes.NewBuffer(patchBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PATCH request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}

	// 5. GET - Verify the patched resource
	resp, err = client.Get(ts.URL + authPath)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	var patchResult bson.M
	json.Unmarshal(body, &patchResult)
	if patchResult["authenticationMethod"] != "5G_AKA" || patchResult["sequenceNumber"] != "000000000002" {
		t.Errorf("Unexpected patched payload: %+v", patchResult)
	}

	// 6. DELETE - Remove the resource
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+authPath, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}

	// 7. GET - Verify deleted resource returns 404
	resp, err = client.Get(ts.URL + authPath)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404 after deletion, got %d", resp.StatusCode)
	}
}
