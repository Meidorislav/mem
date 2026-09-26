.PHONY: all build test clean download-artifacts

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

# Fetch the prebuilt LanceDB static libraries and headers into lib/ and include/.
download-artifacts:
	curl -sSL --fail $(LANCEDB_ARCHIVE) | tar -xz -C $(CURDIR) lib include

lib/$(PLATFORM)/liblancedb_go.a:
	$(MAKE) download-artifacts

build: lib/$(PLATFORM)/liblancedb_go.a
	go build -o mem ./cmd/mem

test: lib/$(PLATFORM)/liblancedb_go.a
	go test -v ./...

clean:
	rm -f mem
