package gateway

import "encoding/json"

const ProtocolVersion = 3

type RequestFrame struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type ResponseFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *GatewayError   `json:"error,omitempty"`
}

type EventFrame struct {
	Type    string          `json:"type"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type GatewayError struct {
	Code         string          `json:"code,omitempty"`
	Message      string          `json:"message,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	Retryable    *bool           `json:"retryable,omitempty"`
	RetryAfterMs *int            `json:"retryAfterMs,omitempty"`
}

type ClientInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Mode        string `json:"mode"`
	InstanceID  string `json:"instanceId,omitempty"`
}

type ConnectParams struct {
	MinProtocol int        `json:"minProtocol"`
	MaxProtocol int        `json:"maxProtocol"`
	Client      ClientInfo `json:"client"`
	Role        string     `json:"role,omitempty"`
	Caps        []string   `json:"caps,omitempty"`
	Commands    []string   `json:"commands,omitempty"`
}

type NodeRegistration struct {
	Client   ClientInfo `json:"client"`
	Role     string     `json:"role"`
	Caps     []string   `json:"caps"`
	Commands []string   `json:"commands"`
}

type InvokeRequestParams struct {
	RequestID string          `json:"id"`
	NodeID    string          `json:"nodeId"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args,omitempty"`
}

type InvokeResultParams struct {
	RequestID  string           `json:"id"`
	NodeID     string           `json:"nodeId"`
	OK         bool             `json:"ok"`
	Result     interface{}      `json:"payload,omitempty"`
	ResultJSON *string          `json:"payloadJSON,omitempty"`
	Error      *NodeInvokeError `json:"error,omitempty"`
}

type NodeInvokeError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type NodeEventParams struct {
	Event       string      `json:"event"`
	Payload     interface{} `json:"payload,omitempty"`
	PayloadJSON *string     `json:"payloadJSON,omitempty"`
}
