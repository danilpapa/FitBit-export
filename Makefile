RUN := go run ./cmd/fitbit-export

.DEFAULT_GOAL := help
.PHONY: help report strength auth fatsecret-auth note test vet fmt build

help: ## Show available commands
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  make %-15s %s\n", $$1, $$2}'

report: ## Print today's report
	@$(RUN)

strength: ## Print strength workouts, weight and nutrition
	@$(RUN) strength

auth: ## Sign in to Google and save the refresh token
	@$(RUN) auth

fatsecret-auth: ## Link FatSecret and save the access token
	@$(RUN) fatsecret-auth

note: ## Add a note for today: make note TEXT="Slept badly"
	@test -n "$(TEXT)" || { echo 'usage: make note TEXT="how you feel today"'; exit 1; }
	@$(RUN) note "$(TEXT)"

test: ## Run tests
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format Go code
	gofmt -w .

build: ## Build the binary into bin/
	go build -o bin/fitbit-export ./cmd/fitbit-export
