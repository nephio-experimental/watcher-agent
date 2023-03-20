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

package watcherservice

import (
	"fmt"

	"github.com/go-logr/logr"
	pb "github.com/nephio-project/edge-watcher/protos"
	nfdeployutils "github.com/nephio-project/nf-deploy-controller/util"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func GetAnnotationVal(key string, annotations map[string]string) (
	string, error) {
	name, ok := annotations[key]
	if !ok {
		return "", fmt.Errorf("annotation not found. Key: %v", key)
	}
	return name, nil
}

func generateEventRequest(logger logr.Logger, params watcher.EventParameters) (
	*pb.EventRequest, error) {

	nfDeployName, err := GetAnnotationVal(nfdeployutils.NFDeployLabel, params.Object.GetLabels())
	if err != nil {
		logger.Error(err, "object doesnt have nfdeploy annotation")
	}

	requestMetadata, err := generateRequestMetadata(params.WatchRequest)
	if err != nil {
		logger.Error(err, "error constructing request metadata")
		return nil, err
	}

	objectJSON, err := params.Object.MarshalJSON()
	if err != nil {
		logger.Error(err, "error in serializing unstructured.Unstructured",
			"object", params.Object)
		return nil, err
	}

	md := &pb.Metadata{
		Request:      requestMetadata,
		ClusterName:  &params.ClusterName,
		NfdeployName: &nfDeployName,
		Type:         &params.Type,
	}

	return &pb.EventRequest{
		Metadata:       md,
		EventTimestamp: timestamppb.New(params.TimeStamp),
		Object:         objectJSON,
	}, nil
}
