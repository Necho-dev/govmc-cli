.PHONY: govmc build install release wheel test check web-smoke

VERSION ?= 0.1.1
PWCLI ?= npx --yes --package @playwright/cli playwright-cli
LDFLAGS = -s -w -X github.com/Necho-dev/govmc-cli.Version=$(VERSION)

govmc:
	go run ./cmd/govmc

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/ ./cmd/govmc

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/govmc

release:
	python3 build/native.py --version "$(VERSION)"

wheel:
	python3 build/wheel.py --version "$(VERSION)"

test:
	go test ./...

check:
	go vet ./...

web-smoke:
	$(PWCLI) -s=govmc-web-qa open about:blank --browser chrome
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/workspace/smoke.cjs
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/datastores/smoke.cjs
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/settings/smoke.cjs
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/settings/themes.cjs
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/tasks/smoke.cjs
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/cache/smoke.cjs
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/wmks/smoke.cjs
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/console/recovery.cjs
	$(PWCLI) -s=govmc-web-qa reload
	$(PWCLI) -s=govmc-web-qa dialog-dismiss
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/console/recovery_cancel.cjs
	$(PWCLI) -s=govmc-web-qa reload
	$(PWCLI) -s=govmc-web-qa dialog-accept
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/console/recovery_accept.cjs
	$(PWCLI) -s=govmc-web-qa run-code --filename tests/web/console/navigation.cjs
	python3 tests/web/console/native_close.py --pwcli '$(PWCLI)' --session govmc-web-qa
	$(PWCLI) -s=govmc-web-qa close
