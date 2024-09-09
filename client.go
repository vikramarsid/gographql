// Package gographql provides a low level GraphQL client.
//
//	// create a client (safe to share across requests)
//	client := gographql.NewClient("https://varsid.io/graphql")
//
//	// make a request
//	req := gographql.NewRequest(`
//	    query ($key: String!) {
//	        items (id:$key) {
//	            field1
//	            field2
//	            field3
//	        }
//	    }
//	`)
//
//	// set any variables
//	req.Var("key", "value")
//
//	// run it and capture the response
//	var respData ResponseStruct
//	if err := client.Run(ctx, req, &respData); err != nil {
//	    log.Fatal(err)
//	}
//
// # Specify client
//
// To specify your own http.Client, use the WithHTTPClient option:
//
//	httpclient := &http.Client{}
//	client := gographql.NewClient("https://varsid.io/graphql", gographql.WithHTTPClient(httpclient))
package gographql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// ErrSendFilesPostField cannot send files with PostFields option.
var ErrSendFilesPostField = errors.New("cannot send files with PostFields option")

// ErrGraphqlServerError graphql server returned a non-200 status code.
var ErrGraphqlServerError = errors.New("graphql server returned a non-200 status code")

// ErrEncodingRequestBody encoding request body error.
var ErrEncodingRequestBody = errors.New("encoding request body error")

// ErrDecodingResponse decoding response error.
var ErrDecodingResponse = errors.New("decoding response error")

// HTTPClient custom HTTP client interface.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is a client for interacting with a GraphQL API.
type Client struct {
	// Endpoint GraphQL Server URL.
	Endpoint string
	// DebugLog enables ddebug logging.
	DebugLog bool

	// closeReq will close the request body immediately allowing for reuse of client.
	closeReq         bool
	httpClient       HTTPClient
	useMultipartForm bool
	log              Logger
}

// NewClient makes a new Client capable of making GraphQL requests.
func NewClient(endpoint string, opts ...ClientOption) *Client {
	c := &Client{
		Endpoint: endpoint,
	}
	for _, optionFunc := range opts {
		optionFunc(c)
	}
	if c.httpClient == nil {
		c.httpClient = http.DefaultClient
	}
	return c
}

// Run executes the query and unmarshals the response from the data field
// into the response object.
// Pass in a nil response object to skip response parsing.
// If the request fails or the server returns an error, the first error
// will be returned.
func (c *Client) Run(ctx context.Context, req *Request, resp interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if len(req.files) > 0 && !c.useMultipartForm {
		return ErrSendFilesPostField
	}
	if c.useMultipartForm {
		return c.runWithPostFields(ctx, req, resp)
	}
	return c.runWithJSON(ctx, req, resp)
}

func (c *Client) runWithJSON(ctx context.Context, req *Request, resp interface{}) error {
	var requestBody bytes.Buffer
	requestBodyObj := struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}{
		Query:     req.q,
		Variables: req.vars,
	}
	if err := json.NewEncoder(&requestBody).Encode(requestBodyObj); err != nil {
		return errors.Join(ErrEncodingRequestBody, err)
	}
	if c.DebugLog {
		c.log.Debugf("variables: %+v", req.vars)
		c.log.Debugf("query: %s", req.q)
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, &requestBody)
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	r.Header.Set("Accept", "application/json; charset=utf-8")
	for key, values := range req.Header {
		for _, value := range values {
			r.Header.Add(key, value)
		}
	}
	return c.doHTTP(ctx, r, resp)
}

