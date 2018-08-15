package agent

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/golang/glog"
	core_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

const (
	MinThreadiness     = 1
	MaxThreadiness     = 10
	DefaultThreadiness = 1
)

type Agent struct {
	certDir     string
	threadiness int
	kube        *kubernetes.Clientset
	queue       workqueue.RateLimitingInterface
	store       cache.Store
	controller  cache.Controller
}

type InitOptions struct {
	CertDir    string
	KubeConfig string
	Thread     int
}

func (a *Agent) Initialize(opts InitOptions) {
	var err error
	// validate certificate path
	if a.certDir, err = validateCertificateDir(opts.CertDir); err != nil {
		glog.Fatalf("bad certificate directory %s: %v", opts.CertDir, err)
	}
	// validate threadiness
	if a.threadiness, err = validateThreadiness(opts.Thread); err != nil {
		glog.Fatalf("invalid threadiness %d: %v", opts.Thread, err)
	}

	// initialize Kubernetes client
	if a.kube, err = initializeKubeClient(opts.KubeConfig); err != nil {
		glog.Fatalf("initialize kubeclient using %s: %v", opts.KubeConfig, err)
	}

	// initialize Secret workqueue
	a.queue = workqueue.NewRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(
		100*time.Millisecond,
		5*time.Second,
	))

	// initialize Secret informer
	a.store, a.controller = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options meta_v1.ListOptions) (runtime.Object, error) {
				return a.kube.CoreV1().Secrets(core_v1.NamespaceAll).List(options)
			},
			WatchFunc: func(options meta_v1.ListOptions) (watch.Interface, error) {
				return a.kube.CoreV1().Secrets(core_v1.NamespaceAll).Watch(options)
			},
		},
		&core_v1.Secret{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    a.secretsEventHandlerAdd,
			UpdateFunc: a.secretsEventHandlerUpdate,
			DeleteFunc: a.secretsEventHandlerDelete,
		},
	)
}

// formalize certificate dir if it's a valid directory, otherwise return error
func validateCertificateDir(dir string) (string, error) {
	absPath, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}

	if !fileInfo.IsDir() {
		return "", errors.New("not a valid directory")
	}

	return absPath, nil
}

// make sure threadiness is in valid range
func validateThreadiness(t int) (int, error) {
	// if not specified, return default value
	if t == 0 {
		return DefaultThreadiness, nil
	}

	if t < MinThreadiness || t > MaxThreadiness {
		return 0, errors.New("threadiness out of range")
	}

	return t, nil
}

// build Kubernetes Clientset from kubeconfig, or fallback to in-cluster initialization
// if kubeconfigPath is empty
func initializeKubeClient(kubeconfigPath string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}
