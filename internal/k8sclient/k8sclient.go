package k8sclient

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"sync/atomic"
)

type k8sClient struct {
	clientset  *kubernetes.Clientset
	tagToCheck *atomic.Value
}

func NewK8sClient(tagToCheck *atomic.Value) (*k8sClient, error) {
	var clientset *kubernetes.Clientset
	var err error
	cfg, err := rest.InClusterConfig()
	if err == nil {
		clientset, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("error building kubernetes clientset: %v", err)
		}
	} else {
		return nil, fmt.Errorf("not in cluster")
	}
	return &k8sClient{
		clientset:  clientset,
		tagToCheck: tagToCheck,
	}, err
}

func (k *k8sClient) Run(ctx context.Context) {
	log.Infof("run image checker")

	selector := fields.ParseSelectorOrDie("metadata.namemespace==deckhouse,status.phase==" + string(v1.PodRunning))
	watchlist := cache.NewListWatchFromClient(
		k.clientset.CoreV1().RESTClient(),
		string(v1.ResourcePods),
		"d8-system",
		selector, //fields.Everything(),
	)

	_, controller := cache.NewInformer(
		watchlist,
		&v1.Pod{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod := obj.(*v1.Pod)
				log.Infof("Pod added %s %s", pod.Namespace, pod.Name)
			},
			DeleteFunc: func(obj interface{}) {
				pod := obj.(*v1.Pod)
				log.Infof("Pod deleted %s %s", pod.Namespace, pod.Name)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				pod := newObj.(*v1.Pod)
				log.Infof("Pod updated %s %s", pod.Namespace, pod.Name)
			},
		},
	)
	controller.Run(ctx.Done())
}
