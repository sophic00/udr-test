# UDR Mock Server (5G Core)

A test Unified Data Repository (UDR) mock server in Go, compliant with 3GPP 5G Core Service-Based Architecture.
The RESTful routes, models, and server stubs are dynamically generated from the official 3GPP OpenAPI specifications (`TS29504_Nudr_DR.yaml`).

## Project Structure

- `flake.nix`: Declares the Nix Flake environment containing packages (Go, Git, Make, NodeJS).
- `.envrc`: Configures `direnv` to automatically activate the flake shell.
- `Makefile`: Automates setup, code generation, compilation, and testing.
- `codegen-config.yaml`: Configuration for `oapi-codegen` to output Go-Chi routing.
- `cmd/udr/main.go`: Application entrypoint that connects to MongoDB and initializes routes.
- `cmd/stubgen/main.go`: Generates Go stubs for the 150+ routes of `ServerInterface` from the OpenAPI spec.
- `internal/api/`: Holds the generated OpenAPI types/stubs and dispatcher handler.
- `internal/datastore/mongo.go`: Stateful, path-based MongoDB CRUD wrapper.

## Prerequisites

- `nix` package manager (with flakes enabled).
- `docker` (for running local MongoDB instances).

## Getting Started & Usage

For details on building, running, verifying, and cleaning up the mock server, please see the [Getting Started and Verification Guide](docs/getting_started.md).

---

## Configuration

You can override defaults using environment variables:

| Environment Variable | Default Value               | Description                  |
| :------------------- | :-------------------------- | :--------------------------- |
| `MONGODB_URI`        | `mongodb://localhost:27017` | MongoDB connection string    |
| `MONGODB_DB`         | `udr`                       | Name of the database         |
| `PORT`               | `8080`                      | Port for the UDR HTTP server |
