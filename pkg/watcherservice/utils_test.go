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

package watcherservice_test

import (
	"fmt"

	"github.com/nephio-project/watcher-agent/pkg/watcherservice"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Utils", func() {
	Describe("GetAnnotationVal", func() {
		var (
			annotations map[string]string
			key, val    string
		)
		BeforeEach(func() {
			key = "key1"
			val = "val1"
			annotations = map[string]string{key: val}
		})
		Context("when key exists in annotations", func() {
			It("should return the correct value", func() {
				receivedVal, err := watcherservice.GetAnnotationVal(key, annotations)
				Expect(err).To(BeNil())
				Expect(receivedVal).To(Equal(val))
			})
		})
		Context("when key doesn't exists in annotations", func() {
			It("should return error", func() {
				_, err := watcherservice.GetAnnotationVal("fakeKey", annotations)
				Expect(err).To(MatchError(fmt.Errorf("annotation not found. Key: fakeKey")))
			})
		})
	})
})
