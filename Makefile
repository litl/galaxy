.SILENT :
.PHONY : commander shuttle discovery galaxy clean fmt test

TAG:=`git describe --abbrev=0 --tags`

all: commander shuttle discovery galaxy

deps:
	godep restore

commander:
	echo "Building commander"
	go install github.com/litl/galaxy/commander

shuttle:
	echo "Building shuttle"
	go install github.com/litl/galaxy/shuttle

discovery:
	echo "Building discovery"
	go install github.com/litl/galaxy/discovery

galaxy:
	echo "Building galaxy"
	go install github.com/litl/galaxy

clean:
	rm -f $(GOPATH)/bin/{commander,discovery,shuttle}

fmt:
	go fmt github.com/litl/galaxy/...

test:
	go test -v github.com/litl/galaxy/...

dist-clean:
	rm -rf dist
	rm -f docker-gen-linux-*.tar.gz

dist-init:
	mkdir -p dist/$$GOOS/$$GOARCH

dist-build: dist-init
	echo "Compiling $$GOOS/$$GOARCH"
	go build -o dist/$$GOOS/$$GOARCH/galaxy github.com/litl/galaxy
	go build -o dist/$$GOOS/$$GOARCH/commander github.com/litl/galaxy/commander
	go build -o dist/$$GOOS/$$GOARCH/shuttle github.com/litl/galaxy/shuttle
	go build -o dist/$$GOOS/$$GOARCH/discovery github.com/litl/galaxy/discovery

dist-linux-amd64:
	export GOOS="linux"; \
	export GOARCH="amd64"; \
	$(MAKE) dist-build

dist-linux-386:
	export GOOS="linux"; \
	export GOARCH="386"; \
	$(MAKE) dist-build

dist-darwin-amd64:
	export GOOS="darwin"; \
	export GOARCH="amd64"; \
	$(MAKE) dist-build

dist-darwin-386:
	export GOOS="darwin"; \
	export GOARCH="386"; \
	$(MAKE) dist-build

dist: dist-clean dist-init dist-linux-amd64 dist-linux-386 dist-darwin-amd64 dist-darwin-386

release-tarball:
	echo "Building $$GOOS-$$GOARCH-latest.tar.gz"
	tar -cvzf galaxy-$$GOOS-$$GOARCH-latest.tar.gz -C dist/$$GOOS/$$GOARCH galaxy commander discovery shuttle >/dev/null 2>&1
	echo "Building $$GOOS-$$GOARCH-$(TAG).tar.gz"
	cp galaxy-$$GOOS-$$GOARCH-latest.tar.gz galaxy-$$GOOS-$$GOARCH-$(TAG).tar.gz

release-linux-amd64:
	export GOOS="linux"; \
	export GOARCH="amd64"; \
	$(MAKE) release-tarball

release-linux-386:
	export GOOS="linux"; \
	export GOARCH="386"; \
	$(MAKE) release-tarball

release-darwin-amd64:
	export GOOS="darwin"; \
	export GOARCH="amd64"; \
	$(MAKE) release-tarball

release-darwin-386:
	export GOOS="darwin"; \
	export GOARCH="386"; \
	$(MAKE) release-tarball

release: deps dist release-linux-amd64 release-linux-386 release-darwin-amd64 release-darwin-386

