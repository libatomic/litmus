/*
 * Copyright (C) 2020 Atomic Media Foundation
 *
 * This software may be modified and distributed under the terms
 * of the MIT license.  See the LICENSE file in the root of this
 * workspace for details.
 */

package litmus

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tj/assert"
)

type (
	// Test is a test requirements object
	Test struct {
		// Operation is the backend api method name
		Operation string

		// OperationArgs are the expected method arguments
		OperationArgs []interface{}

		// OperationReturns are the return vars for the api
		OperationReturns []interface{}

		// Method the http method
		Method string

		// The request path
		Path string

		// Query is the request query parameters
		Query url.Values

		// Request is the http request body put on the wire
		// []byte or string will be posted directly
		// everything else will be marshalled to json
		Request interface{}

		// RequestIndex will use one of the operation parameters as the request
		RequestIndex *int

		// RequestContentType is the request content type, default application/json
		RequestContentType string

		// ExpectedStatus is the expected http status
		ExpectedStatus int

		// ExpectedContentType is the expected content-type
		ExpectedContentType string

		// ExpectedResponse is expected wire response
		// []byte or string will be posted directly
		// everything else will be marshalled to json
		ExpectedResponse interface{}

		// ExpectedResponseIndex uses the operation argument as the expected response
		ExpectedResponseIndex *int
	}

	// Args are test args
	Args []interface{}

	// Returns are test returns
	Returns []interface{}
)

var (
	// Context is a mocked context.Context
	Context = mock.AnythingOfType("*context.valueCtx")
)

// Do executes the test
func (t *Test) Do(backend *mock.Mock, handler http.Handler, tt *testing.T) {
	defer func() {
		backend.AssertExpectations(tt)
	}()

	if t.Operation != "" {
		args := make([]interface{}, 0)
		for _, a := range t.OperationArgs {
			if any, ok := a.(mock.AnythingOfTypeArgument); ok {
				args = append(args, any)
			} else {
				args = append(args, mock.AnythingOfType(reflect.TypeOf(a).String()))
			}
		}
		backend.On(t.Operation, args...).Return(t.OperationReturns...)
	}

	ts := httptest.NewTLSServer(handler)
	defer ts.Close()

	client := ts.Client()

	var body io.Reader

	switch t := t.Request.(type) {
	case []byte:
		body = bytes.NewReader(t)
	case string:
		body = strings.NewReader(t)
	case nil:
		// do nothing
	default:
		data, err := json.Marshal(t)
		if err != nil {
			tt.Fatalf("failed to marshal request: %s", err.Error())
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(t.Method, ts.URL+t.Path, body)
	if err != nil {
		tt.Fatalf("failed to create request: %s", err.Error())
	}
	req.URL.RawQuery = t.Query.Encode()

	resp, err := client.Do(req)
	if err != nil {
		tt.Fatalf("failed to execute request: %s", err.Error())
	}

	assert.Equal(tt, t.ExpectedStatus, resp.StatusCode)

	if t.ExpectedContentType != "" {
		assert.Equal(tt, t.ExpectedContentType, resp.Header.Get("Content-Type"))
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		if err != io.EOF {
			require.NoError(tt, err)
		}
	}

	var expectedResp string

	if t.ExpectedResponseIndex != nil {
		t.ExpectedResponse = t.OperationReturns[*t.ExpectedResponseIndex]
	}

	switch t := t.ExpectedResponse.(type) {
	case []byte:
		expectedResp = string(t)
	case string:
		expectedResp = t
	case nil:
		return
	default:
		data, err := json.Marshal(t)
		if err != nil {
			tt.Fatalf("failed to marshal response: %s", err.Error())
		}
		expectedResp = string(data)
	}

	if expectedResp == "" {
		return
	}

	if len(data) > 0 {
		assert.JSONEq(tt, expectedResp, string(data))
	}
}
