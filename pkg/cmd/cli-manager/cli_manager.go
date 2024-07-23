package cli_manager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	routeclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/openshift/cli-manager/pkg/controller"
	"github.com/openshift/cli-manager/pkg/git"
)

const (
	PortNumber = 9449
)

var ServeArtifactAsHttp bool

func RunCLIManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	dynamicClient, err := dynamic.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	route, err := routeclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	repo, err := git.PrepareLocalGit()
	if err != nil {
		return err
	}

	informers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	cliSyncController, err := controller.NewCLISyncController(repo, informers, client, route, ServeArtifactAsHttp, controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	mux := git.PrepareGitServer()
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", PortNumber),
		Handler:      mux,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 15 * time.Minute,
		// 1MB size should be sufficient
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			klog.Errorf("git server exited with error %s", err.Error())
		}
	}()
	go informers.Start(ctx.Done())
	go cliSyncController.Run(ctx, 1)
	<-ctx.Done()
	return nil
}
