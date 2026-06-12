.PHONY: all build test lint clean tailwind

all: tailwind build

build:
	go build -o pi_dashboard .

test:
	go test ./... -v -count=1

lint:
	golangci-lint run ./...

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -f pi_dashboard pi_bundle/pi_portfolio_arm64 pi_bundle/pi_portfolio_normal
	rm -f static/output.css
	rm -f tailwindcss-*
	rm -rf pi_bundle/pi_bundle.tar.gz

tailwind:
	@if [ ! -f tailwindcss-* ]; then \
		echo "Downloading Tailwind CSS standalone CLI..."; \
		UNAME_S=$$(uname -s); \
		UNAME_M=$$(uname -m); \
		case "$$UNAME_S" in \
			Darwin) PLATFORM="macos";; \
			Linux) PLATFORM="linux";; \
		esac; \
		case "$$UNAME_M" in \
			arm64) ARCH="arm64";; \
			x86_64) ARCH="x64";; \
		esac; \
		BIN="tailwindcss-$${PLATFORM}-$${ARCH}"; \
		curl -sL "https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.17/$$BIN" -o "$$BIN"; \
		chmod +x "$$BIN"; \
	fi; \
	./tailwindcss-* -i web/static/input.css -o static/output.css --minify