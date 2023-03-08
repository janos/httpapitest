// Copyright (c) 2023, Janoš Guljaš <janos@resenje.org>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpapitest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"resenje.org/httpapitest"
)

func TestRequest_method(t *testing.T) {

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectStatus(http.StatusOK),
		)
	})

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodPost, endpoint,
			httpapitest.ExpectStatus(http.StatusMethodNotAllowed),
		)
	})
}
func TestRequest_url(t *testing.T) {

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectStatus(http.StatusOK),
		)
	})

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodPost, endpoint+"/test",
			httpapitest.ExpectStatus(http.StatusNotFound),
		)
	})
}

func TestExpectStatus(t *testing.T) {

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectStatus(http.StatusBadRequest),
		)
	})

	assert(t, "got response status 400 Bad Request, want 200 OK", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectStatus(http.StatusOK),
		)
	})
}

func TestExpectResponseHeader(t *testing.T) {

	headerName := "Test-Header"
	headerValue := "somevalue"

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerName, headerValue)
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectResponseHeader(headerName, headerValue),
		)
	})
}

func TestWithContext(t *testing.T) {

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel the context to detect the fatal error

	assert(t, "", fmt.Sprintf("Get %q: context canceled", endpoint), func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.WithContext(ctx),
		)
	})
}

func TestWithRequestBody(t *testing.T) {

	wantBody := []byte("body")
	var gotBody []byte
	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodPost, endpoint,
			httpapitest.WithRequestBody(bytes.NewReader(wantBody)),
		)
	})
	if !bytes.Equal(gotBody, wantBody) {
		t.Errorf("got body %q, want %q", string(gotBody), string(wantBody))
	}
}

func TestWithJSONRequestBody(t *testing.T) {

	type response struct {
		Message string `json:"message"`
	}
	message := "text"

	wantBody := response{
		Message: message,
	}

	var gotBody response
	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, err := io.ReadAll(r.Body)
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, err)
			return
		}
		if err := json.Unmarshal(v, &gotBody); err != nil {
			respondJSON(w, http.StatusBadRequest, err)
			return
		}
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodPost, endpoint,
			httpapitest.WithJSONRequestBody(wantBody),
		)
	})
	if gotBody.Message != message {
		t.Errorf("got message %q, want %q", gotBody.Message, message)
	}
}

func TestWithMultipartRequest(t *testing.T) {

	wantBody := []byte("somebody")
	filename := "Test.jpg"
	contentType := "image/jpeg"
	var gotBody []byte
	var gotContentDisposition, gotContentType string

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			respondJSON(w, http.StatusBadRequest, err)
			return
		}
		if strings.HasPrefix(mediaType, "multipart/") {
			mr := multipart.NewReader(r.Body, params["boundary"])

			p, err := mr.NextPart()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				respondJSON(w, http.StatusBadRequest, err)
				return
			}
			gotContentDisposition = p.Header.Get("Content-Disposition")
			gotContentType = p.Header.Get("Content-Type")
			gotBody, err = io.ReadAll(p)
			if err != nil {
				respondJSON(w, http.StatusBadRequest, err)
				return
			}
		}
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodPost, endpoint,
			httpapitest.WithMultipartRequest(bytes.NewReader(wantBody), len(wantBody), filename, contentType),
		)
	})
	if !bytes.Equal(gotBody, wantBody) {
		t.Errorf("got body %q, want %q", string(gotBody), string(wantBody))
	}
	if gotContentType != contentType {
		t.Errorf("got content type %q, want %q", gotContentType, contentType)
	}
	if contentDisposition := fmt.Sprintf("form-data; name=%q", filename); gotContentDisposition != contentDisposition {
		t.Errorf("got content disposition %q, want %q", gotContentDisposition, contentDisposition)
	}
}

func TestWithRequestHeader(t *testing.T) {

	headerName := "Test-Header"
	headerValue := "value"
	var gotValue string

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotValue = r.Header.Get(headerName)
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodPost, endpoint,
			httpapitest.WithRequestHeader(headerName, headerValue),
		)
	})
	if gotValue != headerValue {
		t.Errorf("got header %q, want %q", gotValue, headerValue)
	}
}

func TestWithRequestHeaders(t *testing.T) {

	headerName := "Test-Header"
	headerValue := "value"
	headers := make(http.Header)
	headers.Set("Test-2", "two")
	headers.Set("Test-3", "three")
	var gotHeaders http.Header

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodPost, endpoint,
			httpapitest.WithRequestHeaders(headers),
			httpapitest.WithRequestHeader(headerName, headerValue),
		)
	})

	wantHeaders := make(http.Header)
	for k := range headers {
		wantHeaders.Set(k, headers.Get(k))
	}
	wantHeaders.Set(headerName, headerValue)
	wantHeaders.Set("Accept-Encoding", "gzip")
	wantHeaders.Set("Content-Length", "0")

	gotHeaders.Del("User-Agent") // do not check for useragent string with version

	if !reflect.DeepEqual(gotHeaders, wantHeaders) {
		t.Errorf("got header %v, want %v", gotHeaders, wantHeaders)
	}
}

