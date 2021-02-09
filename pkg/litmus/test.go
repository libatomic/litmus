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
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tj/assert"
)

type (
	// Mock is the litmus mock wrapper
	Mock struct {
		mock.Mock

		t *Test
	}

	// Operation is a backend operation
	Operation struct {
		// Name is the operation name
		Name string

		// Args is the operation args
		Args []interface{}

		// Returns in the operation returns
		Returns []interface{}

		// ReturnStack handles a return stack for multiple calls
		ReturnStack [][]interface{}

		// Optional backend for this operation
		Backend *mock.Mock

		call *mock.Call
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

		// Setup is call before the request is executed
		Setup func(r *http.Request)
	}

	// RequestHandler can be used to generate a request body dynamically
	RequestHandler func(backend interface{}, t *Test) (io.Reader, error)

	// Values embeds a url values
	Values struct {
		q url.Values
	}

	// Args are test args
	Args []interface{}

	// Returns are test returns
	Returns []interface{}

	// ReturnStack handles a return stack
	ReturnStack [][]interface{}
)

var (
	// Context is a mocked context.Context
	Context = mock.AnythingOfType("*context.valueCtx")

	// OperationArg is a convenience for referencing an arg
	OperationArg = func(a int, o ...int) *OperationRef {
		if len(o) > 0 {
			return &OperationRef{Index: o[0], Arg: a}
		}
		return &OperationRef{Index: 0, Arg: a}
	}

	// OperationReturn is a convenience for referencing a return
	OperationReturn = func(r int, o ...int) *OperationRef {
		if len(o) > 0 {
			return &OperationRef{Index: o[0], Return: r}
		}
		return &OperationRef{Index: 0, Return: r}
	}

	// NoRedirect forces no redirects
	NoRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
)

// Do executes the test
func (t *Test) Do(backend *Mock, handler http.Handler, tt *testing.T) {
	defer func() {
		backend.AssertExpectations(tt)
	}()

	backend.t = t

	for i, o := range t.Operations {
		args := make([]interface{}, 0)
		for _, a := range o.Args {
			if any, ok := a.(mock.AnythingOfTypeArgument); ok {
				args = append(args, any)
			} else {
				args = append(args, mock.AnythingOfType(reflect.TypeOf(a).String()))
			}
		}
		returns := o.Returns
		if returns == nil && len(o.ReturnStack) > 0 {
			returns = o.ReturnStack[len(o.ReturnStack)-1]
		}
		if o.Backend != nil {
			o.call = o.Backend.On(o.Name, args...).Return(returns...)
		} else {
			o.call = backend.On(o.Name, args...).Return(returns...)
		}

		t.Operations[i] = o
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
	case RequestHandler:
		b, err := m(backend, t)
		if err != nil {
			tt.Fatalf("failed to build request body: %s", err.Error())
		}
		body = b
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

	if t.RequestContentType == "" {
		t.RequestContentType = "application/json"
	}

	req.Header.Set("Content-Type", t.RequestContentType)

	if t.Setup != nil {
		t.Setup(req)
	}
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

// Called tells the mock object that a method has been called, and gets an array
// of arguments to return.  Panics if the call is unexpected (i.e. not preceded by
// appropriate .On .Return() calls)
// If Call.WaitFor is set, blocks until the channel is closed or receives a message.
func (m *Mock) Called(arguments ...interface{}) mock.Arguments {
	// get the calling function's name
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		panic("Couldn't get the caller information")
	}
	functionPath := runtime.FuncForPC(pc).Name()
	//Next four lines are required to use GCCGO function naming conventions.
	//For Ex:  github_com_docker_libkv_store_mock.WatchTree.pN39_github_com_docker_libkv_store_mock.Mock
	//uses interface information unlike golang github.com/docker/libkv/store/mock.(*Mock).WatchTree
	//With GCCGO we need to remove interface information starting from pN<dd>.
	re := regexp.MustCompile("\\.pN\\d+_")
	if re.MatchString(functionPath) {
		functionPath = re.Split(functionPath, -1)[0]
	}
	parts := strings.Split(functionPath, ".")
	functionName := parts[len(parts)-1]
	return m.MethodCalled(functionName, arguments...)
}

// MethodCalled wraps mock.MethodCalled to handle return stacks
func (m *Mock) MethodCalled(methodName string, arguments ...interface{}) mock.Arguments {
	for i, op := range m.t.Operations {
		if op.Name == methodName {
			if len(op.ReturnStack) > 0 {
				n := len(op.ReturnStack) - 1
				op.call.ReturnArguments = mock.Arguments(op.ReturnStack[0])
				op.ReturnStack = op.ReturnStack[n:]

				m.t.Operations[i] = op
			}
			break
		}
	}

	return m.Mock.MethodCalled(methodName, arguments...)
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

// Encode encodes the result
func (v Values) Encode() string {
	return v.q.Encode()
}
