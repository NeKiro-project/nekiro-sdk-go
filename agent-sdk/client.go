// Package agentsdk provides a thin, runtime-neutral SDK for managed Agents to
// make nested invocations through the NeKiro A2A Router. It validates inherited
// platform context and sends exactly one request to the Agent Router v1
// boundary. It contains no model, tool, workflow, memory, retry, cache, or
// Agent Runtime behavior.
package agentsdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Nene7ko/NeKiro/contracts"
)

// PlatformContext carries the trusted inherited Invocation identity presented
// by the managed transport. All fields are required safe identifiers; no value
// is inferred or synthesized.
type PlatformContext struct {
	InvocationID string
	RootTaskID   string
	TraceID      string
	WorkspaceID  string
	AgentID      string
}

// Validate checks that all PlatformContext fields are present and safe
// identifiers. It fails without synthesizing identity or correlation.
func (pc PlatformContext) Validate() error {
	fields := []struct {
		name  string
		value string
	}{
		{"invocationId", pc.InvocationID},
		{"rootTaskId", pc.RootTaskID},
		{"traceId", pc.TraceID},
		{"workspaceId", pc.WorkspaceID},
		{"agentId", pc.AgentID},
	}
	for _, field := range fields {
		if field.value == "" {
			return fmt.Errorf("agentsdk: platform context %s is required", field.name)
		}
		if !safeIdentifier(field.value) {
			return fmt.Errorf("agentsdk: platform context %s is invalid", field.name)
		}
	}
	if _, err := contracts.ParseTraceID(pc.TraceID); err != nil {
		return fmt.Errorf("agentsdk: platform context traceId is invalid: %w", err)
	}
	return nil
}

// NestedRequest carries the untrusted target work for a nested invocation.
// It contains only the fields permitted by the Agent Router v1 contract.
type NestedRequest struct {
	TargetAgentID string
	Capability    string
	Input         json.RawMessage
	Stream        bool
}

// Validate checks that the NestedRequest fields are present and safe.
func (nr NestedRequest) Validate() error {
	if nr.TargetAgentID == "" {
		return errors.New("agentsdk: targetAgentId is required")
	}
	if !safeIdentifier(nr.TargetAgentID) {
		return errors.New("agentsdk: targetAgentId is invalid")
	}
	if nr.Capability == "" {
		return errors.New("agentsdk: capability is required")
	}
	if !safeIdentifier(nr.Capability) {
		return errors.New("agentsdk: capability is invalid")
	}
	if nr.Input == nil {
		return errors.New("agentsdk: input is required")
	}
	var inputObj map[string]json.RawMessage
	if err := json.Unmarshal(nr.Input, &inputObj); err != nil || inputObj == nil {
		return errors.New("agentsdk: input must be a JSON object")
	}
	return nil
}

// NestedResult carries the response from a successful non-streaming nested
// invocation.
type NestedResult struct {
	InvocationID string
	RootTaskID   string
	TraceID      string
	Status       string
	Result       json.RawMessage
}

// HTTPDoer is the minimal HTTP transport interface. The SDK performs exactly
// one request and does not follow redirects or retry.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client is a thin SDK client for nested Router calls. Both limits are
// explicit deployment policy; no response or event-size default is supplied.
type Client struct {
	doer          HTTPDoer
	routerURL     string
	token         string
	responseLimit int64
	eventLimit    int64
	runtime       *contracts.RuntimeContractValidator
	results       *contracts.ResultContractValidator
}

// NewClient creates a nested invocation SDK client with explicit limits in
// bytes. The limits must be in the contract range 1..2147483647.
func NewClient(doer HTTPDoer, routerURL, token string, responseLimit, eventLimit int64) (*Client, error) {
	if doer == nil {
		return nil, errors.New("agentsdk: HTTP doer is required")
	}
	if routerURL == "" {
		return nil, errors.New("agentsdk: router URL is required")
	}
	if token == "" {
		return nil, errors.New("agentsdk: bearer token is required")
	}
	if responseLimit < contracts.RuntimeByteLimitMinimum || responseLimit > contracts.RuntimeByteLimitMaximum {
		return nil, errors.New("agentsdk: response limit is invalid")
	}
	if eventLimit < contracts.RuntimeByteLimitMinimum || eventLimit > contracts.RuntimeByteLimitMaximum {
		return nil, errors.New("agentsdk: event limit is invalid")
	}
	if httpClient, ok := doer.(*http.Client); ok {
		if httpClient == nil {
			return nil, errors.New("agentsdk: HTTP doer is required")
		}
		client := *httpClient
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		doer = &client
	}
	runtimeValidator, err := contracts.NewRuntimeContractValidator()
	if err != nil {
		return nil, fmt.Errorf("agentsdk: initialize runtime validator: %w", err)
	}
	resultValidator, err := contracts.NewResultContractValidator()
	if err != nil {
		return nil, fmt.Errorf("agentsdk: initialize result validator: %w", err)
	}
	return &Client{
		doer: doer, routerURL: routerURL, token: token,
		responseLimit: responseLimit, eventLimit: eventLimit,
		runtime: runtimeValidator, results: resultValidator,
	}, nil
}

