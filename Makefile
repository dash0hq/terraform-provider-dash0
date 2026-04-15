# Tools
TOOLS_BIN_DIR?=$(shell pwd)/.tools
GOLANGCI_LINT_VERSION=v2.9.0
GOLANGCI_LINT=$(TOOLS_BIN_DIR)/golangci-lint
CHLOGGEN_VERSION=v0.23.1
CHLOGGEN=$(TOOLS_BIN_DIR)/chloggen

default: build

.PHONY: build
build:
	go build -o terraform-provider-dash0

.PHONY: install
install: build
	mkdir -p ~/terraform-provider-mirror/registry.terraform.io/dash0hq/dash0/0.0.1/$(shell go env GOOS)_$(shell go env GOARCH)/
	cp terraform-provider-dash0 ~/terraform-provider-mirror/registry.terraform.io/dash0hq/dash0/0.0.1/$(shell go env GOOS)_$(shell go env GOARCH)/terraform-provider-dash0_v0.0.1

test: test-unit test-roundtrip

test-unit:
	go test -v ./...

.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 20m

.PHONY: test-roundtrip
test-roundtrip:
	./test/roundtrip/run_all.sh

.PHONY: docs
docs:
	go generate ./...

.PHONY: clean
clean:
	rm -f terraform-provider-dash0

lint-install: lint-go-install lint-sh-install

lint-go-install: $(GOLANGCI_LINT)

$(GOLANGCI_LINT):
	@mkdir -p $(TOOLS_BIN_DIR)
	GOBIN=$(TOOLS_BIN_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint-sh-install:
	@command -v shellcheck >/dev/null 2>&1 || { echo "Installing shellcheck..."; brew install shellcheck 2>/dev/null || sudo apt-get install -y shellcheck; }

lint: lint-go lint-sh

lint-go: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

lint-sh:
	shellcheck -x -e SC1091 $(shell find . -name '*.sh' -not -path './.claude/*' -not -path './.git/*')

.PHONY: fmt
fmt:
	golangci-lint fmt --enable goimports
	golangci-lint run --fix --allow-parallel-runners --verbose --timeout=30m

# Changelog management
$(CHLOGGEN):
	@mkdir -p $(TOOLS_BIN_DIR)
	GOBIN=$(TOOLS_BIN_DIR) go install go.opentelemetry.io/build-tools/chloggen@$(CHLOGGEN_VERSION)

chlog-install: $(CHLOGGEN)

chlog-new: $(CHLOGGEN)
	$(CHLOGGEN) new --config .chloggen/config.yaml --filename $(shell git branch --show-current)

chlog-validate: $(CHLOGGEN)
	$(CHLOGGEN) validate --config .chloggen/config.yaml

chlog-preview: $(CHLOGGEN)
	$(CHLOGGEN) update --config .chloggen/config.yaml --dry

chlog-update: $(CHLOGGEN)
	$(CHLOGGEN) update --config .chloggen/config.yaml --version $(VERSION)