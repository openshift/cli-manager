package v1

import (
	"context"
	"net/http"

	toolsv1 "github.com/deejross/openshift-cli-manager/api/v1"
)

func (v *V1) toolsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "LIST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if len(r.URL.Query().Get("name")) == 0 {
		v.listTools(w, r)
	} else {
		v.downloadTool(w, r)
	}
}

func (v *V1) listTools(w http.ResponseWriter, r *http.Request) {
	list := &toolsv1.CLIToolList{}

	if err := v.cli.List(context.Background(), list); err != nil {
		v.respondError(w, 500, err, "obtaining list of tools from k8s API")
		return
	}

	v.respondJSON(w, list)
}

func (v *V1) downloadTool(w http.ResponseWriter, r *http.Request) {
	// TODO: finish
}