func TestExpectedResponse(t *testing.T) {

	body := []byte(strings.Repeat("something to want ", 10))

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write(body)
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, err)
		}
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectedResponse(bytes.NewReader(body)),
		)
	})

	assert(t, fmt.Sprintf(`data not equal at position 0: got %q, want "invalid"`, body[:128]), "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectedResponse(strings.NewReader("invalid")),
		)
	})
}

func TestExpectedJSONResponse(t *testing.T) {

	type response struct {
		Message string `json:"message"`
	}

	want := response{
		Message: "text",
	}

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, want)
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectedJSONResponse(want),
		)
	})

	assert(t, `got json response "{\"message\":\"text\"}", want "{\"message\":\"invalid\"}"`, "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectedJSONResponse(response{
				Message: "invalid",
			}),
		)
	})
}

func TestUnmarshalJSONResponse(t *testing.T) {

	message := "text"

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, message)
	}))

	var r jsonStatusResponse
	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.UnmarshalJSONResponse(&r),
		)
	})
	if r.Message != message {
		t.Errorf("got message %q, want %q", r.Message, message)
	}
}

func TestPutResponseBody(t *testing.T) {

	wantBody := []byte("somebody")

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write(wantBody)
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, err)
		}
	}))

	var gotBody []byte
	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.PutResponseBody(&gotBody),
		)
	})
	if !bytes.Equal(gotBody, wantBody) {
		t.Errorf("got body %q, want %q", string(gotBody), string(wantBody))
	}
}

func TestExpectNoResponseBody(t *testing.T) {

	c, endpoint := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			fmt.Fprint(w, "not found")
		}
	}))

	assert(t, "", "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint,
			httpapitest.ExpectNoResponseBody(),
		)
	})

	assert(t, `got response body "not found", want none`, "", func(m *mock) {
		httpapitest.Request(m, c, http.MethodGet, endpoint+"/test",
			httpapitest.ExpectNoResponseBody(),
		)
	})
}

func newClient(t *testing.T, handler http.Handler) (c *http.Client, endpoint string) {
	t.Helper()

	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return s.Client(), s.URL
}

// assert is a test helper that validates a functionality of another helper
// function by mocking Errorf, Fatal and Helper methods on testing.TB.
func assert(t *testing.T, wantError, wantFatal string, f func(m *mock)) {
	t.Helper()

	defer func() {
		if v := recover(); v != nil {
			if err, ok := v.(error); ok && errors.Is(err, errFailed) {
				return // execution of the goroutine is stopped by a mock Fatal function
			}
			t.Fatalf("panic: %v", v)
		}
	}()

	m := &mock{
		wantError: wantError,
		wantFatal: wantFatal,
	}

	f(m)

	if !m.isHelper { // Request function is tested and it must be always a helper
		t.Error("not a helper function")
	}

	if m.gotError != m.wantError {
		t.Errorf("got error %q, want %q", m.gotError, m.wantError)
	}

	if m.gotFatal != m.wantFatal {
		t.Errorf("got error %v, want %v", m.gotFatal, m.wantFatal)
	}
}

// mock provides the same interface as testing.TB with overridden Errorf, Fatal
// and Helper methods.
type mock struct {
	testing.TB
	isHelper  bool
	gotError  string
	wantError string
	gotFatal  string
	wantFatal string
}

func (m *mock) Helper() {
	m.isHelper = true
}

func (m *mock) Errorf(format string, args ...interface{}) {
	m.gotError = fmt.Sprintf(format, args...)
}

func (m *mock) Fatal(args ...interface{}) {
	m.gotFatal = fmt.Sprint(args...)
	panic(errFailed) // terminate the goroutine to detect it in the assert function
}

var errFailed = errors.New("failed")

type jsonStatusResponse struct {
	Message string `json:"message,omitempty"`
	Code    int    `json:"code,omitempty"`
}

func respondJSON(w http.ResponseWriter, statusCode int, response interface{}) {
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if response == nil {
		response = &jsonStatusResponse{
			Message: http.StatusText(statusCode),
			Code:    statusCode,
		}
	} else {
		switch message := response.(type) {
		case string:
			response = &jsonStatusResponse{
				Message: message,
				Code:    statusCode,
			}
		case error:
			response = &jsonStatusResponse{
				Message: message.Error(),
				Code:    statusCode,
			}
		case fmt.Stringer:
			response = &jsonStatusResponse{
				Message: message.String(),
				Code:    statusCode,
			}
		}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(response); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	fmt.Fprintln(w, b.String())
}
