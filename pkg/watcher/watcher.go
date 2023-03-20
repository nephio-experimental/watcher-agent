/*
Copyright 2022-2023 The Nephio Authors.

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

package watcher

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	pb "github.com/nephio-project/edge-watcher/protos"
	"github.com/nephio-project/watcher-agent/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"
)

// Watcher watches the k8s resources using
// cache.Reflector(https://pkg.go.dev/k8s.io/client-go/tools/cache#Reflector).
// It creates a watcherStore (which implements
// cache.Store[https://pkg.go.dev/k8s.io/client-go/tools/cache#Store]), which is
// passed on to cache.Reflector. watcherStore then sends the events provided by
// cache.Reflector to the GRPCClient.
// It also sends the current resourceVersion to the watcheragent controller. It
// first puts the new resourceVersion in the ResourceVersionCh (replacing the
// old one if present), then it invokes the controller's Reconcile func with the
// correct ctrl.Request(https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile#Request)
// using the Params.InvokeWatcherAgentController func.
type Watcher struct {
	ctx    context.Context
	Cancel context.CancelFunc
	logger logr.Logger

	grpcClientMu sync.RWMutex
	grpcClient   GRPCClient

	*Params

	// watchBackoffManager handles backoff for cache.Reflector restarts that
	// happen because of GRPC errors
	watchBackoffManager wait.BackoffManager

	// resyncSignalCh carries the resync signal from sendEvent (in case of an
	// error in sending the event to edgewatcher or receiving an error in response)
	// or from watcheragent controller ( in case of a resync request from
	// edgewatcher)
	resyncSignalCh chan string

	// restartCount is the number of time the cache.reflector is recreated in
	// Watcher.Run. It is meant to be used by tests only.
	restartCountMu sync.RWMutex
	restartCount   int

	// ResourceVersionCh carries the most recent resourceVersion from
	// cache.Reflector for a one time read by watcheragent controller
	ResourceVersionCh chan string
}

// EventParameters is used to send the event related info to GRPCClient
type EventParameters struct {
	v1alpha1.WatchRequest
	Type        pb.EventType
	ClusterName string
	Object      *unstructured.Unstructured
	TimeStamp   time.Time
}

// GRPCClient sends events to the edgewatcher server
type GRPCClient interface {
	SendEvent(ctx context.Context, params EventParameters) (pb.ResponseType, error)
	GetServerParams() (addr, port string)
}

// Params is used to initialize Watcher
type Params struct {
	v1alpha1.WatchRequest

	// InvokeWatcherAgentController is provided by watcheragent controller to
	// invoke the controller's Reconcile(https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile#Func.Reconcile)
	// func with the correct request (https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile#Request).
	// This is needed for notifying the controller to update the resourceVersion
	// of a watch in the v1alpha1.WatcherAgentStatus.
	InvokeWatcherAgentController func()

	Clock             clock.Clock
	ResourceInterface dynamic.ResourceInterface
	ClusterName       string
}

// New returns a new Watcher
func New(ctx context.Context, logger logr.Logger, params *Params, grpcClient GRPCClient) *Watcher {
	currCtx, cancel := context.WithCancel(ctx)

	return &Watcher{
		ctx:    currCtx,
		Cancel: cancel,
		logger: logger.WithName("watcher"),

		// we are using the backOff parameters from container crash-loop backoff
		// since a watch restart is similar to a controller container restart
		watchBackoffManager: wait.NewExponentialBackoffManager(10*time.Second,
			5*time.Minute, 10*time.Minute, 2.0, 1.0,
			params.Clock),

		Params:     params,
		grpcClient: grpcClient,

		resyncSignalCh:    make(chan string, 1),
		ResourceVersionCh: make(chan string, 1),
	}
}

func (w *Watcher) SetGRPCClient(client GRPCClient) {
	w.grpcClientMu.Lock()
	defer w.grpcClientMu.Unlock()

	w.grpcClient = client
}

func (w *Watcher) GetGRPCClient() GRPCClient {
	w.grpcClientMu.RLock()
	defer w.grpcClientMu.RUnlock()

	return w.grpcClient
}

func (w *Watcher) ReSync(ctx context.Context, source string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.ctx.Done():
		return w.ctx.Err()
	case w.resyncSignalCh <- source:
	default:
	}
	return nil
}

func (w *Watcher) GetRestartCount() int {
	w.restartCountMu.RLock()
	defer w.restartCountMu.RUnlock()
	return w.restartCount
}

func (w *Watcher) updateRestartCount() {
	w.restartCountMu.Lock()
	defer w.restartCountMu.Unlock()
	w.restartCount += 1
}

// Run creates a new cache.Reflector and watcherStore and sets them to watch
// the given resources. It recreates the reflector whenever an error is received
// since reflector doesn't provide method to forcefully re-list.
func (w *Watcher) Run() {
	logger := w.logger.WithName("Run").WithValues("watchReq", w.WatchRequest)
	debugLogger := logger.V(1)

loop:
	for {
		select {
		case <-w.ctx.Done():
			debugLogger.Info("closing watcher")
			return
		default:
		}

		t := w.watchBackoffManager.Backoff()

		ctx, cancel := context.WithCancel(w.ctx)

		store := &watcherStore{
			ctx:     ctx,
			logger:  w.logger,
			watcher: w,
		}

		lw := &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return w.ResourceInterface.List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return w.ResourceInterface.Watch(ctx, options)
			},
		}

		reflector := cache.NewReflector(lw, nil, store, 0)
		go reflector.Run(ctx.Done())
		debugLogger.Info("started reflector")

		select {
		case <-w.ctx.Done():
			if !t.Stop() {
				<-t.C()
			}
			cancel()
			continue loop
		case <-t.C():
		}

		select {
		case source := <-w.resyncSignalCh:
			debugLogger.Info("restarting reflector", "source", source)
		case <-w.ctx.Done():
		}

		w.updateRestartCount()
		cancel()
		debugLogger.Info("updated restart count")
	}
}
