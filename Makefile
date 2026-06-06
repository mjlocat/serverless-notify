# Build the Lambda binaries as statically-linked arm64 `bootstrap` executables
# for the provided.al2023 custom runtime. Terraform's archive_file zips each
# dist/<name> directory, so we only need to produce the binaries here.

GOOS    := linux
GOARCH  := arm64
LDFLAGS := -s -w
LAMBDAS := api ws

.PHONY: build $(LAMBDAS) tidy clean

build: $(LAMBDAS)

$(LAMBDAS):
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -tags lambda.norpc -ldflags "$(LDFLAGS)" \
		-o dist/$@/bootstrap ./cmd/$@

tidy:
	go mod tidy

clean:
	rm -rf dist
