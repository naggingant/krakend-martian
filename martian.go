package martian

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/devopsfaith/krakend/config"
	"github.com/devopsfaith/krakend/logging"
	"github.com/devopsfaith/krakend/proxy"
	"github.com/devopsfaith/krakend/transport/http/client"

	// import the required martian packages so they can be used
	"github.com/google/martian"
	_ "github.com/google/martian/body"
	_ "github.com/google/martian/cookie"
	_ "github.com/google/martian/fifo"
	_ "github.com/google/martian/header"
	_ "github.com/google/martian/martianurl"
	"github.com/google/martian/parse"
	_ "github.com/google/martian/port"
	_ "github.com/google/martian/priority"
	_ "github.com/google/martian/stash"
	_ "github.com/google/martian/static"
	_ "github.com/google/martian/status"
)

// NewBackendFactory creates a proxy.BackendFactory with the martian request executor wrapping the injected one.
// If there is any problem parsing the extra config data, it just uses the injected request executor.
func NewBackendFactory(logger logging.Logger, re client.HTTPRequestExecutor) proxy.BackendFactory {
	return NewConfiguredBackendFactory(logger, func(_ *config.Backend) client.HTTPRequestExecutor { return re })
}

// NewConfiguredBackendFactory creates a proxy.BackendFactory with the martian request executor wrapping the injected one.
// If there is any problem parsing the extra config data, it just uses the injected request executor.
func NewConfiguredBackendFactory(logger logging.Logger, ref func(*config.Backend) client.HTTPRequestExecutor) proxy.BackendFactory {
	return func(remote *config.Backend) proxy.Proxy {
		re := ref(remote)
		result, ok := ConfigGetter(remote.ExtraConfig).(Result)
		if !ok {
			return proxy.NewHTTPProxyWithHTTPExecutor(remote, re, remote.Decoder)
		}
		switch result.Err {
		case nil:
			return proxy.NewHTTPProxyWithHTTPExecutor(remote, HTTPRequestExecutor(result.Result, re), remote.Decoder)
		case ErrEmptyValue:
			return proxy.NewHTTPProxyWithHTTPExecutor(remote, re, remote.Decoder)
		default:
			logger.Error(result, remote.ExtraConfig)
			return proxy.NewHTTPProxyWithHTTPExecutor(remote, re, remote.Decoder)
		}
	}
}

// HTTPRequestExecutor creates a wrapper over the received request executor, so the martian modifiers can be
// executed before and after the execution of the request
func HTTPRequestExecutor(result *parse.Result, re client.HTTPRequestExecutor) client.HTTPRequestExecutor {
	return func(ctx context.Context, req *http.Request) (resp *http.Response, err error) {
		if err = modifyRequest(result.RequestModifier(), req); err != nil {
			return
		}
		resp, err = re(ctx, req)
		if err != nil {
			return
		}
		if resp == nil {
			err = ErrEmptyResponse
			return
		}
		if err = modifyResponse(result.ResponseModifier(), resp); err != nil {
			return
		}
		return
	}
}

func modifyRequest(mod martian.RequestModifier, req *http.Request) error {
	if req.Body == nil {
		req.Body = ioutil.NopCloser(bytes.NewBufferString(""))
	}
	if req.Header == nil {
		req.Header = http.Header{}
	}

	if mod == nil {
		return nil
	}
	return mod.ModifyRequest(req)
}

func modifyResponse(mod martian.ResponseModifier, resp *http.Response) error {
	if resp.Body == nil {
		resp.Body = ioutil.NopCloser(bytes.NewBufferString(""))
	}
	if resp.Header == nil {
		resp.Header = http.Header{}
	}

	if mod == nil {
		return nil
	}
	return mod.ModifyResponse(resp)
}

// Namespace is the key to look for extra configuration details
const Namespace = "github.com/devopsfaith/krakend-martian"

// Result is a simple wrapper over the parse.FromJSON response tuple
type Result struct {
	Result *parse.Result
	Err    error
}

// ConfigGetter implements the config.ConfigGetter interface. It parses the extra config for the
// martian adapter and returns a Result wrapping the results.
func ConfigGetter(e config.ExtraConfig) interface{} {
	cfg, ok := e[Namespace]
	if !ok {
		return Result{nil, ErrEmptyValue}
	}

	data, ok := cfg.(map[string]interface{})
	if !ok {
		return Result{nil, ErrBadValue}
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return Result{nil, ErrMarshallingValue}
	}

	r, err := parse.FromJSON(raw)

	return Result{r, err}
}

var (
	// ErrEmptyValue is the error returned when there is no config under the namespace
	ErrEmptyValue = fmt.Errorf("getting the extra config for the martian module")
	// ErrBadValue is the error returned when the config is not a map
	ErrBadValue = fmt.Errorf("casting the extra config for the martian module")
	// ErrMarshallingValue is the error returned when the config map can not be marshalled again
	ErrMarshallingValue = fmt.Errorf("marshalling the extra config for the martian module")
	// ErrEmptyResponse is the error returned when the modifier receives a nil response
	ErrEmptyResponse = fmt.Errorf("getting the http response from the request executor")
)
