/*
Copyright 2022 Flant JSC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"docker-secret-validation-webhook/internal/registryclient"
	"docker-secret-validation-webhook/internal/webhook"
)

var (
	webhookAddr = flag.String("webhook-addr", ":8443", "Webhook address and port")
	healthAddr  = flag.String("health-addr", ":8001", "Health address and port")
	tagToCheck  = flag.String("tag-to-check", "tag-to-chek", "Image tag name to check")
	tlsCertFile = flag.String("tls-cert-file", "/tls/tls.crt", "Path to the TLS certificate file")
	tlsKeyFile  = flag.String("tls-key-file", "/tls/tls.key", "Path to the TLS key file")
	logLevelStr = flag.String("log-level", "info", "Log level")
)

var (
	BuildDatetime = "none"
	AppName       = "docker-secret-validating-webhook"
)

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// catch signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	go func() {
		s := <-sigChan
		close(sigChan)
		log.Infof("catch signal: %s", s)
		cancel()
	}()

	// Parse command-line flags
	flag.Parse()
	logLevel, err := log.ParseLevel(*logLevelStr)
	if err != nil {
		logLevel = log.InfoLevel
	}

	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(logLevel)
	log.Infof("%s build time %s", AppName, BuildDatetime)

	// health endpoint
	health := mux.NewRouter()
	health.PathPrefix("/healthz").HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	healthSrv := http.Server{
		Addr:    *healthAddr,
		Handler: health,
	}
	log.Infof("starting healthz on %s", *healthAddr)
	go func() { _ = healthSrv.ListenAndServe() }()

	registryClient := registryclient.NewRegistryClient()
	// run webnhook
	wh := webhook.NewValidatingWebhook(*webhookAddr, *tagToCheck, *tlsCertFile, *tlsKeyFile, registryClient)
	err = wh.Run(ctx)
	if err != nil {
		log.Errorf("error serving webhook: %v", err)
	}
}
