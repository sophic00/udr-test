# UDR Mock Server (5G Core)

A test Unified Data Repository (UDR) mock server in Go, compliant with 3GPP 5G Core Service-Based Architecture.
The RESTful routes, models, and server stubs are dynamically generated from the official 3GPP OpenAPI specifications (`TS29504_Nudr_DR.yaml`).
Dependencies are managed via Nix Flakes, and MongoDB is used for stateful, path-based persistence.

## Project Structure

*   `flake.nix`: Declares the Nix Flake environment containing packages (Go, Git, Make, NodeJS).
*   `.envrc`: Configures `direnv` to automatically activate the flake shell.
*   `Makefile`: Automates setup, code generation, compilation, and testing.
*   `codegen-config.yaml`: Configuration for `oapi-codegen` to output Go-Chi routing.
*   `cmd/udr/main.go`: Application entrypoint that connects to MongoDB and initializes routes.
*   `cmd/stubgen/main.go`: Generates Go stubs for the 150+ routes of `ServerInterface` from the OpenAPI spec.
*   `internal/api/`: Holds the generated OpenAPI types/stubs and dispatcher handler.
*   `internal/datastore/mongo.go`: Stateful, path-based MongoDB CRUD wrapper.

## Prerequisites

*   `nix` package manager (with flakes enabled).
*   `docker` (for running local MongoDB instances).

## Getting Started

Follow these steps to build and run the UDR test server:

### 1. Enter the Nix Shell
Enter the Nix environment containing the Go version and utilities:
```bash
nix develop
```
*(Note: If you use `direnv`, the dev shell is automatically activated when you enter the repository.)*

### 2. Start MongoDB (via Docker)
To run a local MongoDB community edition instance:
```bash
make docker-mongo-start
```

### 3. Clone Specs, Generate Code and Build
Download official 3GPP specs, bundle them, generate stubs, and build the binary:
```bash
make
```

### 4. Run the UDR Server
Start the UDR server listening on port `8080` (by default):
```bash
make run
```

---

## Configuration

You can override defaults using environment variables:

| Environment Variable | Default Value | Description |
| :--- | :--- | :--- |
| `MONGODB_URI` | `mongodb://localhost:27017` | MongoDB connection string |
| `MONGODB_DB` | `udr` | Name of the database |
| `PORT` | `8080` | Port for the UDR HTTP server |

---

## Verification & API Examples

The API examples below assume that the database has been pre-seeded with appropriate profiles, or you can create them first using `PUT` requests as shown below.

### 1. Healthcheck
```bash
curl -i http://localhost:8080/health
```

### 2. Query Authentication Subscription Data (GET)
```bash
curl -i http://localhost:8080/nudr-dr/v1/subscription-data/imsi-208950000000001/authentication-data/authentication-subscription
```

### 3. Query Access & Mobility Subscription Data (GET)
```bash
curl -i http://localhost:8080/nudr-dr/v1/subscription-data/imsi-208950000000001/20895/provisioned-data/am-data
```

### 4. Modify Profile Data (PATCH)
Updates specific parameters in the AM data subscription using standard JSON Merge Patch (RFC 7386):
```bash
curl -i -X PATCH http://localhost:8080/nudr-dr/v1/subscription-data/imsi-208950000000001/20895/provisioned-data/am-data \
  -H "Content-Type: application/json" \
  -d '{"subscribedUeAmbr": {"downlink": "2000 Mbps"}}'
```
Verify the change:
```bash
curl -i http://localhost:8080/nudr-dr/v1/subscription-data/imsi-208950000000001/20895/provisioned-data/am-data
```

### 5. Create a New Custom Profile (PUT)
Create or overwrite arbitrary 3GPP subscription records:
```bash
curl -i -X PUT http://localhost:8080/nudr-dr/v1/subscription-data/imsi-999990000000001/authentication-data/authentication-subscription \
  -H "Content-Type: application/json" \
  -d '{"authenticationMethod": "5G_AKA", "sequenceNumber": "000000000010"}'
```

### 6. Delete a Profile (DELETE)
```bash
curl -i -X DELETE http://localhost:8080/nudr-dr/v1/subscription-data/imsi-999990000000001/authentication-data/authentication-subscription
```

---

## Clean Up

To stop and remove the MongoDB container and delete generated files:
```bash
make docker-mongo-stop
make clean
```
