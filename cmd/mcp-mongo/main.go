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
		mcp.WithDescription("Create a fresh isolated session-based MongoDB database."),
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
		log.Printf("[MCP] Creating session database: %s", sessionDbName)

		// 1. Drop existing database if any to start fresh
		if err := db.DropDatabase(ctx, sessionDbName); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to drop database: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Session database '%s' created successfully.", sessionDbName)), nil
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