// Invoke sends one non-streaming nested invocation. Streaming requests must
// use InvokeStream so the caller can consume events incrementally.
func (c *Client) Invoke(ctx context.Context, pc PlatformContext, nr NestedRequest) (*NestedResult, error) {
	if nr.Stream {
		return nil, errors.New("agentsdk: streaming requests must use InvokeStream")
	}
	resp, err := c.do(ctx, pc, nr, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, c.routerError(resp)
	}
	if mediaType(resp.Header.Get("Content-Type")) != "application/json" {
		return nil, errors.New("agentsdk: nested JSON response media is invalid")
	}
	body, err := readBounded(resp.Body, c.responseLimit)
	if err != nil {
		return nil, fmt.Errorf("agentsdk: read nested response: %w", err)
	}
	var invocationResult contracts.InvocationResult
	if err := json.Unmarshal(body, &invocationResult); err != nil {
		return nil, fmt.Errorf("agentsdk: decode nested result: %w", err)
	}
	if err := c.results.ValidateInvocationResult(invocationResult); err != nil {
		return nil, fmt.Errorf("agentsdk: validate nested result: %w", err)
	}
	if invocationResult.RootTaskID != pc.RootTaskID || string(invocationResult.TraceID) != pc.TraceID {
		return nil, errors.New("agentsdk: nested result correlation does not match platform context")
	}
	return &NestedResult{
		InvocationID: invocationResult.InvocationID,
		RootTaskID:   invocationResult.RootTaskID,
		TraceID:      string(invocationResult.TraceID),
		Status:       invocationResult.Status,
		Result:       invocationResult.Result,
	}, nil
}

// InvokeStream sends one streaming nested invocation and returns a decoder
// over the live SSE body. Call Recv until io.EOF, then Close the stream.
func (c *Client) InvokeStream(ctx context.Context, pc PlatformContext, nr NestedRequest) (*NestedResultStream, error) {
	if !nr.Stream {
		return nil, errors.New("agentsdk: InvokeStream requires NestedRequest.Stream=true")
	}
	resp, err := c.do(ctx, pc, nr, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, c.routerError(resp)
	}
	if mediaType(resp.Header.Get("Content-Type")) != "text/event-stream" {
		_ = resp.Body.Close()
		return nil, errors.New("agentsdk: nested stream response media is invalid")
	}
	return &NestedResultStream{
		body: resp.Body, reader: bufio.NewReader(resp.Body),
		eventLimit: c.eventLimit, rootTaskID: pc.RootTaskID,
		traceID: contracts.TraceID(pc.TraceID), runtime: c.runtime,
	}, nil
}

func (c *Client) do(ctx context.Context, pc PlatformContext, nr NestedRequest, stream bool) (*http.Response, error) {
	if err := pc.Validate(); err != nil {
		return nil, err
	}
	if err := nr.Validate(); err != nil {
		return nil, err
	}
	body := contracts.NestedInvocationRequestV1{
		ParentInvocationID: pc.InvocationID,
		TargetAgentID:      nr.TargetAgentID,
		Capability:         nr.Capability,
		Input:              nr.Input,
		Stream:             stream,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("agentsdk: encode nested request: %w", err)
	}
	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.routerURL+"/agent/v1/invocations", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("agentsdk: construct nested request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agentsdk: nested request failed: %w", err)
	}
	return resp, nil
}

// NestedResultStream incrementally decodes and validates Agent Router SSE
// events. The child Invocation ID is learned from the first accepted event.
type NestedResultStream struct {
	body         io.ReadCloser
	reader       *bufio.Reader
	eventLimit   int64
	rootTaskID   string
	traceID      contracts.TraceID
	runtime      *contracts.RuntimeContractValidator
	sequence     *contracts.RuntimeResultStreamSequenceValidator
	invocationID string
	terminal     bool
	finished     bool
	closed       bool
	bodyClosed   bool
}

// InvocationID returns the child ID after the accepted event has been read.
func (stream *NestedResultStream) InvocationID() string { return stream.invocationID }

