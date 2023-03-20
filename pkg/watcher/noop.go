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

import "fmt"

// These functions are part of
// cache.Store(https://pkg.go.dev/k8s.io/client-go/tools/cache#Store) but not
// required by the
// cache.reflector(https://pkg.go.dev/k8s.io/client-go/tools/cache#Reflector)

func (w *watcherStore) List() []interface{} {
	panic("unexpected: watcher.List called")
}

func (w *watcherStore) ListKeys() []string {
	panic("unexpected: watcher.ListKeys called")
}

func (w *watcherStore) Get(obj interface{}) (item interface{}, exists bool,
	err error) {
	panic(fmt.Errorf("unexpected: watcher.Get(%v) called", obj))
}

func (w *watcherStore) GetByKey(key string) (item interface{}, exists bool,
	err error) {
	panic(fmt.Errorf("unexpected: watcher.GetByKey(%v) called", key))
}

func (w *watcherStore) Resync() error {
	return nil
}
