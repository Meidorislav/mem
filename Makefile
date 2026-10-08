.PHONY: all build install uninstall test lint clean download-artifacts

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

# Where `make install` puts the binary: $GOBIN, else $GOPATH/bin. Override
# with e.g. `make install BINDIR=/usr/local/bin`.
BINDIR ?= $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

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

# The LanceDB library is linked statically, so the installed binary runs
# from anywhere without the repo.
install: lib/$(PLATFORM)/liblancedb_go.a
	mkdir -p $(BINDIR)
	go build -ldflags "-s -w" -o $(BINDIR)/mem ./cmd/mem
	@echo "Installed $(BINDIR)/mem"
	@case ":$$PATH:" in *":$(BINDIR):"*) ;; *) echo "Note: $(BINDIR) is not in your PATH; add it, e.g. export PATH=\"$(BINDIR):\$$PATH\"";; esac

uninstall:
	rm -f $(BINDIR)/mem

test: lib/$(PLATFORM)/liblancedb_go.a
	go test -v ./...

lint: lib/$(PLATFORM)/liblancedb_go.a
	@unformatted=$$(gofmt -l cmd internal); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go vet ./...

clean:
	rm -f mem
