REGISTRY_REPO=fl64
CONTAINER_NAME=docker-secret-validation-webhook
CONTAINER_VER:=$(shell git describe --tags --always)

HADOLINT_VER:=v2.12.0-alpine
GOLANGLINT_VER:=v1.50.1

CONTAINER_NAME_TAG=$(REGISTRY_REPO)/$(CONTAINER_NAME):$(CONTAINER_VER)
CONTAINER_NAME_LATEST=$(REGISTRY_REPO)/$(CONTAINER_NAME):latest

NS:=docker-secret-validation-webhook

.PHONY: build latest push push_latest lint_hadolint lint_golangci lint helm_install helm_uninstall test_install test_uninstall get_wh_cert get_wh_ca

mod:
	go mod tidy

test:
	go test ./... -v

build:
	docker build -t $(CONTAINER_NAME_TAG) .

latest: build
	docker tag $(CONTAINER_NAME_TAG) $(CONTAINER_NAME_LATEST)

push: build
	docker push $(CONTAINER_NAME_TAG)

push_latest: push latest
	docker push $(CONTAINER_NAME_LATEST)

lint_hadolint:
	docker run --rm -v "${PWD}":/app:ro -w /app hadolint/hadolint:$(HADOLINT_VER) hadolint /app/Dockerfile

lint_golangci: # -u $(shell id -u)
	docker run  --rm -v $(PWD):/app:ro -w /app golangci/golangci-lint:$(GOLANGLINT_VER) golangci-lint run -v --timeout=360s

lint: lint-hadolint lint-golangci

helm_install:
	helm upgrade --install docker-secret-validation-webhook -n $(NS) ./helm --create-namespace

helm_uninstall:
	helm uninstall  docker-secret-validation-webhook -n $(NS)

logs:
	stern -n $(NS) -l app.kubernetes.io/name=docker-secret-validation-webhook

rollout:
	 kubectl -n $(NS) rollout restart deployment docker-secret-validation-webhook

test_install:
	kubectl apply -f test.yaml

test_uninstall:
	kubectl delete -f test.yaml

get_wh_cert:
	kubectl -n $(NS) get secret docker-secret-validation-webhook -o json | jq -r '.data."tls.crt" | @base64d' | openssl x509 -text -noout

get_wh_ca:
	kubectl get ValidatingWebhookConfiguration docker-secret-validation-webhook -o json | jq -r ".webhooks[0].clientConfig.caBundle| @base64d" | openssl x509 -text -noout

