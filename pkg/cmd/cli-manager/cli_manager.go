package cli_manager

import (
	"context"
	"fmt"
	"net/http"

	routeclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/cli-manager/pkg/controller"
	"github.com/openshift/cli-manager/pkg/git"
)

const (
	PortNumber = 9449
)

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
	cliSyncController, err := controller.NewCLISyncController(repo, informers, client, route, controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	mux := git.PrepareGitServer()

	go http.ListenAndServe(fmt.Sprintf(":%d", PortNumber), mux)
	go informers.Start(ctx.Done())
	go cliSyncController.Run(ctx, 1)
	<-ctx.Done()
	return nil
}
