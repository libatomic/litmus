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

		// Request is the http request body put on the wire
		// []byte or string will be posted directly
		// everything else will be marshalled to json
		Request interface{}

		// RequestContentType is the request content type, default application/json
		RequestContentType string

		// ExpectedStatus is the expected http status
		ExpectedStatus int

		// ExpectedResponse is expected wire response
		// []byte or string will be posted directly
		// everything else will be marshalled to json
		ExpectedResponse interface{}

		// ExpectedContentType is the expected content-type
		ExpectedContentType string
	}
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
		backend.On(t.Operation, t.OperationArgs...).Return(t.OperationReturns...)
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

	resp, err := client.Do(req)
	if err != nil {
		tt.Fatalf("failed to execute request: %s", err.Error())
	}

	assert.Equal(tt, t.ExpectedStatus, resp.StatusCode)

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		if err != io.EOF {
			require.NoError(t, err)
		}
	}

	var expectedResp string

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

	if len(data) > 0 {
		assert.JSONEq(tt, expectedResp, string(data))
	}
}
