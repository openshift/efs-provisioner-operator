BINDATA=pkg/generated/bindata.go
GOBINDATA_BIN=$(GOPATH)/bin/go-bindata

all: build

build: generate
	operator-sdk build quay.io/openshift/efs-provisioner-operator

generate: $(GOBINDATA_BIN)
	go-bindata -nometadata -pkg generated -o $(BINDATA) manifests/...

$(GOBINDATA_BIN):
	go get -u github.com/jteeuwen/go-bindata/...

helm:
	mkdir -p build/_output
	helm template deploy/olm/chart -f deploy/olm/chart/values.yaml --output-dir build/_output

deploy-olm: helm
	-kubectl create -f build/_output/olm/templates

deploy-subscription: deploy-olm
	kubectl create -f deploy/olm-catalog/subscription.yaml

deploy-installplan: deploy-olm
	kubectl create -f deploy/olm-catalog/installplan.yaml

deploy-vanilla:
	kubectl create -f deploy

test-e2e:
	cd test/e2e; dep ensure; ginkgo

clean:
	rm -rf build/_output
