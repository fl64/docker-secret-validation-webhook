package webhook

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"io"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"text/template"
	"time"
)

type FakeRegistryClient struct {
}

func (r FakeRegistryClient) CheckImage(registry, image string, authCfg authn.AuthConfig) error {
	if authCfg.Username != "valid" {
		return fmt.Errorf("Auth failed")
	}
	return nil
}

func TestWebhook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook")
}

const webhookAddrTest = ""

const admisstionReviewJsonTemplate = `
{
  "kind": "AdmissionReview",
  "apiVersion": "admission.k8s.io/v1",
  "request": {
    "uid": "12345678-1234-1234-1234-123456789012",
    "name": "test",
    "namespace": "default",
    "operation": "CREATE",
    "object": {
      "kind": "Secret",
      "apiVersion": "v1",
      "metadata": {
        "name": "test",
        "namespace": "default",
        "uid": "69eb5e7f-eae6-4f42-af0a-f83fe36ee5c4",
        "managedFields": []
      },
      "data": {
		{{ if .DockerConfigB64 }}
        ".dockerconfigjson": "{{ .DockerConfigB64 }}"
		{{ end }}
      },
      "type": "{{ .SecretType }}"
    },
    "options": {}
  }
}
`

type templateParams struct {
	SecretType       string
	DockerConfigJson string
	DockerConfigB64  string
}

func AdmisstionJson(params templateParams) string {
	var output bytes.Buffer
	if params.SecretType == "" {
		params.SecretType = "kubernetes.io/dockerconfigjson"
	}
	if params.DockerConfigB64 == "" {
		params.DockerConfigB64 = base64.StdEncoding.EncodeToString([]byte(params.DockerConfigJson))
	}

	t := template.Must(template.New("").Parse(admisstionReviewJsonTemplate))
	_ = t.Execute(&output, params)

	return output.String()
}

type wanted struct {
	BodySubstring    string
	StatusCode       int
	AdmissionAllowed bool
}

var _ = Describe("ValidatingWebhook", func() {
	Context("Test Webhook Run", func() {
		ctx := context.Background()
		ctx, cancel := context.WithTimeout(ctx, time.Second*1)
		defer cancel()
		r := FakeRegistryClient{}
		vw := NewValidatingWebhook(":36363", "test-image", "", "", r)
		err := vw.Run(ctx)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("Test Webhook Handler", func() {
		r := FakeRegistryClient{}
		vw := NewValidatingWebhook(":36363", "test-image", "", "", r)
		DescribeTable("",
			func(admissionReview string, want *wanted) {
				r := httptest.NewRequest("POST", "/validate", strings.NewReader(admissionReview))
				w := httptest.NewRecorder()
				vw.ValidatingWebhook(w, r)
				resp := w.Result()
				body, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(want.StatusCode))
				Expect(string(body)).To(ContainSubstring(want.BodySubstring))
				if resp.StatusCode == http.StatusOK {
					review := &admissionv1.AdmissionReview{}
					err := json.Unmarshal(body, review)
					Expect(err).NotTo(HaveOccurred())
					Expect(review.Response.UID).To(Equal(types.UID("12345678-1234-1234-1234-123456789012")))
					Expect(review.Response.Allowed).To(Equal(want.AdmissionAllowed))
				}

			},
			Entry("Invalid admission review",
				"{}",
				&wanted{
					BodySubstring: "bad admission review",
					StatusCode:    http.StatusBadRequest,
				}),
			Entry("Secret with wrong type",
				AdmisstionJson(templateParams{
					DockerConfigJson: "",
					SecretType:       "Opaque",
				}),
				&wanted{
					AdmissionAllowed: false,
					BodySubstring:    "secret should be kubernetes.io/dockerconfigjson type",
					StatusCode:       http.StatusOK,
				}),
			Entry("Field .dockerconfigjson is missed in the secret",
				AdmisstionJson(templateParams{
					DockerConfigJson: "",
				}),
				&wanted{
					AdmissionAllowed: false,
					BodySubstring:    "secret should contain .dockerconfigjson field",
					StatusCode:       http.StatusOK,
				}),
			Entry("Bad .dockerconfigjson data",
				AdmisstionJson(templateParams{
					DockerConfigJson: `{"aaa": "bbb"}`, // {"aaa":"bbb"}
				}),
				&wanted{
					AdmissionAllowed: false,
					BodySubstring:    "bad docker config",
					StatusCode:       http.StatusOK,
				}),
			Entry("Empty auths",
				AdmisstionJson(templateParams{
					DockerConfigJson: `{ "auths": { } }`,
				}),
				&wanted{
					AdmissionAllowed: false,
					BodySubstring:    "bad docker config",
					StatusCode:       http.StatusOK,
				}),
			Entry("Valid Secret with invalid creds",
				AdmisstionJson(templateParams{
					DockerConfigJson: `{ "auths": { "registry.example.com": { "auth": "aW52YWxpZDppbnZhbGlkCg==" } } }`, // invalid:invalid
				}),
				&wanted{
					AdmissionAllowed: false,
					BodySubstring:    "Auth failed",
					StatusCode:       http.StatusOK,
				}),
			Entry("Valid Secret with working creds",
				AdmisstionJson(templateParams{
					DockerConfigJson: `{ "auths": { "registry.example.com": { "auth": "dmFsaWQ6dmFsaWQK" } } }`, // valid:valid
				}),
				&wanted{
					AdmissionAllowed: true,
					BodySubstring:    "",
					StatusCode:       http.StatusOK,
				}),
		)
	})

})
