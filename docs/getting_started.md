# Getting Started and Verification

This document contains instructions on how to run, verify, and clean up the UDR Mock Server.

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
