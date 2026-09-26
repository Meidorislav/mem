.PHONY: all build test lint clean download-artifacts

UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Darwin)
    ifeq ($(UNAME_M),arm64)
        PLATFORM := darwin_arm64
    else
        PLATFORM := darwin_amd64
    endif
    CGO_LDFLAGS := $(CURDIR)/lib/$(PLATFORM)/liblancedb_go.a -framework Security -framework CoreFoundation
else ifeq ($(UNAME_S),Linux)
    ifeq ($(UNAME_M),aarch64)
        PLATFORM := linux_arm64
    else
        PLATFORM := linux_amd64
    endif
    CGO_LDFLAGS := $(CURDIR)/lib/$(PLATFORM)/liblancedb_go.a -lm -ldl -lpthread
endif

LANCEDB_VERSION := $(shell go list -m -f '{{.Version}}' github.com/lancedb/lancedb-go)
LANCEDB_ARCHIVE := https://github.com/lancedb/lancedb-go/releases/download/$(LANCEDB_VERSION)/lancedb-go-native-binaries.tar.gz

CGO_CFLAGS := -I$(CURDIR)/include
export CGO_CFLAGS
export CGO_LDFLAGS

all: build

# Fetch the prebuilt LanceDB static library for this platform and the headers
# into lib/ and include/. The archive holds every platform (~450MB).
download-artifacts:
	curl -sSL --fail $(LANCEDB_ARCHIVE) | tar -xz -C $(CURDIR) lib/$(PLATFORM) include

lib/$(PLATFORM)/liblancedb_go.a:
	$(MAKE) download-artifacts

build: lib/$(PLATFORM)/liblancedb_go.a
	go build -o mem ./cmd/mem

test: lib/$(PLATFORM)/liblancedb_go.a
	go test -v ./...

lint: lib/$(PLATFORM)/liblancedb_go.a
	@unformatted=$$(gofmt -l cmd internal); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go vet ./...

clean:
	rm -f mem
