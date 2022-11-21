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
	"strings"
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

	configv1 "github.com/openshift/cli-manager/api/v1"
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
		"Plugins Suite",
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

	log = ctrl.Log.WithName("plugins_test")
	handler = NewHTTPHandler(cli, log)

	// load some test resources
	plugin := &configv1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bash",
			Namespace: "default",
		},
		Spec: configv1.PluginSpec{
			ShortDescription: "just a test",
			Version:          "v4.4.20",
			Platforms: []configv1.PluginPlatform{
				{
					Platform: "linux/amd64",
					Image:    "redhat/ubi8-micro:latest",
					Bin:      "bash",
					Files: []configv1.FileLocation{
						{
							From: "/usr/bin/bash",
							To:   ".",
						},
					},
				},
			},
		},
	}

	err = cli.Create(context.Background(), plugin)
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

var _ = Describe("plugins", func() {
	It("should reject unsupported methods", func() {
		req := httptest.NewRequest("POST", "/v1/plugins/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusMethodNotAllowed))
	})

	It("should list Plugins", func() {
		req := httptest.NewRequest("GET", "/v1/plugins/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		list := &configv1.PluginList{}
		err := json.NewDecoder(rec.Body).Decode(list)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Namespace).To(Equal("default"))
		Expect(list.Items[0].Name).To(Equal("bash"))
		Expect(list.Items[0].Spec.ShortDescription).To(Equal("just a test"))
		Expect(list.Items[0].Spec.Platforms).To(HaveLen(1))
		Expect(list.Items[0].Spec.Platforms[0].Platform).To(Equal("linux/amd64"))
		Expect(list.Items[0].Spec.Version).To(Equal("v4.4.20"))
	})

	It("should list Plugins for specific platform", func() {
		req := httptest.NewRequest("GET", "/v1/plugins/?platform=linux/amd64", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		list := &configv1.PluginList{}
		err := json.NewDecoder(rec.Body).Decode(list)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Namespace).To(Equal("default"))
		Expect(list.Items[0].Name).To(Equal("bash"))
		Expect(list.Items[0].Spec.ShortDescription).To(Equal("just a test"))
		Expect(list.Items[0].Spec.Platforms).To(HaveLen(1))
		Expect(list.Items[0].Spec.Platforms[0].Platform).To(Equal("linux/amd64"))
		Expect(list.Items[0].Spec.Version).To(Equal("v4.4.20"))
	})

	It("should not list unexpected Plugins", func() {
		req := httptest.NewRequest("GET", "/v1/plugins/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		list := &configv1.PluginList{}
		err := json.NewDecoder(rec.Body).Decode(list)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Name).NotTo(Equal("curl"))
	})

	It("should not list unexpected Plugins for specific platform", func() {
		req := httptest.NewRequest("GET", "/v1/plugins/?platform=windows/amd64", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		list := &configv1.PluginList{}
		err := json.NewDecoder(rec.Body).Decode(list)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(list.Items).To(BeEmpty())
	})

	It("should get info for Plugin", func() {
		req := httptest.NewRequest("GET", "/v1/plugins/info/?namespace=default&name=bash", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))

		item := &configv1.Plugin{}
		err := json.NewDecoder(rec.Body).Decode(item)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(item.Namespace).To(Equal("default"))
		Expect(item.Name).To(Equal("bash"))
		Expect(item.Spec.ShortDescription).To(Equal("just a test"))
		Expect(item.Spec.Version).To(Equal("v4.4.20"))
		Expect(item.Spec.Platforms).NotTo(BeEmpty())
		Expect(item.Spec.Platforms[0].Platform).To(Equal("linux/amd64"))
		Expect(item.Spec.Platforms[0].Image).To(Equal("redhat/ubi8-micro:latest"))
		Expect(item.Spec.Platforms[0].Files).To(HaveLen(1))
		Expect(item.Spec.Platforms[0].Files[0].From).To(Equal("/usr/bin/bash"))
		Expect(item.Spec.Platforms[0].Files[0].To).To(Equal("."))
	})

	It("should download the requested Plugin", func() {
		req := httptest.NewRequest("GET", "/v1/plugins/download/?namespace=default&name=bash&platform=linux/amd64", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/octet-stream"))
		Expect(rec.Header().Get("Content-Encoding")).To(Equal("gzip"))
		Expect(rec.Header().Get("Content-Disposition")).To(Equal("attachment; filename=bash.tar.gz"))
		Expect(rec.Header().Get("Content-Transfer-Encoding")).To(Equal("binary"))
		Expect(rec.Body).NotTo(BeNil())
		Expect(rec.Body.Bytes()).NotTo(BeEmpty())
	})

	It("should be git-compatible", func() {
		req := httptest.NewRequest("GET", "/v1/my-repo.git/info/refs?service=git-upload-pack", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body).NotTo(BeNil())
		Expect(rec.Body.Len()).NotTo(BeZero())

		body := rec.Body.String()
		Expect(body).To(HavePrefix("001e# service=git-upload-pack\n0000"))
		Expect(body).To(ContainSubstring("HEAD\x00agent=go-git"))
		Expect(body).To(ContainSubstring("symref=HEAD:refs/heads/master"))
		Expect(body).To(HaveSuffix("refs/heads/master\n0000"))

		By("getting a hash from the response")
		hexStart := strings.Index(body, "\n003f")
		hexStop := strings.Index(body, " refs/heads/master")
		hex := body[hexStart+1 : hexStop]
		Expect(hex).To(HaveLen(44))
	})
})
