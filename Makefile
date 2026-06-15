.PHONY: all shell-deps clone-specs generate build run test clean docker-mongo-start docker-mongo-stop

all: clone-specs generate build

clone-specs:
	@if [ ! -d "spec" ]; then \
		echo "Cloning official 3GPP OpenAPI specs..."; \
		git clone --depth 1 -b REL-16 https://forge.3gpp.org/rep/all/5G_APIs.git spec; \
	else \
		echo "Specs already cloned."; \
	fi

generate:
	@echo "Bundling OpenAPI specs with Redocly..."
	nix develop --command npx -y @redocly/cli bundle spec/TS29504_Nudr_DR.yaml -o spec/TS29504_Nudr_DR_bundled.yaml
	@echo "Generating Go code from bundled OpenAPI spec..."
	mkdir -p internal/api
	nix develop --command go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -config codegen-config.yaml -o internal/api/udr.gen.go spec/TS29504_Nudr_DR_bundled.yaml
	@echo "Generating stubs for ServerInterface..."
	nix develop --command go run cmd/stubgen/main.go

build:
	@echo "Building UDR test server..."
	nix develop --command go build -o udr cmd/udr/main.go

run:
	@echo "Running UDR test server..."
	./udr

test:
	@echo "Running verification tests..."
	nix develop --command go test ./...

clean:
	rm -rf spec internal/api/udr.gen.go internal/api/stubs.gen.go udr

docker-mongo-start:
	docker run -d --name udr-mongo -p 27017:27017 mongodb/mongodb-community-server:latest

docker-mongo-stop:
	docker stop udr-mongo && docker rm udr-mongo
