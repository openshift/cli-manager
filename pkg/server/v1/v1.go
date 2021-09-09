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
	mux.HandleFunc("/v1/tools/", v.listTools)
	mux.HandleFunc("/v1/tools/download/", v.downloadTool)
}

func (v *V1) respondUserError(w http.ResponseWriter, code int, err error) {
	msg := map[string]string{
		"error": err.Error(),
	}
	v.respondJSONWithCode(w, code, msg)
}

func (v *V1) respondSystemError(w http.ResponseWriter, code int, err error, while string) {
	v.respondUserError(w, code, err)
	v.log.Error(err, while)
}

func (v *V1) respondJSON(w http.ResponseWriter, val interface{}) {
	v.respondJSONWithCode(w, http.StatusOK, val)
}

func (v *V1) respondJSONWithCode(w http.ResponseWriter, code int, val interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(val); err != nil {
		v.respondSystemError(w, 500, err, "encoding JSON for response")
		return
	}
}
