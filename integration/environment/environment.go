package environment

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"code.cloudfoundry.org/cf-operator/pkg/bosh/manifest"
	"code.cloudfoundry.org/cf-operator/pkg/kube/client/clientset/versioned"
	"code.cloudfoundry.org/cf-operator/pkg/kube/operator"
	"code.cloudfoundry.org/cf-operator/pkg/kube/util/config"
	"code.cloudfoundry.org/cf-operator/pkg/kube/util/ctxlog"
	helper "code.cloudfoundry.org/cf-operator/pkg/testhelper"
	"code.cloudfoundry.org/cf-operator/testing"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" //from https://github.com/kubernetes/client-go/issues/345
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// StopFunc is used to clean up the environment
type StopFunc func()

// Environment starts our operator and handles interaction with the k8s
// cluster used in the tests
type Environment struct {
	Machine
	testing.Catalog
	mgr        manager.Manager
	kubeConfig *rest.Config
	stop       chan struct{}

	Log          *zap.SugaredLogger
	Config       *config.Config
	ObservedLogs *observer.ObservedLogs
	Namespace    string
}

// NewEnvironment returns a new struct
func NewEnvironment() *Environment {
	return &Environment{
		Namespace: "",
		Config: &config.Config{
			CtxTimeOut: 10 * time.Second,
			Fs:         afero.NewOsFs(),
		},
		Machine: Machine{
			pollTimeout:  300 * time.Second,
			pollInterval: 500 * time.Millisecond,
		},
	}
}

// Setup prepares the test environment by loading config and finally starting the operator
func (e *Environment) Setup() (StopFunc, error) {
	err := e.setupCFOperator()
	if err != nil {
		return nil, err
	}

	err = e.startClients(e.kubeConfig)
	if err != nil {
		return nil, err
	}

	e.stop = e.startOperator()
	err = helper.WaitForPort(
		"127.0.0.1",
		strconv.Itoa(int(e.Config.WebhookServerPort)),
		1*time.Minute)

	if err != nil {
		return nil, err
	}

	return func() {
		if e.stop != nil {
			close(e.stop)
		}
	}, nil
}

// FlushLog flushes the zap log
func (e *Environment) FlushLog() error {
	return e.Log.Sync()
}

// AllLogMessages returns only the message part of existing logs to aid in debugging
func (e *Environment) AllLogMessages() (msgs []string) {
	for _, m := range e.ObservedLogs.All() {
		msgs = append(msgs, m.Message)
	}

	return
}

func (e *Environment) setupCFOperator() (err error) {
	whh, found := os.LookupEnv("CF_OPERATOR_WEBHOOK_SERVICE_HOST")
	if !found {
		return fmt.Errorf("no webhook host set. Please set CF_OPERATOR_WEBHOOK_SERVICE_HOST to the host/ip the operator runs on and try again")
	}
	e.Config.WebhookServerHost = whh

	whp := int32(2999)
	portString, found := os.LookupEnv("CF_OPERATOR_WEBHOOK_SERVICE_PORT")
	if found {
		port, err := strconv.ParseInt(portString, 10, 32)
		if err != nil {
			return err
		}
		whp = int32(port)
	}
	e.Config.WebhookServerPort = whp

	ns, found := os.LookupEnv("TEST_NAMESPACE")
	if !found {
		ns = "default"
	}

	e.Namespace = ns
	e.Config.Namespace = ns

	e.ObservedLogs, e.Log = helper.NewTestLogger()

	err = e.setupKube()
	if err != nil {
		return
	}

	operatorDockerImageOrg, found := os.LookupEnv("DOCKER_IMAGE_ORG")
	if !found {
		operatorDockerImageOrg = "cfcontainerization"
	}

	operatorDockerImageRepo, found := os.LookupEnv("DOCKER_IMAGE_REPOSITORY")
	if !found {
		operatorDockerImageRepo = "cf-operator"
	}

	operatorDockerImageTag, found := os.LookupEnv("DOCKER_IMAGE_TAG")
	if !found {
		return fmt.Errorf("Required environment variable DOCKER_IMAGE_TAG not set")
	}

	manifest.DockerImageOrganization = operatorDockerImageOrg
	manifest.DockerImageRepository = operatorDockerImageRepo
	manifest.DockerImageTag = operatorDockerImageTag

	ctx := ctxlog.NewParentContext(e.Log)
	e.mgr, err = operator.NewManager(ctx, e.Config, e.kubeConfig, manager.Options{Namespace: e.Namespace})

	return
}

func (e *Environment) setupKube() (err error) {
	location := os.Getenv("KUBECONFIG")
	if location == "" {
		location = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	e.kubeConfig, err = clientcmd.BuildConfigFromFlags("", location)
	if err != nil {
		log.Printf("INFO: cannot use kube config: %s\n", err)
		e.kubeConfig, err = rest.InClusterConfig()
		if err != nil {
			return
		}
	}

	return
}

func (e *Environment) startClients(kubeConfig *rest.Config) (err error) {
	e.Clientset, err = kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return
	}
	e.VersionedClientset, err = versioned.NewForConfig(kubeConfig)
	return
}

func (e *Environment) startOperator() chan struct{} {
	stop := make(chan struct{})
	go e.mgr.Start(stop)
	return stop
}
