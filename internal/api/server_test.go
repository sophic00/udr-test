package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
				if err := api.ValidateSessionID(sessionID); err != nil {
					api.WriteProblemDetails(w, http.StatusBadRequest, "Invalid Session ID", err.Error())
					return
				}
				dbName = "udr_session_" + sessionID
			} else if customDB := r.Header.Get("X-UDR-Database"); customDB != "" {
				if err := api.ValidateDatabaseName(customDB); err != nil {
					api.WriteProblemDetails(w, http.StatusBadRequest, "Invalid Database Name", err.Error())
					return
				}
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

	// 10. Nested JSON Merge Patch - Verify recursive merging and that sibling fields are not lost.
	// Create a resource with nested objects
	nestedPath := "/nudr-dr/v1/subscription-data/group-data/5g-vn-groups/groupNested"
	nestedData := bson.M{
		"vnGroupData": bson.M{
			"routingIndicator": "1234",
			"someOtherField":   "keep-me",
		},
		"internalGroupId": "intGroupNested",
	}
	bodyBytes, _ = json.Marshal(nestedData)
	req, _ = http.NewRequest(http.MethodPut, ts.URL+nestedPath, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", "sessionA")
	resp, _ = client.Do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected PUT status 204, got %d", resp.StatusCode)
	}

	// Patch only the nested field "routingIndicator"
	patchNestedData := bson.M{
		"vnGroupData": bson.M{
			"routingIndicator": "5678",
		},
	}
	patchBytes, _ = json.Marshal(patchNestedData)
	req, _ = http.NewRequest(http.MethodPatch, ts.URL+nestedPath, bytes.NewBuffer(patchBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", "sessionA")
	resp, _ = client.Do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected PATCH status 204, got %d", resp.StatusCode)
	}

	// Fetch and verify nested map values
	req, _ = http.NewRequest(http.MethodGet, ts.URL+nestedPath, nil)
	req.Header.Set("X-Session-ID", "sessionA")
	resp, _ = client.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	var getNestedResult bson.M
	json.Unmarshal(body, &getNestedResult)
	vnGroupData, hasVn := getNestedResult["vnGroupData"].(map[string]interface{})
	if !hasVn {
		t.Fatalf("Expected vnGroupData to exist and be a map, got: %+v", getNestedResult["vnGroupData"])
	}
	if vnGroupData["routingIndicator"] != "5678" {
		t.Errorf("Expected routingIndicator to be '5678', got '%v'", vnGroupData["routingIndicator"])
	}
	if vnGroupData["someOtherField"] != "keep-me" {
		t.Errorf("Sibling field 'someOtherField' was lost! got '%v'", vnGroupData["someOtherField"])
	}

	// 11. Test database validation and blacklist
	// Bad session ID (special chars)
	req, _ = http.NewRequest(http.MethodGet, ts.URL+testPath, nil)
	req.Header.Set("X-Session-ID", "invalid;session$db")
	resp, _ = client.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for invalid X-Session-ID, got %d", resp.StatusCode)
	}

	// Blacklisted custom DB name
	req, _ = http.NewRequest(http.MethodGet, ts.URL+testPath, nil)
	req.Header.Set("X-UDR-Database", "admin")
	resp, _ = client.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for blacklisted X-UDR-Database, got %d", resp.StatusCode)
	}

	// 12. Test POST (sub-resource creation) and GET collection listing
	collectionPath := "/nudr-dr/v1/subscription-data/imsi-208950000000001/context-data/ee-subscriptions"
	subData := bson.M{
		"eeSubscription": bson.M{
			"callbackReference": "http://callback-uri",
		},
	}
	bodyBytes, _ = json.Marshal(subData)
	req, _ = http.NewRequest(http.MethodPost, ts.URL+collectionPath, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", "sessionA")
	resp, _ = client.Do(req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected POST status 201, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" || !strings.HasPrefix(loc, collectionPath+"/") {
		t.Errorf("Expected Location header starting with '%s/', got '%s'", collectionPath, loc)
	}

	// Get collection listing
	req, _ = http.NewRequest(http.MethodGet, ts.URL+collectionPath, nil)
	req.Header.Set("X-Session-ID", "sessionA")
	resp, _ = client.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected collection GET status 200, got %d", resp.StatusCode)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	var listResult []bson.M
	if err := json.Unmarshal(body, &listResult); err != nil {
		t.Fatalf("Failed to decode collection list: %v", err)
	}
	if len(listResult) != 1 {
		t.Errorf("Expected 1 item in list, got %d", len(listResult))
	} else {
		sub, ok := listResult[0]["eeSubscription"].(map[string]interface{})
		if !ok || sub["callbackReference"] != "http://callback-uri" {
			t.Errorf("Unexpected item in collection list: %+v", listResult[0])
		}
	}
}
