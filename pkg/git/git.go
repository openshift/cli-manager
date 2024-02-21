package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"github.com/openshift/cli-manager/pkg/image"
	krew "github.com/openshift/cli-manager/pkg/krew/v1alpha2"
)

const GitRepoPath = "/var/run/git/cli-manager"

type Repo struct {
	repo *git.Repository
}

// Delete deletes the plugin yaml from the git repository
// and commits.
func (r *Repo) Delete(name string) error {
	fileName := fmt.Sprintf("plugins/%s.yaml", name)
	tree, err := r.repo.Worktree()
	if err != nil {
		return err
	}

	_, err = tree.Filesystem.Stat(fileName)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	tree.Filesystem.Remove(fileName)
	_, err = tree.Add(fileName)
	if err != nil {
		return err
	}
	_, err = tree.Commit(fmt.Sprintf("remove plugin %s", name), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "OpenShift CLI Manager",
			Email: "info@redhat.com",
			When:  time.Now(),
		}})
	if err != nil {
		return err
	}

	return nil
}

// Upsert adds new plugin yaml if currently it doesn't exist,
// updates if it does and commits this to git repository.
func (r *Repo) Upsert(name string, plugin *krew.Plugin) error {
	if plugin == nil {
		return nil
	}
	fileName := fmt.Sprintf("plugins/%s.yaml", name)
	tree, err := r.repo.Worktree()
	if err != nil {
		return err
	}

	f, err := tree.Filesystem.Create(fileName)
	if err != nil {
		return err
	}

	k, err := yaml.Marshal(plugin)
	if err != nil {
		return err
	}
	_, err = f.Write(k)
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}

	_, err = tree.Add(fileName)
	if err != nil {
		return err
	}

	_, err = tree.Commit(fmt.Sprintf("add plugin %s", name), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "OpenShift CLI Manager",
			Email: "info@redhat.com",
			When:  time.Now(),
		}})
	if err != nil {
		return err
	}

	return nil
}

// PrepareLocalGit creates a git directory and applies first commit
// to make it ready consumed by Krew.
func PrepareLocalGit() (*Repo, error) {
	os.RemoveAll(GitRepoPath)
	r, err := git.PlainInit(GitRepoPath, false)
	if err != nil {
		return nil, err
	}

	tree, err := r.Worktree()
	if err != nil {
		return nil, err
	}

	err = tree.Filesystem.MkdirAll("plugins/", 0755)
	if err != nil {
		return nil, err
	}

	f, err := tree.Filesystem.Create("plugins/README.md")
	_, err = f.Write([]byte("CLI Manager"))
	if err != nil {
		return nil, err
	}
	err = f.Close()
	if err != nil {
		return nil, err
	}

	err = tree.AddGlob(".")
	if err != nil {
		return nil, err
	}

	_, err = tree.Commit("Add README.md", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "OpenShift CLI Manager",
			Email: "info@redhat.com",
			When:  time.Now(),
		}})
	if err != nil {
		return nil, err
	}

	err = r.CreateBranch(&gitconfig.Branch{
		Name: string(plumbing.Master),
	})
	if err != nil {
		return nil, err
	}
	return &Repo{
		repo: r,
	}, nil
}

// PrepareGitServer creates a http server mux to support git compatible
// endpoints in addition to plugin download mechanism.
func PrepareGitServer() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli-manager/plugins/download/", HandleDownloadPlugin)
	mux.HandleFunc("/cli-manager/info/refs", HandleGitAdversitement)
	mux.HandleFunc("/cli-manager/git-upload-pack", HandleGitUploadPack)
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	return mux
}

// HandleGitAdversitement handles the git advertisement requests done by client tools
// relying on git compatibility. This function only supports upload-pack requests to limit
// the supported functionality only to git fetch and git clone.
func HandleGitAdversitement(w http.ResponseWriter, r *http.Request) {
	klog.Infof("plugin git advertisement request")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	vals := r.URL.Query()
	if len(vals) == 0 {
		http.Error(w, "too few query parameters", http.StatusBadRequest)
		return
	}

	if len(vals) > 1 {
		http.Error(w, "too many query parameters", http.StatusBadRequest)
		return
	}

	name := vals.Get("service")
	if name != transport.UploadPackServiceName {
		http.Error(w, "invalid service name", http.StatusForbidden)

		return
	}

	// We are using native git command execution instead of go-git library.
	// Because go-git does not properly work on some git requests (especially git fetch).
	// Besides, relying on git tool for such a simple but crucial functionality for our case
	// would be better for long term.
	cmd := exec.CommandContext(context.TODO(), "git", "upload-pack", "--stateless-rpc", "--advertise-refs", GitRepoPath)
	errbuf, outbuf := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r.Body, outbuf, io.MultiWriter(errbuf, os.Stderr)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("endpoint failure: %s", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	w.Header().Add("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(
		func(str string) []byte {
			s := strconv.FormatInt(int64(len(str)+4), 16)
			if len(s)%4 != 0 {
				s = strings.Repeat("0", 4-len(s)%4) + s
			}
			return []byte(s + str)
		}("# service=git-upload-pack"))
	w.Write([]byte("0000"))
	w.Write(outbuf.Bytes())
}

func HandleGitUploadPack(w http.ResponseWriter, r *http.Request) {
	klog.Infof("plugin git upload pack request")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// We are using native git command execution instead of go-git library.
	// Because go-git does not properly work on some git requests (especially git fetch).
	// Besides, relying on git tool for such a simple but crucial functionality for our case
	// would be better for long term.
	cmd := exec.CommandContext(context.TODO(), "git", "upload-pack", "--stateless-rpc", GitRepoPath)
	errbuf, outbuf := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r.Body, outbuf, io.MultiWriter(errbuf, os.Stderr)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("endpoint failure: %s", err), http.StatusBadRequest)
		return
	}

	w.Header().Add("Content-Type", "application/x-git-upload-pack-result")
	w.Header().Add("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(outbuf.Bytes())
}

func HandleDownloadPlugin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if len(name) == 0 {
		http.Error(w, "missing name in query", http.StatusBadRequest)
		return
	}

	platform := r.URL.Query().Get("platform")
	if len(platform) == 0 {
		http.Error(w, "missing platform in query", http.StatusBadRequest)
		return
	}

	fileName := fmt.Sprintf("%s_%s.tar.gz", name, platform)
	filePath := fmt.Sprintf("%s/%s", image.TarballPath, fileName)
	f, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Errorf("getting Plugin: name: %s, platform: %s err: %w", name, platform, err).Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+fileName)
	w.Header().Set("Content-Transfer-Encoding", "binary")

	if _, err = io.Copy(w, f); err != nil {
		http.Error(w, fmt.Errorf("getting Plugin: name: %s, platform: %s err: %w", name, platform, err).Error(), http.StatusInternalServerError)
		return
	}
}