// Recv reads and validates the next SSE event. io.EOF is returned only after
// a valid terminal event has been consumed and the stream has ended.
func (stream *NestedResultStream) Recv() (contracts.InvocationResultStreamEventV2, error) {
	if stream.closed {
		return contracts.InvocationResultStreamEventV2{}, errors.New("agentsdk: result stream is closed")
	}
	if stream.finished {
		return contracts.InvocationResultStreamEventV2{}, io.EOF
	}
	frame, err := readSSEFrame(stream.reader, stream.eventLimit)
	if errors.Is(err, io.EOF) {
		if stream.sequence == nil {
			return contracts.InvocationResultStreamEventV2{}, errors.New("agentsdk: result stream ended before an accepted event")
		}
		if err := stream.sequence.Finish(); err != nil {
			return contracts.InvocationResultStreamEventV2{}, fmt.Errorf("agentsdk: finish result stream: %w", err)
		}
		stream.finished = true
		stream.closeBody()
		return contracts.InvocationResultStreamEventV2{}, io.EOF
	}
	if err != nil {
		return contracts.InvocationResultStreamEventV2{}, fmt.Errorf("agentsdk: read SSE frame: %w", err)
	}
	var event contracts.InvocationResultStreamEventV2
	if err := rejectDuplicateJSONMembers(frame); err != nil {
		return contracts.InvocationResultStreamEventV2{}, fmt.Errorf("agentsdk: decode SSE event: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return contracts.InvocationResultStreamEventV2{}, fmt.Errorf("agentsdk: decode SSE event: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return contracts.InvocationResultStreamEventV2{}, fmt.Errorf("agentsdk: decode SSE event: %w", err)
	}
	if stream.sequence == nil {
		if event.Type != contracts.ResultStreamEventAccepted {
			return contracts.InvocationResultStreamEventV2{}, errors.New("agentsdk: result stream must begin with accepted")
		}
		sequence, err := contracts.NewRuntimeResultStreamSequenceValidator(stream.runtime, event.InvocationID, stream.rootTaskID, stream.traceID)
		if err != nil {
			return contracts.InvocationResultStreamEventV2{}, fmt.Errorf("agentsdk: initialize result stream validator: %w", err)
		}
		stream.sequence = sequence
		stream.invocationID = event.InvocationID
	}
	if err := stream.sequence.Accept(event); err != nil {
		return contracts.InvocationResultStreamEventV2{}, fmt.Errorf("agentsdk: validate SSE event: %w", err)
	}
	stream.terminal = stream.sequence.IsTerminal()
	return event, nil
}

// Close releases the response body. A stream that has not reached a valid
// terminal event is reported as interrupted rather than silently accepted.
func (stream *NestedResultStream) Close() error {
	if stream.closed {
		return nil
	}
	stream.closed = true
	var closeErr error
	if !stream.bodyClosed {
		closeErr = stream.body.Close()
		stream.bodyClosed = true
	}
	if !stream.finished && stream.sequence != nil {
		if err := stream.sequence.Finish(); err != nil {
			return errors.Join(closeErr, fmt.Errorf("agentsdk: close interrupted result stream: %w", err))
		}
	}
	if !stream.finished {
		return errors.Join(closeErr, errors.New("agentsdk: close interrupted result stream"))
	}
	return closeErr
}

func (stream *NestedResultStream) closeBody() {
	if !stream.bodyClosed {
		_ = stream.body.Close()
		stream.bodyClosed = true
	}
}

// RouterError contains only validated safe error fields from the Agent Router.
// Raw response bytes are deliberately not retained or exposed.
type RouterError struct {
	StatusCode   int
	Code         contracts.PlatformErrorCode
	TraceID      contracts.TraceID
	InvocationID string
	RootTaskID   string
}

func (e *RouterError) Error() string {
	return fmt.Sprintf("agentsdk: router returned status %d (%s)", e.StatusCode, e.Code)
}

func (c *Client) routerError(resp *http.Response) error {
	if mediaType(resp.Header.Get("Content-Type")) != "application/json" {
		return errors.New("agentsdk: router error media is invalid")
	}
	body, err := readBounded(resp.Body, c.responseLimit)
	if err != nil {
		return fmt.Errorf("agentsdk: read router error: %w", err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil || members == nil {
		return errors.New("agentsdk: router error body is invalid")
	}
	_, hasInvocation := members["invocationId"]
	_, hasRoot := members["rootTaskId"]
	if hasInvocation != hasRoot {
		return errors.New("agentsdk: router error correlation is incomplete")
	}
	if err := rejectDuplicateJSONMembers(body); err != nil {
		return errors.New("agentsdk: router error body is invalid")
	}
	headerTrace, err := contracts.ParseTraceID(resp.Header.Get("x-nek-trace-id"))
	if err != nil {
		return errors.New("agentsdk: router error trace header is invalid")
	}
	result := &RouterError{StatusCode: resp.StatusCode}
	if hasInvocation {
		if err := c.runtime.ValidateCorrelatedPlatformErrorV4JSON(body); err != nil {
			return errors.New("agentsdk: router correlated error body is invalid")
		}
		var value contracts.CorrelatedPlatformErrorV4
		if err := json.Unmarshal(body, &value); err != nil {
			return errors.New("agentsdk: router correlated error body is invalid")
		}
		if value.TraceID != headerTrace {
			return errors.New("agentsdk: router error trace header changed")
		}
		result.Code, result.TraceID = value.Code, value.TraceID
		result.InvocationID, result.RootTaskID = value.InvocationID, value.RootTaskID
	} else {
		if err := c.runtime.ValidatePreCorrelationPlatformErrorV4JSON(body); err != nil {
			return errors.New("agentsdk: router pre-correlation error body is invalid")
		}
		var value contracts.PreCorrelationPlatformErrorV4
		if err := json.Unmarshal(body, &value); err != nil {
			return errors.New("agentsdk: router pre-correlation error body is invalid")
		}
		if value.TraceID != headerTrace {
			return errors.New("agentsdk: router error trace header changed")
		}
		result.Code, result.TraceID = value.Code, value.TraceID
	}
	if !validRouterErrorStatus(resp.StatusCode, result.Code, hasInvocation) {
		return errors.New("agentsdk: router error status and code do not match")
	}
	return result
}

func validRouterErrorStatus(statusCode int, code contracts.PlatformErrorCode, correlated bool) bool {
	switch statusCode {
	case http.StatusBadRequest:
		return code == contracts.ErrorCodeValidationError && !correlated
	case http.StatusUnauthorized:
		return code == contracts.ErrorCodeUnauthenticated && !correlated
	case http.StatusForbidden:
		return code == contracts.ErrorCodeForbidden && !correlated
	case http.StatusNotFound:
		return code == contracts.ErrorCodeNotFound && !correlated
	case http.StatusNotAcceptable:
		return code == contracts.ErrorCodeNotAcceptable && !correlated
	case http.StatusConflict:
		return (code == contracts.ErrorCodeConflict && !correlated) || (code == contracts.ErrorCodeCanceled && correlated)
	case http.StatusRequestEntityTooLarge:
		return code == contracts.ErrorCodePayloadTooLarge && !correlated
	case http.StatusBadGateway:
		return correlated && (code == contracts.ErrorCodeAgentAuthUnsupported || code == contracts.ErrorCodeAgentResponseTooLarge || code == contracts.ErrorCodeA2AProtocol || code == contracts.ErrorCodeAgentExecutionFailed)
	case http.StatusServiceUnavailable:
		return code == contracts.ErrorCodeRouteNotFound || code == contracts.ErrorCodeAgentUnavailable || code == contracts.ErrorCodeDependency
	case http.StatusGatewayTimeout:
		return code == contracts.ErrorCodeTimeout
	default:
		return false
	}
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response exceeds the configured limit")
	}
	return data, nil
}

func readSSEFrame(reader *bufio.Reader, limit int64) ([]byte, error) {
	line, err := readSSELine(reader, limit)
	if errors.Is(err, io.EOF) {
		return nil, io.EOF
	}
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(line, []byte("data: ")) || bytes.IndexByte(line, '\r') >= 0 || len(line) < len("data: \n") || line[len(line)-1] != '\n' {
		return nil, errors.New("SSE frame must contain exactly one data line")
	}
	blank, err := readSSELine(reader, limit)
	if err != nil {
		return nil, errors.New("SSE frame must end with one blank line")
	}
	if !bytes.Equal(blank, []byte("\n")) {
		return nil, errors.New("SSE frame must end with one blank line")
	}
	payload := line[len("data: ") : len(line)-1]
	if len(payload) == 0 {
		return nil, errors.New("SSE data payload is empty")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil || !bytes.Equal(compact.Bytes(), payload) {
		return nil, errors.New("SSE data payload must be compact JSON")
	}
	if int64(len(line)+len(blank)) > limit {
		return nil, errors.New("SSE frame exceeds the configured limit")
	}
	return payload, nil
}

func readSSELine(reader *bufio.Reader, limit int64) ([]byte, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if int64(len(line)) > limit {
			return nil, errors.New("SSE frame exceeds the configured limit")
		}
		if err == nil {
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return nil, io.EOF
			}
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
}

func mediaType(value string) string {
	return value
}

func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, isDelimiter := token.(json.Delim)
		if !isDelimiter {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("JSON object member name is invalid")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON object member %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return errors.New("unexpected JSON delimiter")
		}
	}
	if err := walk(); err != nil {
		return err
	}
	return requireEOF(decoder)
}

// safeIdentifier validates a string matching the platform identifier grammar.
func safeIdentifier(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for index, character := range []byte(value) {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == ':' || character == '-' {
			if index > 0 || character != '.' && character != '_' && character != ':' && character != '-' {
				continue
			}
		}
		return false
	}
	return true
}
