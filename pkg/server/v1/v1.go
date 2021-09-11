package v1

import (
	"encoding/json"
	"net/http"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type V1 struct {
	cli client.Client
	log logr.Logger
}

// NewV1 returns a new V1 object.
func NewV1(cli client.Client, logger logr.Logger) *V1 {
	return &V1{
		cli: cli,
		log: logger,
	}
}

// RegisterRoutes registers all V1 routes on the given `http.ServeMux`.
func (v *V1) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		v.respondJSON(w, map[string]string{"name": "openshift-cli-manager"})
	})

	mux.HandleFunc("/v1/tools/", v.handleListTools)
	mux.HandleFunc("/v1/tools/info/", v.handleToolInfo)
	mux.HandleFunc("/v1/tools/download/", v.handleDownloadTool)
}

// responseUserError returns a JSON error object to the requestor.
func (v *V1) respondUserError(w http.ResponseWriter, code int, err error) {
	msg := map[string]string{
		"error": err.Error(),
	}
	v.respondJSONWithCode(w, code, msg)
}

// responseSystemError returns a JSON error object to the requestor, and adds an entry to the controller's error log.
func (v *V1) respondSystemError(w http.ResponseWriter, code int, err error, while string) {
	v.respondUserError(w, code, err)
	v.log.Error(err, while)
}

// respondJSON is a helper method for returning a JSON object with HTTP status code 200.
func (v *V1) respondJSON(w http.ResponseWriter, val interface{}) {
	v.respondJSONWithCode(w, http.StatusOK, val)
}

// respondJSONWithCode is a helper method for returning a JSON object with a custom HTTP status code.
func (v *V1) respondJSONWithCode(w http.ResponseWriter, code int, val interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(val); err != nil {
		v.respondSystemError(w, 500, err, "encoding JSON for response")
		return
	}
}
