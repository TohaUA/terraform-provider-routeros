VERSION=$(shell git describe --tags --abbrev=0)

EXT :=
ifeq ($(OS),Windows_NT)
	EXT := .exe
endif

.PHONY: docs debug schema-drift

all: docs tfformat compile checksum clean

test:
	go test -timeout 30s github.com/terraform-routeros/terraform-provider-routeros

# Compare the resource schemas with a live RouterOS device (GET only).
# Needs ROS_HOSTURL, ROS_USERNAME, ROS_PASSWORD and optionally ROS_CACERT / ROS_INSECURE in the environment.
# Optional: SCHEMA=schema.json (from `terraform providers schema -json`), INSPECT=inspect-7.24.json[.gz],
#           RESOURCES=routeros_ip_service,/interface/bridge, DRIFT_FLAGS="-all -fail-on-missing".
SCHEMA_DRIFT_MD ?= schema-drift.md
SCHEMA_DRIFT_JSON ?= schema-drift.json
schema-drift:
	go test ./tools/schema-drift/
	go run ./tools/schema-drift \
		$(if $(SCHEMA),-schema $(SCHEMA)) \
		$(if $(INSPECT),-inspect $(INSPECT)) \
		$(if $(RESOURCES),-resources $(RESOURCES)) \
		-md $(SCHEMA_DRIFT_MD) -json $(SCHEMA_DRIFT_JSON) $(DRIFT_FLAGS)

docs:
	go generate ./...
	# !!! GNU Sed
	find docs -type f -exec sed -i -E '/^.*__[[:alpha:]_]+__/d' {} \;

tfformat:
	terraform fmt -recursive examples/

debug:
	go generate routeros/provider.go
	go build -gcflags="all=-N -l" -o terraform-provider-routeros_${VERSION}$(EXT) main.go

compile:
	mkdir -p pkg
	echo "Removing previously built packages"
	rm -rf pkg/*
	go generate routeros/provider.go
	echo "Compiling for every OS and Platform"
	GOOS=linux GOARCH=arm go build -o terraform-provider-routeros_${VERSION} main.go
	zip pkg/terraform-provider-routeros_${VERSION}_linux_arm.zip terraform-provider-routeros_${VERSION}
	
	GOOS=linux GOARCH=arm64 go build -o terraform-provider-routeros_${VERSION} main.go
	zip pkg/terraform-provider-routeros_${VERSION}_linux_arm64.zip terraform-provider-routeros_${VERSION}

	GOOS=linux GOARCH=386 go build -o terraform-provider-routeros_${VERSION} main.go
	zip pkg/terraform-provider-routeros_${VERSION}_linux_386.zip terraform-provider-routeros_${VERSION}

	GOOS=linux GOARCH=amd64 go build -o terraform-provider-routeros_${VERSION} main.go
	zip pkg/terraform-provider-routeros_${VERSION}_linux_amd64.zip terraform-provider-routeros_${VERSION}

	GOOS=windows GOARCH=amd64 go build -o terraform-provider-routeros_${VERSION}.exe main.go
	zip pkg/terraform-provider-routeros_${VERSION}_windows_amd64.zip terraform-provider-routeros_${VERSION}.exe

	GOOS=windows GOARCH=386 go build -o terraform-provider-routeros_${VERSION}.exe main.go
	zip pkg/terraform-provider-routeros_${VERSION}_windows_386.zip terraform-provider-routeros_${VERSION}.exe

	GOOS=darwin GOARCH=amd64 go build -o terraform-provider-routeros_${VERSION} main.go
	zip pkg/terraform-provider-routeros_${VERSION}_darwin_amd64.zip terraform-provider-routeros_${VERSION}

	GOOS=darwin GOARCH=arm64 go build -o terraform-provider-routeros_${VERSION} main.go
	zip pkg/terraform-provider-routeros_${VERSION}_darwin_arm64.zip terraform-provider-routeros_${VERSION}

checksum:
	cd pkg && sha256sum *.zip > terraform-provider-routeros_${VERSION}_SHA256SUMS

clean:
	rm terraform-provider-routeros_${VERSION}