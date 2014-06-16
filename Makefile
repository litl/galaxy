.SILENT :
.PHONY : commander shuttle discovery galaxy quasar clean fmt test

TAG:=`git describe --abbrev=0 --tags`
LDFLAGS:=-X main.buildVersion `git describe --long`

all: commander shuttle discovery galaxy quasar

deps:
	godep restore

commander:
	echo "Building commander"
	go install -ldflags "$(LDFLAGS)" github.com/litl/galaxy/commander

shuttle:
	echo "Building shuttle"
	go install -ldflags "$(LDFLAGS)" github.com/litl/galaxy/shuttle

discovery:
	echo "Building discovery"
	go install -ldflags "$(LDFLAGS)" github.com/litl/galaxy/discovery

galaxy:
	echo "Building galaxy"
	go install -ldflags "$(LDFLAGS)" github.com/litl/galaxy

quasar:
	echo "Building quasar"
	go install -ldflags "$(LDFLAGS)" github.com/litl/galaxy/quasar

clean:
	rm -f $(GOPATH)/bin/{commander,discovery,shuttle}

fmt:
	go fmt github.com/litl/galaxy/...

test:
	go test -v github.com/litl/galaxy/...

dist-clean:
	rm -rf dist
	rm -f galaxy-*.tar.gz

dist-init:
	mkdir -p dist/$$GOOS/$$GOARCH

dist-build: dist-init
	echo "Compiling $$GOOS/$$GOARCH"
	go build -ldflags "$(LDFLAGS)" -o dist/$$GOOS/$$GOARCH/galaxy github.com/litl/galaxy
	go build -ldflags "$(LDFLAGS)" -o dist/$$GOOS/$$GOARCH/commander github.com/litl/galaxy/commander
	go build -ldflags "$(LDFLAGS)" -o dist/$$GOOS/$$GOARCH/shuttle github.com/litl/galaxy/shuttle
	go build -ldflags "$(LDFLAGS)" -o dist/$$GOOS/$$GOARCH/discovery github.com/litl/galaxy/discovery

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
	echo "Building $$GOOS-$$GOARCH-$(TAG).tar.gz"
	tar -cvzf galaxy-$$GOOS-$$GOARCH-$(TAG).tar.gz -C dist/$$GOOS/$$GOARCH galaxy commander discovery shuttle >/dev/null 2>&1

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

