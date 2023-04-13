BIN_FILE="a-overlayfs"

build-test:
	@echo "build test"
	@git pull origin
	@go build -o $(BIN_FILE)

run-test:
	@sudo ./$(BIN_FILE)

push-test:
	@git add .
	@git commit -m "test"
	@git push origin a-overlay:a-overlay
