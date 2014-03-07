.SILENT :
.PHONY : commander shuttle discovery clean fmt test

all: commander shuttle discovery

commander:
	echo "Building commander"
	go install github.com/litl/galaxy/commander

shuttle:
	echo "Building shuttle"
	go install github.com/litl/galaxy/shuttle

discovery:
	echo "Building discovery"
	go install github.com/litl/galaxy/discovery

clean:
	rm -f $(GOPATH)/bin/{commander,discovery,shuttle}

fmt:
	go fmt github.com/litl/galaxy/...

test:
	go test -v github.com/litl/galaxy/...

