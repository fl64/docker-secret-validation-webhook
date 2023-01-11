package webhook

import (
	"context"
	"docker-secret-validation-webhook/internal/registryclient"
	"encoding/json"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"io"
	v12 "k8s.io/api/admission/v1"
	"k8s.io/api/core/v1"
	v13 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"os"
	"time"
)

type DockerConfig struct {
	Auths map[string]authn.AuthConfig `json:"auths"`
}

type ValidatingWebhook struct {
	addr           string
	tlsCertFile    string
	tlsKeyFile     string
	imageToCheck   string
	srv            *http.Server
	registryClient registryclient.RegistryClientInterface
}

func NewValidatingWebhook(addr, imageToCheck, tlsCertFile, tlsKeyFile string, registryClient registryclient.RegistryClientInterface) *ValidatingWebhook {
	return &ValidatingWebhook{
		tlsCertFile:    tlsCertFile,
		tlsKeyFile:     tlsKeyFile,
		imageToCheck:   imageToCheck,
		addr:           addr,
		registryClient: registryClient,
	}
}

func (vw *ValidatingWebhook) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		ctxShutDown := context.Background()
		ctxShutDown, cancel := context.WithTimeout(ctxShutDown, time.Second*5)
		defer func() {
			cancel()
		}()

		if vw.srv != nil {
			if err := vw.srv.Shutdown(ctxShutDown); err != nil {
				logrus.Fatalf("https server Shutdown Failed:%s", err)
			} else {
				logrus.Info("https server stopped")
			}
		}
	}()
	r := mux.NewRouter()
	r.PathPrefix("/validate").HandlerFunc(vw.ValidatingWebhook)
	vw.srv = &http.Server{
		Addr:    vw.addr,
		Handler: r,
	}

	// check if cert, key exist
	_, errKey := os.Stat(vw.tlsKeyFile)
	_, errCrt := os.Stat(vw.tlsCertFile)
	var err error
	if errKey == nil && errCrt == nil {
		logrus.Infof("serving https on %s", vw.addr)
		err = vw.srv.ListenAndServeTLS(vw.tlsCertFile, vw.tlsKeyFile)
	} else {
		logrus.Warnf("TLS cert and key files not found, serving http on %s", vw.addr)
		err = vw.srv.ListenAndServe()
	}

	if err != nil && err != http.ErrServerClosed {
		return err
	}
	logrus.Info("app stopped")
	return nil
}

func (vw *ValidatingWebhook) validateSecret(secret *v1.Secret) error {
	// Check secret type, it must be "kubernetes.io/dockerconfigjson"
	if secret.Type != v1.SecretTypeDockerConfigJson {
		return fmt.Errorf("secret should be %s type", v1.SecretTypeDockerConfigJson)
	}

	// Secret must contain ".dockerconfigjson" field
	dockerCfgRaw, ok := secret.Data[v1.DockerConfigJsonKey]
	if !ok {
		return fmt.Errorf("secret should contain %s field", v1.DockerConfigJsonKey)
	}

	dockerCfg := &DockerConfig{}
	err := json.Unmarshal(dockerCfgRaw, dockerCfg)
	if err != nil {
		return fmt.Errorf("сan't umarshal docker config: %v", err)
	}

	if len(dockerCfg.Auths) == 0 {
		return fmt.Errorf("bad docker config")
	}

	// check registries in docker config
	for registry, authCfg := range dockerCfg.Auths {
		err = vw.registryClient.CheckImage(registry, vw.imageToCheck, authCfg)
		if err != nil {
			return err
		}
	}

	return nil
}

func (vw *ValidatingWebhook) ValidatingWebhook(w http.ResponseWriter, r *http.Request) {
	// read request body
	var body []byte
	defer r.Body.Close()
	if r.Body != nil {
		if data, err := io.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	logrus.Debug("AdmissionReview:")
	logrus.Debug(string(body))

	// Decode the request body into an admission review struct
	review := &v12.AdmissionReview{}
	err := json.Unmarshal(body, review)
	if err != nil {
		logrus.Errorf("can't unmarshal admission review: %v", err)
		http.Error(w, "can't unmarshal admission review", http.StatusBadRequest)
		return
	}

	if review.Request == nil {
		logrus.Errorf("bad admission review")
		http.Error(w, "bad admission review", http.StatusBadRequest)
		return
	}

	// Decode secret
	secretJson := review.Request.Object.Raw

	secret := &v1.Secret{}
	err = json.Unmarshal(secretJson, secret)
	if err != nil {
		logrus.Errorf("can't unmarshal secret: %v", err)
		http.Error(w, "can't unmarshal secret", http.StatusBadRequest)
		return
	}

	// Respinse with same UID
	review.Response = &v12.AdmissionResponse{
		UID: review.Request.UID,
	}

	// Validate secret
	err = vw.validateSecret(secret)
	if err != nil {
		logrus.Errorf("validation of %s/%s secret failed: %v", secret.Namespace, secret.Name, err)
		review.Response.Allowed = false
		review.Response.Result = &v13.Status{
			Message: err.Error(),
		}
	} else {
		logrus.Infof("validation of the %s/%s secret was successful", secret.Namespace, secret.Name)
		review.Response.Allowed = true
	}

	// Send response
	reviewBytes, err := json.Marshal(review)
	if err != nil {
		logrus.Errorf("failed to marshal review: %v", err)
		http.Error(w, "failed to marshal review", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(reviewBytes)
}
