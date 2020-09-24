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
	// Operation is a backend operation
	Operation struct {
		// Name is the operation name
		Name string

		// Args is the operation args
		Args []interface{}

		// Returns in the operation returns
		Returns []interface{}
	}

	// OperationRef is used to reference on operation
	OperationRef struct {
		// Index refers the operation slice index
		Index int

		// Arg refers to the Args index
		Arg int

		// Return refers to the Returns index
		Return int
	}

	// Test is a test requirements object
	Test struct {
		// Operations are the backend operations to prepare for test
		Operations []Operation

		// Method the http method
		Method string

		// The request path
		Path string

		// Query is the request query parameters
		Query url.Values

		// Request is the http request body put on the wire
		// []byte or string will be posted directly
		// if Request is *OperationRef that value will be used
		// everything else will be marshalled to json
		Request interface{}

		// RequestContentType is the request content type, default application/json
		RequestContentType string

		// ExpectedStatus is the expected http status
		ExpectedStatus int

		// ExpectedHeaders are expected response headers
		ExpectedHeaders map[string]string

		// ExpectedContentType is the expected content-type
		ExpectedContentType string

		// ExpectedResponse is expected wire response
		// []byte or string will be posted directly
		// if Request is *OperationRef that value will be used
		// everything else will be marshalled to json
		ExpectedResponse interface{}

		// Redirect overrides the http client redirect
		Redirect func(req *http.Request, via []*http.Request)
	}

	// Values embeds a url values
	Values struct {
		q url.Values
	}

	// Args are test args
	Args []interface{}

	// Returns are test returns
	Returns []interface{}
)

var (
	// Context is a mocked context.Context
	Context = mock.AnythingOfType("*context.valueCtx")

	// OperationArg is a convenience for referencing an arg
	OperationArg = func(o, a int) *OperationRef { return &OperationRef{Index: o, Arg: a} }

	// OperationReturn is a convenience for referencing a return
	OperationReturn = func(o, a int) *OperationRef { return &OperationRef{Index: o, Return: a} }

	// NoRedirect forces no redirects
	NoRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
)

// Do executes the test
func (t *Test) Do(backend *mock.Mock, handler http.Handler, tt *testing.T) {
	defer func() {
		backend.AssertExpectations(tt)
	}()

	for _, o := range t.Operations {
		args := make([]interface{}, 0)
		for _, a := range o.Args {
			if any, ok := a.(mock.AnythingOfTypeArgument); ok {
				args = append(args, any)
			} else {
				args = append(args, mock.AnythingOfType(reflect.TypeOf(a).String()))
			}
		}
		backend.On(o.Name, args...).Return(o.Returns...)

	}

	ts := httptest.NewTLSServer(handler)
	defer ts.Close()

	client := ts.Client()

	if t.Redirect == nil {
		client.CheckRedirect = NoRedirect
	}

	var body io.Reader

	switch m := t.Request.(type) {
	case []byte:
		body = bytes.NewReader(m)
	case string:
		body = strings.NewReader(m)
	case nil:
		// do nothing
	case *OperationRef:
		data, err := json.Marshal(t.Operations[m.Index].Args[m.Arg])
		if err != nil {
			tt.Fatalf("failed to marshal request: %s", err.Error())
		}
		body = bytes.NewReader(data)
	default:
		data, err := json.Marshal(m)
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

	for k, v := range t.ExpectedHeaders {
		assert.Regexp(tt, v, resp.Header.Get(k))
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		if err != io.EOF {
			require.NoError(tt, err)
		}
	}

	var expectedResp string

	switch m := t.ExpectedResponse.(type) {
	case []byte:
		expectedResp = string(m)
	case string:
		expectedResp = m
	case nil:
		return
	case *OperationRef:
		data, err := json.Marshal(t.Operations[m.Index].Returns[m.Return])
		if err != nil {
			tt.Fatalf("failed to marshal response: %s", err.Error())
		}
		expectedResp = string(data)
	default:
		data, err := json.Marshal(m)
		if err != nil {
			tt.Fatalf("failed to marshal response: %s", err.Error())
		}
		expectedResp = string(data)
	}

	if len(data) > 0 {
		assert.JSONEq(tt, expectedResp, string(data))
	}
}

// BeginQuery returns an intialized values
func BeginQuery() Values {
	return Values{make(url.Values)}
}

// Add adds a value
func (v Values) Add(key, value string) Values {
	v.q.Add(key, value)
	return v
}

// EndQuery returns the query values
func (v Values) EndQuery() url.Values {
	return v.q
}
