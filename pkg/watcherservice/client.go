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
	"context"

	"github.com/go-logr/logr"
	pb "github.com/nephio-project/edge-watcher/protos"
	"github.com/nephio-project/watcher-agent/pkg/watcher"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/util/retry"
)

type Client struct {
	Cancel context.CancelFunc
	logger logr.Logger

	Addr, Port string
	pb.WatcherServiceClient
}

func (c *Client) GetServerParams() (addr, port string) {
	return c.Addr, c.Port
}

// SendEvent takes the event params from watcherStore and sends a
// pb.EventRequest to edgewatcher server
func (c *Client) SendEvent(ctx context.Context, params watcher.EventParameters) (
	pb.ResponseType, error) {
	logger := c.logger.WithName("SendEvent")
	debugLogger := logger.V(1)

	debugLogger.Info("generating event request")
	req, err := generateEventRequest(logger, params)
	if err != nil {
		logger.Error(err, "error generating event request")
		return pb.ResponseType_RESET, err
	}
	var resp *pb.EventResponse

	err = retry.OnError(retry.DefaultRetry, func(err error) bool {
		if status.Code(err) == codes.Unavailable {
			return true
		}
		return false
	}, func() error {
		debugLogger.Info("trying to send the EventRequest to edgewatcher")
		resp, err = c.ReportEvent(ctx, req)
		return err
	})
	if err != nil {
		logger.Error(err, "error received from edgewatcher")
		return pb.ResponseType_RESET, err
	}
	return *resp.Response, nil
}

func NewClient(ctx context.Context, logger logr.Logger, addr,
	port string, opts ...grpc.DialOption) (*Client, error) {
	currCtx, cancel := context.WithCancel(ctx)

	conn, err := grpcDial(currCtx, addr, port, opts...)
	if err != nil {
		cancel()
		return nil, err
	}

	// close the connection when the context is cancelled
	go func() {
		<-currCtx.Done()
		conn.Close()
	}()

	c := &Client{
		Cancel: cancel,
		logger: logger.WithName("client").WithValues("addr", addr, "port", port),

		Addr:                 addr,
		Port:                 port,
		WatcherServiceClient: pb.NewWatcherServiceClient(conn),
	}
	return c, nil
}

var _ watcher.GRPCClient = &Client{}
