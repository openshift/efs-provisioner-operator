# TODO https://github.com/openshift/cluster-ingress-operator/blob/master/Makefile

BINDATA=pkg/generated/bindata.go

GOBINDATA_BIN=$(GOPATH)/bin/go-bindata

# Using "-modtime 1" to make generate target deterministic. It sets all file time stamps to unix timestamp 1
generate: $(GOBINDATA_BIN)
	go-bindata -nometadata -pkg generated -o $(BINDATA) manifests/...
	# go-bindata -nometadata -pkg generated -o $(TEST_BINDATA) test/manifests/...

$(GOBINDATA_BIN):
	go get -u github.com/jteeuwen/go-bindata/...
