package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"udr-test/internal/datastore"
)

type Server struct {
	db *datastore.Datastore
}

func NewServer(db *datastore.Datastore) *Server {
	return &Server{
		db: db,
	}
}

// Dispatch handles all stubs by routing requests to the generic MongoDB path-based handler.
func (s *Server) Dispatch(w http.ResponseWriter, r *http.Request, methodName string) {
	log.Printf("[UDR API] Dispatching %s %s via %s", r.Method, r.URL.Path, methodName)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Normalize URL path: strip trailing slash if present to make key lookup consistent
	path := r.URL.Path
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGet(ctx, w, r, path)
	case http.MethodPut:
		s.handlePut(ctx, w, r, path)
	case http.MethodPost:
		s.handlePost(ctx, w, r, path)
	case http.MethodPatch:
		s.handlePatch(ctx, w, r, path)
	case http.MethodDelete:
		s.handleDelete(ctx, w, r, path)
	default:
		s.writeProblemDetails(w, http.StatusMethodNotAllowed, "Method Not Allowed", fmt.Sprintf("Method %s is not supported", r.Method))
	}
}

func (s *Server) handleGet(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) {
	// 1. First, check if there is an exact match for the path in the datastore
	data, err := s.db.Get(ctx, path)
	if err != nil {
		log.Printf("Error getting path %s: %v", path, err)
		s.writeProblemDetails(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	if data != nil {
		s.writeJSON(w, http.StatusOK, data)
		return
	}

	// 2. If not found, check if it's a list endpoint by fetching all sub-resources (prefix matching)
	// Typical list endpoints in UDR: ee-subscriptions, sdm-subscriptions, bdt-data, influenceData, etc.
	if isListEndpoint(path) {
		prefix := path + "/"
		list, err := s.db.List(ctx, prefix)
		if err != nil {
			log.Printf("Error listing path %s: %v", path, err)
			s.writeProblemDetails(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
			return
		}
		if len(list) > 0 {
			s.writeJSON(w, http.StatusOK, list)
			return
		}
		// Return empty array for list endpoints
		s.writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	// 3. Not found
	s.writeProblemDetails(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Resource not found at path: %s", path))
}

func (s *Server) handlePut(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) {
	defer r.Body.Close()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeProblemDetails(w, http.StatusBadRequest, "Bad Request", "Failed to read request body")
		return
	}

	var data bson.M
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &data); err != nil {
			s.writeProblemDetails(w, http.StatusBadRequest, "Bad Request", "Failed to parse body as JSON")
			return
		}
	} else {
		data = bson.M{}
	}

	err = s.db.Put(ctx, path, data)
	if err != nil {
		log.Printf("Error putting path %s: %v", path, err)
		s.writeProblemDetails(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePost(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) {
	defer r.Body.Close()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeProblemDetails(w, http.StatusBadRequest, "Bad Request", "Failed to read request body")
		return
	}

	var data bson.M
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &data); err != nil {
			s.writeProblemDetails(w, http.StatusBadRequest, "Bad Request", "Failed to parse body as JSON")
			return
		}
	} else {
		data = bson.M{}
	}

	// Generate a unique sub-resource ID
	id := uuid.New().String()
	resourcePath := fmt.Sprintf("%s/%s", path, id)

	err = s.db.Put(ctx, resourcePath, data)
	if err != nil {
		log.Printf("Error creating sub-resource %s: %v", resourcePath, err)
		s.writeProblemDetails(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	w.Header().Set("Location", resourcePath)
	s.writeJSON(w, http.StatusCreated, data)
}

func (s *Server) handlePatch(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) {
	defer r.Body.Close()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeProblemDetails(w, http.StatusBadRequest, "Bad Request", "Failed to read request body")
		return
	}

	var patch bson.M
	if err := json.Unmarshal(bodyBytes, &patch); err != nil {
		s.writeProblemDetails(w, http.StatusBadRequest, "Bad Request", "Failed to parse patch body as JSON")
		return
	}

	// Get existing
	existing, err := s.db.Get(ctx, path)
	if err != nil {
		s.writeProblemDetails(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	if existing == nil {
		s.writeProblemDetails(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Cannot patch non-existent resource: %s", path))
		return
	}

	// Apply merge patch
	merged := mergePatch(existing, patch)

	err = s.db.Put(ctx, path, merged)
	if err != nil {
		log.Printf("Error patching path %s: %v", path, err)
		s.writeProblemDetails(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDelete(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) {
	err := s.db.Delete(ctx, path)
	if err != nil {
		log.Printf("Error deleting path %s: %v", path, err)
		s.writeProblemDetails(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RFC 7386 JSON Merge Patch implementation
func mergePatch(target, patch bson.M) bson.M {
	for k, v := range patch {
		if v == nil {
			delete(target, k)
			continue
		}

		// If value is a nested map, recurse
		if targetVal, ok := target[k]; ok {
			if targetMap, ok1 := toMap(targetVal); ok1 {
				if patchMap, ok2 := toMap(v); ok2 {
					target[k] = mergePatch(targetMap, patchMap)
					continue
				}
			}
		}
		target[k] = v
	}
	return target
}

// Helper to convert interface{} to bson.M if it is a map[string]interface{} or bson.M
func toMap(v interface{}) (bson.M, bool) {
	if m, ok := v.(bson.M); ok {
		return m, true
	}
	if m, ok := v.(map[string]interface{}); ok {
		return bson.M(m), true
	}
	return nil, false
}


func isListEndpoint(path string) bool {
	// Identify common collection list endpoints
	listSuffixes := []string{
		"ee-subscriptions",
		"sdm-subscriptions",
		"subs-to-notify",
		"bdt-data",
		"5g-vn-groups",
		"influenceData",
		"iptvConfigData",
		"pfds",
		"serviceParamData",
		"smf-registrations",
	}
	for _, suffix := range listSuffixes {
		if strings.HasSuffix(path, "/"+suffix) {
			return true
		}
	}
	return false
}

func (s *Server) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func (s *Server) writeProblemDetails(w http.ResponseWriter, status int, title, detail string) {
	WriteProblemDetails(w, status, title, detail)
}

// WriteProblemDetails writes a standard 3GPP ProblemDetails response to the writer.
func WriteProblemDetails(w http.ResponseWriter, status int, title, detail string) {
	problem := bson.M{
		"status": status,
		"title":  title,
		"detail": detail,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		log.Printf("Error encoding ProblemDetails response: %v", err)
	}
}

var (
	safeNameRegex  = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	blacklistedDBs = map[string]bool{
		"admin":  true,
		"config": true,
		"local":  true,
	}
)

// ValidateDatabaseName checks if the database name is safe and not a system database.
func ValidateDatabaseName(name string) error {
	if name == "" {
		return fmt.Errorf("database name cannot be empty")
	}
	if !safeNameRegex.MatchString(name) {
		return fmt.Errorf("database name '%s' contains invalid characters or exceeds 64 characters", name)
	}
	if blacklistedDBs[strings.ToLower(name)] {
		return fmt.Errorf("access to database '%s' is forbidden", name)
	}
	return nil
}

// ValidateSessionID checks if the session ID is safe.
func ValidateSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}
	if !safeNameRegex.MatchString(sessionID) {
		return fmt.Errorf("session ID '%s' contains invalid characters or exceeds 64 characters", sessionID)
	}
	return nil
}

