.PHONY: build check run
build:
	mkdir -p bin
	go build -trimpath -o bin/voex-server.new .
	mv bin/voex-server.new bin/voex-server
check:
	go vet ./...
	go mod verify
run: build
	./bin/voex-server