func (c *Client) runWithPostFields(ctx context.Context, req *Request, resp interface{}) error {
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	if err := writer.WriteField("query", req.q); err != nil {
		return fmt.Errorf("write query field error: %w", err)
	}
	var variablesBuf bytes.Buffer
	if len(req.vars) > 0 {
		variablesField, err := writer.CreateFormField("variables")
		if err != nil {
			return fmt.Errorf("create variables field error: %w", err)
		}
		if err := json.NewEncoder(io.MultiWriter(variablesField, &variablesBuf)).Encode(req.vars); err != nil {
			return fmt.Errorf("encode variables error: %w", err)
		}
	}
	for i := range req.files {
		part, err := writer.CreateFormFile(req.files[i].Field, req.files[i].Name)
		if err != nil {
			return fmt.Errorf("create form file error: %w", err)
		}
		if _, err := io.Copy(part, req.files[i].R); err != nil {
			return fmt.Errorf("preparing file error: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close writer error: %w", err)
	}
	if c.DebugLog {
		c.log.Debugf("variables: %s", variablesBuf.String())
		c.log.Debugf("num of files: %d", len(req.files))
		c.log.Debugf("query: %s", req.q)
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, &requestBody)
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Accept", "application/json; charset=utf-8")
	for key, values := range req.Header {
		for _, value := range values {
			r.Header.Add(key, value)
		}
	}
	return c.doHTTP(ctx, r, resp)
}

func (c *Client) doHTTP(ctx context.Context, r *http.Request, resp interface{}) error {
	gr := &GraphQLResponse{
		Data: resp,
	}
	r.Close = c.closeReq
	if c.DebugLog {
		c.log.Debugf("headers: %+v", r.Header)
	}
	r = r.WithContext(ctx)
	res, err := c.httpClient.Do(r)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, res.Body); err != nil {
		return errors.Join(ErrDecodingResponse, err)
	}
	if c.DebugLog {
		c.log.Debugf("response body: %s", buf.String())
	}
	if err := json.NewDecoder(&buf).Decode(&gr); err != nil {
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("%w; statuscode: %v", ErrGraphqlServerError, res.StatusCode)
		}
		return errors.Join(ErrDecodingResponse, err)
	}
	if len(gr.Errors) > 0 {
		return gr.Errors
	}
	return nil
}

// DisableDebugLog disable debug level log (disabled by default).
func (c *Client) DisableDebugLog() *Client {
	c.DebugLog = false
	return c
}

// EnableDebugLog enable debug level log (disabled by default).
func (c *Client) EnableDebugLog() *Client {
	c.DebugLog = true
	return c
}

// GetLogger return the internal logger, usually used in middleware.
func (c *Client) GetLogger() Logger {
	if c.log != nil {
		return c.log
	}
	c.log = createDefaultLogger()
	return c.log
}

// SetLogger set the customized logger for client, will disable log if set to nil.
func (c *Client) SetLogger(log Logger) *Client {
	if log == nil {
		c.log = &disableLogger{}
		return c
	}
	c.log = log
	return c
}

// ClientOption are functions that are passed into NewClient to
// modify the behaviour of the Client.
type ClientOption func(*Client)

// WithHTTPClient specifies the underlying http.Client to use when
// making requests.
//
//	NewClient(endpoint, WithHTTPClient(specificHTTPClient))
func WithHTTPClient(httpclient HTTPClient) ClientOption {
	return func(client *Client) {
		client.httpClient = httpclient
	}
}

// UseMultipartForm uses multipart/form-data and activates support for
// files.
func UseMultipartForm() ClientOption {
	return func(client *Client) {
		client.useMultipartForm = true
	}
}

// ImmediatelyCloseReqBody will close the req body immediately after each request body is ready.
func ImmediatelyCloseReqBody() ClientOption {
	return func(client *Client) {
		client.closeReq = true
	}
}

// GraphQLErrors reepresents errors rom graphql server.
type GraphQLErrors []GraphQLError

func (e GraphQLErrors) Error() string {
	if len(e) == 0 {
		return "graphql: no errors"
	}
	messages := make([]string, 0)
	for _, err := range e {
		messages = append(messages, err.Message)
	}
	return "graphql: " + strings.Join(messages, "; ")
}

// GraphQLError represents a GraphQL error.
type GraphQLError struct {
	// Message contains the error message.
	Message string `json:"message"`
	// Locations contains the locations in the GraphQL document that caused the
	// error if the error can be associated to a particular point in the
	// requested GraphQL document.
	Locations []Location `json:"locations"`
	// Path contains the key path of the response field which experienced the
	// error. This allows clients to identify whether a nil result is
	// intentional or caused by a runtime error.
	Path []interface{} `json:"path"`
	// Extensions may contain additional fields set by the GraphQL service,
	// such as	an error code.
	Extensions map[string]interface{} `json:"extensions"`
}

// A Location is a location in the GraphQL query that resulted in an error.
// The location may be returned as part of an error response.
type Location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func (e GraphQLError) Error() string {
	return "graphql: " + e.Message
}

// GraphQLResponse represents a GraphQL response.
type GraphQLResponse struct {
	Data   interface{}   `json:"data"`
	Errors GraphQLErrors `json:"errors,omitempty"`
}
