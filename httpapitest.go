// Copyright (c) 2023, Janoš Guljaš <janos@resenje.org>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package httpapitest helps HTTP response testing.

To test specific endpoint, Request function should be called with options that
should validate response from the server:

	httpapitest.Request(t, http.MethodGet, "/", http.StatusOk, httpapitest.WithRequestHeader("Content-Type", "application/json"))
	// ...

The HTTP request will be executed using the supplied client, and response
checked in expected status code is returned, as well as with each configured
option function.
*/
package httpapitest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"testing"
)

// Request is a testing helper function that makes an HTTP request using
// provided client with provided method and url. It performs a validation on
// expected response code and additional options. It returns response headers if
// the request and all validation are successful. In case of any error, testing
// Errorf or Fatal functions will be called.
func Request(t testing.TB, client *http.Client, method, url string, opts ...Option) {
	t.Helper()

	o := new(options)
	for _, opt := range opts {
		if err := opt.apply(o); err != nil {
			t.Fatal(err)
		}
	}

	req, err := http.NewRequest(method, url, o.requestBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header = o.requestHeaders
	if o.ctx != nil {
		req = req.WithContext(o.ctx)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if o.responseCode != 0 {
		if resp.StatusCode != o.responseCode {
			t.Errorf("got response status %s, want %v %s", resp.Status, o.responseCode, http.StatusText(o.responseCode))
		}
	}

	for key := range o.responseHeaders {
		want := o.responseHeaders.Get(key)
		got := resp.Header.Get(key)
		if got != want {
			t.Errorf("got header %q value %q, want %q", key, got, want)
		}
	}

	if o.expectedResponse != nil {
		readerContentEqual(t, resp.Body, o.expectedResponse)
		return
	}

	if o.expectedJSONResponse != nil {
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		got = bytes.TrimSpace(got)

		want, err := json.Marshal(o.expectedJSONResponse)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("got json response %q, want %q", string(got), string(want))
		}
		return
	}

	if o.unmarshalResponse != nil {
		if err := json.NewDecoder(resp.Body).Decode(&o.unmarshalResponse); err != nil {
			t.Fatal(err)
		}
		return
	}

	if o.responseBody != nil {
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		*o.responseBody = got
		return
	}

	if o.noResponseBody {
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) > 0 {
			t.Errorf("got response body %q, want none", string(got))
		}
	}
}

// WithContext sets a context to the request made by the Request function.
func WithContext(ctx context.Context) Option {
	return optionFunc(func(o *options) error {
		o.ctx = ctx
		return nil
	})
}

// WithRequestBody writes a request body to the request made by the Request
// function.
func WithRequestBody(body io.Reader) Option {
	return optionFunc(func(o *options) error {
		o.requestBody = body
		return nil
	})
}

// WithJSONRequestBody writes a request JSON-encoded body to the request made by
// the Request function.
func WithJSONRequestBody(r interface{}) Option {
	return optionFunc(func(o *options) error {
		b, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("json encode request body: %w", err)
		}
		o.requestBody = bytes.NewReader(b)
		return nil
	})
}

// WithMultipartRequest writes a multipart request with a single file in it to
// the request made by the Request function.
func WithMultipartRequest(body io.Reader, length int, filename, contentType string) Option {
	return optionFunc(func(o *options) error {
		buf := bytes.NewBuffer(nil)
		mw := multipart.NewWriter(buf)
		hdr := make(textproto.MIMEHeader)
		if filename != "" {
			hdr.Set("Content-Disposition", fmt.Sprintf("form-data; name=%q", filename))
		}
		if contentType != "" {
			hdr.Set("Content-Type", contentType)
		}
		if length > 0 {
			hdr.Set("Content-Length", strconv.Itoa(length))
		}
		part, err := mw.CreatePart(hdr)
		if err != nil {
			return fmt.Errorf("create multipart part: %w", err)
		}
		if _, err = io.Copy(part, body); err != nil {
			return fmt.Errorf("copy file data to multipart part: %w", err)
		}
		if err := mw.Close(); err != nil {
			return fmt.Errorf("close multipart writer: %w", err)
		}
		o.requestBody = buf
		if o.requestHeaders == nil {
			o.requestHeaders = make(http.Header)
		}
		o.requestHeaders.Set("Content-Type", fmt.Sprintf("multipart/form-data; boundary=%q", mw.Boundary()))
		return nil
	})
}

