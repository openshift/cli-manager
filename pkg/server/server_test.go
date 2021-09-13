/*
Copyright 2021.

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

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	configv1 "github.com/deejross/openshift-cli-manager/api/v1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cli client.Client
var handler http.Handler
var log logr.Logger
var testEnv *envtest.Environment

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Tools Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = configv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	cli, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(cli).NotTo(BeNil())

	log = ctrl.Log.WithName("tools_test")
	handler = NewHTTPHandler(cli, log)

	// load some test resources
	tool := &configv1.CLITool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bash",
			Namespace: "default",
		},
		Spec: configv1.CLIToolSpec{
			Description: "just a test",
			Versions: []configv1.CLIToolVersion{
				{
					Version: "v4.4.20",
					Binaries: []configv1.CLIToolVersionBinary{
						{
							Platform: "linux/amd64",
							Image:    "redhat/ubi8-micro:latest",
							Path:     "/usr/bin/bash",
						},
					},
				},
			},
		},
	}

	err = cli.Create(context.Background(), tool)
	Expect(err).NotTo(HaveOccurred())
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("v1", func() {
	It("should return index.html with no path", func() {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring("<html>"))
	})

	It("should return index.css with explicit path", func() {
		req := httptest.NewRequest("GET", "/resources/index.css", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring("body"))
	})

	It("should return 404 for unknown paths", func() {
		req := httptest.NewRequest("GET", "/unknown", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})
})

var _ = Describe("tools", func() {
	It("should reject unsupported methods", func() {
		req := httptest.NewRequest("POST", "/v1/tools/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusMethodNotAllowed))
	})

	It("should list CLITools", func() {
		req := httptest.NewRequest("GET", "/v1/tools/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		list := &configv1.HTTPCLIToolList{}
		err := json.NewDecoder(rec.Body).Decode(list)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Namespace).To(Equal("default"))
		Expect(list.Items[0].Name).To(Equal("bash"))
		Expect(list.Items[0].Description).To(Equal("just a test"))
		Expect(list.Items[0].Platforms).To(Equal([]string{"linux/amd64"}))
		Expect(list.Items[0].LatestVersion).To(Equal("v4.4.20"))
	})

	It("should list CLITools for specific platform", func() {
		req := httptest.NewRequest("GET", "/v1/tools/?platform=linux/amd64", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		list := &configv1.HTTPCLIToolList{}
		err := json.NewDecoder(rec.Body).Decode(list)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Namespace).To(Equal("default"))
		Expect(list.Items[0].Name).To(Equal("bash"))
		Expect(list.Items[0].Description).To(Equal("just a test"))
		Expect(list.Items[0].Platforms).To(Equal([]string{"linux/amd64"}))
		Expect(list.Items[0].LatestVersion).To(Equal("v4.4.20"))
	})

	It("should not list unexpected CLITools", func() {
		req := httptest.NewRequest("GET", "/v1/tools/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		list := &configv1.HTTPCLIToolList{}
		err := json.NewDecoder(rec.Body).Decode(list)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Name).NotTo(Equal("curl"))
	})

	It("should not list unexpected CLITools for specific platform", func() {
		req := httptest.NewRequest("GET", "/v1/tools/?platform=windows/amd64", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		list := &configv1.HTTPCLIToolList{}
		err := json.NewDecoder(rec.Body).Decode(list)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(list.Items).To(BeEmpty())
	})

	It("should get info for CLITool", func() {
		req := httptest.NewRequest("GET", "/v1/tools/info/?namespace=default&name=bash", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		item := &configv1.HTTPCLIToolInfo{}
		err := json.NewDecoder(rec.Body).Decode(item)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(item.Namespace).To(Equal("default"))
		Expect(item.Name).To(Equal("bash"))
		Expect(item.Description).To(Equal("just a test"))
		Expect(item.Versions).NotTo(BeEmpty())
		Expect(item.Versions[0].Version).To(Equal("v4.4.20"))
		Expect(item.Versions[0].Binaries).NotTo(BeEmpty())
		Expect(item.Versions[0].Binaries[0].Platform).To(Equal("linux/amd64"))
		Expect(item.Versions[0].Binaries[0].Image).To(Equal("redhat/ubi8-micro:latest"))
		Expect(item.Versions[0].Binaries[0].Path).To(Equal("/usr/bin/bash"))
	})

	It("should get info for latest version of CLITool", func() {
		req := httptest.NewRequest("GET", "/v1/tools/info/?namespace=default&name=bash&version=latest", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		item := &configv1.HTTPCLIToolInfo{}
		err := json.NewDecoder(rec.Body).Decode(item)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(item.Namespace).To(Equal("default"))
		Expect(item.Name).To(Equal("bash"))
		Expect(item.Description).To(Equal("just a test"))
		Expect(item.Versions).NotTo(BeEmpty())
		Expect(item.Versions[0].Version).To(Equal("v4.4.20"))
		Expect(item.Versions[0].Binaries).NotTo(BeEmpty())
		Expect(item.Versions[0].Binaries[0].Platform).To(Equal("linux/amd64"))
		Expect(item.Versions[0].Binaries[0].Image).To(Equal("redhat/ubi8-micro:latest"))
		Expect(item.Versions[0].Binaries[0].Path).To(Equal("/usr/bin/bash"))
	})

	It("should get info for specific version of CLITool", func() {
		req := httptest.NewRequest("GET", "/v1/tools/info/?namespace=default&name=bash&version=v4.4.20", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		item := &configv1.HTTPCLIToolInfo{}
		err := json.NewDecoder(rec.Body).Decode(item)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(item.Namespace).To(Equal("default"))
		Expect(item.Name).To(Equal("bash"))
		Expect(item.Description).To(Equal("just a test"))
		Expect(item.Versions).NotTo(BeEmpty())
		Expect(item.Versions[0].Version).To(Equal("v4.4.20"))
		Expect(item.Versions[0].Binaries).NotTo(BeEmpty())
		Expect(item.Versions[0].Binaries[0].Platform).To(Equal("linux/amd64"))
		Expect(item.Versions[0].Binaries[0].Image).To(Equal("redhat/ubi8-micro:latest"))
		Expect(item.Versions[0].Binaries[0].Path).To(Equal("/usr/bin/bash"))
	})

	It("should download the requested CLITool", func() {
		req := httptest.NewRequest("GET", "/v1/tools/download/?namespace=default&name=bash&platform=linux/amd64", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/octet-stream"))
		Expect(rec.Header().Get("Content-Disposition")).To(Equal("attachment; filename=bash"))
		Expect(rec.Header().Get("Content-Transfer-Encoding")).To(Equal("binary"))
		Expect(rec.Body.Bytes()).NotTo(BeEmpty())
	})

	It("should have calculated the digest after a download", func() {
		req := httptest.NewRequest("GET", "/v1/tools/info/?namespace=default&name=bash&version=v4.4.20", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		item := &configv1.HTTPCLIToolInfo{}
		err := json.NewDecoder(rec.Body).Decode(item)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(item.Namespace).To(Equal("default"))
		Expect(item.Name).To(Equal("bash"))
		Expect(item.Description).To(Equal("just a test"))
		Expect(item.Versions).NotTo(BeEmpty())
		Expect(item.Versions[0].Version).To(Equal("v4.4.20"))
		Expect(item.Versions[0].Binaries).NotTo(BeEmpty())
		Expect(item.Versions[0].Binaries[0].Platform).To(Equal("linux/amd64"))
		Expect(item.Versions[0].Binaries[0].Image).To(Equal("redhat/ubi8-micro:latest"))
		Expect(item.Versions[0].Binaries[0].Path).To(Equal("/usr/bin/bash"))
		Expect(item.Digests).NotTo(BeEmpty())
		Expect(item.Digests[0].Name).To(Equal("v4.4.20/linux/amd64"))
		Expect(item.Digests[0].Calculated).NotTo(BeZero())
		Expect(item.Digests[0].Digest).To(ContainSubstring("sha256:"))
	})

	It("should get info for CLITool from digest", func() {
		By("getting a known digest first")
		req := httptest.NewRequest("GET", "/v1/tools/info/?namespace=default&name=bash&version=v4.4.20", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		item := &configv1.HTTPCLIToolInfo{}
		err := json.NewDecoder(rec.Body).Decode(item)
		Expect(err).NotTo(HaveOccurred())
		Expect(item.Digests).NotTo(BeEmpty())
		Expect(item.Digests[0].Name).To(Equal("v4.4.20/linux/amd64"))
		Expect(item.Digests[0].Calculated).NotTo(BeZero())
		Expect(item.Digests[0].Digest).To(ContainSubstring("sha256:"))

		By("using known digest to query for the tool")
		digest := item.Digests[0].Digest
		req = httptest.NewRequest("GET", "/v1/tools/info/?digest="+digest, nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		Expect(rec.Code).To(Equal(http.StatusOK))

		item = &configv1.HTTPCLIToolInfo{}
		err = json.NewDecoder(rec.Body).Decode(item)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(item.Namespace).To(Equal("default"))
		Expect(item.Name).To(Equal("bash"))
		Expect(item.Description).To(Equal("just a test"))
		Expect(item.Versions).NotTo(BeEmpty())
		Expect(item.Versions[0].Version).To(Equal("v4.4.20"))
		Expect(item.Versions[0].Binaries).NotTo(BeEmpty())
		Expect(item.Versions[0].Binaries[0].Platform).To(Equal("linux/amd64"))
		Expect(item.Versions[0].Binaries[0].Image).To(Equal("redhat/ubi8-micro:latest"))
		Expect(item.Versions[0].Binaries[0].Path).To(Equal("/usr/bin/bash"))
		Expect(item.Digests).NotTo(BeEmpty())
		Expect(item.Digests[0].Name).To(Equal("v4.4.20/linux/amd64"))
		Expect(item.Digests[0].Calculated).NotTo(BeZero())
		Expect(item.Digests[0].Digest).To(ContainSubstring("sha256:"))
	})
})
