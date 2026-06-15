package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"udr-test/internal/datastore"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/bson"
)

func main() {
	// MCP communicates over stdio or SSE. We must not write logs to stdout, only to stderr!
	log.SetOutput(os.Stderr)

	transport := getEnv("MCP_TRANSPORT", "stdio")
	log.Printf("Starting MongoDB MCP server (transport: %s)...", transport)

	mongoURI := getEnv("MONGODB_URI", "mongodb://localhost:27017")
	dbName := getEnv("MONGODB_DB", "udr")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := datastore.NewDatastore(ctx, mongoURI, dbName)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer db.Close(context.Background())

	// Create MCP server
	s := server.NewMCPServer(
		"mcp-mongo",
		"1.0.0",
		server.WithLogging(),
	)

	// Register create_session_db tool
	s.AddTool(mcp.NewTool(
		"create_session_db",
		mcp.WithDescription("Create a fresh isolated session-based MongoDB database and seed it with default 5G Core mock profiles."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("The unique identifier for the session/sandbox (e.g. session-123)")),
	), createSessionDbHandler(db))

	// Register seed_profile tool
	s.AddTool(mcp.NewTool(
		"seed_profile",
		mcp.WithDescription("Seed an arbitrary JSON profile under a path key in a session-based MongoDB database."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("The unique identifier for the session/sandbox")),
		mcp.WithString("path", mcp.Required(), mcp.Description("The URL path of the profile resource (e.g. /nudr-dr/v1/subscription-data/imsi-208950000000001/authentication-data/authentication-subscription)")),
		mcp.WithString("profile_json", mcp.Required(), mcp.Description("The JSON content of the profile structure")),
	), seedProfileHandler(db))

	// Register delete_session_db tool
	s.AddTool(mcp.NewTool(
		"delete_session_db",
		mcp.WithDescription("Delete (drop) an isolated session-based MongoDB database."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("The unique identifier for the session/sandbox to delete")),
	), deleteSessionDbHandler(db))

	// Run server based on chosen transport
	if transport == "sse" {
		port := getEnv("PORT", "8081")
		log.Printf("Running MCP SSE server on port %s (endpoint: /sse, messages: /messages)", port)
		sseServer := server.NewSSEServer(s)
		if err := sseServer.Start(":" + port); err != nil {
			log.Fatalf("Failed to start MCP SSE server: %v", err)
		}
	} else {
		log.Println("Running MCP stdio server...")
		if err := server.ServeStdio(s); err != nil {
			log.Fatalf("Failed to run MCP stdio server: %v", err)
		}
	}
}

func createSessionDbHandler(db *datastore.Datastore) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := request.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		sessionDbName := fmt.Sprintf("udr_session_%s", sessionID)
		log.Printf("[MCP] Creating and seeding session database: %s", sessionDbName)

		// 1. Drop existing database if any to start fresh
		if err := db.DropDatabase(ctx, sessionDbName); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to drop database: %v", err)), nil
		}

		// 2. Prepare context with session database name
		sessionCtx := context.WithValue(ctx, datastore.DbNameKey, sessionDbName)

		// 3. Seed default mock profiles
		ueId := "imsi-208950000000001"
		plmnId := "20895"

		// Auth Subscription Data
		authPath := fmt.Sprintf("/nudr-dr/v1/subscription-data/%s/authentication-data/authentication-subscription", ueId)
		authData := bson.M{
			"authenticationMethod":  "5G_AKA",
			"encPermanentKey":       "00112233445566778899aabbccddeeff",
			"protectionParameterId": "00112233445566778899aabbccddeeff",
			"sequenceNumber":        "000000000001",
		}
		if err := db.Put(sessionCtx, authPath, authData); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to seed auth: %v", err)), nil
		}

		// AM Data
		amPath := fmt.Sprintf("/nudr-dr/v1/subscription-data/%s/%s/provisioned-data/am-data", ueId, plmnId)
		amData := bson.M{
			"gpsis": []interface{}{"msisdn-33600000001"},
			"subscribedUeAmbr": bson.M{
				"downlink": "1000 Mbps",
				"uplink":   "500 Mbps",
			},
			"nssai": bson.M{
				"defaultSingleNssais": []interface{}{
					bson.M{"sst": 1, "sd": "010203"},
				},
			},
		}
		if err := db.Put(sessionCtx, amPath, amData); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to seed am-data: %v", err)), nil
		}

		// SM Data
		smPath := fmt.Sprintf("/nudr-dr/v1/subscription-data/%s/%s/provisioned-data/sm-data", ueId, plmnId)
		smData := bson.M{
			"singleNssai": bson.M{"sst": 1, "sd": "010203"},
			"dnnConfigurations": bson.M{
				"internet": bson.M{
					"pduSessionTypes": bson.M{
						"defaultSessionType":  "IPV4V6",
						"allowedSessionTypes": []interface{}{"IPV4", "IPV6", "IPV4V6"},
					},
					"sscModes": bson.M{
						"defaultSscMode":  "SSC_MODE_1",
						"allowedSscModes": []interface{}{"SSC_MODE_1", "SSC_MODE_2", "SSC_MODE_3"},
					},
					"sessionAmbr": bson.M{
						"downlink": "100 Mbps",
						"uplink":   "50 Mbps",
					},
					"5gQosProfile": bson.M{
						"5qi": 9,
						"arp": bson.M{
							"priorityLevel": 8,
							"preemptCap":    "NOT_PREEMPT",
							"preemptVuln":   "PREEMPTABLE",
						},
					},
				},
			},
		}
		if err := db.Put(sessionCtx, smPath, smData); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to seed sm-data: %v", err)), nil
		}

		// Policy Data
		policyPath := fmt.Sprintf("/nudr-dr/v1/policy-data/ues/%s/am-data", ueId)
		policyData := bson.M{
			"subscCats": []interface{}{"default"},
		}
		if err := db.Put(sessionCtx, policyPath, policyData); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to seed policy: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Session database '%s' created and seeded successfully with default profiles.", sessionDbName)), nil
	}
}

func seedProfileHandler(db *datastore.Datastore) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := request.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		profileJSON, err := request.RequireString("profile_json")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Parse JSON string to bson.M
		var data bson.M
		if err := json.Unmarshal([]byte(profileJSON), &data); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid profile_json: %v", err)), nil
		}

		sessionDbName := fmt.Sprintf("udr_session_%s", sessionID)
		sessionCtx := context.WithValue(ctx, datastore.DbNameKey, sessionDbName)

		log.Printf("[MCP] Seeding profile under path %s in session %s", path, sessionDbName)
		if err := db.Put(sessionCtx, path, data); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to seed profile: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Profile seeded successfully at '%s' in session '%s'.", path, sessionDbName)), nil
	}
}

func deleteSessionDbHandler(db *datastore.Datastore) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := request.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		sessionDbName := fmt.Sprintf("udr_session_%s", sessionID)
		log.Printf("[MCP] Deleting session database: %s", sessionDbName)

		if err := db.DropDatabase(ctx, sessionDbName); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to delete database: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Session database '%s' deleted successfully.", sessionDbName)), nil
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}
