FROM golang:1.19-buster as builder

ENV GO111MODULE "on"

ARG BUILD_VER

WORKDIR /usr/local/go/src/webhook
COPY . .
RUN go mod download
ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0

RUN go build \
  -v \
  -ldflags "-w -s -X 'main.BuildDatetime=$(date --iso-8601=seconds)'" \
  -o webhook \
  ./main.go

FROM alpine:3.17.0
WORKDIR /app
COPY --from=builder /usr/local/go/src/webhook/webhook /webhook/
RUN apk add tzdata --no-cache
ENTRYPOINT ["/webhook/webhook"]
LABEL maintainer="flsixtyfour@gmail.com"
LABEL org.label-schema.vcs-url="https://github.com/fl64/docker-secret-validation-webhook"
LABEL org.label-schema.docker.cmd="docker run --rm fl64/docker-secret-validation-webhook:latest"