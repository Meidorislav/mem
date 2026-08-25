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

CGO_CFLAGS := -I$(CURDIR)/include
export CGO_CFLAGS
export CGO_LDFLAGS

all: build

build:
	go build -o mem ./cmd/mem

test:
	go test -v ./...

clean:
	rm -f mem
