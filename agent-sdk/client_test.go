package agentsdk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nene7ko/NeKiro/contracts"
)

func validContext() PlatformContext {
	return PlatformContext{
		InvocationID: "inv_parent123",
		RootTaskID:   "task_root456",
		TraceID:      "trc_abc123_1",
		WorkspaceID:  "ws_test789",
		AgentID:      "agent_caller01",
	}
}

func validNestedRequest() NestedRequest {
	return NestedRequest{
		TargetAgentID: "agent_target02",
		Capability:    "summarize",
		Input:         json.RawMessage(`{"text":"hello"}`),
		Stream:        false,
	}
}

func TestPlatformContextValidate(t *testing.T) {
	tests := []struct {
		name    string
		context PlatformContext
		wantErr bool
	}{
		{"valid", validContext(), false},
		{"missing invocationId", PlatformContext{RootTaskID: "task_root456", TraceID: "trc_abc123_1", WorkspaceID: "ws_test789", AgentID: "agent_caller01"}, true},
		{"missing rootTaskId", PlatformContext{InvocationID: "inv_parent123", TraceID: "trc_abc123_1", WorkspaceID: "ws_test789", AgentID: "agent_caller01"}, true},
		{"missing traceId", PlatformContext{InvocationID: "inv_parent123", RootTaskID: "task_root456", WorkspaceID: "ws_test789", AgentID: "agent_caller01"}, true},
		{"missing workspaceId", PlatformContext{InvocationID: "inv_parent123", RootTaskID: "task_root456", TraceID: "trc_abc123_1", AgentID: "agent_caller01"}, true},
		{"missing agentId", PlatformContext{InvocationID: "inv_parent123", RootTaskID: "task_root456", TraceID: "trc_abc123_1", WorkspaceID: "ws_test789"}, true},
		{"invalid identifier", PlatformContext{InvocationID: "inv parent", RootTaskID: "task_root456", TraceID: "trc_abc123_1", WorkspaceID: "ws_test789", AgentID: "agent_caller01"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.context.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("PlatformContext.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNestedRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request NestedRequest
		wantErr bool
	}{
		{"valid", validNestedRequest(), false},
		{"missing targetAgentId", NestedRequest{Capability: "summarize", Input: json.RawMessage(`{}`)}, true},
		{"missing capability", NestedRequest{TargetAgentID: "agent_target02", Input: json.RawMessage(`{}`)}, true},
		{"missing input", NestedRequest{TargetAgentID: "agent_target02", Capability: "summarize"}, true},
		{"input not object", NestedRequest{TargetAgentID: "agent_target02", Capability: "summarize", Input: json.RawMessage(`"string"`)}, true},
		{"input null", NestedRequest{TargetAgentID: "agent_target02", Capability: "summarize", Input: json.RawMessage(`null`)}, true},
		{"invalid targetAgentId", NestedRequest{TargetAgentID: "agent target", Capability: "summarize", Input: json.RawMessage(`{}`)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("NestedRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewClientValidation(t *testing.T) {
	tests := []struct {
		name      string
		doer      HTTPDoer
		routerURL string
		token     string
		wantErr   bool
	}{
		{"valid", http.DefaultClient, "https://router.example.dev", "token123", false},
		{"nil doer", nil, "https://router.example.dev", "token123", true},
		{"empty url", http.DefaultClient, "", "token123", true},
		{"empty token", http.DefaultClient, "https://router.example.dev", "", true},
		{"typed nil http.Client", (*http.Client)(nil), "https://router.example.dev", "token123", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.doer, tt.routerURL, tt.token, 4096, 4096)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewClientRequiresExplicitContractLimits(t *testing.T) {
	for _, limits := range [][2]int64{{0, 4096}, {4096, 0}, {contracts.RuntimeByteLimitMaximum + 1, 4096}, {4096, contracts.RuntimeByteLimitMaximum + 1}} {
		if _, err := NewClient(http.DefaultClient, "https://router.example.dev", "token", limits[0], limits[1]); err == nil {
			t.Fatalf("NewClient accepted invalid limits %v", limits)
		}
	}
}

func TestClientInvokeSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/v1/invocations" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content type: %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected accept: %s", r.Header.Get("Accept"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body["parentInvocationId"] != "inv_parent123" {
			t.Errorf("unexpected parentInvocationId: %v", body["parentInvocationId"])
		}
		if body["targetAgentId"] != "agent_target02" {
			t.Errorf("unexpected targetAgentId: %v", body["targetAgentId"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schemaVersion": "1",
			"invocationId":  "inv_child999",
			"rootTaskId":    "task_root456",
			"traceId":       "trc_abc123_1",
			"status":        "succeeded",
			"result":        map[string]any{"answer": "42"},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL, "test-token", 4096, 4096)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := client.Invoke(context.Background(), validContext(), validNestedRequest())
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.InvocationID != "inv_child999" {
		t.Errorf("unexpected invocationId: %s", result.InvocationID)
	}
	if result.Status != "succeeded" {
		t.Errorf("unexpected status: %s", result.Status)
	}
}

func TestClientInvokeInvalidContext(t *testing.T) {
	client, err := NewClient(http.DefaultClient, "https://router.example.dev", "token", 4096, 4096)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.Invoke(context.Background(), PlatformContext{}, validNestedRequest())
	if err == nil {
		t.Error("Invoke() should fail with invalid context")
	}
}

func TestClientInvokeRouterError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-nek-trace-id", "trc_test123_1")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    "UNAUTHENTICATED",
			"message": "Authentication is required.",
			"traceId": "trc_test123_1",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL, "bad-token", 4096, 4096)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.Invoke(context.Background(), validContext(), validNestedRequest())
	if err == nil {
		t.Error("Invoke() should fail with router error")
	}
	var routerErr *RouterError
	if !errors.As(err, &routerErr) {
		t.Errorf("expected RouterError, got %T", err)
	}
	if routerErr != nil {
		if routerErr.StatusCode != http.StatusUnauthorized || routerErr.Code != contracts.ErrorCodeUnauthenticated || routerErr.TraceID != "trc_test123_1" {
			t.Errorf("unexpected router error: %#v", routerErr)
		}
	}
}

func TestClientInvokeRouterErrorDoesNotExposeRawDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-nek-trace-id", "trc_abc123_1")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"code":"AGENT_EXECUTION_FAILED","message":"The Agent failed to complete the invocation.","traceId":"trc_abc123_1","invocationId":"inv_child999","rootTaskId":"task_root456","detail":"secret"}`)
	}))
	defer server.Close()
	client, err := NewClient(server.Client(), server.URL, "test-token", 4096, 4096)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Invoke(context.Background(), validContext(), validNestedRequest())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("raw error detail escaped: %v", err)
	}
	var routerErr *RouterError
	if errors.As(err, &routerErr) {
		t.Fatalf("invalid error body returned RouterError: %#v", routerErr)
	}
}

func TestClientInvokeRejectsStreamingUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("streaming request must not reach HTTP server")
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL, "test-token", 4096, 4096)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	req := validNestedRequest()
	req.Stream = true
	if _, err := client.Invoke(context.Background(), validContext(), req); err == nil {
		t.Fatal("Invoke() should reject streaming requests")
	}
	if req.Stream != true {
		t.Fatal("test request unexpectedly changed")
	}
}

func TestClientInvokeStreamIncrementalAndValidated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected text/event-stream accept, got: %s", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		events := []string{
			`data: {"schemaVersion":"2","sequence":0,"type":"accepted","status":"pending","invocationId":"inv_child999","rootTaskId":"task_root456","traceId":"trc_abc123_1"}` + "\n\n",
			`data: {"schemaVersion":"2","sequence":1,"type":"chunk","status":"running","invocationId":"inv_child999","rootTaskId":"task_root456","traceId":"trc_abc123_1","chunkIndex":0,"chunk":{"answer":"part"}}` + "\n\n",
			`data: {"schemaVersion":"2","sequence":2,"type":"completed","status":"succeeded","invocationId":"inv_child999","rootTaskId":"task_root456","traceId":"trc_abc123_1"}` + "\n\n",
		}
		for _, event := range events {
			_, _ = io.WriteString(w, event)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()
	client, err := NewClient(server.Client(), server.URL, "test-token", 4096, 4096)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	req := validNestedRequest()
	req.Stream = true
	stream, err := client.InvokeStream(context.Background(), validContext(), req)
	if err != nil {
		t.Fatalf("InvokeStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	for index := int64(0); ; index++ {
		event, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			if index != 3 {
				t.Fatalf("received %d events, want 3", index)
			}
			break
		}
		if recvErr != nil {
			t.Fatalf("Recv() error = %v", recvErr)
		}
		if event.Sequence != index {
			t.Fatalf("event sequence = %d, want %d", event.Sequence, index)
		}
	}
	if stream.InvocationID() != "inv_child999" {
		t.Fatalf("InvocationID() = %s", stream.InvocationID())
	}
}

func TestClientInvokeStreamRejectsMalformedSSE(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "wrong media", body: `{"schemaVersion":"2"}`},
		{name: "wrong framing", body: "event: accepted\ndata: {}\n\n"},
		{name: "non compact", body: "data: { \"schemaVersion\": \"2\" }\n\n"},
		{name: "duplicate member", body: "data: {\"schemaVersion\":\"2\",\"schemaVersion\":\"2\"}\n\n"},
		{name: "accepted only", body: `data: {"schemaVersion":"2","sequence":0,"type":"accepted","status":"pending","invocationId":"inv_child999","rootTaskId":"task_root456","traceId":"trc_abc123_1"}` + "\n\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.name == "wrong media" {
					w.Header().Set("Content-Type", "application/json")
				} else {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client, err := NewClient(server.Client(), server.URL, "test-token", 4096, 4096)
			if err != nil {
				t.Fatal(err)
			}
			req := validNestedRequest()
			req.Stream = true
			stream, err := client.InvokeStream(context.Background(), validContext(), req)
			if test.name == "wrong media" {
				if err == nil {
					t.Fatal("wrong media accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.Close() }()
			for {
				_, recvErr := stream.Recv()
				if recvErr != nil {
					if errors.Is(recvErr, io.EOF) && test.name == "accepted only" {
						t.Fatal("interrupted accepted-only stream returned EOF")
					}
					break
				}
			}
		})
	}
}

func TestClientInvokeStreamRejectsCorrelationOversizeAndPostTerminalEvents(t *testing.T) {
	accepted := `data: {"schemaVersion":"2","sequence":0,"type":"accepted","status":"pending","invocationId":"inv_child999","rootTaskId":"task_root456","traceId":"trc_abc123_1"}` + "\n\n"
	completed := `data: {"schemaVersion":"2","sequence":1,"type":"completed","status":"succeeded","invocationId":"inv_child999","rootTaskId":"task_root456","traceId":"trc_abc123_1"}` + "\n\n"
	postTerminal := `data: {"schemaVersion":"2","sequence":2,"type":"completed","status":"succeeded","invocationId":"inv_child999","rootTaskId":"task_root456","traceId":"trc_abc123_1"}` + "\n\n"
	tests := []struct {
		name      string
		body      string
		eventSize int64
		want      string
	}{
		{name: "mismatched root", body: strings.Replace(accepted, "task_root456", "task_other", 1), eventSize: 4096, want: "error"},
		{name: "oversize", body: accepted, eventSize: 16, want: "error"},
		{name: "event after terminal", body: accepted + completed + postTerminal, eventSize: 4096, want: "post-terminal"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client, err := NewClient(server.Client(), server.URL, "test-token", 4096, test.eventSize)
			if err != nil {
				t.Fatal(err)
			}
			req := validNestedRequest()
			req.Stream = true
			stream, err := client.InvokeStream(context.Background(), validContext(), req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.Close() }()
			if test.want == "post-terminal" {
				if _, err := stream.Recv(); err != nil {
					t.Fatal(err)
				}
				if _, err := stream.Recv(); err != nil {
					t.Fatal(err)
				}
				if _, err := stream.Recv(); err == nil {
					t.Fatal("event after terminal accepted")
				}
				return
			}
			if _, err := stream.Recv(); err == nil {
				t.Fatal("invalid stream accepted")
			}
		})
	}
}