// WithRequestHeader adds a single header to the request made by the Request
// function. To add multiple headers call multiple times this option when as
// arguments to the Request function.
func WithRequestHeader(key, value string) Option {
	return optionFunc(func(o *options) error {
		if o.requestHeaders == nil {
			o.requestHeaders = make(http.Header)
		}
		o.requestHeaders.Add(key, value)
		return nil
	})
}

// WithRequestHeaders sets headers to be sent with the request. It will override
// already set headers, so any possible WithRequestHeader options must be
// specified as later arguments in the Request function call.
func WithRequestHeaders(h http.Header) Option {
	return optionFunc(func(o *options) error {
		o.requestHeaders = h
		return nil
	})
}

// ExpectStatus validates that the response from the request has the
// specific HTTP response status code.
func ExpectStatus(code int) Option {
	return optionFunc(func(o *options) error {
		o.responseCode = code
		return nil
	})
}

// ExpectResponseHeader validates a response header value.
func ExpectResponseHeader(key, value string) Option {
	return optionFunc(func(o *options) error {
		if o.responseHeaders == nil {
			o.responseHeaders = make(http.Header)
		}
		o.responseHeaders.Add(key, value)
		return nil
	})
}

// ExpectedResponse validates that the response from the request in the
// Request function matches the date rad from the reader.
func ExpectedResponse(r io.Reader) Option {
	return optionFunc(func(o *options) error {
		o.expectedResponse = r
		return nil
	})
}

// ExpectedJSONResponse validates that the response from the request in the
// Request function matches JSON-encoded body provided here.
func ExpectedJSONResponse(response interface{}) Option {
	return optionFunc(func(o *options) error {
		o.expectedJSONResponse = response
		return nil
	})
}

// UnmarshalJSONResponse unmarshals response body from the request in the
// Request function to the provided response. Response must be a pointer.
func UnmarshalJSONResponse(response interface{}) Option {
	return optionFunc(func(o *options) error {
		o.unmarshalResponse = response
		return nil
	})
}

// PutResponseBody replaces the data in the provided byte slice with the
// data from the response body of the request in the Request function.
//
// Example:
//
//	var respBytes []byte
//	options := []httpapitest.Option{
//		httpapitest.PutResponseBody(&respBytes),
//	}
func PutResponseBody(b *[]byte) Option {
	return optionFunc(func(o *options) error {
		o.responseBody = b
		return nil
	})
}

// ExpectNoResponseBody ensures that there is no data sent by the response of the
// request in the Request function.
func ExpectNoResponseBody() Option {
	return optionFunc(func(o *options) error {
		o.noResponseBody = true
		return nil
	})
}

type options struct {
	ctx                  context.Context
	responseCode         int
	requestBody          io.Reader
	requestHeaders       http.Header
	responseHeaders      http.Header
	expectedResponse     io.Reader
	expectedJSONResponse interface{}
	unmarshalResponse    interface{}
	responseBody         *[]byte
	noResponseBody       bool
}

type Option interface {
	apply(*options) error
}
type optionFunc func(*options) error

func (f optionFunc) apply(r *options) error { return f(r) }

func readerContentEqual(t testing.TB, r1, r2 io.Reader) {
	t.Helper()

	const bufSize = 128

	buf1 := make([]byte, bufSize)
	buf2 := make([]byte, bufSize)

	var cursor int
	for {
		n1, err := r1.Read(buf1)
		buf1 = buf1[:n1]
		if err != nil && err != io.EOF {
			t.Fatalf("read input data at position %v: %v", cursor, err)
		}
		n2, err := r2.Read(buf2)
		buf2 = buf2[:n2]
		if err != nil && err != io.EOF {
			t.Fatalf("read validation data at position %v: %v", cursor, err)
		}

		if !bytes.Equal(buf1, buf2) {
			t.Errorf("data not equal at position %v: got %q, want %q", cursor, string(buf1), string(buf2))
			return
		}

		if err == io.EOF {
			break
		}

		cursor += n1
	}
}
