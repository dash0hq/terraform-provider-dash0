default: build

.PHONY: build
build:
	go build -o terraform-provider-dash0

.PHONY: install
install: build
	mkdir -p ~/terraform-provider-mirror/registry.terraform.io/dash0hq/dash0/0.0.1/$(shell go env GOOS)_$(shell go env GOARCH)/
	cp terraform-provider-dash0 ~/terraform-provider-mirror/registry.terraform.io/dash0hq/dash0/0.0.1/$(shell go env GOOS)_$(shell go env GOARCH)/terraform-provider-dash0_v0.0.1

.PHONY: test
test:
	go test -v ./...

.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 20m

.PHONY: docs
docs:
	go generate ./...

.PHONY: clean
clean:
	rm -f terraform-provider-dash0

.PHONY: lint
lint:
	golangci-lint run
	
.PHONY: fmt
fmt:
	golangci-lint fmt --enable goimports
	golangci-lint run --fix --allow-parallel-runners --verbose --timeout=30m