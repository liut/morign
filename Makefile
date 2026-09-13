.SILENT :
.PHONY: docs

ifneq (,$(wildcard .env))
  include .env
  export
endif

NAME:=morign
ROOF:=github.com/liut/morign
DATE := $(shell date '+%Y%m%d')
TAG:=$(shell git describe --tags --always)
GO=$(shell which go)
GOMOD=$(shell echo "$${GO111MODULE:-auto}")
WEBAPIS=$(shell find pkg/web -type f \( -name "*.go" ! -name "*_test.go" \) -print )
LDFLAGS:=-X $(ROOF)/pkg/settings.name=$(NAME) -X $(ROOF)/pkg/settings.version=$(DATE)-$(TAG)
SOURCES=$(shell find . -type f \( -name "*.go" ! -name "*_test.go" \) -print )
MDs=$(shell find docs -type f \( -name "*.yaml" ! -name "swagger.yaml" \) -print )
SPEC=7

help:
	echo "make deps"
	echo "make dist"

codegen:
	mkdir -p ./pkg/models ./pkg/services/stores ./pkg/web
	for name in $(MDs); do \
		echo $${name}; \
		GO111MODULE=on $(GO) run -tags=codegen ../scaffold/scripts/codegen -spec $(SPEC) $${name} ; \
	done

generate:
	GO111MODULE=$(GOMOD) $(GO) generate ./...

vet:
	echo "Checking ./... , with GOMOD=$(GOMOD)"
	GO111MODULE=$(GOMOD) $(GO) vet -all ./...

deps:
	GO111MODULE=on $(GO) install golang.org/x/tools/cmd/goimports@latest
	GO111MODULE=on $(GO) install github.com/ddollar/forego@latest
	GO111MODULE=on $(GO) install github.com/liut/rerun@latest
	GO111MODULE=on $(GO) install github.com/swaggo/swag/cmd/swag@latest
	GO111MODULE=on $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

lint:
	GO111MODULE=$(GOMOD) golangci-lint -v run ./...

clean:
	echo "Cleaning dist"
	rm -rf dist
	rm -f $(NAME) $(NAME)-*

showver:
	echo "version: $(DATE)-$(TAG)"

dist/linux_amd64/$(NAME): $(SOURCES) showver
	echo "Building $(NAME) of linux/x64"
	mkdir -p dist/linux_amd64 && GO111MODULE=$(GOMOD) GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS) -s -w" -o dist/linux_amd64/$(NAME) .

dist/darwin_amd64/$(NAME): $(SOURCES) showver
	echo "Building $(NAME) of darwin/x64"
	mkdir -p dist/darwin_amd64 && GO111MODULE=$(GOMOD) GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS) -w" -o dist/darwin_amd64/$(NAME) .

dist/darwin_arm64/$(NAME): $(SOURCES) showver
	echo "Building $(NAME) of darwin/arm64"
	mkdir -p dist/darwin_arm64 && GO111MODULE=$(GOMOD) GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS) -w" -o dist/darwin_arm64/$(NAME) .

dist: vet dist/linux_amd64/$(NAME) dist/darwin_amd64/$(NAME) dist/darwin_arm64/$(NAME)

package-linux: dist/linux_amd64/$(NAME)
	echo "Packaging $(NAME)"
	ls dist/linux_amd64 | xargs tar -cvJf $(NAME)-linux-amd64-$(DATE)-$(TAG).tar.xz -C dist/linux_amd64

package-darwin: dist/darwin_amd64/$(NAME)
	echo "Packaging $(NAME)"
	ls dist/darwin_amd64 | xargs tar -cvJf $(NAME)-darwin-amd64-$(DATE)-$(TAG).tar.xz -C dist/darwin_amd64

docs/swagger.json: $(WEBAPIS)
	GO111MODULE=on swag init -g ./pkg/web/server.go -d ./ --ot json --parseDependency
# 	GO111MODULE=on swag init -d ./ --ot json --parseDependency

docs/swagger.yaml: $(WEBAPIS) docs/swagger.json
	test -f docs/swagger.json && which yq && yq -Poy docs/swagger.json > docs/swagger.yaml

touch-web-api:
	touch pkg/web/server.go

gen-apidoc: touch-web-api docs/swagger.json docs/swagger.yaml

test-models:
	go test -v -cover ./pkg/models/...

test-stores:
	go test -v -cover -tags=integration ./pkg/services/stores/...
